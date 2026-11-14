-- One-time, short-lived client enrollment codes.
-- Only a SHA-256 hash is stored; the raw code is returned once to the administrator.
CREATE TABLE IF NOT EXISTS vpn_user_enrollment_codes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES vpn_users(id) ON DELETE RESTRICT,
    created_by_operator_id UUID NOT NULL REFERENCES operators(id) ON DELETE RESTRICT,
    token_hash BYTEA NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    device_id UUID REFERENCES vpn_devices(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (consumed_at IS NULL OR revoked_at IS NULL),
    CHECK (expires_at > created_at),
    CHECK (consumed_at IS NULL OR device_id IS NOT NULL)
);

-- At most one unused, non-revoked code per user. Issuing a new one revokes the old code.
CREATE UNIQUE INDEX IF NOT EXISTS vpn_user_enrollment_codes_one_active_uidx
    ON vpn_user_enrollment_codes (user_id)
    WHERE consumed_at IS NULL AND revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS vpn_user_enrollment_codes_expiry_idx
    ON vpn_user_enrollment_codes (expires_at)
    WHERE consumed_at IS NULL AND revoked_at IS NULL;
