package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

func OpenPostgres(ctx context.Context, dsn string, production bool) (*PostgresStore, error) {
	if dsn == "" {
		return nil, errors.New("database URL is required")
	}
	if production {
		parsed, err := url.Parse(dsn)
		if err != nil {
			return nil, errors.New("database URL is invalid")
		}
		sslMode := parsed.Query().Get("sslmode")
		if sslMode != "verify-full" {
			return nil, errors.New("production database connections require sslmode=verify-full")
		}
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database configuration: %w", err)
	}
	config.MaxConns = 10
	config.MinConns = 1
	config.MaxConnLifetime = time.Hour
	config.MaxConnIdleTime = 15 * time.Minute
	config.HealthCheckPeriod = 30 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	return &PostgresStore{pool: pool}, nil
}

func (s *PostgresStore) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *PostgresStore) Ping(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return errors.New("database is not configured")
	}
	if err := s.pool.Ping(ctx); err != nil {
		return err
	}
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM information_schema.tables
		WHERE table_schema = 'public'
		  AND table_name = ANY($1::text[])
	`, []string{"operators", "operator_sessions", "vpn_users", "vpn_nodes", "vpn_devices", "wireguard_peers", "audit_events", "idempotency_records", "node_commands"}).Scan(&count)
	if err != nil {
		return err
	}
	if count != 9 {
		return errors.New("database schema is not migrated")
	}
	return nil
}

func (s *PostgresStore) HasActiveOwner(ctx context.Context) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM operators WHERE role = 'owner' AND status = 'active')").Scan(&exists)
	return exists, err
}

func (s *PostgresStore) FindOperatorByLogin(ctx context.Context, login string) (Operator, error) {
	var operator Operator
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, login, role, status, password_hash
		FROM operators
		WHERE login = $1
	`, login).Scan(&operator.ID, &operator.Login, &operator.Role, &operator.Status, &operator.PasswordHash)
	return operator, err
}

func (s *PostgresStore) EnsureBootstrapOperator(ctx context.Context, login, password string) error {
	if !validPasswordForBootstrap(password) {
		return errors.New("bootstrap password must contain 14 to 1024 bytes")
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM operators WHERE login = $1)", login).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO operators (login, password_hash, role, status)
		VALUES ($1, $2, 'owner', 'active')
		ON CONFLICT (login) DO NOTHING
	`, login, hash)
	return err
}

func (s *PostgresStore) CreateSession(ctx context.Context, operatorID string, tokenHash []byte, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO operator_sessions (operator_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
	`, operatorID, tokenHash, expiresAt)
	return err
}

func (s *PostgresStore) FindOperatorForSession(ctx context.Context, tokenHash []byte) (Operator, error) {
	var operator Operator
	err := s.pool.QueryRow(ctx, `
		SELECT o.id::text, o.login, o.role, o.status
		FROM operator_sessions s
		JOIN operators o ON o.id = s.operator_id
		WHERE s.token_hash = $1
		  AND s.expires_at > now()
		  AND s.revoked_at IS NULL
		  AND o.status = 'active'
	`, tokenHash).Scan(&operator.ID, &operator.Login, &operator.Role, &operator.Status)
	return operator, err
}

