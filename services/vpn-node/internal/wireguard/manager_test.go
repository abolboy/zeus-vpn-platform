package wireguard

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	testKey  = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	testKey2 = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
)

type fakeRunner struct {
	calls  [][]string
	fail   bool
	routes map[string]string
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{routes: map[string]string{
		"10.77.77.0/24":   "wg0",
		"fd77:77:77::/64": "wg0",
		"0.0.0.0/0":       "eth0",
		"::/0":            "eth0",
	}}
}

func (f *fakeRunner) Run(_ context.Context, binary string, args ...string) error {
	call := append([]string{binary}, args...)
	f.calls = append(f.calls, call)
	if f.fail {
		return errors.New("simulated command failure")
	}
	if strings.Contains(binary, "ip") && len(args) >= 6 && args[1] == "route" {
		prefix := args[3]
		switch args[2] {
		case "add":
			f.routes[prefix] = args[5]
		case "del":
			delete(f.routes, prefix)
		}
	}
	return nil
}

func (f *fakeRunner) Output(_ context.Context, _ string, args ...string) ([]byte, error) {
	if len(args) < 3 || args[1] != "route" {
		return nil, errors.New("unexpected route query")
	}
	switch args[2] {
	case "show":
		if len(args) < 5 || args[3] != "exact" {
			return nil, errors.New("unexpected route show arguments")
		}
		prefix := args[4]
		device, ok := f.routes[prefix]
		if !ok {
			return []byte{}, nil
		}
		return []byte(prefix + " dev " + device + "\n"), nil
	case "get":
		if len(args) < 4 {
			return nil, errors.New("missing route destination")
		}
		address, err := netip.ParseAddr(args[3])
		if err != nil {
			return nil, err
		}
		bestBits := -1
		bestDevice := ""
		bestPrefix := ""
		for rawPrefix, device := range f.routes {
			prefix, err := netip.ParsePrefix(rawPrefix)
			if err != nil || prefix.Addr().Is4() != address.Is4() || !prefix.Contains(address) {
				continue
			}
			if prefix.Bits() > bestBits {
				bestBits = prefix.Bits()
				bestDevice = device
				bestPrefix = rawPrefix
			}
		}
		if bestBits < 0 {
			return nil, errors.New("network is unreachable")
		}
		if bestBits == 0 {
			return []byte(fmt.Sprintf("%s via 192.0.2.1 dev %s\n", address, bestDevice)), nil
		}
		return []byte(fmt.Sprintf("%s dev %s\n", address, bestDevice)), nil
	default:
		return nil, errors.New("unexpected route operation")
	}
}

