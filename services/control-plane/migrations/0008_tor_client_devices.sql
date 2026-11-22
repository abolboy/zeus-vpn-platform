-- Account-bound Tor client devices are separate from WireGuard devices and peers.
-- The migration is additive; it does not modify existing WireGuard resources.
CREATE TABLE IF NOT EXISTS tor_client_devices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES vpn_users(id) ON DELETE RESTRICT,
    device_ref TEXT NOT NULL CHECK (length(device_ref) BETWEEN 16 AND 128),
    device_name TEXT NOT NULL CHECK (length(device_name) BETWEEN 1 AND 80),
    platform TEXT NOT NULL CHECK (platform = 'android'),
    client_version TEXT NOT NULL CHECK (length(client_version) BETWEEN 1 AND 64),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ,
    UNIQUE (user_id, device_ref),
    CHECK ((status = 'revoked') = (revoked_at IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS tor_client_devices_user_status_idx
    ON tor_client_devices (user_id, status);

-- Invitation secrets are high-entropy, one-time values. Only SHA-256 hashes are persisted.
CREATE TABLE IF NOT EXISTS tor_client_enrollment_codes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES vpn_users(id) ON DELETE RESTRICT,
    created_by_operator_id UUID NOT NULL REFERENCES operators(id) ON DELETE RESTRICT,
    token_hash BYTEA NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    device_id UUID REFERENCES tor_client_devices(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (consumed_at IS NULL OR revoked_at IS NULL),
    CHECK (expires_at > created_at),
    CHECK (consumed_at IS NULL OR device_id IS NOT NULL)
);
CREATE UNIQUE INDEX IF NOT EXISTS tor_client_enrollment_codes_one_active_uidx
    ON tor_client_enrollment_codes (user_id)
    WHERE consumed_at IS NULL AND revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS tor_client_enrollment_codes_expiry_idx
    ON tor_client_enrollment_codes (expires_at)
    WHERE consumed_at IS NULL AND revoked_at IS NULL;
