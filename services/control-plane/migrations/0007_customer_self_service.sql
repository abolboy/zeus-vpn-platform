-- Public customer device enrollment is bound to a verified external OIDC subject.
-- Idempotency is customer-scoped and deliberately independent of operator sessions.
CREATE TABLE IF NOT EXISTS client_device_idempotency_records (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES vpn_users(id) ON DELETE CASCADE,
    route TEXT NOT NULL,
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 16 AND 128),
    request_hash BYTEA NOT NULL CHECK (octet_length(request_hash) = 32),
    response_body JSONB,
    response_status SMALLINT,
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (now() + interval '24 hours'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, route, idempotency_key),
    CHECK ((response_body IS NULL) = (response_status IS NULL))
);

CREATE INDEX IF NOT EXISTS client_device_idempotency_expiry_idx
    ON client_device_idempotency_records (expires_at);