func testConfig(t *testing.T, allowMutations bool) (Config, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wg0.conf")
	content := strings.Join([]string{
		"[Interface]",
		"PrivateKey = DO_NOT_LOG_OR_CHANGE",
		"Address = 10.77.77.1/24, fd77:77:77::1/64",
		"ListenPort = 51820",
		"",
		"[Peer]",
		"PublicKey = " + testKey2,
		"AllowedIPs = 10.77.77.2/32, fd77:77:77::2/128",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return Config{
		Interface:      "wg0",
		ConfigPath:     path,
		WGBinary:       "/usr/bin/wg",
		IPv4Pool:       netip.MustParsePrefix("10.77.77.0/24"),
		IPv6Pool:       netip.MustParsePrefix("fd77:77:77::/64"),
		AllowMutations: allowMutations,
	}, content
}

func testPeer() PeerSpec {
	return PeerSpec{
		PublicKey: testKey,
		IPv4:      netip.MustParseAddr("10.77.77.3"),
		IPv6:      netip.MustParseAddr("fd77:77:77::3"),
	}
}

func TestMutationIsDisabledByDefault(t *testing.T) {
	config, original := testConfig(t, false)
	runner := newFakeRunner()
	manager, err := NewManager(config, runner)
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.EnsurePeer(context.Background(), testPeer())
	if err != nil {
		t.Fatal(err)
	}
	if !result.DryRun || !result.Changed {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("default mode executed commands: %+v", runner.calls)
	}
	got, err := os.ReadFile(config.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatal("default mode modified WireGuard config")
	}
}

func TestEnsurePeerDryRunDoesNotWriteOrExecute(t *testing.T) {
	config, original := testConfig(t, false)
	runner := newFakeRunner()
	manager, err := NewManager(config, runner)
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.EnsurePeer(context.Background(), testPeer())
	if err != nil {
		t.Fatal(err)
	}
	if !result.DryRun || !result.Changed {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("dry-run executed commands: %+v", runner.calls)
	}
	got, err := os.ReadFile(config.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatal("dry-run modified WireGuard config")
	}
	if !reflect.DeepEqual(result.Command, []string{"/usr/bin/wg", "set", "wg0", "peer", testKey, "allowed-ips", "10.77.77.3/32, fd77:77:77::3/128"}) {
		t.Fatalf("unexpected command preview: %#v", result.Command)
	}
}

func TestEnsurePeerWritesOnlyPeerSectionAndPreservesInterface(t *testing.T) {
	config, _ := testConfig(t, true)
	runner := newFakeRunner()
	manager, err := NewManager(config, runner)
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.EnsurePeer(context.Background(), testPeer())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.DryRun {
		t.Fatalf("unexpected result: %+v", result)
	}
	got, err := os.ReadFile(config.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if !strings.Contains(text, "PrivateKey = DO_NOT_LOG_OR_CHANGE") || !strings.Contains(text, "Address = 10.77.77.1/24, fd77:77:77::1/64") {
		t.Fatal("interface settings were not preserved")
	}
	if !strings.Contains(text, "PublicKey = "+testKey) || !strings.Contains(text, "AllowedIPs = 10.77.77.3/32, fd77:77:77::3/128") {
		t.Fatal("new peer settings were not persisted")
	}
	if len(runner.calls) != 1 || runner.calls[0][0] != "/usr/bin/wg" || runner.calls[0][1] != "set" {
		t.Fatalf("unexpected command argv: %#v", runner.calls)
	}
}

func TestEnsurePeerRejectsAddressConflictAndInvalidPools(t *testing.T) {
	config, _ := testConfig(t, false)
	manager, err := NewManager(config, newFakeRunner())
	if err != nil {
		t.Fatal(err)
	}
	peer := testPeer()
	peer.IPv4 = netip.MustParseAddr("10.77.77.2")
	if _, err := manager.EnsurePeer(context.Background(), peer); err == nil {
		t.Fatal("overlapping peer address was accepted")
	}
	peer = testPeer()
	peer.IPv4 = netip.MustParseAddr("10.77.77.1")
	if _, err := manager.EnsurePeer(context.Background(), peer); err == nil {
		t.Fatal("interface IPv4 address was accepted")
	}
	peer = testPeer()
	peer.IPv4 = netip.MustParseAddr("10.77.77.255")
	if _, err := manager.EnsurePeer(context.Background(), peer); err == nil {
		t.Fatal("IPv4 broadcast address was accepted")
	}
	peer = testPeer()
	peer.IPv6 = netip.MustParseAddr("2001:db8::3")
	if _, err := manager.EnsurePeer(context.Background(), peer); err == nil {
		t.Fatal("out-of-pool IPv6 address was accepted")
	}
}

func TestEnsurePeerRejectsInvalidKeyAndIPv6InterfaceAddress(t *testing.T) {
	config, _ := testConfig(t, false)
	manager, err := NewManager(config, newFakeRunner())
	if err != nil {
		t.Fatal(err)
	}
	peer := testPeer()
	peer.PublicKey = "not-a-key"
	if _, err := manager.EnsurePeer(context.Background(), peer); err == nil {
		t.Fatal("invalid public key was accepted")
	}
	peer = testPeer()
	peer.IPv6 = netip.MustParseAddr("fd77:77:77::1")
	if _, err := manager.EnsurePeer(context.Background(), peer); err == nil {
		t.Fatal("interface IPv6 address was accepted")
	}
}

func TestRemovePeerPreservesOtherSections(t *testing.T) {
	config, _ := testConfig(t, true)
	runner := newFakeRunner()
	manager, err := NewManager(config, runner)
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.RemovePeer(context.Background(), testKey2)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("expected peer removal to change config")
	}
	got, err := os.ReadFile(config.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), testKey2) {
		t.Fatal("removed peer remains in config")
	}
	if !strings.Contains(string(got), "PrivateKey = DO_NOT_LOG_OR_CHANGE") {
		t.Fatal("interface private key line was removed")
	}
	if len(runner.calls) != 1 || !reflect.DeepEqual(runner.calls[0][1:], []string{"set", "wg0", "peer", testKey2, "remove"}) {
		t.Fatalf("unexpected command: %#v", runner.calls)
	}
}

func TestRuntimeFailureRestoresPreviousFile(t *testing.T) {
	config, original := testConfig(t, true)
	runner := &fakeRunner{fail: true}
	manager, err := NewManager(config, runner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.EnsurePeer(context.Background(), testPeer()); err == nil {
		t.Fatal("expected simulated WireGuard failure")
	}
	got, err := os.ReadFile(config.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatal("config file was not rolled back")
	}
	if len(runner.calls) != 2 {
		t.Fatalf("expected apply plus compensating remove, got %d calls", len(runner.calls))
	}
}

func TestEnsurePeerAddsHostRoutesWhenClientPoolIsOutsideInterfaceSubnet(t *testing.T) {
	config, _ := testConfig(t, true)
	config.IPv4Pool = netip.MustParsePrefix("10.88.0.0/24")
	config.IPv6Pool = netip.MustParsePrefix("fd88:88:88::/64")
	runner := newFakeRunner()
	manager, err := NewManager(config, runner)
	if err != nil {
		t.Fatal(err)
	}
	peer := testPeer()
	peer.IPv4 = netip.MustParseAddr("10.88.0.3")
	peer.IPv6 = netip.MustParseAddr("fd88:88:88::3")
	result, err := manager.EnsurePeer(context.Background(), peer)
	if err != nil {
		t.Fatal(err)
	}
	if result.DryRun {
		t.Fatal("expected a live test manager")
	}
	for _, prefix := range []string{"10.88.0.3/32", "fd88:88:88::3/128"} {
		if runner.routes[prefix] != "wg0" {
			t.Fatalf("missing client route %s through wg0: %#v", prefix, runner.routes)
		}
	}
	if len(runner.calls) != 3 {
		t.Fatalf("expected two route additions and one wg set, got %#v", runner.calls)
	}
}

func TestEnsurePeerRefusesToOverrideAnExactRouteOnAnotherInterface(t *testing.T) {
	config, original := testConfig(t, true)
	runner := newFakeRunner()
	runner.routes["10.77.77.3/32"] = "eth1"
	manager, err := NewManager(config, runner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.EnsurePeer(context.Background(), testPeer()); err == nil {
		t.Fatal("conflicting exact client route was overridden")
	}
	got, err := os.ReadFile(config.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatal("WireGuard config was not restored after route conflict")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("unexpected mutations occurred before conflict was rejected: %#v", runner.calls)
	}
}

func TestConfigRejectsSymlinkAndBroadPermissions(t *testing.T) {
	config, _ := testConfig(t, false)
	link := config.ConfigPath + ".link"
	if err := os.Symlink(config.ConfigPath, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readSecureConfig(link); err == nil {
		t.Fatal("symlink config path was accepted")
	}
	if err := os.Chmod(config.ConfigPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readSecureConfig(config.ConfigPath); err == nil {
		t.Fatal("world-readable config was accepted")
	}
}
