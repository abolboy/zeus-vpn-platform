# ZEUS Tor transport integration design

**Status:** Superseded architecture record; Tor is the sole Android client VPN engine by product decision (2026-11-22). Implementation is incomplete; this is not yet a functioning Tor VPN.  
**Implementation:** PR #28 on `feat/android-tor-transport-pr27`.  
**Scope:** Android client only. Remove the WireGuard Android backend after the Tor-only service passes acceptance gates. Server-side WireGuard node/control-plane support remains unchanged.

## Decision

Tor is the only supported Android VPN transport in the final ZEUS client. Do not implement a WireGuard/Tor selector or parallel engine modes. A ZEUS-owned `VpnService` must own the connection state and the single Android TUN interface.

- Remove the Android `GoBackend` dependency and app-side WireGuard engine/profile UI after replacing their provisioning and connection flows with the Tor-only design.
- Tor mode runs an embedded Tor daemon and exposes a loopback SOCKS5 endpoint.
- A dedicated, audited TUN-to-SOCKS adapter is required to route Android app traffic into that SOCKS endpoint. The Tor daemon alone is only a local proxy; it does not transparently tunnel all device traffic.
- The Tor adapter must be integrated into ZEUS's single VPN-service lifecycle, not by starting Orbot's independent VPN service alongside WireGuard. The pinned WireGuard AAR (`1.0.20260102`) contributes `GoBackend.VpnService`. Inspection of the upstream `GoBackend` implementation confirms that `GoBackend` is final, its static VPN-service future is private, `setStateInternal()` starts the hard-coded `GoBackend.VpnService` class, and the backend itself establishes the TUN and detaches its descriptor directly into `wgTurnOn()`. There is no public API to hand that TUN to another engine. **Superseded:** the WireGuard fork is no longer the selected architecture. Implement a ZEUS-owned `VpnService` directly; do not retain or fork `GoBackend` merely as a host. This service and its TUN lifecycle are not yet implemented or tested.
- Guardian Project's embedded `TorService` has no `VpnService.protect()` integration. Because it runs under the ZEUS app UID, the Tor VPN builder must exclude the ZEUS package from its TUN (or omit it from an allowlist) so Tor's own outbound sockets reach the physical network instead of looping into the TUN. This also means ZEUS app/control-plane traffic itself bypasses Tor by design. If product requirements later demand tunneling ZEUS's own app traffic, a reviewed per-socket protection hook in Tor's native layer is required.

## Current implementation increment (2026-11-22)

- Hev config now explicitly declares IPv4 `198.18.0.1` and IPv6 `fc00::1` TUN addresses.
- `TorVpnTunnelPolicy` centralizes the matching IPv4/IPv6 interface addresses, full-tunnel default routes, fake-DNS server, MTU, and ZEUS-package exclusion for the future Android service.
- Unit tests cover the policy values and the Hev config asserts the IPv6 address. These are source/unit-level checks only; no Android `VpnService` consumes the policy yet, so no device traffic is routed through Tor.

## Android component boundaries

1. `TransportCoordinator`: serializes connect, switch, disconnect, and recovery; stops and verifies the current owner before starting another.
2. `ZeusVpnService`: the single manifest-declared Android VPN service; it owns consent/Always-on lifecycle and hands one TUN descriptor to Hev. It is not yet implemented.
3. `TorDaemonController`: owns the embedded Tor daemon lifecycle, readiness, SOCKS endpoint discovery, error propagation, and bounded shutdown.
4. `TorVpnEngine`: owns the TUN-to-SOCKS native adapter after `VpnServiceHost` transfers the TUN descriptor. It must not claim connected until Tor has a usable circuit and the adapter is forwarding.
5. No WireGuard Android engine exists in the final client. The server-side WireGuard node remains a separate, unchanged product component.
6. UI state: `DISCONNECTED / STARTING / CONNECTED / STOPPING / FAILED`, with transport identity and a user-visible failure reason. Never infer internet reachability from daemon startup alone.

## Routing and leak-prevention requirements

