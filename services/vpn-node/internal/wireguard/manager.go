package wireguard

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Config struct {
	Interface      string
	ConfigPath     string
	WGBinary       string
	IPBinary       string
	IPv4Pool       netip.Prefix
	IPv6Pool       netip.Prefix
	AllowMutations bool
}

type PeerSpec struct {
	PublicKey string
	IPv4      netip.Addr
	IPv6      netip.Addr
}

type Result struct {
	Changed  bool
	DryRun   bool
	Command  []string
	Commands [][]string
}

type CommandRunner interface {
	Run(context.Context, string, ...string) error
}

type CommandOutputRunner interface {
	Output(context.Context, string, ...string) ([]byte, error)
}

type Manager struct {
	config Config
	runner CommandRunner
	mu     sync.Mutex
}

type routeSpec struct {
	family string
	prefix netip.Prefix
}

var interfacePattern = regexp.MustCompile("^[A-Za-z0-9_.=-]{1,15}$")

func NewManager(config Config, runner CommandRunner) (*Manager, error) {
	if !interfacePattern.MatchString(config.Interface) {
		return nil, errors.New("invalid WireGuard interface name")
	}
	if !filepath.IsAbs(config.ConfigPath) || filepath.Clean(config.ConfigPath) != config.ConfigPath {
		return nil, errors.New("WireGuard config path must be absolute and clean")
	}
	if !filepath.IsAbs(config.WGBinary) || filepath.Clean(config.WGBinary) != config.WGBinary {
		return nil, errors.New("WireGuard binary path must be absolute and clean")
	}
	if config.IPBinary == "" {
		config.IPBinary = "/usr/sbin/ip"
	}
	if !filepath.IsAbs(config.IPBinary) || filepath.Clean(config.IPBinary) != config.IPBinary {
		return nil, errors.New("iproute2 binary path must be absolute and clean")
	}
	if !config.IPv4Pool.IsValid() || !config.IPv4Pool.Addr().Is4() || config.IPv4Pool != config.IPv4Pool.Masked() {
		return nil, errors.New("invalid IPv4 client pool")
	}
	if !config.IPv6Pool.IsValid() || !config.IPv6Pool.Addr().Is6() || config.IPv6Pool != config.IPv6Pool.Masked() {
		return nil, errors.New("invalid IPv6 client pool")
	}
	if config.IPv4Pool.Bits() > 30 {
		return nil, errors.New("IPv4 client pool must contain usable host addresses")
	}
	if config.IPv6Pool.Bits() > 120 {
		return nil, errors.New("IPv6 client pool must contain usable host addresses")
	}
	if runner == nil {
		return nil, errors.New("command runner is required")
	}
	if config.AllowMutations {
		if _, ok := runner.(CommandOutputRunner); !ok {
			return nil, errors.New("route inspection support is required when WireGuard mutations are enabled")
		}
	}
	return &Manager{config: config, runner: runner}, nil
}

func (m *Manager) EnsurePeer(ctx context.Context, peer PeerSpec) (Result, error) {
	if err := validatePeer(peer, m.config); err != nil {
		return Result{}, err
	}
	return m.withLock(ctx, func() (Result, error) {
		original, err := readSecureConfig(m.config.ConfigPath)
		if err != nil {
			return Result{}, err
		}
		next, prior, changed, err := ensurePeerConfig(original, peer)
		if err != nil {
			return Result{}, err
		}
		args := []string{"set", m.config.Interface, "peer", peer.PublicKey, "allowed-ips", allowedIPs(peer)}
		wgCommand := append([]string{m.config.WGBinary}, args...)
		commands := append([][]string{wgCommand}, m.routeCommandPreviews(peer)...)
		if !m.config.AllowMutations {
			return Result{Changed: changed, DryRun: true, Command: wgCommand, Commands: commands}, nil
		}
		if err := m.applyEnsurePeer(ctx, original, next, changed, peer, prior, args); err != nil {
			return Result{}, err
		}
		return Result{Changed: changed, Command: wgCommand, Commands: commands}, nil
	})
}

func (m *Manager) RemovePeer(ctx context.Context, publicKey string) (Result, error) {
	if err := validatePublicKey(publicKey); err != nil {
		return Result{}, err
	}
	return m.withLock(ctx, func() (Result, error) {
		original, err := readSecureConfig(m.config.ConfigPath)
		if err != nil {
			return Result{}, err
		}
		next, prior, changed, err := removePeerConfig(original, publicKey)
		if err != nil {
			return Result{}, err
		}
		args := []string{"set", m.config.Interface, "peer", publicKey, "remove"}
		command := append([]string{m.config.WGBinary}, args...)
		if !m.config.AllowMutations {
			return Result{Changed: changed, DryRun: true, Command: command, Commands: [][]string{command}}, nil
		}
		if err := m.applyRemovePeer(ctx, original, next, changed, publicKey, prior, args); err != nil {
			return Result{}, err
		}
		return Result{Changed: changed, Command: command, Commands: [][]string{command}}, nil
	})
}

