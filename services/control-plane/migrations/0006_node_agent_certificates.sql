-- Multiple independently revocable node-agent client certificates.
-- Existing pinned fingerprints are carried forward; TLS still enforces each leaf's own validity.

CREATE TABLE IF NOT EXISTS vpn_node_agent_certificates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    node_id UUID NOT NULL REFERENCES vpn_nodes(id) ON DELETE RESTRICT,
    fingerprint_sha256 BYTEA NOT NULL UNIQUE
        CHECK (octet_length(fingerprint_sha256) = 32),
    not_before TIMESTAMPTZ NOT NULL,
    not_after TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (not_after > not_before)
);

CREATE INDEX IF NOT EXISTS vpn_node_agent_certificates_active_node_idx
    ON vpn_node_agent_certificates (node_id, not_after)
    WHERE revoked_at IS NULL;

INSERT INTO vpn_node_agent_certificates
    (node_id, fingerprint_sha256, not_before, not_after)
SELECT id, agent_cert_sha256, now() - interval '5 minutes', 'infinity'::timestamptz
FROM vpn_nodes
WHERE agent_cert_sha256 IS NOT NULL
ON CONFLICT (fingerprint_sha256) DO NOTHING;
