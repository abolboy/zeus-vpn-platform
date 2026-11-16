# Offline Operation and Constrained-Network Resilience

## Honest capability boundary

The app shell, encrypted local profile, and local routing preferences remain available offline. A VPN tunnel still requires a usable path to a reachable endpoint. No client can guarantee connectivity when the carrier blocks every enabled transport, the endpoint is down, or Android/device policy denies the VPN.

## Client behavior

- Keep non-secret settings and the last-known-good profile locally available.
- Store private tunnel material only in Android Keystore-backed encrypted storage. Never place private keys in ordinary preferences, logs, analytics, or API responses.
- Keep the data plane independent from Control Plane availability after a profile is provisioned, where the selected engine allows it.
- Queue only idempotent management operations. Never queue account deletion, key rotation, or server mutations without explicit conflict and authorization handling.
- Use bounded reconnect backoff, cancellation, and retry budgets to protect battery and infrastructure.
- Show device internet state, Control Plane reachability, VPN engine state, peer handshake, and HTTPS reachability as separate signals.

## Application routing

The Android WireGuard backend accepts `IncludedApplications` and `ExcludedApplications` in the Interface section. ZEUS exposes three modes:

- All apps through VPN.
- Only selected launchable apps through VPN.
- Selected launchable apps bypass VPN.

The selection is persisted as non-secret package names in local DataStore preferences. Package visibility is limited to launchable activities through an Android `<queries>` declaration; ZEUS does not request the broad `QUERY_ALL_PACKAGES` permission. Changes apply on the next tunnel start; an active tunnel must be restarted to use the new policy.

Split tunneling is a routing choice, not a censorship-bypass transport. It cannot make an unreachable endpoint reachable and does not defeat network allow-lists by itself.

## Endpoint and transport fallback requirements

- Discover and test only endpoints authorized by the configured provider or ZEUS Control Plane.
- Use bounded, cancellable tests and small concurrency limits; do not continuously scan broad public IP ranges.
- A UDP port-open check or ICMP ping is not proof of a usable WireGuard tunnel. Rank candidates by an actual peer handshake, traffic counters, and a successful HTTPS request through the tunnel.
- Cache a last-known-good endpoint with a bounded age and re-test it after network changes.
- Keep transport adapters isolated. Standard WireGuard, Cloudflare WARP, AmneziaWG, VLESS/REALITY, and Hysteria2 are different protocols and must not be represented as interchangeable WireGuard profiles.
- Do not silently change providers or send user traffic to third-party endpoints without disclosure and a valid profile.

## Test matrix

No network; validated Wi-Fi; captive portal; DNS failure; restricted mobile data; UDP blocked; HTTPS-only reachability; Control Plane outage while a tunnel is active; endpoint loss; Wi-Fi/mobile handoff; app allow-list and exclude-list behavior; process death; device reboot; IPv4 and IPv6 leak policy.

## Current verified scope

The app includes the official WireGuard userspace engine, encrypted profile storage, device-network monitoring, handshake/HTTPS tunnel checks, bounded reconnect policy, and (on the split-tunneling feature branch) per-app route selection. WARP account provisioning, dynamic endpoint discovery, AmneziaWG runtime, VLESS/REALITY and Hysteria2 adapters, and real Iranian carrier acceptance tests remain future work. A successful CI build is not proof that any transport works on a specific Iranian ISP.