func (m *Manager) applyEnsurePeer(ctx context.Context, original, next []byte, changed bool, peer PeerSpec, prior *peerBlock, wgArgs []string) error {
	if changed {
		if err := atomicWriteConfig(m.config.ConfigPath, next); err != nil {
			rollbackErr := atomicWriteConfig(m.config.ConfigPath, original)
			return errors.Join(fmt.Errorf("persist WireGuard peer config: %w", err), wrapRollback(rollbackErr))
		}
	}

	addedRoutes, routeErr := m.ensurePeerRoutes(ctx, peer)
	if routeErr != nil {
		var rollbackErr error
		if changed {
			rollbackErr = atomicWriteConfig(m.config.ConfigPath, original)
		}
		routeRollbackErr := m.rollbackRoutes(ctx, addedRoutes)
		return errors.Join(
			fmt.Errorf("ensure WireGuard client routes: %w", routeErr),
			wrapRollback(rollbackErr),
			wrapRouteRollback(routeRollbackErr),
		)
	}

	if err := m.runner.Run(ctx, m.config.WGBinary, wgArgs...); err != nil {
		var rollbackErr error
		if changed {
			rollbackErr = atomicWriteConfig(m.config.ConfigPath, original)
		}
		routeRollbackErr := m.rollbackRoutes(ctx, addedRoutes)
		compensationErr := m.restoreAllowedIPs(peer.PublicKey, prior)
		return errors.Join(
			fmt.Errorf("apply WireGuard peer operation: %w", err),
			wrapRollback(rollbackErr),
			wrapRouteRollback(routeRollbackErr),
			wrapCompensation(compensationErr),
		)
	}
	return nil
}

func (m *Manager) applyRemovePeer(ctx context.Context, original, next []byte, changed bool, publicKey string, prior *peerBlock, wgArgs []string) error {
	if changed {
		if err := atomicWriteConfig(m.config.ConfigPath, next); err != nil {
			rollbackErr := atomicWriteConfig(m.config.ConfigPath, original)
			return errors.Join(fmt.Errorf("persist WireGuard peer removal: %w", err), wrapRollback(rollbackErr))
		}
	}
	if err := m.runner.Run(ctx, m.config.WGBinary, wgArgs...); err != nil {
		var rollbackErr error
		if changed {
			rollbackErr = atomicWriteConfig(m.config.ConfigPath, original)
		}
		compensationErr := m.restoreAllowedIPs(publicKey, prior)
		return errors.Join(fmt.Errorf("remove WireGuard peer: %w", err), wrapRollback(rollbackErr), wrapCompensation(compensationErr))
	}
	// Keep the narrow host routes. They carry no traffic after peer removal and
	// remain correct if the control plane later reuses the same client address.
	return nil
}

func (m *Manager) peerRouteSpecs(peer PeerSpec) []routeSpec {
	return []routeSpec{
		{family: "-4", prefix: netip.PrefixFrom(peer.IPv4, 32)},
		{family: "-6", prefix: netip.PrefixFrom(peer.IPv6, 128)},
	}
}

func (m *Manager) routeCommandPreviews(peer PeerSpec) [][]string {
	var commands [][]string
	for _, spec := range m.peerRouteSpecs(peer) {
		commands = append(commands, []string{
			m.config.IPBinary, spec.family, "route", "add",
			spec.prefix.String(), "dev", m.config.Interface,
		})
	}
	return commands
}

func (m *Manager) ensurePeerRoutes(ctx context.Context, peer PeerSpec) ([]routeSpec, error) {
	outputRunner, ok := m.runner.(CommandOutputRunner)
	if !ok {
		return nil, errors.New("route inspection is unavailable")
	}
	added := make([]routeSpec, 0, 2)
	for _, spec := range m.peerRouteSpecs(peer) {
		exact, err := outputRunner.Output(ctx, m.config.IPBinary, spec.family, "route", "show", "exact", spec.prefix.String())
		if err != nil {
			return added, fmt.Errorf("inspect exact route %s: %w", spec.prefix, err)
		}
		if strings.TrimSpace(string(exact)) != "" {
			if routeDevice(exact) != m.config.Interface {
				return added, fmt.Errorf("client address %s already has an exact route outside %s", spec.prefix, m.config.Interface)
			}
			continue
		}

		current, routeErr := outputRunner.Output(ctx, m.config.IPBinary, spec.family, "route", "get", spec.prefix.Addr().String())
		if routeErr == nil {
			device := routeDevice(current)
			if device == m.config.Interface {
				continue
			}
			if device != "" && !strings.Contains(" "+strings.TrimSpace(string(current))+" ", " via ") {
				return added, fmt.Errorf("client address %s resolves through local interface %s; refusing to override a connected route", spec.prefix, device)
			}
		}

		addArgs := []string{spec.family, "route", "add", spec.prefix.String(), "dev", m.config.Interface}
		if err := m.runner.Run(ctx, m.config.IPBinary, addArgs...); err != nil {
			after, inspectErr := outputRunner.Output(ctx, m.config.IPBinary, spec.family, "route", "get", spec.prefix.Addr().String())
			if inspectErr == nil && routeDevice(after) == m.config.Interface {
				continue
			}
			return added, fmt.Errorf("add route %s via %s: %w", spec.prefix, m.config.Interface, err)
		}
		after, inspectErr := outputRunner.Output(ctx, m.config.IPBinary, spec.family, "route", "get", spec.prefix.Addr().String())
		if inspectErr != nil || routeDevice(after) != m.config.Interface {
			return append(added, spec), fmt.Errorf("route %s did not resolve through %s after installation", spec.prefix, m.config.Interface)
		}
		added = append(added, spec)
	}
	return added, nil
}

