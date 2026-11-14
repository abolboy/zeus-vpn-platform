package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (s *PostgresStore) CreateVPNNode(ctx context.Context, actorID, route, key string, requestHash []byte, input CreateVPNNodeInput, requestID string, sourceIP *string) (VPNNode, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return VPNNode{}, false, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		DELETE FROM idempotency_records
		WHERE actor_operator_id = $1::uuid AND expires_at <= now()
	`, actorID); err != nil {
		return VPNNode{}, false, err
	}
	var reserved bool
	err = tx.QueryRow(ctx, `
		INSERT INTO idempotency_records (actor_operator_id, route, idempotency_key, request_hash)
		VALUES ($1::uuid, $2, $3, $4)
		ON CONFLICT (actor_operator_id, route, idempotency_key) DO NOTHING
		RETURNING true
	`, actorID, route, key, requestHash).Scan(&reserved)
	if errors.Is(err, pgx.ErrNoRows) {
		var existingHash, responseBody []byte
		var responseStatus *int16
		err = tx.QueryRow(ctx, `
			SELECT request_hash, response_body, response_status
			FROM idempotency_records
			WHERE actor_operator_id = $1::uuid AND route = $2 AND idempotency_key = $3
		`, actorID, route, key).Scan(&existingHash, &responseBody, &responseStatus)
		if err != nil {
			return VPNNode{}, false, err
		}
		if !bytes.Equal(existingHash, requestHash) {
			return VPNNode{}, false, ErrIdempotencyConflict
		}
		if len(responseBody) == 0 || responseStatus == nil {
			return VPNNode{}, false, errors.New("node idempotency record is incomplete")
		}
		var node VPNNode
		if err := json.Unmarshal(responseBody, &node); err != nil {
			return VPNNode{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return VPNNode{}, false, err
		}
		return node, true, nil
	}
	if err != nil {
		return VPNNode{}, false, err
	}

	var node VPNNode
	err = tx.QueryRow(ctx, `
		INSERT INTO vpn_nodes (
			name, region, endpoint_host, endpoint_port, public_key, interface_name,
			status, client_ipv4_pool, client_ipv6_pool, interface_ipv4, interface_ipv6
		)
		VALUES ($1, $2, $3, $4, $5, $6, 'pending', $7::cidr, $8::cidr, $9::inet, $10::inet)
		RETURNING id::text, name, region, endpoint_host, endpoint_port, public_key,
		          interface_name, status, last_seen_at, client_ipv4_pool::text,
		          client_ipv6_pool::text, interface_ipv4::text, interface_ipv6::text,
		          0::int, created_at
	`, input.Name, input.Region, input.EndpointHost, input.EndpointPort, input.PublicKey,
		input.InterfaceName, input.ClientIPv4Pool, input.ClientIPv6Pool,
		input.InterfaceIPv4, input.InterfaceIPv6).Scan(
		&node.ID, &node.Name, &node.Region, &node.EndpointHost, &node.EndpointPort,
		&node.PublicKey, &node.InterfaceName, &node.Status, &node.LastSeenAt,
		&node.ClientIPv4Pool, &node.ClientIPv6Pool, &node.InterfaceIPv4, &node.InterfaceIPv6,
		&node.ActiveCertificateCount, &node.CreatedAt,
	)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return VPNNode{}, false, ErrNodeConflict
		}
		return VPNNode{}, false, err
	}

	body, err := json.Marshal(node)
	if err != nil {
		return VPNNode{}, false, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE idempotency_records
		SET response_status = 201, response_body = $4::jsonb
		WHERE actor_operator_id = $1::uuid AND route = $2 AND idempotency_key = $3
	`, actorID, route, key, string(body)); err != nil {
		return VPNNode{}, false, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events
			(actor_operator_id, action, outcome, request_id, source_ip, metadata)
		VALUES ($1::uuid, 'node.create', 'success', $2, $3::inet,
		        jsonb_build_object('node_id', $4::text, 'name', $5::text, 'region', $6::text))
	`, actorID, requestID, sourceIP, node.ID, node.Name, node.Region); err != nil {
		return VPNNode{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return VPNNode{}, false, err
	}
	return node, false, nil
}

var _ = time.Second
