-- Node-agent identity and durable, constrained desired-state command queue.
ALTER TABLE vpn_nodes
    ADD COLUMN IF NOT EXISTS agent_cert_sha256 BYTEA;

CREATE UNIQUE INDEX IF NOT EXISTS vpn_nodes_agent_cert_sha256_uidx
    ON vpn_nodes (agent_cert_sha256)
    WHERE agent_cert_sha256 IS NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'vpn_nodes_agent_cert_sha256_len_check'
    ) THEN
        ALTER TABLE vpn_nodes
            ADD CONSTRAINT vpn_nodes_agent_cert_sha256_len_check
            CHECK (agent_cert_sha256 IS NULL OR octet_length(agent_cert_sha256) = 32);
    END IF;
END
$$;

CREATE TABLE IF NOT EXISTS node_commands (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    node_id UUID NOT NULL REFERENCES vpn_nodes(id) ON DELETE RESTRICT,
    created_by_operator_id UUID REFERENCES operators(id) ON DELETE SET NULL,
    command_type TEXT NOT NULL CHECK (command_type IN ('peer.ensure', 'peer.remove')),
    payload JSONB NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    request_hash BYTEA NOT NULL CHECK (octet_length(request_hash) = 32),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 16 AND 128),
    status TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'leased', 'succeeded', 'failed', 'expired', 'cancelled')),
    attempts SMALLINT NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 10),
    lease_expires_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (now() + interval '10 minutes'),
    last_error TEXT CHECK (last_error IS NULL OR length(last_error) <= 500),
    result JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (node_id, idempotency_key),
    CHECK (expires_at > created_at),
    CHECK (status <> 'leased' OR lease_expires_at IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS node_commands_dispatch_idx
    ON node_commands (node_id, status, created_at)
    WHERE status IN ('queued', 'leased');

CREATE INDEX IF NOT EXISTS node_commands_expiry_idx
    ON node_commands (expires_at)
    WHERE status IN ('queued', 'leased');