func (m *Manager) rollbackRoutes(ctx context.Context, routes []routeSpec) error {
	var failures []error
	for index := len(routes) - 1; index >= 0; index-- {
		spec := routes[index]
		err := m.runner.Run(ctx, m.config.IPBinary, spec.family, "route", "del", spec.prefix.String(), "dev", m.config.Interface)
		if err != nil {
			failures = append(failures, fmt.Errorf("remove route %s: %w", spec.prefix, err))
		}
	}
	return errors.Join(failures...)
}

func routeDevice(output []byte) string {
	fields := strings.Fields(string(output))
	for index := 0; index+1 < len(fields); index++ {
		if fields[index] == "dev" {
			return fields[index+1]
		}
	}
	return ""
}

func (m *Manager) restoreAllowedIPs(publicKey string, prior *peerBlock) error {
	if prior == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return m.runner.Run(ctx, m.config.WGBinary, "set", m.config.Interface, "peer", publicKey, "remove")
	}
	values := make([]string, 0, len(prior.Allowed))
	for _, prefix := range prior.Allowed {
		values = append(values, prefix.String())
	}
	if len(values) == 0 {
		return errors.New("cannot safely restore a peer whose prior AllowedIPs list was empty")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return m.runner.Run(ctx, m.config.WGBinary, "set", m.config.Interface, "peer", publicKey, "allowed-ips", strings.Join(values, ","))
}

func wrapRollback(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("restore previous WireGuard config: %w", err)
}

func wrapRouteRollback(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("restore previous network routes: %w", err)
}

func wrapCompensation(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("restore previous WireGuard runtime state: %w", err)
}

func (m *Manager) withLock(ctx context.Context, operation func() (Result, error)) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	lockPath := m.config.ConfigPath + ".zeus.lock"
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return Result{}, fmt.Errorf("open WireGuard lock: %w", err)
	}
	defer lock.Close()
	if err := lock.Chmod(0o600); err != nil {
		return Result{}, fmt.Errorf("secure WireGuard lock: %w", err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return Result{}, fmt.Errorf("acquire WireGuard lock: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	return operation()
}

func validatePeer(peer PeerSpec, config Config) error {
	if err := validatePublicKey(peer.PublicKey); err != nil {
		return err
	}
	if !peer.IPv4.IsValid() || !peer.IPv4.Is4() || !peer.IPv4.IsGlobalUnicast() || !config.IPv4Pool.Contains(peer.IPv4) {
		return errors.New("peer IPv4 address is outside the configured client pool")
	}
	if !peer.IPv6.IsValid() || !peer.IPv6.Is6() || !peer.IPv6.IsGlobalUnicast() || !config.IPv6Pool.Contains(peer.IPv6) {
		return errors.New("peer IPv6 address is outside the configured client pool")
	}
	if peer.IPv4 == config.IPv4Pool.Addr() || peer.IPv6 == config.IPv6Pool.Addr() {
		return errors.New("peer address cannot be the pool network address")
	}
	if isIPv4Broadcast(peer.IPv4, config.IPv4Pool) {
		return errors.New("peer IPv4 address cannot be the subnet broadcast address")
	}
	return nil
}

func isIPv4Broadcast(address netip.Addr, prefix netip.Prefix) bool {
	if !address.Is4() || prefix.Bits() >= 31 {
		return false
	}
	value := address.As4()
	var raw uint32
	for _, b := range value {
		raw = (raw << 8) | uint32(b)
	}
	hostBits := uint(32 - prefix.Bits())
	hostMask := uint32((uint64(1) << hostBits) - 1)
	network := prefix.Masked().Addr().As4()
	var networkRaw uint32
	for _, b := range network {
		networkRaw = (networkRaw << 8) | uint32(b)
	}
	return raw == (networkRaw | hostMask)
}
