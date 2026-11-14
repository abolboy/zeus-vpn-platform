# ZEUS VPN Node Agent Protocol

Status: the server-side mTLS listener, certificate-to-node mapping, durable command queue, outbound mTLS client, command worker, and dry-run-first WireGuard executor are implemented. An offline CA and per-node ClientAuth certificate utility is included. Staging enrollment, live rotation/revocation drills, and isolated peer mutation tests remain pending. The current AWS VPN node is not enrolled or connected.

## Security boundary

- Nodes initiate outbound HTTPS polling; the control plane does not open SSH or arbitrary inbound management connections to nodes.
- The node-agent listener is separate from the browser/admin listener and is disabled unless the three dedicated TLS settings are supplied.
- TLS 1.2+ with a verified ClientAuth certificate is required. The leaf cannot be a CA, and its SHA-256 fingerprint must match an active, in-window certificate record attached to a non-disabled node.
- Identity is never inferred from X-Forwarded headers or caller-supplied node IDs.
- Client private key files must be regular no-follow files with owner-only permissions; server-side TLS private keys are also read without following symlinks and must be owner-only.
- The current AWS WireGuard node has not been enrolled and has not been modified.

## Node certificate issuance and rotation

- Build zeus-node-cert from services/vpn-node on a trusted administrator workstation.
- Create the CA on a separate encrypted/offline workstation. The CA private key must be owner-only and must never be copied to the Control Plane host, CI, or a VPN node.
- Issue one distinct ECDSA P-256 ClientAuth certificate per pre-registered vpn_nodes.id. The default validity is 90 days (maximum 365 days); the leaf includes spiffe://zeus/node/<UUID> as a URI SAN.
- The utility prints the certificate SHA-256 fingerprint and a SQL insert statement for vpn_node_agent_certificates. It does not connect to PostgreSQL or configure a node automatically.
- The database registry supports multiple active fingerprints for one node. For routine rotation, register the replacement, install its certificate/key, verify the agent can poll and acknowledge, then set revoked_at on the old fingerprint.
- Emergency compromise requires immediate revocation of the compromised fingerprint. Availability is not preserved unless a replacement has already been installed.
- The node receives only its own client certificate/key and the Control Plane server CA, never the CA private key.
- Do not enable provisioning for a node until its endpoint, public key, interface addresses, address pools, and pre-existing peers have been independently verified.

## Device lifecycle and queued commands

- Device registration stores a client-generated public key only. Client private keys never leave the device.
- Owner/admin operators issue 256-bit, 15-minute activation codes; only SHA-256 hashes are stored, and an activation code is limited to one client identity.
- The public client enrollment endpoint is rate-limited and does not accept operator bearer tokens. A retry with the same activation code, device reference, and public key recovers the original record; different identities are rejected.
- Registration is accepted only for an active VPN user and an online mTLS-registered node with explicit client pools and interface addresses.
- IP allocation considers the node's interface IPs and every non-revoked peer assignment. Per-node unique indexes prevent concurrent duplicate assignment.
- Device and peer records, the peer.ensure command, idempotency response, and audit event are committed in the same PostgreSQL transaction.
- A newly registered device is provisioning and its peer is pending.
- A successful peer.ensure acknowledgement moves both records to active. A failure or expired command moves the associated records to error.
- Revocation moves the device and peer to revoking, then queues a peer.remove. Only a successful acknowledgement changes them to revoked.
- An address remains reserved while a peer is pending, active, revoking, or error. It becomes reusable only after removal has been acknowledged and the peer status is revoked.

## Node-agent endpoints

On the separate mTLS listener:
- GET /agent/v1/commands/next leases one command for the verified node identity; 204 means no command is ready.
- POST /agent/v1/commands/{commandID}/ack acknowledges succeeded or failed using a constrained error-code allowlist.

The command enum is closed:
- peer.ensure: payload includes only the WireGuard public key and assigned IPv4/IPv6 host addresses.
- peer.remove: payload includes only the public key to remove.

Arbitrary shell text, executable paths, interface names, firewall scripts, arbitrary logs, or opaque command strings are not accepted.

## Queue guarantees

- Commands are node-scoped and delivered at least once.
- A command receives a 45-second lease and can be re-leased after a lease timeout, provided it has not expired and has attempts remaining.
- Commands expire and are limited to 10 attempts.
- Acknowledgements are scoped to the authenticated node ID and command ID. Identical replays are idempotent; conflicting replies are rejected.
- Node heartbeat updates do not prove successful peer application.

## Local executor

- The worker checks expiry, strictly decodes payloads, and dispatches only peer.ensure / peer.remove.
- The local executor's mutation flag defaults to false. In dry-run mode it validates and previews operations, then reports mutation_disabled; it does not claim a successful mutation.
- Real execution uses a fixed wg set argument list with no shell, validates public keys/pool membership/address collisions, reads config without following symlinks, and writes atomically with rollback handling.
- Explicitly enabling mutations requires a staging node, a backup, failure-injection tests, and least-privilege OS review.

## Remaining rollout gates

1. Implement operator-managed node inventory, automated certificate revocation/API or UI, and live staging rotation/revocation drills.
2. Import/represent existing unmanaged WireGuard peers and addresses before enabling allocation on a real node.
3. Verify node pools/interface addresses against the actual WireGuard configuration.
4. Test add/remove on an isolated interface, then a staging VPS.
5. Test duplicate delivery, lease expiry, failed wg set, process termination, and reboot persistence.
6. Confirm encrypted client profile delivery and offline reconnect.
7. Obtain explicit approval before connecting or mutating any production node.
