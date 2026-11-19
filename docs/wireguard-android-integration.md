# Android WireGuard integration

## Included

- Official embeddable WireGuard Android tunnel library: `com.wireguard.android:tunnel:1.0.20260102`.
- The library supplies `GoBackend`, its userspace `wireguard-go` engine, and its VPN service declaration through its AAR manifest.
- The app requests Android VPN consent using `VpnService.prepare()` before asking the backend to start the tunnel.
- User-supplied `.conf` content is parsed with the library's `Config.parse` and checked for an interface address, at least one peer endpoint, and allowed IPs.
- Profiles are written to `noBackupFilesDir`, encrypted with AES-256-GCM, and keyed by a non-exportable Android Keystore key. Android backup is disabled.
- The UI keeps device internet state separate from WireGuard engine state.
- ZEUS uses the backend's supported `IncludedApplications` and `ExcludedApplications` settings. It validates package names, limits selections to 100 launchable apps, and applies the chosen policy when starting the tunnel.

## Split-tunnel semantics

- `ALL`: remove any profile-supplied app-routing directives and route according to the profile's network AllowedIPs.
- `INCLUDE_SELECTED`: only the chosen launchable Android packages are allowed to use the VPN interface.
- `EXCLUDE_SELECTED`: chosen packages bypass the VPN interface; other apps follow the VPN routes.

The app selector uses an Android launcher-intent package visibility query rather than `QUERY_ALL_PACKAGES`. It stores package names locally; the list is not uploaded to ZEUS. Policy changes are applied on the next connection, so an already-running tunnel must be restarted.

## Important state semantics

**Engine active** means the WireGuard backend entered its UP state and the Android TUN interface was created. It does not prove that a peer handshake completed or application traffic can reach the internet. ZEUS requires a recent handshake and a successful HTTPS probe before showing the tunnel as verified.

A successful HTTPS probe is still not proof that every app, domain, or protocol is reachable.

## Profile handling

Never paste a profile containing a private key into GitHub, issue trackers, chat messages, crash reports, or logs. Obtain a profile from a trusted server administrator. Replacing or deleting the saved profile is performed locally. The profile is intentionally excluded from Android backup.

The encrypted blob uses a versioned header, a 96-bit GCM nonce, authenticated additional data, and a 128-bit authentication tag. If decryption fails or the Android Keystore key is unavailable, the app does not fall back to plaintext storage.

## VPN service lifecycle

The official WireGuard AAR manifest contributes the VPN service protected by `android.permission.BIND_VPN_SERVICE`. The app requests current consent before a manual connection. The backend manages the TUN descriptor and protects its transport sockets from recursive routing.

## Not implemented by the standard WireGuard engine

- Cloudflare WARP account registration or WARP-specific `Reserved` field handling.
- AmneziaWG obfuscation parameters and native runtime.
- Dynamic discovery/ranking of provider-authorized endpoints.
- VLESS/REALITY or Hysteria2 transport adapters.
- Physical-device acceptance tests on Iranian mobile and fixed-line networks.

Do not feed a WARP or AmneziaWG profile to the standard WireGuard parser and assume the extra protocol fields are supported. Each protocol needs a compatible backend and its own validation tests.

No production VPN server or peer was modified by this feature.
