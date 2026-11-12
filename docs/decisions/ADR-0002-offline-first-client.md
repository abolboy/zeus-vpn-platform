# ADR-0002: Offline-first management client

- Status: Accepted for Phase 1
- Date: 2026-11-13

The Android client separates local availability, control-plane availability, and VPN tunnel state. Safe metadata may be cached locally; private keys must be protected with Android Keystore-backed storage. An already provisioned tunnel should not depend on a live control API for every packet or reconnect, where the underlying engine supports this.

Offline mode cannot create internet access when the device has no network path. Cached credentials need expiry and revocation semantics. Retry loops must be bounded and battery-aware. Only explicitly idempotent operations may be queued.
