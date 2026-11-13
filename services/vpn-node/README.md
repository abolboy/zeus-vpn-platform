# ZEUS VPN Node Agent

This module contains a standalone node poller, an mTLS control-plane client, a constrained command worker, and the local WireGuard peer executor.

## Required files

- Client certificate signed by the ZEUS node CA, with ClientAuth EKU.
- Matching client private key in a regular file with permissions 0600 or stricter.
- CA bundle used to verify the control-plane HTTPS server.
- Existing WireGuard interface and persistent configuration file.

Do not place private keys in the repository, app package, or a shared environment file. Provision them directly on the Linux node with root-only permissions.

## Run

Build and run the node process with these environment settings:

    ZEUS_CONTROL_PLANE_URL=https://control-plane.example
    ZEUS_NODE_CLIENT_CERT_FILE=/etc/zeus-node/client.crt
    ZEUS_NODE_CLIENT_KEY_FILE=/etc/zeus-node/client.key
    ZEUS_NODE_SERVER_CA_FILE=/etc/zeus-node/control-plane-ca.crt

Optional WireGuard settings:

    ZEUS_WG_INTERFACE=wg0
    ZEUS_WG_CONFIG_PATH=/etc/wireguard/wg0.conf
    ZEUS_WG_BINARY=/usr/bin/wg
    ZEUS_WG_IPV4_POOL=10.77.77.0/24
    ZEUS_WG_IPV6_POOL=fd77:77:77::/64
    ZEUS_NODE_POLL_INTERVAL=5s

The process defaults to dry-run. It validates commands and reports mutation_disabled without writing the WireGuard file or executing wg.

Real changes require both:
- ZEUS_NODE_ALLOW_MUTATIONS=true
- A root-run service process in this implementation.

Never enable this on the live AWS node until a staging node has passed peer add/remove, rollback, replay, timeout, and reboot-persistence tests. The current AWS node is not enrolled or connected to this agent.

## Command safety

- HTTPS is mandatory. The client verifies the control-plane certificate against the configured CA and presents its mTLS client certificate.
- Redirects and system proxy configuration are disabled to avoid sending node credentials to another host.
- Responses are size-limited and decoded strictly.
- Only peer.ensure and peer.remove commands are accepted.
- Payloads reject unknown fields and are validated by the local executor.
- Commands are checked for expiry before execution.
- The worker acknowledges only after the local executor returns. If the acknowledgement is lost, the lease queue may redeliver; executor operations are designed to be idempotent.
- Private keys are never included in a command, parsed into a peer payload, or logged.

## Tests

    go test -race ./...
    go vet ./...

Unit and TLS integration tests use temporary certificates and a fake WireGuard runner. They do not prove a real production peer mutation or staging VPS connection.
