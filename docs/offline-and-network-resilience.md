# Offline Operation and Constrained-Network Resilience

## Honest capability boundary
The interface, cached metadata, and local profiles can remain available offline. A VPN tunnel still needs a usable network route to a reachable VPN endpoint. No app can guarantee connectivity when all paths are unavailable, the carrier blocks all supported transports, the endpoint is down, or device policy denies the connection.

## Client behavior
- Keep the app shell and last-synced non-secret metadata available offline.
- Store sensitive connection material only in protected storage backed by Android Keystore. Never place private keys in ordinary preferences, logs, analytics, or API responses.
- Keep the data plane independent from control-plane availability after a profile is provisioned, where the selected engine supports it.
- Queue only idempotent management operations. Do not queue key rotation, account deletion, or server mutations without explicit conflict and authorization handling.
- Use bounded exponential backoff with jitter, cancellation, and retry budgets to protect battery and infrastructure.
- Show device internet, control API reachability, and VPN tunnel state separately.

## Fallback and validation
- Maintain an authorized endpoint list and last-known-good metadata.
- Probe only configured endpoints, with timeouts and rate limits.
- Prefer the last successful endpoint while its profile and policy remain valid.
- Add alternative transports only after protocol review and tests on representative networks.
- Verify tunnel handshake and traffic counters; never infer VPN success from Android internet validation.

## Test matrix
No network; validated Wi-Fi; captive portal or DNS failure; restricted mobile network; control API outage while a tunnel is active; endpoint unreachable or UDP blocked; process killed and restored; Wi-Fi/mobile handoff.

## Current scope
This foundation detects device connectivity and persists one non-secret preference. It does not establish a VPN tunnel, cache private profiles, implement background reconnect, or provide transport fallback.