- Tor supports TCP streams, not arbitrary UDP forwarding; SOCKS5 UDP ASSOCIATE is unsupported. The current Hev config now declares IPv6 TUN address `fc00::1`; the future ZEUS VPN builder must install the matching IPv6 address and default route so IPv6 cannot escape outside the TUN. Tor SOCKS IPv6 exit behavior depends on `IPv6Traffic` on `SocksPort`; the embedded TorService's effective `torrc` has not been verified. If Tor rejects IPv6 destinations, those flows must fail inside the tunnel; never add a direct-network fallback. Runtime IPv6 no-egress and usable-exit tests remain mandatory.
- DNS requests must be resolved through Tor (remote hostname resolution after a controlled fake-DNS mapping, or another reviewed design); local resolver fallback is forbidden in Tor mode. Verify that the chosen adapter never sends DNS to the device's physical resolver.
- IPv4 and IPv6 policy must be explicit. The current implementation foundation configures dual-stack Hev TUN addresses; the not-yet-implemented `ZeusVpnService` must route both address families into TUN and exclude the ZEUS package. If Tor's IPv6 SOCKS path is unavailable, IPv6 must be dropped inside the tunnel rather than routed directly.
- The TUN adapter must have bounded queues, clean cancellation, descriptor ownership, and deterministic shutdown.
- On startup failure, adapter crash, Tor circuit loss, or transport switch, fail closed: keep the TUN/service in a drop-only state until teardown is coordinated; never fall back to direct forwarding while Tor mode is selected.
- A process/service crash can make Android remove the VPN and resume the underlying network unless system lockdown is enabled. For a strict no-leak Tor mode, require Android Always-on VPN with **Block connections without VPN** (lockdown); verify `VpnService.isLockdownEnabled()` on API 29+ and guide the user through system Settings. Android's framework exempts the VPN provider package from lockdown's blocked-UID set; Tor mode still must explicitly exclude the ZEUS package from the TUN so Tor's own sockets do not loop. Verify this interaction on supported Android/OEM versions before claiming crash-resilient fail-closed protection.
- Per-app include/exclude policy must be implemented and tested for Tor separately; do not assume WireGuard profile directives apply to the Tor engine. Never allow the ZEUS package itself into the Tor TUN: in include-only mode omit it, and in full/exclude mode add it to the disallowed set. This is required for Tor's outbound sockets and means ZEUS app traffic is outside Tor.
- Do not expose the SOCKS or control port on LAN interfaces. Bind to loopback only.

## Dependency research

- Guardian Project's `tor-android` provides the native Tor daemon and Android `TorService`; it does not by itself implement device-wide VPN routing.
- The current Maven Central artifact metadata reports `info.guardianproject:tor-android:0.4.9.17`. Pinning and ABI packaging must be verified in CI before adoption.
- CI run `44978277773` produced an Android debug APK containing `libtor.so` for `arm64-v8a`, `armeabi-v7a`, and `x86_64`; the `x86` ABI has no `libtor.so`. `TorDaemonController` now rejects x86-only devices before binding the Tor service. This confirms the packaged APK ABI matrix, not the original AAR checksum or its internal JNI archive layout.
- Orbot uses `hev-socks5-tunnel` as a native TUN-to-SOCKS component. Upstream Hev is MIT-licensed. The official `2.20.0` release (tag commit `75dbc1e`, 2026-11-13) attaches `hev-socks5-tunnel.aar` as a GitHub release asset (617,551 bytes; release metadata SHA-256: `ae8a2e96b80b4eccccd037805a4bb4a278090c89dbcae8bc6fcb7634e9a255b9`). The uploaded AAR was independently inspected: its ZIP structure and CRC are valid; `classes.jar` contains `hev.htproxy.TProxyService`; `javap` confirms the four static JNI methods and their descriptors; `proguard.txt` keeps the JNI package; and all four native libraries are valid ELF files for `armeabi-v7a`, `arm64-v8a`, `x86`, and `x86_64`. Its SHA-256 matches the official release metadata. This is a release asset, not a Maven coordinate, so `scripts/fetch-hev-aar.sh` downloads it from the pinned release URL and verifies the exact SHA-256 before Android builds. The app consumes it as a local AAR dependency and adds an explicit R8 keep rule.
- Hev supports UDP relay through SOCKS5 UDP ASSOCIATE or its own UDP-over-TCP `FWD_UDP` extension; its `socks5.udp` setting selects a relay mode and has no documented `off` value. For Tor, configure the standard `udp: 'udp'` mode only: Tor rejects UDP ASSOCIATE, and the Hev source returns a handshake error for every non-success SOCKS reply; the session runner then exits and the UDP-session destructor frees queued datagrams/removes the PCB. Do not enable Hev's UDP-over-TCP extension. This is source-level fail-closed evidence, not runtime proof; add an integration test with a SOCKS endpoint returning `REP=0x07` and verify no direct egress. Hev's MapDNS handler answers DNS packets addressed to its configured fake-DNS IP/port with synthetic A records. Its IPv4-to-SOCKS address conversion checks the fake-IP map and converts a match back to a SOCKS domain-name request, allowing Tor-side resolution. This is source-level evidence; runtime DNS capture and no-physical-resolver-fallback tests remain mandatory.
- Do not copy Orbot's VPN service or assume its app-level integration is reusable as a library.

