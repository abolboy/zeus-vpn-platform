# ZEUS Tor-only migration QA acceptance plan

Status: Acceptance plan; execution pending implementation.

| Gate | Test | Expected result | Status |
|---|---|---|---|
| API | Valid signed OIDC Tor enrollment | 201, transport=tor, no peer object | NOT RUN |
| API | Wrong issuer/audience, expired token, unverified email | 401 safe error | NOT RUN |
| API security | Account A reads/revokes account B device | 404; no state change | NOT RUN |
| API reliability | Same idempotency key and body | Same device, replay header | NOT RUN |
| API reliability | Same key, changed body | 409 idempotency_conflict | NOT RUN |
| API concurrency | Parallel enrollment at max_devices | Never exceed quota across Tor + WG | NOT RUN |
| Invitation | Valid one-time Tor invitation | One device only; hash stored | NOT RUN |
| Invitation | Expired/revoked/replayed invitation | Rejected | NOT RUN |
| Android storage | Inspect pending record | No WG keys; tokens encrypted | NOT RUN |
| Android service | VPN consent denied | No false Connected state; cleanup | NOT RUN |
| Android service | Start/stop/revoke/process death | Deterministic cleanup; no stale worker | NOT RUN |
| Data plane | IPv4 TCP request | Tor exit only; no physical direct egress | NOT RUN |
| Data plane | IPv6 request | Tor if supported; otherwise blocked in TUN | NOT RUN |
| Data plane | UDP/QUIC | Fails closed; no direct egress | NOT RUN |
| DNS | A/AAAA lookup and DNS failure | DNS stays in tunnel; no fallback | NOT RUN |
| UI | Tor ready but TUN/Hev not ready | Not Connected | NOT RUN |
| Build | Remove Android WG AAR after migration | Debug/release build; one ZEUS VPN service | NOT RUN |
| Compatibility | API 24, 28, 29 and target API 36/37 | Build/runtime matrix passes | NOT RUN |

Existing evidence is not proof of a Tor VPN: previous Hev native compatibility checks established JNI load and an idle native call only. Existing Kotlin tests are unit-level, not VpnService or physical-network tests.

Required commands when implementation is ready:
- Android: ./gradlew :apps:android:testDebugUnitTest :apps:android:assembleDebug
- Android instrumentation: ./gradlew :apps:android:connectedDebugAndroidTest on recorded emulator/device.
- Control plane: go test ./... from services/control-plane.
- Migrations: disposable PostgreSQL only; test fresh install and upgrade.
- Native compatibility: pinned CI for API 24/28/29 plus actual app runtime matrix.

For every PASS record exact commit, command, environment, exit code, test count and logs. Unexecuted tests remain NOT RUN.
