package main

import (
	"context"
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
	`, []string{"operators", "operator_sessions", "vpn_users", "vpn_nodes", "vpn_devices", "wireguard_peers", "audit_events"}).Scan(&count)
	if err != nil {
		return err
	}
	if count != 7 {
		return errors.New("database schema is not migrated")
	}
	return nil
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
