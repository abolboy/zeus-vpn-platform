# Android WireGuard integration

## Included in this milestone

- Official embeddable WireGuard Android tunnel library: `com.wireguard.android:tunnel:1.0.20260102`.
- The library supplies `GoBackend`, its userspace `wireguard-go` engine, and its VPN service declaration through its AAR manifest.
- The app requests Android VPN consent using `VpnService.prepare()` before asking the backend to start the tunnel.
- User-supplied `.conf` content is parsed with the library's `Config.parse` and checked for an interface address, at least one peer endpoint, and allowed IPs.
- Profiles are written to `noBackupFilesDir`, encrypted with AES-256-GCM, and keyed by a non-exportable Android Keystore key. The app disables Android backup.
- The UI keeps device internet state separate from WireGuard engine state.

## Important state semantics

The state labelled **Engine active** means the WireGuard backend successfully entered its UP state and the Android TUN interface was created. It does not prove a peer handshake completed or that application traffic can reach the internet. Handshake timestamps, traffic counters, DNS tests, reconnect behavior, and end-to-end network validation are not implemented in this milestone.

## Profile handling

Do not paste a profile containing a private key into GitHub, issue trackers, chat messages, crash reports, or logs. Obtain a client profile from a trusted server administrator. Replacing or deleting the saved profile is done locally on the device. The profile is intentionally excluded from Android backup.

The encrypted blob uses a versioned header, a 96-bit GCM nonce, authenticated additional data, and a 128-bit authentication tag. If decryption fails or the Android Keystore key is unavailable, the app does not fall back to plaintext storage.

## VPN service lifecycle

The official WireGuard AAR manifest contributes the VPN service protected by `android.permission.BIND_VPN_SERVICE`. The app always requests current consent before a manual connect. The backend manages the TUN descriptor and protects its transport sockets from recursive routing.

## Not included yet

- Automatic key generation and authenticated provisioning from ZEUS Control Plane.
- Linux node installation, firewall/NAT configuration, and server-side peer enrollment.
- Peer handshake/transfer telemetry and reachability checks.
- Exponential-backoff reconnect policy and network handover tests.
- Control-plane authentication, user/subscription APIs, and admin RBAC.

No VPN server was provisioned or modified by this change.
