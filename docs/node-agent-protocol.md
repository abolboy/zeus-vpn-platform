# ZEUS VPN Node Agent Protocol — Design Draft

Status: architecture/design only. No node-agent command endpoint or WireGuard mutation is enabled by this document.

## Security boundary

- The control plane never opens an inbound management connection to a VPN node.
- Each node agent initiates outbound HTTPS connections to the control plane.
- A node uses a dedicated client certificate signed by a ZEUS node CA. The certificate's SHA-256 fingerprint is registered to exactly one `vpn_nodes` row.
- The node listener is a separate HTTPS listener from the human admin API. It requires and verifies client certificates; it is not exposed through a browser-facing reverse-proxy route.
- The API maps the verified leaf-certificate fingerprint to a node ID on every request. Headers such as `X-Forwarded-Client-Cert`, `X-Node-ID`, and `X-Forwarded-For` are not identity sources.
- Certificate rotation is an explicit operation: enroll the replacement fingerprint, validate the new certificate, then revoke the old fingerprint. A node with no registered fingerprint cannot poll or acknowledge commands.
- The agent private key stays on its own Linux node, file permissions root-only. It is never uploaded to GitHub, the Android app, or the control-plane database.

## Transport and command delivery

The agent polls over mTLS and receives commands only for its authenticated node. Polling is outbound and can be retried after transient network loss. The database queue provides at-least-once delivery; every operation must therefore be idempotent.

Planned command envelope:

```json
{
  "id": "command UUID",
  "node_id": "authenticated node UUID",
  "type": "peer.ensure",
  "payload": {
    "public_key": "base64 WireGuard public key",
    "ipv4": "assigned-address",
    "ipv6": "assigned-address"
  },
  "issued_at": "RFC3339 UTC",
  "expires_at": "RFC3339 UTC",
  "attempt": 1
}
```

Allowed command types are a closed enum:
- `peer.ensure`: ensure one public key has exactly its assigned host routes.
- `peer.remove`: remove one known public key.

The protocol never accepts shell commands, executable paths, arbitrary interface names, arbitrary routes, firewall scripts, or opaque command strings. The node agent constructs a fixed argv list itself and never invokes a shell.

## Peer validation

Before applying a command, the agent validates:
- Public key decodes to exactly 32 bytes and is canonical base64.
- IPv4 and IPv6 values are valid host addresses inside the configured per-node client pools.
- IPv4 is encoded as a single `/32`; IPv6 as a single `/128`.
- The assigned addresses are not the interface address, subnet/network address, broadcast address, or already assigned to another peer.
- The command is for the configured WireGuard interface and has not expired.
- Duplicate delivery of the same command produces the same final peer state.

Private keys never appear in the command payload or agent logs.

## Durable WireGuard configuration

A runtime-only `wg set` is insufficient because peers would disappear after reboot. The eventual executor must make the persistent configuration and live interface converge:

1. Acquire a per-interface lock and verify the config file is a regular, root-owned file with restrictive permissions.
2. Read and preserve the existing file. The interface section, server private key, addresses, MTU, and existing unmanaged peers must remain byte-for-byte unchanged.
3. Modify only the matching `[Peer]` block (or append/remove a peer block) using validated public-key and host-route values.
4. Write a same-directory temporary file with mode `0600`, fsync it, atomically rename it, and fsync the directory.
5. Apply the fixed `wg set` argv to the already-existing interface.
6. If runtime application fails, restore the prior file atomically and report a failed command. Never run `wg-quick down`, replace the interface, or use an unreviewed whole-interface sync.

This persistence strategy requires fixture tests against representative existing `wg0.conf` files and a staging node before production enablement. No current AWS configuration is modified by this design task.

## Queue lifecycle

- Commands are created in the same database transaction as the device/peer desired-state change.
- A node leases only commands belonging to its certificate identity. Lease expiry allows safe redelivery.
- An acknowledgement contains command ID, outcome, and a bounded, sanitized result. It cannot change the identity or payload of a command.
- Success moves the corresponding desired state to applied. Failure records a bounded error category and can be retried up to a fixed attempt ceiling.
- Expired commands are never executed. Replayed acknowledgements are idempotent.
- Node heartbeats update last-seen state but never imply a successful peer mutation.

## Operational rollout gates

1. Unit tests with a fake command runner and filesystem fixtures.
2. Property tests for route containment, duplicate addresses, malformed keys, and malformed peer blocks.
3. Integration tests on an isolated WireGuard test interface.
4. Failure-injection tests for config write, fsync, rename, `wg set`, timeout, replay, and lease expiry.
5. Staging node backup/restore and reboot persistence test.
6. Explicit production deployment approval after the above checks.

Until these gates pass, the control plane may store node metadata and pending command records, but no agent command is sent to the live AWS node.
