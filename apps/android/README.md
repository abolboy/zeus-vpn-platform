# ZEUS Android Client

## Build

The default build intentionally has no public Control Plane endpoint configured. The app can be built for tests and profile import without connecting to a backend.

For an environment with a deployed HTTPS Control Plane:

```sh
./gradlew --no-daemon :apps:android:assembleDebug -PZEUS_API_BASE_URL=https://api.example.invalid
./gradlew --no-daemon :apps:android:testDebugUnitTest
```

Replace the example host with the actual HTTPS base URL. Plain HTTP is rejected by the enrollment client. The URL is compiled into the app; it is not an operator secret.

## Device enrollment and key handling

1. An owner/admin issues a 15-minute, one-time activation code for an active VPN user from the web admin.
2. The Android client generates a Curve25519 key pair using the official WireGuard Android library.
3. Only the public key, device reference, device name, platform, and activation code are sent to \`POST /v1/client/enroll\`. No operator bearer token is included.
4. The API returns node endpoint/public details and server-assigned IPv4/IPv6 addresses. The client builds the WireGuard profile locally.
5. The profile is validated and stored with AES-GCM using an Android Keystore key under \`noBackupFilesDir\`.

The private key is never sent to the Control Plane or the node agent. The in-flight enrollment record (activation code, private key, public key, device reference, and idempotency key) is separately encrypted so a lost response can be retried with the same device identity. After the profile is saved, that pending record and its Keystore key are deleted.

A retry with the same activation code, device reference, and public key recovers the existing device record. A different device identity cannot consume the same code.

## Runtime requirements

- The Control Plane must be reachable over HTTPS and have migration \`0005_client_enrollment_codes.sql\` applied.
- The VPN node must be online, mTLS-enrolled, configured with explicit client IPv4/IPv6 pools and interface addresses, and have a working outbound path to the Control Plane.
- The node executor remains dry-run by default. Enrollment may create a queued peer command, but the tunnel will not become active until a staging/production node actually applies that command and acknowledges success.
- The Android profile can reconnect without the Control Plane after a successful first provisioning because the profile is stored locally.

## Not a production approval

This client implementation and CI build do not mean the Control Plane is deployed or that the current AWS WireGuard host is enrolled. No production server, security group, live peer, or database was modified by this change.
