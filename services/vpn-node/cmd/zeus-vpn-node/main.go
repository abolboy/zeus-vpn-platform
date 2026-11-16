package main

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/abolboy/zeus-vpn-platform/services/vpn-node/internal/controlplaneclient"
	"github.com/abolboy/zeus-vpn-platform/services/vpn-node/internal/wireguard"
	"github.com/abolboy/zeus-vpn-platform/services/vpn-node/internal/worker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	controlPlaneURL := strings.TrimSpace(os.Getenv("ZEUS_CONTROL_PLANE_URL"))
	if controlPlaneURL == "" {
		logger.Error("ZEUS_CONTROL_PLANE_URL is required")
		os.Exit(1)
	}

	allowMutations, err := parseMutationFlag(os.Getenv("ZEUS_NODE_ALLOW_MUTATIONS"))
	if err != nil {
		logger.Error("invalid mutation setting", "error", err)
		os.Exit(1)
	}
	if allowMutations && os.Geteuid() != 0 {
		logger.Error("WireGuard mutations require root or an explicitly delegated CAP_NET_ADMIN service")
		os.Exit(1)
	}

	pollInterval, err := parseDuration("ZEUS_NODE_POLL_INTERVAL", 5*time.Second, time.Second, time.Minute)
	if err != nil {
		logger.Error("invalid poll interval", "error", err)
		os.Exit(1)
	}
	ipv4Pool, err := netip.ParsePrefix(envOrDefault("ZEUS_WG_IPV4_POOL", "10.77.77.0/24"))
	if err != nil {
		logger.Error("ZEUS_WG_IPV4_POOL is invalid")
		os.Exit(1)
	}
	ipv6Pool, err := netip.ParsePrefix(envOrDefault("ZEUS_WG_IPV6_POOL", "fd77:77:77::/64"))
	if err != nil {
		logger.Error("ZEUS_WG_IPV6_POOL is invalid")
		os.Exit(1)
	}

	client, err := controlplaneclient.New(controlplaneclient.Config{
		BaseURL:        controlPlaneURL,
		ClientCertFile: os.Getenv("ZEUS_NODE_CLIENT_CERT_FILE"),
		ClientKeyFile:  os.Getenv("ZEUS_NODE_CLIENT_KEY_FILE"),
		ServerCAFile:   os.Getenv("ZEUS_NODE_SERVER_CA_FILE"),
		Timeout:        10 * time.Second,
	})
	if err != nil {
		logger.Error("node control-plane client initialization failed", "error", err)
		os.Exit(1)
	}
	defer client.CloseIdleConnections()

	manager, err := wireguard.NewManager(wireguard.Config{
		Interface:      envOrDefault("ZEUS_WG_INTERFACE", "wg0"),
		ConfigPath:     envOrDefault("ZEUS_WG_CONFIG_PATH", "/etc/wireguard/wg0.conf"),
		WGBinary:       envOrDefault("ZEUS_WG_BINARY", "/usr/bin/wg"),
		IPBinary:       envOrDefault("ZEUS_IP_BINARY", "/usr/sbin/ip"),
		IPv4Pool:       ipv4Pool,
		IPv6Pool:       ipv6Pool,
		AllowMutations: allowMutations,
	}, wireguard.ExecRunner{})
	if err != nil {
		logger.Error("WireGuard executor initialization failed", "error", err)
		os.Exit(1)
	}

	nodeWorker, err := worker.New(client, manager, worker.Config{PollInterval: pollInterval})
	if err != nil {
		logger.Error("node worker initialization failed", "error", err)
		os.Exit(1)
	}

	if !allowMutations {
		logger.Warn("node agent is running in dry-run mode; WireGuard configuration and runtime will not be changed")
	} else {
		logger.Warn("WireGuard peer mutation is explicitly enabled on this node")
	}
	logger.Info("ZEUS node agent started", "control_plane", controlPlaneURL, "interface", envOrDefault("ZEUS_WG_INTERFACE", "wg0"), "poll_interval", pollInterval.String())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := nodeWorker.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("node worker stopped", "error", err)
		os.Exit(1)
	}
	logger.Info("ZEUS node agent stopped")
}

func parseMutationFlag(value string) (bool, error) {
	switch strings.TrimSpace(value) {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, errors.New("ZEUS_NODE_ALLOW_MUTATIONS must be exactly true or false")
	}
}

func parseDuration(key string, fallback, minimum, maximum time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration < minimum || duration > maximum {
		return 0, errors.New(key + " must be between " + minimum.String() + " and " + maximum.String())
	}
	return duration, nil
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
