# ZEUS VPN Node Agent Protocol

Status: the server-side mTLS listener, certificate-to-node lookup, command queue, outbound mTLS client, command worker, and dry-run-first WireGuard executor are implemented. The standalone service has no certificate enrollment/rotation workflow, and production WireGuard mutation remains disabled by default. The current AWS VPN node is not connected to this agent.

## Security boundary

- Each node initiates outbound HTTPS polling; the control plane does not open inbound management connections to VPN nodes.
- The node-agent listener is separate from the browser/admin HTTP listener.
- TLS 1.2 or newer and a verified client certificate from the configured node CA are required.
- The leaf certificate must have ClientAuth EKU, must not be a CA certificate, and its SHA-256 fingerprint must match exactly one non-disabled vpn_nodes row.
- Headers such as X-Forwarded-Client-Cert, X-Node-ID, and X-Forwarded-For are not identity sources.
- The server listener is disabled unless all three ZEUS_NODE_TLS_CERT_FILE, ZEUS_NODE_TLS_KEY_FILE, and ZEUS_NODE_CA_FILE settings are supplied. Its default bind address is 127.0.0.1:8443.
- The node client uses a separate client certificate and private key. Private key files must be regular, no-follow files with owner-only permissions. HTTPS redirects and ambient proxy settings are disabled.

## Implemented mTLS endpoints

Available only on the separate node listener:

- GET /agent/v1/commands/next — leases one command for the node identified by the verified certificate; returns 204 when no command is ready.
- POST /agent/v1/commands/{commandID}/ack — acknowledges a leased command with status succeeded or failed.

Acknowledgements accept only these error codes: validation_failed, config_rejected, wg_apply_failed, command_expired, internal_error, and mutation_disabled. Arbitrary logs, shell text, file paths, or commands are rejected.

## Command envelope

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

No arbitrary shell command, executable path, interface name, firewall script, or opaque command string is accepted.

## Queue lifecycle and device state
- User/device APIs create peer.ensure and peer.remove commands in the same PostgreSQL transaction as device/peer desired-state changes.
- A newly registered device stays provisioning until the node acknowledges peer.ensure successfully.
- A revocation stays revoking until peer.remove succeeds; only then are the device and peer marked revoked.
- Failed commands mark the peer/device error and preserve an operator-visible error code.
- Device limits are checked while locking the owning user row. Provisioning, active, revoking, and error devices count against the limit; revoked devices do not.

## Queue lifecycle

- Commands are persisted in PostgreSQL and scoped to one vpn_nodes row.
- A successful poll leases one command for 45 seconds.
- Commands expire after their configured expiry; the schema default is 10 minutes.
- Delivery is at least once; the client retries after transport failures and the worker must be idempotent.
- The maximum attempt count is 10.
- Identical acknowledgements are idempotent; conflicting acknowledgements are rejected.
- Expired leases can be re-leased after their deadline. Expired commands are never executed.
- Polling updates the node last-seen heartbeat, but a heartbeat does not prove a successful peer mutation.

## Standalone node client and worker

The node client:
- Requires an HTTPS origin without path, query, fragment, or URL credentials.
- Verifies the server certificate using an explicit CA bundle and presents a dedicated client certificate.
- Disables redirects and system proxy settings.
- Limits response size, rejects unknown fields, and accepts only peer.ensure and peer.remove.
- Uses bounded HTTP request timeouts.

The worker:
- Checks command expiry and strictly decodes the command payload.
- Calls the local executor only for peer.ensure and peer.remove.
- Acknowledges command_expired or validation_failed without calling WireGuard.
- Reports mutation_disabled when the executor is in its default dry-run mode; it never reports a dry-run as a successful mutation.
- Reports wg_apply_failed for an executor failure.
- Retries transport and acknowledgement failures with bounded exponential backoff. A command may be redelivered after its lease expires.

## WireGuard executor

The local Go executor validates public keys and pool membership, checks existing AllowedIPs and interface-address collisions, and writes only the target [Peer] block. Its AllowMutations setting defaults to false.

Before mutation is enabled:
1. Confirm the interface, client pools, and config path against the actual node.
2. Ensure the agent service has only the privileges it needs.
3. Test peer add/remove on an isolated interface.
4. Verify rollback after wg set failure and crash recovery after config writes.
5. Test duplicate command delivery and expired leases.
6. Verify existing peers, routes, NAT, IPv4/IPv6 forwarding, and reboot persistence.
7. Obtain explicit production approval before enrolling or enabling this on any live node.

The current AWS WireGuard node has not been modified, enrolled, or connected to this agent.
