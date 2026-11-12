# ZEUS VPN — Initial Architecture

## Product definition

ZEUS is an Android-first hybrid VPN platform. The Android application is the operator's management console and may later expose carefully scoped local diagnostics. VPN ingress and traffic forwarding are hosted on Linux nodes in the initial supported deployment.

## Logical planes

### Management plane
- Android application: Kotlin, Jetpack Compose, Android Keystore for local secrets.
- Control API: Go, HTTPS, short-lived access tokens, role-based authorization.
- Database: PostgreSQL for durable identities, node inventory, subscriptions, and audit metadata.
- Optional cache/queue: Redis only when measured requirements justify it.

### Data plane
- Linux VPN nodes.
- WireGuard is the first candidate for the initial tunnel implementation.
- Node agent receives authenticated, narrowly scoped desired-state instructions from the control plane.
- VPN peer private keys remain on the node/client that owns them; do not expose them through ordinary API responses.

### Delivery plane
- GitHub repository and pull requests.
- GitHub Actions for repository checks, tests, and later signed build artifacts.
- VPS or cloud hosts for actual VPN nodes and backend services.
- GitHub Actions is CI/CD, not a persistent VPN traffic host.

## Android role and platform boundary

Android `VpnService` provides an API for an app to create a local VPN interface and route traffic through it. It does not, by itself, provide a general inbound VPN server facility. A phone-hosted server mode would require a separate feasibility spike covering inbound reachability, carrier-grade NAT, firewalling, battery/background execution, thermal limits, and possibly privileged/root networking. It is not part of the initial production path.

## Initial technology choices

| Component | Initial choice | Rationale |
|---|---|---|
| Android app | Kotlin + Jetpack Compose | Native Android integration |
| Control plane | Go | Small deployable service and concurrency |
| Node agent | Go initially | Shared operational language with control plane |
| VPN data plane | WireGuard on Linux | Established implementation |
| Database | PostgreSQL | Durable relational state |
| Cache/queue | Defer Redis | Avoid premature infrastructure |
| Web dashboard | Defer until Android/API flows stabilize | Android-first scope |
| Native optimization | Rust only when profiling justifies it | Avoid unnecessary FFI complexity |

## Trust boundaries

1. Android operator device ↔ control API: TLS, authentication, authorization, rate limits.
2. Control API ↔ node agent: mutually authenticated channel or equivalent per-node credentials with rotation and revocation.
3. Node ↔ VPN peers: WireGuard key-based peer authentication.
4. CI ↔ deployment: least-privilege credentials, protected environments, human approval for production.

## Non-goals for Phase 0

- Custom cryptographic protocols.
- Stealth/evasion features.
- Production deployment.
- Multi-region automatic failover.
- Phone-hosted public VPN server.
- Claims of throughput or battery performance before measurement.