## Delivery sequence and acceptance gates

### Gate A — Tor daemon only
- [x] Add a pinned Tor Android dependency and an initial lifecycle wrapper.
- [x] Bind to the embedded TorService and wait for its first-circuit-ready status plus a valid loopback SOCKS port.
- [x] Handle startup timeout, startup error, unexpected service stop, binding loss, and stop-during-start at the controller level.
- [x] Verify the CI debug APK packages Tor native libraries for `arm64-v8a`, `armeabi-v7a`, and `x86_64`; reject x86-only devices before Tor service binding.
- [x] Independently inspect the uploaded Hev 2.20.0 AAR archive, bundled JNI class/method descriptors, ABI directories, R8 rules, and SHA-256 against the official release metadata.
- [x] Remove the readiness race by waiting for Tor's first-circuit event and polling the live ControlPort `GETINFO net/listeners/socks` value under the startup deadline; accept loopback IPv4/IPv6 only (Android build and unit-test CI passed in run `44987352663`; real embedded TorService and first-circuit runtime testing on an emulator/device remain pending).
- [x] Handle controller rebind to an already-running Tor daemon by polling `GETINFO status/circuit-established` because the original status broadcast is not replayed (Android build and unit-test CI passed in run `44987352663`; real embedded TorService and first-circuit runtime testing on an emulator/device remain pending).
- [x] Add unit regression cases for circuit-established status parsing and quoted IPv4/IPv6 loopback listener parsing, including rejection of non-loopback or invalid endpoints (unit regression tests passed in CI run `44987352663`; this does not establish embedded Tor daemon runtime behavior).
- [ ] Test start, first-circuit readiness, stop-during-start, repeated start/stop, process recreation, and bounded native shutdown on an emulator/device.
- Keep this gate explicitly labelled **local Tor proxy only**; no VPN-connected claim. The controller cannot independently observe completion of native Tor shutdown after unbinding.

### Gate B — TUN-to-SOCKS data plane
- [x] Review Hev upstream source, MIT license, JNI API, Android AAR packaging, and release ABI assets; identify it as the leading candidate.
- [x] Add `HevTun2SocksRuntime` with process-wide serialization, private config creation, loopback-only SOCKS validation, and fail-closed TUN descriptor retention if native shutdown cannot be confirmed; unit/config tests passed in CI run `44987352663`; native TUN forwarding remains untested.
- [x] Treat both a negative Hev stop result and JNI/linkage exceptions as unconfirmed shutdown; retain the TUN descriptor and prohibit retries that cannot prove native worker termination. Regression tests passed in CI run `44987352663`; native worker lifecycle under real TUN traffic remains untested.
- [x] Static source review confirms Hev terminates UDP sessions when the SOCKS server returns a non-success response; runtime UDP drop/no-egress test remains required.
- [x] Verify the uploaded Hev 2.20.0 AAR hash, archive, JNI signatures, R8 rule, and all four ELF ABIs; add a checksum-pinned acquisition script and local AAR dependency (the pinned source build, API 24 target audit, and native dependency audit passed in CI run `44987352663`).
- [x] Inspect the upstream `GoBackend` service/TUN ownership implementation and select a controlled fork as the integration architecture; this is a source-review decision, not a completed integration.

#### Pinned WireGuard fork — verified source constraints

