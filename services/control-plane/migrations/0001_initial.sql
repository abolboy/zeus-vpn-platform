-- ZEUS Control Plane — initial PostgreSQL schema.
-- Apply only after taking a database backup. This migration is additive and does not
-- modify the live WireGuard host or any existing tunnel configuration.
BEGIN;

CREATE TABLE IF NOT EXISTS operators (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    login TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'support', 'read_only')),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Store only SHA-256 hashes of high-entropy opaque session tokens, never raw tokens.
CREATE TABLE IF NOT EXISTS operator_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    operator_id UUID NOT NULL REFERENCES operators(id) ON DELETE CASCADE,
    token_hash BYTEA NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS operator_sessions_operator_expiry_idx
    ON operator_sessions (operator_id, expires_at);

CREATE TABLE IF NOT EXISTS vpn_users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    external_ref TEXT UNIQUE,
    display_name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'suspended', 'disabled')),
    max_devices SMALLINT NOT NULL DEFAULT 3 CHECK (max_devices BETWEEN 1 AND 20),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS vpn_users_status_created_idx
    ON vpn_users (status, created_at DESC);

CREATE TABLE IF NOT EXISTS vpn_nodes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    region TEXT NOT NULL,
    endpoint_host TEXT NOT NULL,
    endpoint_port INTEGER NOT NULL CHECK (endpoint_port BETWEEN 1 AND 65535),
    public_key TEXT NOT NULL,
    interface_name TEXT NOT NULL DEFAULT 'wg0',
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'online', 'degraded', 'offline', 'disabled')),
    last_seen_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS vpn_devices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES vpn_users(id) ON DELETE RESTRICT,
    device_ref TEXT NOT NULL,
    device_name TEXT NOT NULL,
    platform TEXT NOT NULL CHECK (platform IN ('android', 'ios', 'windows', 'macos', 'linux', 'other')),
    status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'revoked')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ,
    UNIQUE (user_id, device_ref)
);
CREATE INDEX IF NOT EXISTS vpn_devices_user_status_idx
    ON vpn_devices (user_id, status);

CREATE TABLE IF NOT EXISTS wireguard_peers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL UNIQUE REFERENCES vpn_devices(id) ON DELETE RESTRICT,
    node_id UUID NOT NULL REFERENCES vpn_nodes(id) ON DELETE RESTRICT,
    public_key TEXT NOT NULL UNIQUE,
    assigned_ipv4 INET NOT NULL UNIQUE,
    assigned_ipv6 INET UNIQUE,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'active', 'revoking', 'revoked', 'error')),
    last_handshake_at TIMESTAMPTZ,
    rx_bytes BIGINT NOT NULL DEFAULT 0 CHECK (rx_bytes >= 0),
    tx_bytes BIGINT NOT NULL DEFAULT 0 CHECK (tx_bytes >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS wireguard_peers_node_status_idx
    ON wireguard_peers (node_id, status);

CREATE TABLE IF NOT EXISTS audit_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_operator_id UUID REFERENCES operators(id) ON DELETE SET NULL,
    subject_user_id UUID REFERENCES vpn_users(id) ON DELETE SET NULL,
    action TEXT NOT NULL,
    outcome TEXT NOT NULL CHECK (outcome IN ('success', 'denied', 'failure')),
    request_id TEXT,
    source_ip INET,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS audit_events_created_idx ON audit_events (created_at DESC);
CREATE INDEX IF NOT EXISTS audit_events_subject_idx ON audit_events (subject_user_id, created_at DESC);
