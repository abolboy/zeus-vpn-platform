# Tor-only Android client: API contract gap

**Status:** Read-only API review; no API, database, or production changes made.  
**Reviewed:** 2026-11-22  
**Branch:** `feat/android-tor-transport-pr27`  
**Related:** PR #28

## Finding

The current Android enrollment flow is not transport-neutral. It creates and persists a WireGuard key pair, sends the WireGuard public key to the control plane, waits for a WireGuard peer to become active, and builds a WireGuard profile from the returned peer fields. Removing the Android WireGuard engine without replacing this enrollment contract would break device activation.

This is a confirmed architecture dependency, not a runtime defect.

## Evidence inspected

### Android client

- `apps/android/src/main/kotlin/com/zeusvpn/app/network/ClientEnrollmentRepository.kt`
  - `createPendingEnrollment()` creates `com.wireguard.crypto.KeyPair`.
  - The pending record persists `public_key` and `private_key` with the device reference, activation code, and idempotency key.
  - `postEnrollment()` sends `device_ref`, `device_name`, `platform`, and `public_key` to `POST /v1/client/enroll`.
  - `enroll()` waits for both device and peer status to become active, then calls `WireGuardProfileBuilder.build()` using peer addresses, server public key, endpoint host, and endpoint port.
- `apps/android/src/main/kotlin/com/zeusvpn/app/network/CustomerIdentityRepository.kt`
  - OIDC authorization uses the customer identity provider and stores the AuthState through `EncryptedWireGuardConfigStore`; identity login itself is not the WireGuard transport.
- `apps/android/src/main/kotlin/com/zeusvpn/app/ui/ZeusDashboard.kt`
  - The current dashboard creates and uses `WireGuardEngine`, checks for a saved WireGuard profile, and starts/stops the WireGuard backend.

### Control plane

- `services/control-plane/cmd/zeus-control-plane/server.go`
  - Registers `POST /v1/client/enroll`, `GET /v1/client/devices/{deviceID}`, and `POST /v1/client/devices/{deviceID}/retry`.
  - `handleClientEnrollment()` routes authenticated requests to `handleCustomerClientEnrollment()`.
  - Both activation-code and customer-OIDC enrollment paths validate `public_key` using `validWireGuardPublicKey()`.
  - Enrollment calls `EnrollVPNDevice()` or `EnrollCustomerVPNDevice()`, which are part of the current VPN peer-provisioning flow.
- `services/control-plane/cmd/zeus-control-plane/customer_oidc.go`
  - Customer identity is verified as an OIDC ID token; the issuer and subject are used as the external customer reference.
  - Customer device retry is explicitly a retry of VPN peer provisioning and returns peer/device provisioning state.

## Existing contract (observed)

| Operation | Contract purpose | Transport coupling |
|---|---|---|
| `POST /v1/client/enroll` | Enroll a device with activation code or verified customer identity | Requires a WireGuard public key; schedules peer provisioning |
| `GET /v1/client/devices/{deviceID}` | Poll device activation state | Response includes peer state; Android waits for peer active |
| `POST /v1/client/devices/{deviceID}/retry` | Retry failed device provisioning | Retries WireGuard peer provisioning |

The current Android client also requires the enrollment response to contain a `peer` object with `assigned_ipv4`, `assigned_ipv6`, `server_public_key`, `endpoint_host`, and `endpoint_port`.

## Required design boundary for Tor

The Tor-only Android transport must not fabricate a WireGuard key, request a WireGuard peer, or wait for WireGuard peer state merely to establish a Tor circuit.

Before changing the enrollment implementation, decide which product contract applies:

1. **Accountless Tor transport:** Tor connectivity is independent of customer enrollment. OIDC and device registration remain optional account features; no control-plane schema change is required for the transport itself.
2. **Account-bound Tor transport:** Introduce a transport-neutral device registration/entitlement contract that records device identity and authorization without requiring a WireGuard key or peer. The existing WireGuard peer provisioning path remains available to server-side/legacy clients but is not called by the Tor Android client.

If option 2 is selected, design a new versioned contract first. It must define:
- transport identifier (for example, `tor`) and supported client/platform versions;
- authentication using the existing verified customer OIDC identity or one-time activation code;
- device ownership and device-limit enforcement;
- idempotent create/retry semantics;
- stable device lifecycle states independent of peer lifecycle;
- entitlement/expiry/revocation semantics;
- bounded request and response sizes, rate limits, safe error envelopes, and audit events;
- tests for unauthenticated access, cross-account device access, duplicate idempotency keys, expired activation codes, revoked devices, and retry behavior.

Do not repurpose the existing peer-provisioning endpoints or silently make the `public_key` optional. Do not add a database migration or deploy an API change until the contract is reviewed and explicitly approved.

## Security and compatibility notes

- Preserve OIDC issuer, audience/client-ID, signature, expiry, and verified-email checks. A client-provided device owner or account ID must never be trusted.
- Scope every device lookup and mutation to the authenticated account or the hash of the one-time activation credential; test BOLA/IDOR boundaries.
- Do not store Tor circuit secrets or expose SOCKS/control ports through the control plane.
- Server-side WireGuard node agents, peers, and production infrastructure remain unchanged by the Android Tor migration unless separately authorized.

## QA acceptance criteria

- A Tor-only Android enrollment path does not generate or persist WireGuard private keys.
- A Tor-only Android enrollment path does not call Wireguard profile builders or require a `peer` response.
- Account/device lifecycle tests prove ownership isolation, idempotency, activation expiry, revocation, and bounded retries.
- The Tor VPN connection can be started and stopped independently of account enrollment if accountless transport is selected.
- No client status says VPN connected until the TUN is established, Hev is active, and an independent end-to-end traffic check passes.
