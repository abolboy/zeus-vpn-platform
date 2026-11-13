package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"time"

	"github.com/abolboy/zeus-vpn-platform/services/vpn-node/internal/controlplaneclient"
	"github.com/abolboy/zeus-vpn-platform/services/vpn-node/internal/wireguard"
)

type CommandClient interface {
	Poll(context.Context) (*controlplaneclient.Command, error)
	Acknowledge(context.Context, string, string, string) error
}

type PeerManager interface {
	EnsurePeer(context.Context, wireguard.PeerSpec) (wireguard.Result, error)
	RemovePeer(context.Context, string) (wireguard.Result, error)
}

type Config struct {
	PollInterval time.Duration
}

type Worker struct {
	client  CommandClient
	manager PeerManager
	config  Config
}

type ensurePeerPayload struct {
	PublicKey string "json:\"public_key\""
	IPv4      string "json:\"ipv4\""
	IPv6      string "json:\"ipv6\""
}

type removePeerPayload struct {
	PublicKey string "json:\"public_key\""
}

func New(client CommandClient, manager PeerManager, config Config) (*Worker, error) {
	if client == nil || manager == nil {
		return nil, errors.New("node command client and WireGuard manager are required")
	}
	if config.PollInterval == 0 {
		config.PollInterval = 5 * time.Second
	}
	if config.PollInterval < time.Second || config.PollInterval > time.Minute {
		return nil, errors.New("node poll interval must be between 1 and 60 seconds")
	}
	return &Worker{client: client, manager: manager, config: config}, nil
}

func (w *Worker) Run(ctx context.Context) error {
	backoff := w.config.PollInterval
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		command, err := w.client.Poll(ctx)
		if err != nil {
			if !wait(ctx, backoff) {
				return nil
			}
			backoff = min(backoff*2, time.Minute)
			continue
		}
		backoff = w.config.PollInterval
		if command == nil {
			if !wait(ctx, w.config.PollInterval) {
				return nil
			}
			continue
		}
		if err := w.process(ctx, command); err != nil {
			if !wait(ctx, backoff) {
				return nil
			}
			backoff = min(backoff*2, time.Minute)
		}
	}
}

func (w *Worker) process(ctx context.Context, command *controlplaneclient.Command) error {
	if command == nil {
		return nil
	}
	if !command.ExpiresAt.After(time.Now().UTC()) {
		return w.client.Acknowledge(ctx, command.ID, "failed", "command_expired")
	}

	switch command.Type {
	case "peer.ensure":
		var payload ensurePeerPayload
		if err := decodePayload(command.Payload, &payload); err != nil {
			return w.client.Acknowledge(ctx, command.ID, "failed", "validation_failed")
		}
		ipv4, err4 := netip.ParseAddr(payload.IPv4)
		ipv6, err6 := netip.ParseAddr(payload.IPv6)
		if err4 != nil || err6 != nil {
			return w.client.Acknowledge(ctx, command.ID, "failed", "validation_failed")
		}
		result, err := w.manager.EnsurePeer(ctx, wireguard.PeerSpec{
			PublicKey: payload.PublicKey,
			IPv4:      ipv4,
			IPv6:      ipv6,
		})
		if err != nil {
			return w.client.Acknowledge(ctx, command.ID, "failed", "wg_apply_failed")
		}
		if result.DryRun {
			return w.client.Acknowledge(ctx, command.ID, "failed", "mutation_disabled")
		}
		return w.client.Acknowledge(ctx, command.ID, "succeeded", "")
	case "peer.remove":
		var payload removePeerPayload
		if err := decodePayload(command.Payload, &payload); err != nil {
			return w.client.Acknowledge(ctx, command.ID, "failed", "validation_failed")
		}
		result, err := w.manager.RemovePeer(ctx, payload.PublicKey)
		if err != nil {
			return w.client.Acknowledge(ctx, command.ID, "failed", "wg_apply_failed")
		}
		if result.DryRun {
			return w.client.Acknowledge(ctx, command.ID, "failed", "mutation_disabled")
		}
		return w.client.Acknowledge(ctx, command.ID, "succeeded", "")
	default:
		return w.client.Acknowledge(ctx, command.ID, "failed", "validation_failed")
	}
}

func decodePayload(body []byte, target any) error {
	if len(body) == 0 || len(body) > 8*1024 {
		return errors.New("node command payload size is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("unexpected trailing command JSON")
	}
	return nil
}

func wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
