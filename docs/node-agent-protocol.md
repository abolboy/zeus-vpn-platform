# ZEUS VPN Node Agent Protocol

Status: server-side mTLS listener, certificate-to-node lookup, command polling/leases, and acknowledgements are implemented behind explicit opt-in TLS configuration. The standalone node polling client, enrollment/rotation tooling, and live WireGuard command dispatch are not implemented. The current AWS VPN node is not connected to this listener.

## Security boundary

- The control plane never opens an inbound management connection to a VPN node. A node will initiate outbound HTTPS polling when the client is implemented.
- The node-agent listener is separate from the browser/admin HTTP listener.
- The listener requires TLS 1.2 or newer and a verified client certificate signed by the configured node CA.
- The leaf certificate must have ClientAuth EKU, must not be a CA certificate, and its SHA-256 fingerprint must match exactly one non-disabled vpn_nodes row.
- Headers such as X-Forwarded-Client-Cert, X-Node-ID, and X-Forwarded-For are not identity sources.
- A certificate fingerprint can be revoked by clearing the stored fingerprint or disabling the node. Rotation is a separate controlled operation.
- The agent private key stays on its Linux node with root-only permissions. The server listener key must also be owner-only and is opened without following symlinks.
- The listener is disabled unless all three ZEUS_NODE_TLS_CERT_FILE, ZEUS_NODE_TLS_KEY_FILE, and ZEUS_NODE_CA_FILE settings are supplied. Its default bind address is 127.0.0.1:8443; remote access requires an intentional bind-address and firewall decision. No firewall change has been made.

## Implemented mTLS endpoints

These endpoints are available only on the separate mTLS listener:

- GET /agent/v1/commands/next — leases one command for the node identified by the verified certificate; returns 204 when no command is ready.
- POST /agent/v1/commands/{commandID}/ack — acknowledges a leased command with status succeeded or failed.

Acknowledgements accept only a small error-code allowlist: validation_failed, config_rejected, wg_apply_failed, command_expired, and internal_error. They do not accept arbitrary logs, shell text, file paths, or commands.

## Command envelope

A command currently contains:

    {
      "id": "command UUID",
      "node_id": "authenticated node UUID",
      "type": "peer.ensure",
      "payload": {
        "public_key": "base64 WireGuard public key",
        "ipv4": "assigned-address",
        "ipv6": "assigned-address"
      },
      "attempt": 1,
      "expires_at": "RFC3339 UTC"
    }

Allowed command types are a closed enum:
- peer.ensure: ensure one public key has exactly its assigned host routes.
- peer.remove: remove one known public key.

No arbitrary shell command, executable path, interface name, firewall script, or opaque command string is part of the schema.

## Queue lifecycle

- Commands are persisted in PostgreSQL and scoped to one vpn_nodes row.
- A successful poll leases one command for 45 seconds.
- Commands expire after their configured expiry; the schema default is 10 minutes.
- A command can be delivered at least once; the agent must therefore be idempotent.
- The maximum attempt count is 10.
- Acknowledgements are scoped to both authenticated node ID and command ID. Duplicate identical acknowledgements are accepted; conflicting acknowledgements are rejected.
- An expired lease can be re-leased after its deadline. Expired commands are not executed.
- Node polling updates the node last-seen heartbeat, but a heartbeat does not prove a successful peer mutation.

## Peer validation and executor boundary

The local Go executor validates public keys and pool membership, checks existing AllowedIPs routes and interface address collisions, and writes only the target [Peer] block. Its AllowMutations setting defaults to false. The executor is not yet invoked by the network command handler.

Before dispatch is enabled, the agent must validate:
- Public key decodes to exactly 32 bytes and is canonical base64.
- IPv4 and IPv6 are valid host addresses inside the configured per-node client pools.
- IPv4 is a single /32; IPv6 is a single /128.
- Assigned addresses are not interface, network, broadcast, multicast, link-local, or already allocated addresses.
- The command is for the configured WireGuard interface and has not expired.
- Duplicate delivery produces the same final peer state.

Private keys never appear in the command payload or agent logs.

## Durable WireGuard configuration

The local executor:
1. Takes a process and file lock.
2. Reads the config through an O_NOFOLLOW file descriptor and requires owner-only permissions.
3. Preserves the existing Interface section and unrelated Peer blocks.
4. Writes a same-directory temporary file with mode 0600, fsyncs it, atomically renames it, and fsyncs the directory.
5. Uses a fixed wg set argv; it never invokes a shell.
6. Restores the previous file on runtime failure and attempts a safe runtime compensation where supported.

This strategy has unit tests with an injected runner. It has not yet been tested against a real isolated WireGuard interface or a staging VPS. Remove-peer rollback and crash-reconciliation still require additional failure-injection tests.

## Operational rollout gates

1. Implement the standalone mTLS poller and a persistent node service.
2. Implement secure certificate enrollment, rotation, and revocation.
3. Add a reviewed path from device/peer desired state to command enqueue.
4. Test the executor on an isolated WireGuard interface and representative config fixtures.
5. Run failure-injection tests for config write, fsync, rename, wg set, timeout, replay, and lease expiry.
6. Run staging node backup/restore and reboot-persistence tests.
7. Obtain explicit production deployment approval before enabling mutations on any live node.

Until these gates pass, no command is dispatched to the current AWS VPN node and no existing tunnel or firewall configuration is changed.
