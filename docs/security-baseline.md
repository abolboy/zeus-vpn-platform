# Security Baseline

## Secrets
- Never commit `.env` files, API tokens, SSH private keys, WireGuard private keys, signing keys, or production credentials.
- Keep local secrets outside the repository and use GitHub Actions secrets/environments only when CI/CD is introduced.
- Rotate credentials after suspected exposure; deleting a file from the latest commit is not sufficient if it existed in history.

## Identity and authorization
- Use least-privilege roles for operators and node agents.
- Require explicit authorization for user, peer, and node mutations.
- Prefer short-lived tokens and documented revocation.
- Log security-relevant actions without logging credentials or private keys.

## Network
- Require TLS for control-plane traffic.
- Restrict node management ports to trusted sources where practical.
- Keep VPN data-plane ports separate from management interfaces.
- Validate firewall rules and IPv4/IPv6 behavior before production.

## Supply chain
- Pin dependencies and review updates.
- Run dependency and static-analysis checks before release.
- Generate build provenance and sign release artifacts when release automation is implemented.

## Operational safety
- Back up state before migrations or deployment.
- Stage changes on a test node before production.
- Require an explicit approval gate for production deployment.
- Test rollback and restore procedures.
