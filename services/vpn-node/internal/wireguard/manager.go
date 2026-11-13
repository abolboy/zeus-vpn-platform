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
	Changed bool
	DryRun  bool
	Command []string
}

type CommandRunner interface {
	Run(context.Context, string, ...string) error
}

type Manager struct {
	config Config
	runner CommandRunner
	mu     sync.Mutex
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
		if !m.config.AllowMutations {
			return Result{Changed: changed, DryRun: true, Command: append([]string{m.config.WGBinary}, args...)}, nil
		}
		if err := m.apply(ctx, original, next, changed, peer.PublicKey, prior, true, args); err != nil {
			return Result{}, err
		}
		return Result{Changed: changed, Command: append([]string{m.config.WGBinary}, args...)}, nil
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
		if !m.config.AllowMutations {
			return Result{Changed: changed, DryRun: true, Command: append([]string{m.config.WGBinary}, args...)}, nil
		}
		if err := m.apply(ctx, original, next, changed, publicKey, prior, false, args); err != nil {
			return Result{}, err
		}
		return Result{Changed: changed, Command: append([]string{m.config.WGBinary}, args...)}, nil
	})
}

func (m *Manager) apply(ctx context.Context, original, next []byte, changed bool, publicKey string, prior *peerBlock, ensure bool, args []string) error {
	if changed {
		if err := atomicWriteConfig(m.config.ConfigPath, next); err != nil {
			rollbackErr := atomicWriteConfig(m.config.ConfigPath, original)
			return errors.Join(fmt.Errorf("persist WireGuard peer config: %w", err), wrapRollback(rollbackErr))
		}
	}
	if err := m.runner.Run(ctx, m.config.WGBinary, args...); err != nil {
		var rollbackErr error
		if changed {
			rollbackErr = atomicWriteConfig(m.config.ConfigPath, original)
		}
		var compensationErr error
		if ensure {
			compensationErr = m.restoreAllowedIPs(publicKey, prior)
		}
		return errors.Join(fmt.Errorf("apply WireGuard peer operation: %w", err), wrapRollback(rollbackErr), wrapCompensation(compensationErr))
	}
	return nil
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
