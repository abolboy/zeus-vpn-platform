# ZEUS VPN Node Agent — WireGuard Executor Foundation

This module contains a constrained WireGuard peer executor foundation. It is not a complete running node agent yet: mTLS enrollment, certificate rotation, the separate node-agent listener, command polling/leases/acknowledgements, and a service entrypoint remain unimplemented.

## Safety defaults

- AllowMutations defaults to false; the manager validates and previews commands without writing configuration or invoking wg.
- Real changes require explicit programmatic opt-in by setting AllowMutations to true.
- Mutations use a fixed wg set argument list; no shell is invoked.
- Public keys must be canonical base64 encoding of 32 bytes.
- Peer addresses must be host addresses inside explicitly configured IPv4/IPv6 pools. Existing peer routes and server interface addresses are checked for collisions.
- The existing WireGuard config must be a regular file with owner-only permissions. Symlink paths and broad permissions are rejected.
- Only the target [Peer] block is changed. Interface settings and other peer blocks are preserved.
- Config changes use a same-directory temporary file, mode 0600, fsync, atomic rename, and directory fsync.
- If runtime application fails, the prior config is restored and the prior AllowedIPs are restored where safe; any rollback or compensation failure is returned to the caller.
- Private keys are neither parsed into application structs nor written to logs.

## Required integration work before deployment

1. Implement per-node mTLS enrollment and certificate rotation.
2. Add a separate mTLS control-plane listener and bind each client certificate to exactly one node.
3. Implement a durable poll/lease/ack loop for typed node commands.
4. Test the executor on an isolated WireGuard interface and representative production-format config fixtures.
5. Run failure-injection and reboot-persistence tests on a staging VPS.
6. Obtain explicit production approval before enabling mutations on any live node.

## Tests

    go test -race ./...
    go vet ./...

This package is deliberately not deployed to the current AWS VPN host.
