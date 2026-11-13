-- Device lifecycle and explicit WireGuard node address pools.
ALTER TABLE vpn_nodes
    ADD COLUMN IF NOT EXISTS client_ipv4_pool CIDR,
    ADD COLUMN IF NOT EXISTS client_ipv6_pool CIDR,
    ADD COLUMN IF NOT EXISTS interface_ipv4 INET,
    ADD COLUMN IF NOT EXISTS interface_ipv6 INET;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'vpn_nodes_client_ipv4_pool_family_check') THEN
        ALTER TABLE vpn_nodes ADD CONSTRAINT vpn_nodes_client_ipv4_pool_family_check
            CHECK (client_ipv4_pool IS NULL OR family(client_ipv4_pool) = 4);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'vpn_nodes_client_ipv6_pool_family_check') THEN
        ALTER TABLE vpn_nodes ADD CONSTRAINT vpn_nodes_client_ipv6_pool_family_check
            CHECK (client_ipv6_pool IS NULL OR family(client_ipv6_pool) = 6);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'vpn_nodes_interface_ipv4_pool_check') THEN
        ALTER TABLE vpn_nodes ADD CONSTRAINT vpn_nodes_interface_ipv4_pool_check
            CHECK (interface_ipv4 IS NULL OR
                (family(interface_ipv4) = 4 AND client_ipv4_pool IS NOT NULL AND interface_ipv4 <<= client_ipv4_pool));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'vpn_nodes_interface_ipv6_pool_check') THEN
        ALTER TABLE vpn_nodes ADD CONSTRAINT vpn_nodes_interface_ipv6_pool_check
            CHECK (interface_ipv6 IS NULL OR
                (family(interface_ipv6) = 6 AND client_ipv6_pool IS NOT NULL AND interface_ipv6 <<= client_ipv6_pool));
    END IF;
END
$$;

-- Address pools are node-scoped; released addresses become reusable only after peer removal is acknowledged.
ALTER TABLE wireguard_peers
    DROP CONSTRAINT IF EXISTS wireguard_peers_assigned_ipv4_key,
    DROP CONSTRAINT IF EXISTS wireguard_peers_assigned_ipv6_key;

CREATE UNIQUE INDEX IF NOT EXISTS wireguard_peers_node_ipv4_reserved_uidx
    ON wireguard_peers (node_id, assigned_ipv4)
    WHERE status <> 'revoked';

CREATE UNIQUE INDEX IF NOT EXISTS wireguard_peers_node_ipv6_reserved_uidx
    ON wireguard_peers (node_id, assigned_ipv6)
    WHERE status <> 'revoked' AND assigned_ipv6 IS NOT NULL;

ALTER TABLE vpn_devices
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN IF NOT EXISTS last_error TEXT
        CHECK (last_error IS NULL OR length(last_error) <= 64);

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'vpn_devices_status_check') THEN
        ALTER TABLE vpn_devices DROP CONSTRAINT vpn_devices_status_check;
    END IF;
    ALTER TABLE vpn_devices ADD CONSTRAINT vpn_devices_status_check
        CHECK (status IN ('provisioning', 'active', 'revoking', 'error', 'revoked'));
END
$$;

CREATE INDEX IF NOT EXISTS vpn_devices_user_active_idx
    ON vpn_devices (user_id, created_at DESC)
    WHERE status IN ('provisioning', 'active', 'revoking', 'error');

CREATE INDEX IF NOT EXISTS vpn_nodes_provisioning_idx
    ON vpn_nodes (status, last_seen_at DESC)
    WHERE agent_cert_sha256 IS NOT NULL
      AND client_ipv4_pool IS NOT NULL
      AND client_ipv6_pool IS NOT NULL
      AND interface_ipv4 IS NOT NULL
      AND interface_ipv6 IS NOT NULL;