func (s *PostgresStore) RevokeSession(ctx context.Context, tokenHash []byte) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE operator_sessions
		SET revoked_at = COALESCE(revoked_at, now())
		WHERE token_hash = $1
	`, tokenHash)
	return err
}

func (s *PostgresStore) ListUsers(ctx context.Context, limit int, cursor *UserCursor) ([]VPNUser, error) {
	var cursorTime any
	var cursorID any
	if cursor != nil {
		cursorTime = cursor.CreatedAt
		cursorID = cursor.ID
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, display_name, status, max_devices, created_at
		FROM vpn_users
		WHERE $1::timestamptz IS NULL
		   OR (created_at, id) < ($1::timestamptz, $2::uuid)
		ORDER BY created_at DESC, id DESC
		LIMIT $3
	`, cursorTime, cursorID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]VPNUser, 0, limit)
	for rows.Next() {
		var user VPNUser
		if err := rows.Scan(&user.ID, &user.DisplayName, &user.Status, &user.MaxDevices, &user.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (s *PostgresStore) GetVPNUser(ctx context.Context, userID string) (VPNUser, error) {
	var user VPNUser
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, display_name, status, max_devices, created_at
		FROM vpn_users WHERE id = $1::uuid
	`, userID).Scan(&user.ID, &user.DisplayName, &user.Status, &user.MaxDevices, &user.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return VPNUser{}, ErrVPNUserNotFound
	}
	return user, err
}

func (s *PostgresStore) CreateVPNUser(ctx context.Context, actorID, route, key string, requestHash []byte, input CreateUserInput, requestID string, sourceIP *string) (VPNUser, bool, error) {
	return s.runIdempotentUserMutation(ctx, actorID, route, key, requestHash, "user.create", requestID, sourceIP, func(tx pgx.Tx) (VPNUser, error) {
		var user VPNUser
		err := tx.QueryRow(ctx, `
			INSERT INTO vpn_users (display_name, max_devices)
			VALUES ($1, $2::smallint)
			RETURNING id::text, display_name, status, max_devices, created_at
		`, input.DisplayName, int16(input.MaxDevices)).Scan(&user.ID, &user.DisplayName, &user.Status, &user.MaxDevices, &user.CreatedAt)
		return user, err
	})
}

func (s *PostgresStore) UpdateVPNUser(ctx context.Context, actorID, userID, route, key string, requestHash []byte, input UpdateUserInput, requestID string, sourceIP *string) (VPNUser, bool, error) {
	return s.runIdempotentUserMutation(ctx, actorID, route, key, requestHash, "user.update", requestID, sourceIP, func(tx pgx.Tx) (VPNUser, error) {
		var displayName any
		var status any
		var maxDevices any
		if input.DisplayName != nil {
			displayName = *input.DisplayName
		}
		if input.Status != nil {
			status = *input.Status
		}
		if input.MaxDevices != nil {
			maxDevices = int16(*input.MaxDevices)
		}
		var user VPNUser
		err := tx.QueryRow(ctx, `
			UPDATE vpn_users
			SET display_name = COALESCE($1::text, display_name),
			    status = COALESCE($2::text, status),
			    max_devices = COALESCE($3::smallint, max_devices),
			    updated_at = now()
			WHERE id = $4::uuid
			RETURNING id::text, display_name, status, max_devices, created_at
		`, displayName, status, maxDevices, userID).Scan(&user.ID, &user.DisplayName, &user.Status, &user.MaxDevices, &user.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return VPNUser{}, ErrVPNUserNotFound
		}
		return user, err
	})
}

func (s *PostgresStore) runIdempotentUserMutation(ctx context.Context, actorID, route, key string, requestHash []byte, action, requestID string, sourceIP *string, mutate func(pgx.Tx) (VPNUser, error)) (VPNUser, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return VPNUser{}, false, err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		DELETE FROM idempotency_records
		WHERE actor_operator_id = $1::uuid AND expires_at <= now()
	`, actorID)
	if err != nil {
		return VPNUser{}, false, err
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
			return VPNUser{}, false, err
		}
		if !bytes.Equal(existingHash, requestHash) {
			return VPNUser{}, false, ErrIdempotencyConflict
		}
		if len(responseBody) == 0 || responseStatus == nil {
			return VPNUser{}, false, errors.New("idempotency record is incomplete")
		}
		var user VPNUser
		if err := json.Unmarshal(responseBody, &user); err != nil {
			return VPNUser{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return VPNUser{}, false, err
		}
		return user, true, nil
	}
	if err != nil {
		return VPNUser{}, false, err
	}

	user, err := mutate(tx)
	if err != nil {
		return VPNUser{}, false, err
	}
	body, err := json.Marshal(user)
	if err != nil {
		return VPNUser{}, false, err
	}
	_, err = tx.Exec(ctx, `
		UPDATE idempotency_records
		SET response_status = CASE WHEN $4 = 'user.create' THEN 201 ELSE 200 END,
		    response_body = $5::jsonb
		WHERE actor_operator_id = $1::uuid AND route = $2 AND idempotency_key = $3
	`, actorID, route, key, action, string(body))
	if err != nil {
		return VPNUser{}, false, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_events
			(actor_operator_id, subject_user_id, action, outcome, request_id, source_ip, metadata)
		VALUES ($1::uuid, $2::uuid, $3, 'success', $4, $5::inet, '{}'::jsonb)
	`, actorID, user.ID, action, requestID, sourceIP)
	if err != nil {
		return VPNUser{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return VPNUser{}, false, err
	}
	return user, false, nil
}

func (s *PostgresStore) RecordAudit(ctx context.Context, actorID, subjectID *string, action, outcome, requestID string, sourceIP *string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO audit_events
			(actor_operator_id, subject_user_id, action, outcome, request_id, source_ip, metadata)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6::inet, '{}'::jsonb)
	`, actorID, subjectID, action, outcome, requestID, sourceIP)
	return err
}

func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}


