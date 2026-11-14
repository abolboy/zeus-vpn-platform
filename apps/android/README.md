# ZEUS Android Client

## Build

The default build intentionally leaves the Control Plane and OIDC provider endpoints unset. Use these properties only for a staging or production environment that has a deployed HTTPS Control Plane and an OIDC identity provider:

```sh
./gradlew --no-daemon :apps:android:assembleDebug \
  -PZEUS_API_BASE_URL=https://api.example.invalid \
  -PZEUS_OIDC_ISSUER=https://identity.example.invalid \
  -PZEUS_OIDC_CLIENT_ID=zeus-android-public
./gradlew --no-daemon :apps:android:testDebugUnitTest
```

The redirect URI defaults to com.zeusvpn.app:/oauth2redirect and must be registered exactly as a public native-app redirect. Use Authorization Code + PKCE. Never embed a client secret in the APK. The identity provider must issue verified email claims and allow self-sign-up if public registration is desired.

## Device enrollment and key handling

1. Preferred: the user signs in or signs up through the configured OIDC provider in the system browser using Authorization Code + PKCE. Existing owner/admin one-time activation codes remain a fallback.
2. The Android client generates a Curve25519 key pair using the official WireGuard Android library.
3. In self-service mode, the client sends a short-lived OIDC ID token as a Bearer token plus the public key and device metadata. The Control Plane verifies signature, issuer, audience, expiry, and verified email before binding the account to the provider subject. Invitation mode sends the one-time activation code instead.
4. The API returns node endpoint/public details and server-assigned IPv4/IPv6 addresses. The client builds the WireGuard profile locally.
5. The profile and OIDC AuthState are stored encrypted with AES-GCM using separate Android Keystore keys under noBackupFilesDir.

The private key is never sent to the Control Plane or the node agent. AppAuth manages the browser authorization and PKCE flow; its AuthState is encrypted under a separate Android Keystore AES-GCM key. The in-flight enrollment record (private key, public key, device reference, and idempotency key) is separately encrypted so a lost response can be retried with the same device identity.

## Runtime requirements

- The Control Plane must be reachable over HTTPS and have migration 0007_customer_self_service.sql applied.
- ZEUS_OIDC_ISSUER and ZEUS_OIDC_CLIENT_ID must be configured on the Control Plane and on the Android build with matching values.
- The VPN node must be online, mTLS-enrolled, configured with explicit client IPv4/IPv6 pools and interface addresses, and have a working outbound path to the Control Plane.
- The node executor remains dry-run by default. A device receives a queued peer command, but the tunnel will not become active until a staging/production node actually applies that command and acknowledges success.
- The Android profile can reconnect without the Control Plane after a successful first provisioning because the profile is stored locally.

## Not a production approval

This client implementation and CI build do not mean the Control Plane is deployed or that the current AWS WireGuard host is enrolled. No production server, security group, live peer, or database was modified by this change.