- The exact upstream source tag `1.0.20260102` resolves to commit `09b75c2bd37f749e2a8c85876394854113c74be7` in `WireGuard/wireguard-android`. The Android tunnel module is Apache-2.0 licensed; preserve its license and attribution in any maintained fork.
- `GoBackend` is declared `public final` and owns private static native methods. The pinned `jni.c` exports JNI symbols named `Java_com_wireguard_android_backend_GoBackend_*`; therefore a fork must preserve the Java class binary name `com.wireguard.android.backend.GoBackend` or deliberately update and test every JNI binding. Merely changing the Kotlin/Gradle namespace or copying the class under a new package is not a safe bridge.
- The same native build also consumes `ANDROID_PACKAGE_NAME`: `CMakeLists.txt` passes it to `wg-quick`, while the Go build derives its WireGuard runtime socket directory from that package name. The fork must set this to the actual ZEUS application ID for each build variant and verify the resulting cache path; it must not accidentally inherit the upstream app ID.
- `GoBackend.setStateInternal()` currently creates the `VpnService.Builder`, applies included/excluded applications, addresses, DNS, search domains, AllowedIPs routes, family kill-switch behavior, MTU, metered state, and blocking mode, then establishes and detaches the TUN descriptor directly into `wgTurnOn()`. It subsequently protects the WireGuard sockets. A shared host refactor must preserve these semantics for WireGuard and must not hand the same descriptor to two engines.
- The upstream `GoBackend.VpnService` is the service declared by the tunnel AAR manifest. Its `onCreate()`/`onStartCommand()` complete the static service future; `onDestroy()` shuts down the active WireGuard handle, notifies the tunnel, and resets that future (with the Android 12+ incomplete-future path). The controlled fork must move these responsibilities to the one ZEUS host and keep Always-on startup, lockdown inspection, and teardown attached to that same service.
- Integration must remove the upstream tunnel AAR from the app dependency graph before packaging the fork, avoiding duplicate `com.wireguard.android.backend` classes and duplicate VPN-service declarations. The merged release/debug manifests must be inspected to prove exactly one `android.net.VpnService` service is declared.
- Required fork regression gates: native JNI load and all WireGuard native calls; application-ID-specific runtime directory; Always-on cold start; VPN consent and lockdown status; service recreation and future reset; split-tunnel allow/disallow policy; route/DNS/MTU/kill-switch parity; socket protection; one-owner TUN handoff; and failure/cancellation during WireGuard↔Tor switching. No Tor engine may start until the current owner has stopped and released the TUN with confirmed completion.
- Source references: [pinned GoBackend.java](https://github.com/WireGuard/wireguard-android/blob/1.0.20260102/tunnel/src/main/java/com/wireguard/android/backend/GoBackend.java), [pinned JNI bridge](https://github.com/WireGuard/wireguard-android/blob/1.0.20260102/tunnel/tools/libwg-go/jni.c), [pinned CMake configuration](https://github.com/WireGuard/wireguard-android/blob/1.0.20260102/tunnel/tools/CMakeLists.txt), and [pinned tunnel manifest](https://github.com/WireGuard/wireguard-android/blob/1.0.20260102/tunnel/src/main/AndroidManifest.xml).
- [ ] Create a maintained internal fork/module of the pinned WireGuard backend; replace its hard-coded service start target with the single ZEUS VPN host and expose a serialized TUN handoff without changing existing WireGuard routing semantics.
- [ ] Verify the merged manifest contains exactly one VPN service and that Always-on starts the same ZEUS host.
- [ ] Integrate one TUN owner with the existing Android VPN consent and foreground-service lifecycle.
- [ ] Exclude the ZEUS app package from the Tor TUN and verify Tor bootstrap works while Android lockdown is enabled; verify other apps remain inside the TUN.
- [ ] Test TCP forwarding, MapDNS/remote hostname resolution, UDP ASSOCIATE rejection with no egress, IPv4/IPv6 leak resistance, adapter crash, cancellation, network handover, and Android lockdown behavior.

### Gate C — transport coordinator and UI
- Add WireGuard / Tor selection, persisted preference, truthful state, and serialized switching only after the shared VPN host and TUN ownership contract are implemented.
- Preserve current WireGuard split-tunnel behavior. Add Tor-specific per-app policy only after the data plane is proven.
- Verify that switching never leaves two active VPN owners and that failure in Tor mode cannot fall back to direct traffic.

### Gate D — release verification
- Unit tests, Android build, instrumentation tests, emulator tests, and physical-device tests.
- Independent external IP and DNS checks, IPv4/IPv6 leak checks, TCP/UDP behavior checks, network handover, process death, and battery/background behavior.
- Keep the PR Draft until all applicable checks pass. No deployment or server-side changes are part of this work.

## Superseded decisions

- The earlier plan to keep WireGuard as the default Android transport is cancelled.
- The earlier controlled-fork approach for `GoBackend` is cancelled. ZEUS will own its VPN service directly.
- The earlier optional WireGuard/Tor transport selector is cancelled. Tor is the only Android VPN engine.

## Explicit non-goals

- Replacing or deleting WireGuard.
- Sending WireGuard server traffic through Tor.
- Claiming anonymity against global traffic correlation or a compromised endpoint.
- Supporting arbitrary UDP in Tor mode.
- Changing AWS, databases, VPN nodes, firewall rules, DNS, customer state, or WireGuard peers.


#### Hev 2.20.0 Android 7 / API 24 compatibility

- **Product decision: CONFIRMED.** ZEUS retains `minSdk = 24` (Android 7). Do not raise the app minimum to API 29 and do not use `tools:overrideLibrary`.
- **Root cause: CONFIRMED / HIGH / build blocker.** The official Hev 2.20.0 AAR declares `minSdkVersion=29`; upstream `Application.mk` sets `APP_PLATFORM := android-29`. The previous Android CI failed during manifest merge, before Android unit or PackageManager instrumentation tests ran.
- **Implementation added to the PR branch:** `scripts/build-hev-aar-api24.sh` checks out no code itself; CI checks out the exact upstream Hev source commit `75dbc1e9ace6c70d3d1b94df7c283efe1de7effc` with recursive submodules, installs Android NDK r27d (`27.2.12479018`), and invokes `ndk-build` with `APP_PLATFORM=android-24`. This overrides the upstream makefile setting at build invocation without editing upstream source.
- The script verifies the exact Hev source commit and submodule gitlinks, checks that all four ABI outputs exist, verifies that the verbose compiler log contains the API 24 target for each ABI, and requires `-Wl,--no-undefined` at link time. It also checks every emitted `DT_NEEDED` dependency against that ABI's Android API 24 NDK sysroot stubs before packaging the JNI binding and native libraries into a locally built AAR whose manifest declares API 24.
- Gradle now consumes `hev-socks5-tunnel-2.20.0-api24.aar`; the old fetch-script entry point delegates to the source build so CI cannot silently fall back to the official API 29 AAR.
- **Runtime compatibility gate added:** a native instrumentation test loads the JNI library and checks the idle-state JNI call. CI runs it on Android API 24, 28, and 29 emulators. This proves only that the library loads and the idle JNI call works on those emulator images; it does not prove TUN forwarding, SOCKS behavior, Tor integration, or real-device compatibility.
- **Status: SOURCE BUILD AND EMULATOR JNI LOAD VERIFIED IN CI.** CI run `44987352663` for commit `273f8530d3cc4225cbfd91bbb4f3d4f2ae9cd63e` completed all 9 jobs successfully. The API 24, 28, and 29 emulator jobs each reported `OK (1 test)` after building and installing the APKs; the Package Visibility instrumentation job also reported `OK (1 test)`. The Android debug build and all 74 unit tests passed (0 failures, 0 skipped). Go control-plane, VPN-node, Web Admin, and repository-structure jobs passed. This verifies the API 24 source build/audit, native library loading plus the idle JNI call, and the listed unit/instrumentation tests—not TUN packet forwarding, Tor daemon runtime, or physical-device compatibility. A physical Android 7 device test remains required before release.
- Android NDK guidance confirms that `APP_PLATFORM` selects the native API level and that the NDK linker resolves native references against the selected API's platform stubs. API 24 also enforces the public-NDK-API boundary for native libraries; private platform-library dependencies are not acceptable. See [Android NDK API-level guidance](https://developer.android.com/ndk/guides/common-problems) and [Android linker changes for NDK developers](https://android.googlesource.com/platform/bionic/+/d13cb58b3afda031fbe38f6dc8eaca28237c8a3b/android-changes-for-ndk-developers.md).
- **Remaining required gates:** inspect the generated AAR and merged APK contents; test on physical Android 7 / API 24 hardware; validate real Tor daemon startup/readiness and shutdown; implement the shared WireGuard/VpnService host and exclusive TUN ownership; then test Hev start/stop under load, TCP forwarding, MapDNS/remote resolution, UDP fail-closed behavior, IPv4/IPv6 leak resistance, lockdown, process death, and network handover. The Tor VPN data plane is still not implemented.
- CI run `44984866825` is the preceding failing baseline: Android build and Android Package Visibility failed at the API 29 AAR manifest merge; Go, Web Admin, VPN node, and repository structure jobs passed. Follow-up run `44987352663` supersedes that result and passed the API 24 source-build and API 24/28/29 emulator checks.