func (s *PostgresStore) FindNodeByCertFingerprint(ctx context.Context, fingerprint []byte) (NodeIdentity, error) {
	if len(fingerprint) != 32 {
		return NodeIdentity{}, ErrNodeNotFound
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return NodeIdentity{}, err
	}
	defer tx.Rollback(ctx)

	var node NodeIdentity
	err = tx.QueryRow(ctx, `
		SELECT id::text
		FROM vpn_nodes
		WHERE agent_cert_sha256 = $1
		  AND status <> 'disabled'
		FOR UPDATE
	`, fingerprint).Scan(&node.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return NodeIdentity{}, ErrNodeNotFound
	}
	if err != nil {
		return NodeIdentity{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE vpn_nodes
		SET status = 'online', last_seen_at = now(), updated_at = now()
		WHERE id = $1::uuid
	`, node.ID); err != nil {
		return NodeIdentity{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return NodeIdentity{}, err
	}
	return node, nil
}

func (s *PostgresStore) LeaseNodeCommand(ctx context.Context, nodeID string, leaseDuration time.Duration) (NodeCommand, error) {
	if leaseDuration < 5*time.Second || leaseDuration > 2*time.Minute {
		return NodeCommand{}, errors.New("node command lease duration is out of range")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return NodeCommand{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE node_commands
		SET status = 'expired', lease_expires_at = NULL, completed_at = COALESCE(completed_at, now())
		WHERE node_id = $1::uuid
		  AND status IN ('queued', 'leased')
		  AND (expires_at <= now() OR attempts >= 10)
	`, nodeID); err != nil {
		return NodeCommand{}, err
	}

	var command NodeCommand
	err = tx.QueryRow(ctx, `
		SELECT id::text, node_id::text, command_type, payload, attempts + 1, expires_at
		FROM node_commands
		WHERE node_id = $1::uuid
		  AND status IN ('queued', 'leased')
		  AND expires_at > now()
		  AND attempts < 10
		  AND (status = 'queued' OR lease_expires_at <= now())
		ORDER BY created_at ASC, id ASC
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`, nodeID).Scan(&command.ID, &command.NodeID, &command.Type, &command.Payload, &command.Attempt, &command.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return NodeCommand{}, err
		}
		return NodeCommand{}, ErrNoNodeCommand
	}
	if err != nil {
		return NodeCommand{}, err
	}
	leaseUntil := time.Now().UTC().Add(leaseDuration)
	if _, err := tx.Exec(ctx, `
		UPDATE node_commands
		SET status = 'leased',
		    attempts = attempts + 1,
		    lease_expires_at = LEAST($2::timestamptz, expires_at),
		    started_at = COALESCE(started_at, now())
		WHERE id = $1::uuid
	`, command.ID, leaseUntil); err != nil {
		return NodeCommand{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return NodeCommand{}, err
	}
	return command, nil
}

func (s *PostgresStore) AcknowledgeNodeCommand(ctx context.Context, nodeID, commandID, status, errorCode string) error {
	if status != "succeeded" && status != "failed" {
		return errors.New("invalid node command acknowledgement status")
	}
	if (status == "succeeded" && errorCode != "") || len(errorCode) > 64 {
		return errors.New("invalid node command acknowledgement error code")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var currentStatus string
	var currentError *string
	var leaseExpiresAt *time.Time
	var expiresAt time.Time
	err = tx.QueryRow(ctx, `
		SELECT status, last_error, lease_expires_at, expires_at
		FROM node_commands
		WHERE id = $1::uuid AND node_id = $2::uuid
		FOR UPDATE
	`, commandID, nodeID).Scan(&currentStatus, &currentError, &leaseExpiresAt, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNodeCommandNotFound
	}
	if err != nil {
		return err
	}

	if currentStatus == status {
		currentCode := ""
		if currentError != nil {
			currentCode = *currentError
		}
		if currentCode == errorCode {
			return tx.Commit(ctx)
		}
		return ErrNodeCommandConflict
	}
	if currentStatus != "leased" {
		return ErrNodeCommandConflict
	}
	now := time.Now().UTC()
	if !expiresAt.After(now) {
		if _, err := tx.Exec(ctx, `
			UPDATE node_commands
			SET status = 'expired', lease_expires_at = NULL, completed_at = COALESCE(completed_at, now())
			WHERE id = $1::uuid
		`, commandID); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return ErrNodeCommandLeaseExpired
	}
	if leaseExpiresAt == nil || !leaseExpiresAt.After(now) {
		return ErrNodeCommandLeaseExpired
	}

	result, err := json.Marshal(map[string]string{"status": status, "error_code": errorCode})
	if err != nil {
		return err
	}
	var lastError any
	if errorCode != "" {
		lastError = errorCode
	}
	if _, err := tx.Exec(ctx, `
		UPDATE node_commands
		SET status = $2,
		    last_error = $3,
		    result = $4::jsonb,
		    lease_expires_at = NULL,
		    completed_at = now()
		WHERE id = $1::uuid
	`, commandID, status, lastError, string(result)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
