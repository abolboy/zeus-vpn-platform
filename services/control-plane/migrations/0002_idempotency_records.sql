-- Idempotent API mutation records. Rows expire after 24 hours and are pruned on key reuse.
CREATE TABLE IF NOT EXISTS idempotency_records (
    actor_operator_id UUID NOT NULL REFERENCES operators(id) ON DELETE CASCADE,
    route TEXT NOT NULL CHECK (length(route) BETWEEN 1 AND 256),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 16 AND 128),
    request_hash BYTEA NOT NULL CHECK (octet_length(request_hash) = 32),
    response_status SMALLINT,
    response_body JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (now() + interval '24 hours'),
    PRIMARY KEY (actor_operator_id, route, idempotency_key),
    CHECK ((response_status IS NULL) = (response_body IS NULL))
);
CREATE INDEX IF NOT EXISTS idempotency_records_expiry_idx
    ON idempotency_records (expires_at);
