# ADR 0002 — Outbound mTLS Node Agent

- Status: Accepted for implementation planning; not deployed.
- Date: 2026-11-14

## Context

ZEUS needs to manage WireGuard peers on Linux nodes without exposing SSH or a general-purpose remote shell to the control plane. Commands must be authenticated, narrowly scoped, replay-safe, durable, and auditable.

## Decision

- Nodes initiate outbound HTTPS polling to the control plane.
- A dedicated node-agent HTTPS listener requires mutual TLS and maps each verified certificate fingerprint to one registered node.
- Human admin traffic and node-agent traffic use separate listeners and trust policies.
- The command queue supports only a closed set of typed operations such as `peer.ensure` and `peer.remove`; it never transports arbitrary shell commands.
- Peer private keys stay on clients and server private keys stay on nodes. Control-plane records contain public keys and assigned addresses only.
- Commands are delivered at least once and must be idempotent. Each command has a bounded lease, expiry, attempt count, and acknowledgement.
- Runtime WireGuard state and its persistent config must change as one recoverable operation, preserving existing interface settings and unmanaged peers.

## Consequences

- Certificate issuance, rotation, and revocation become part of node lifecycle.
- The API must isolate node routes from browser/admin routes and must not trust client-supplied identity headers.
- The agent requires focused filesystem and process-execution tests before privileged mode can be enabled.
- Existing production nodes remain untouched until a staging rollout and explicit approval.
