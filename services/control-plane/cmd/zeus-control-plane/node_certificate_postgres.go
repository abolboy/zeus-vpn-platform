package main

import (
	"context"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) ListNodeAgentCertificates(ctx context.Context, nodeID string) ([]NodeAgentCertificate, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM vpn_nodes WHERE id = $1::uuid)", nodeID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNodeNotFound
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, node_id::text, encode(fingerprint_sha256, 'hex'),
		       not_before, not_after, revoked_at, created_at
		FROM vpn_node_agent_certificates
		WHERE node_id = $1::uuid
		ORDER BY created_at DESC, id DESC
	`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	certificates := make([]NodeAgentCertificate, 0)
	for rows.Next() {
		var certificate NodeAgentCertificate
		if err := rows.Scan(
			&certificate.ID, &certificate.NodeID, &certificate.FingerprintSHA256,
			&certificate.NotBefore, &certificate.NotAfter, &certificate.RevokedAt, &certificate.CreatedAt,
		); err != nil {
			return nil, err
		}
		certificates = append(certificates, certificate)
	}
	return certificates, rows.Err()
}

func (s *PostgresStore) RegisterNodeAgentCertificate(ctx context.Context, actorID, nodeID string, fingerprint []byte, notBefore, notAfter time.Time, requestID string, sourceIP *string) (NodeAgentCertificate, bool, error) {
	if len(fingerprint) != 32 || !notAfter.After(notBefore) {
		return NodeAgentCertificate{}, false, ErrNodeCertificateConflict
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return NodeAgentCertificate{}, false, err
	}
	defer tx.Rollback(ctx)

	var nodeExists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM vpn_nodes WHERE id = $1::uuid AND status <> 'disabled')
	`, nodeID).Scan(&nodeExists); err != nil {
		return NodeAgentCertificate{}, false, err
	}
	if !nodeExists {
		return NodeAgentCertificate{}, false, ErrNodeNotFound
	}

	var certificate NodeAgentCertificate
	err = tx.QueryRow(ctx, `
		INSERT INTO vpn_node_agent_certificates
			(node_id, fingerprint_sha256, not_before, not_after)
		VALUES ($1::uuid, $2, $3, $4)
		ON CONFLICT (fingerprint_sha256) DO NOTHING
		RETURNING id::text, node_id::text, encode(fingerprint_sha256, 'hex'),
		          not_before, not_after, revoked_at, created_at
	`, nodeID, fingerprint, notBefore, notAfter).Scan(
		&certificate.ID, &certificate.NodeID, &certificate.FingerprintSHA256,
		&certificate.NotBefore, &certificate.NotAfter, &certificate.RevokedAt, &certificate.CreatedAt,
	)
	replayed := false
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
			SELECT id::text, node_id::text, encode(fingerprint_sha256, 'hex'),
			       not_before, not_after, revoked_at, created_at
			FROM vpn_node_agent_certificates
			WHERE fingerprint_sha256 = $1
			FOR UPDATE
		`, fingerprint).Scan(
			&certificate.ID, &certificate.NodeID, &certificate.FingerprintSHA256,
			&certificate.NotBefore, &certificate.NotAfter, &certificate.RevokedAt, &certificate.CreatedAt,
		)
		if err != nil {
			return NodeAgentCertificate{}, false, err
		}
		if certificate.NodeID != nodeID || certificate.RevokedAt != nil ||
			!certificate.NotBefore.Equal(notBefore) || !certificate.NotAfter.Equal(notAfter) {
			return NodeAgentCertificate{}, false, ErrNodeCertificateConflict
		}
		replayed = true
	} else if err != nil {
		return NodeAgentCertificate{}, false, err
	}

	if !replayed {
		_, err = tx.Exec(ctx, `
			INSERT INTO audit_events
				(actor_operator_id, action, outcome, request_id, source_ip, metadata)
			VALUES ($1::uuid, 'node.certificate.register', 'success', $2, $3::inet,
			        jsonb_build_object('node_id', $4::text, 'fingerprint_sha256', $5::text))
		`, actorID, requestID, sourceIP, nodeID, hex.EncodeToString(fingerprint))
		if err != nil {
			return NodeAgentCertificate{}, false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return NodeAgentCertificate{}, false, err
	}
	return certificate, replayed, nil
}

func (s *PostgresStore) RevokeNodeAgentCertificate(ctx context.Context, actorID, nodeID, certificateID, requestID string, sourceIP *string) (NodeAgentCertificate, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return NodeAgentCertificate{}, false, err
	}
	defer tx.Rollback(ctx)

	var nodeExists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM vpn_nodes WHERE id = $1::uuid)", nodeID).Scan(&nodeExists); err != nil {
		return NodeAgentCertificate{}, false, err
	}
	if !nodeExists {
		return NodeAgentCertificate{}, false, ErrNodeNotFound
	}
	var certificate NodeAgentCertificate
	err = tx.QueryRow(ctx, `
		SELECT id::text, node_id::text, encode(fingerprint_sha256, 'hex'),
		       not_before, not_after, revoked_at, created_at
		FROM vpn_node_agent_certificates
		WHERE id = $1::uuid AND node_id = $2::uuid
		FOR UPDATE
	`, certificateID, nodeID).Scan(
		&certificate.ID, &certificate.NodeID, &certificate.FingerprintSHA256,
		&certificate.NotBefore, &certificate.NotAfter, &certificate.RevokedAt, &certificate.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return NodeAgentCertificate{}, false, ErrNodeCertificateNotFound
	}
	if err != nil {
		return NodeAgentCertificate{}, false, err
	}
	replayed := certificate.RevokedAt != nil
	if !replayed {
		if err := tx.QueryRow(ctx, `
			UPDATE vpn_node_agent_certificates
			SET revoked_at = now()
			WHERE id = $1::uuid AND node_id = $2::uuid
			RETURNING revoked_at
		`, certificateID, nodeID).Scan(&certificate.RevokedAt); err != nil {
			return NodeAgentCertificate{}, false, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO audit_events
				(actor_operator_id, action, outcome, request_id, source_ip, metadata)
			VALUES ($1::uuid, 'node.certificate.revoke', 'success', $2, $3::inet,
			        jsonb_build_object('node_id', $4::text, 'certificate_id', $5::text,
			                           'fingerprint_sha256', $6::text))
		`, actorID, requestID, sourceIP, nodeID, certificateID, certificate.FingerprintSHA256); err != nil {
			return NodeAgentCertificate{}, false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return NodeAgentCertificate{}, false, err
	}
	return certificate, replayed, nil
}
