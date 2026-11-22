# ZEUS Tor-only migration QA acceptance plan

Status: Tor API, additive schema, shared quota logic, Android Tor enrollment repository, encrypted retry record, Tor-only dashboard, VpnService host, and validation tests are present on the feature branch. CI and disposable PostgreSQL verification are pending; Android service and VPN data-plane tests remain unrun.

| Gate | Test | Expected result | Status |
|---|---|---|---|
| API | Valid signed OIDC Tor enrollment | 201, transport=tor, no peer object | NOT RUN — implementation added; test execution pending |
| API | Wrong issuer/audience, expired token, unverified email | 401 safe error | NOT RUN |
| API readiness | Missing customer OIDC verifier | 503 fail-closed response | NOT RUN — test added |
| Android client | OIDC/invitation enrollment, status and revoke routes | Tor endpoints only; no WG key or profile | NOT RUN — repository and dashboard added |
| Android storage | Pending Tor device ref and idempotency survive process death | Dedicated Keystore AES-GCM record; no ID token persisted | NOT RUN — implementation added |
| API validation | Unknown fields including public_key/user_id/node_id | 400 strict-schema rejection | NOT RUN — test added |
| API validation | Non-Android platform and invalid client version | 400 validation error | NOT RUN — tests added |
| API security | Account A reads/revokes account B device | 404; no state change | NOT RUN |
| API administration | Admin lists and revokes a lost Tor device | Account-scoped audit record; device status becomes revoked | BLOCKED — endpoint not implemented |
| VPN authorization | Start with locally stored but server-revoked device ID | Service refuses to start; no TUN or Tor worker | NOT RUN — start path has no server-status preflight |
| VPN authorization | Start with inactive account or expired entitlement | Fail closed before TUN establishment | BLOCKED — entitlement expiry/runtime enforcement not integrated |
| Invitation management | Invitation-only enrollment reads/revokes device without OIDC | Explicit supported auth contract; no stuck device | BLOCKED — current status/revoke endpoints require OIDC |
| Revocation propagation | Admin revokes device while tunnel is active | Client stops within documented revocation window; no direct fallback | BLOCKED — admin revoke and propagation policy not implemented |
| API abuse control | Tor status/revoke over 30 requests per source IP per minute | 429 with Retry-After | NOT RUN — limiter added |
| API reliability | Same idempotency key and body | Same device, replay header | NOT RUN |
| API reliability | Same key, changed body | 409 idempotency_conflict | NOT RUN |
| API concurrency | Parallel enrollment at max_devices | Never exceed quota across Tor + WG, with shared account-row lock | NOT RUN — SQL implemented; DB test pending |
| Invitation | Valid one-time Tor invitation | One device only; hash stored | NOT RUN |
| Invitation | Expired/revoked/replayed invitation | Rejected | NOT RUN |
| Android storage | Inspect pending record | No WG keys; tokens encrypted | NOT RUN |
| Android service | VPN consent denied | No false Connected state; cleanup | NOT RUN — service host added |
| Android service | Foreground service start on API 24/28/29/34+ | Correct foreground type and notification lifecycle | NOT RUN — manifest/service added |
| Android service | Start/stop/revoke/process death | Deterministic cleanup; no stale worker | NOT RUN — lifecycle host added |
| Data plane | IPv4 TCP request | Tor exit only; no physical direct egress | NOT RUN |
| Data plane | IPv6 request | Tor if supported; otherwise blocked in TUN | NOT RUN |
| Data plane | UDP/QUIC | Fails closed; no direct egress | NOT RUN |
| DNS | A/AAAA lookup and DNS failure | DNS stays in tunnel; no fallback | NOT RUN |
| UI | Tor ready but TUN/Hev not ready | Not Connected | NOT RUN — Tor-only dashboard added; runtime test pending |
| Connectivity | Probe through Tor SOCKS using remote-domain SOCKS5 request | Proves Tor path; app UID bypass cannot satisfy the check | NOT RUN |
| Connectivity negative | Physical-network HTTPS probe while ZEUS UID is excluded | Must not be accepted as Tor connectivity evidence | NOT RUN |
| Build | Remove Android WG AAR after migration | Debug/release build; one ZEUS VPN service | NOT RUN |
| Compatibility | API 24, 28, 29 and target API 36/37 | Build/runtime matrix passes | NOT RUN |

Existing evidence is not proof of a Tor VPN: previous Hev native compatibility checks established JNI load and an idle native call only. Existing Kotlin tests are unit-level, not VpnService or physical-network tests.

Required commands (not yet executed for the current API changes):
- Android: ./gradlew :apps:android:testDebugUnitTest :apps:android:assembleDebug
- Android instrumentation: ./gradlew :apps:android:connectedDebugAndroidTest on recorded emulator/device.
- Control plane: go test ./... from services/control-plane.
- Migrations: disposable PostgreSQL only; test fresh install and upgrade.
- Native compatibility: pinned CI for API 24/28/29 plus actual app runtime matrix.

For every PASS record exact commit, command, environment, exit code, test count and logs. Unexecuted tests remain NOT RUN.


## Additional acceptance gates for server-enforced access

The following tests are required for the accepted gateway architecture. All are currently **BLOCKED / NOT RUN** because the gateway, grant API, admin routes, and device-bound invitation exchange have not been implemented.

| Gate | Test | Expected result | Status |
|---|---|---|---|
| API | Request gateway grant for active device/account | Short-lived grant scoped to one device and gateway audience | BLOCKED — grant endpoint not implemented |
| API | Request grant for revoked device, suspended account, expired entitlement | Denied with stable safe error; no grant issued | BLOCKED — runtime entitlement enforcement not implemented |
| API | Invitation-only enrollment exchanges invitation for device credential | One-time invitation consumed; no long-lived invitation reused | BLOCKED — device-auth exchange not implemented |
| API security | Replay, wrong audience, expired grant, altered device binding | Gateway rejects each attempt | BLOCKED — gateway not implemented |
| Admin API | Operator lists devices and revokes selected device | Role-checked, account-scoped, audited, idempotent state transition | BLOCKED — admin routes not implemented |
| Gateway | Revoked device opens a new proxy session | Denied after documented revocation SLA | BLOCKED — gateway not implemented |
| Gateway | Device revoked during active proxy sessions | All sessions for that device are terminated within SLA | BLOCKED — session registry/event path not implemented |
| Gateway | Control-plane/introspection unavailable | New sessions fail closed; no local/direct fallback | BLOCKED — gateway not implemented |
| Gateway | Gateway process or Tor daemon fails | No direct destination egress; health state is degraded | BLOCKED — gateway not implemented |
| Transport | Inspect Android-to-gateway wire path | Authenticated encryption; no reusable plaintext SOCKS credentials | BLOCKED — secure transport/adapter not selected |
| Android | Modified/stale local device state attempts to connect | Gateway rejects revoked/expired device independently of app UI | BLOCKED — gateway not implemented |
| Privacy | Inspect gateway logs and telemetry | No destination history, SOCKS request content, Tor circuit IDs, or traffic payloads | BLOCKED — gateway not implemented |
| Data plane | IPv4/IPv6/DNS/UDP/QUIC and physical-interface packet capture | Only supported flows traverse gateway-to-Tor; unsupported traffic fails closed | BLOCKED — gateway not implemented |

Do not mark the existing embedded-Tor client as satisfying these server-enforcement gates. The current client implementation and its exact-HEAD CI checks are separate prerequisites.
