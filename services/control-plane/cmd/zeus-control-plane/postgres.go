package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	`, []string{"operators", "operator_sessions", "vpn_users", "vpn_nodes", "vpn_devices", "wireguard_peers", "audit_events", "idempotency_records", "node_commands", "vpn_user_enrollment_codes", "vpn_node_agent_certificates", "client_device_idempotency_records"}).Scan(&count)
	if err != nil {
		return err
	}
	if count != 12 {
		return errors.New("database schema is not migrated")
	}
	var poolColumns int
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'vpn_nodes'
		  AND column_name IN ('client_ipv4_pool', 'client_ipv6_pool', 'interface_ipv4', 'interface_ipv6')
	`).Scan(&poolColumns); err != nil {
		return err
	}
	if poolColumns != 4 {
		return errors.New("device provisioning schema is not migrated")
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



type provisioningNode struct {
	id string
	ipv4Pool netip.Prefix
	ipv6Pool netip.Prefix
	interfaceIPv4 netip.Addr
	interfaceIPv6 netip.Addr
	serverPublicKey string
	endpointHost string
	endpointPort int
}

func getVPNDeviceTx(ctx context.Context, tx pgx.Tx, deviceID string) (VPNDevice, error) {
	var device VPNDevice
	err := tx.QueryRow(ctx, `
		SELECT d.id::text, d.user_id::text, d.device_ref, d.device_name, d.platform,
		       d.status, d.created_at, COALESCE(d.last_error, ''),
		       p.id::text, p.node_id::text, p.public_key, host(p.assigned_ipv4),
		       COALESCE(host(p.assigned_ipv6), ''), n.public_key, n.endpoint_host,
		       n.endpoint_port, p.status
		FROM vpn_devices d
		JOIN wireguard_peers p ON p.device_id = d.id
		JOIN vpn_nodes n ON n.id = p.node_id
		WHERE d.id = $1::uuid
	`, deviceID).Scan(
		&device.ID, &device.UserID, &device.DeviceRef, &device.DeviceName,
		&device.Platform, &device.Status, &device.CreatedAt, &device.LastError,
		&device.Peer.ID, &device.Peer.NodeID, &device.Peer.PublicKey,
		&device.Peer.AssignedIPv4, &device.Peer.AssignedIPv6,
		&device.Peer.ServerPublicKey, &device.Peer.EndpointHost,
		&device.Peer.EndpointPort, &device.Peer.Status,
	)
	return device, err
}

func (s *PostgresStore) GetCustomerVPNDevice(ctx context.Context, userID, deviceID string) (VPNDevice, error) {
	if !uuidPattern.MatchString(userID) || !uuidPattern.MatchString(deviceID) {
		return VPNDevice{}, ErrDeviceNotFound
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return VPNDevice{}, err
	}
	defer tx.Rollback(ctx)

	device, err := getVPNDeviceTx(ctx, tx, deviceID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && device.UserID != userID) {
		return VPNDevice{}, ErrDeviceNotFound
	}
	if err != nil {
		return VPNDevice{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return VPNDevice{}, err
	}
	return device, nil
}

func (s *PostgresStore) ListVPNDevices(ctx context.Context, userID string) ([]VPNDevice, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d.id::text, d.user_id::text, d.device_ref, d.device_name, d.platform,
		       d.status, d.created_at, COALESCE(d.last_error, ''),
		       p.id::text, p.node_id::text, p.public_key, host(p.assigned_ipv4),
		       COALESCE(host(p.assigned_ipv6), ''), n.public_key, n.endpoint_host,
		       n.endpoint_port, p.status
		FROM vpn_devices d
		JOIN wireguard_peers p ON p.device_id = d.id
		JOIN vpn_nodes n ON n.id = p.node_id
		WHERE d.user_id = $1::uuid
		ORDER BY d.created_at DESC, d.id DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	devices := make([]VPNDevice, 0)
	for rows.Next() {
		var device VPNDevice
		if err := rows.Scan(
			&device.ID, &device.UserID, &device.DeviceRef, &device.DeviceName,
			&device.Platform, &device.Status, &device.CreatedAt, &device.LastError,
			&device.Peer.ID, &device.Peer.NodeID, &device.Peer.PublicKey,
			&device.Peer.AssignedIPv4, &device.Peer.AssignedIPv6,
			&device.Peer.ServerPublicKey, &device.Peer.EndpointHost,
			&device.Peer.EndpointPort, &device.Peer.Status,
		); err != nil {
			return nil, err
		}
		devices = append(devices, device)
	}
	return devices, rows.Err()
}

func (s *PostgresStore) CreateVPNDevice(ctx context.Context, actorID, userID, route, key string, requestHash []byte, input CreateDeviceInput, requestID string, sourceIP *string) (VPNDevice, bool, error) {
	return s.runIdempotentDeviceMutation(ctx, actorID, route, key, requestHash, "device.create", requestID, sourceIP, func(tx pgx.Tx) (VPNDevice, error) {
		return s.createVPNDeviceTx(ctx, tx, actorID, userID, input)
	})
}

func (s *PostgresStore) EnsureExternalVPNUser(ctx context.Context, externalRef, displayName string) (VPNUser, error) {
	if externalRef == "" || len(externalRef) > 512 || displayName == "" || len(displayName) > 120 {
		return VPNUser{}, errors.New("invalid external customer identity")
	}
	var user VPNUser
	err := s.pool.QueryRow(ctx, `
		INSERT INTO vpn_users (external_ref, display_name, status, max_devices)
		VALUES ($1, $2, 'active', 3)
		ON CONFLICT (external_ref) DO UPDATE SET updated_at = now()
		RETURNING id::text, display_name, status, max_devices, created_at
	`, externalRef, displayName).Scan(&user.ID, &user.DisplayName, &user.Status, &user.MaxDevices, &user.CreatedAt)
	return user, err
}

func (s *PostgresStore) EnrollCustomerVPNDevice(
	ctx context.Context,
	userID, idempotencyKey string,
	requestHash []byte,
	input CreateDeviceInput,
	requestID string,
	sourceIP *string,
) (VPNDevice, bool, error) {
	if !uuidPattern.MatchString(userID) || !idempotencyKeyPattern.MatchString(idempotencyKey) || len(requestHash) != 32 {
		return VPNDevice{}, false, errors.New("invalid self-service device enrollment request")
	}
	route := "POST:/v1/client/enroll"
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return VPNDevice{}, false, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		DELETE FROM client_device_idempotency_records
		WHERE user_id = $1::uuid AND expires_at <= now()
	`, userID); err != nil {
		return VPNDevice{}, false, err
	}

	var reserved bool
	err = tx.QueryRow(ctx, `
		INSERT INTO client_device_idempotency_records
			(user_id, route, idempotency_key, request_hash)
		VALUES ($1::uuid, $2, $3, $4)
		ON CONFLICT (user_id, route, idempotency_key) DO NOTHING
		RETURNING true
	`, userID, route, idempotencyKey, requestHash).Scan(&reserved)
	if errors.Is(err, pgx.ErrNoRows) {
		var existingHash, responseBody []byte
		var responseStatus *int16
		err = tx.QueryRow(ctx, `
			SELECT request_hash, response_body, response_status
			FROM client_device_idempotency_records
			WHERE user_id = $1::uuid AND route = $2 AND idempotency_key = $3
		`, userID, route, idempotencyKey).Scan(&existingHash, &responseBody, &responseStatus)
		if err != nil {
			return VPNDevice{}, false, err
		}
		if !bytes.Equal(existingHash, requestHash) {
			return VPNDevice{}, false, ErrIdempotencyConflict
		}
		if len(responseBody) == 0 || responseStatus == nil {
			return VPNDevice{}, false, errors.New("customer idempotency record is incomplete")
		}
		var device VPNDevice
		if err := json.Unmarshal(responseBody, &device); err != nil {
			return VPNDevice{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return VPNDevice{}, false, err
		}
		return device, true, nil
	}
	if err != nil {
		return VPNDevice{}, false, err
	}

	device, err := s.createVPNDeviceTx(ctx, tx, "", userID, input)
	if err != nil {
		return VPNDevice{}, false, err
	}
	body, err := json.Marshal(device)
	if err != nil {
		return VPNDevice{}, false, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE client_device_idempotency_records
		SET response_status = 202, response_body = $4::jsonb
		WHERE user_id = $1::uuid AND route = $2 AND idempotency_key = $3
	`, userID, route, idempotencyKey, string(body)); err != nil {
		return VPNDevice{}, false, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events
			(actor_operator_id, subject_user_id, action, outcome, request_id, source_ip, metadata)
		VALUES (NULL, $1::uuid, 'device.enroll.self_service', 'success', $2, $3::inet, '{}'::jsonb)
	`, userID, requestID, sourceIP); err != nil {
		return VPNDevice{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return VPNDevice{}, false, err
	}
	return device, false, nil
}

func (s *PostgresStore) createVPNDeviceTx(ctx context.Context, tx pgx.Tx, actorID, userID string, input CreateDeviceInput) (VPNDevice, error) {
		var userStatus string
		var maxDevices int
		if err := tx.QueryRow(ctx, `
			SELECT status, max_devices FROM vpn_users
			WHERE id = $1::uuid
			FOR UPDATE
		`, userID).Scan(&userStatus, &maxDevices); errors.Is(err, pgx.ErrNoRows) {
			return VPNDevice{}, ErrVPNUserNotFound
		} else if err != nil {
			return VPNDevice{}, err
		}
		if userStatus != "active" {
			return VPNDevice{}, ErrUserNotActive
		}
		var activeDevices int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM vpn_devices
			WHERE user_id = $1::uuid
			  AND status IN ('provisioning', 'active', 'revoking', 'error')
		`, userID).Scan(&activeDevices); err != nil {
			return VPNDevice{}, err
		}
		if activeDevices >= maxDevices {
			return VPNDevice{}, ErrDeviceLimitReached
		}
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM vpn_devices WHERE user_id = $1::uuid AND device_ref = $2
			) OR EXISTS (
				SELECT 1 FROM wireguard_peers WHERE public_key = $3
			)
		`, userID, input.DeviceRef, input.PublicKey).Scan(&exists); err != nil {
			return VPNDevice{}, err
		}
		if exists {
			return VPNDevice{}, ErrDeviceConflict
		}

		node, err := selectProvisioningNode(ctx, tx)
		if err != nil {
			return VPNDevice{}, err
		}
		usedIPv4 := map[netip.Addr]struct{}{node.interfaceIPv4: {}}
		usedIPv6 := map[netip.Addr]struct{}{node.interfaceIPv6: {}}
		rows, err := tx.Query(ctx, `
			SELECT host(assigned_ipv4), COALESCE(host(assigned_ipv6), '')
			FROM wireguard_peers
			WHERE node_id = $1::uuid AND status <> 'revoked'
		`, node.id)
		if err != nil {
			return VPNDevice{}, err
		}
		for rows.Next() {
			var v4Text, v6Text string
			if err := rows.Scan(&v4Text, &v6Text); err != nil {
				rows.Close()
				return VPNDevice{}, err
			}
			if address, parseErr := netip.ParseAddr(v4Text); parseErr == nil {
				usedIPv4[address] = struct{}{}
			}
			if address, parseErr := netip.ParseAddr(v6Text); parseErr == nil {
				usedIPv6[address] = struct{}{}
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return VPNDevice{}, err
		}
		rows.Close()

		ipv4, err := allocateNodeAddress(node.ipv4Pool, usedIPv4, true)
		if err != nil {
			return VPNDevice{}, err
		}
		ipv6, err := allocateNodeAddress(node.ipv6Pool, usedIPv6, false)
		if err != nil {
			return VPNDevice{}, err
		}

		device := VPNDevice{
			UserID: userID, DeviceRef: input.DeviceRef, DeviceName: input.DeviceName,
			Platform: input.Platform, Status: "provisioning",
			Peer: VPNPeerProfile{
				NodeID: node.id, PublicKey: input.PublicKey,
				AssignedIPv4: ipv4.String(), AssignedIPv6: ipv6.String(),
				ServerPublicKey: node.serverPublicKey, EndpointHost: node.endpointHost,
				EndpointPort: node.endpointPort, Status: "pending",
			},
		}
		err = tx.QueryRow(ctx, `
			INSERT INTO vpn_devices (user_id, device_ref, device_name, platform, status)
			VALUES ($1::uuid, $2, $3, $4, 'provisioning')
			RETURNING id::text, created_at
		`, userID, input.DeviceRef, input.DeviceName, input.Platform).Scan(&device.ID, &device.CreatedAt)
		if err != nil {
			return VPNDevice{}, mapDeviceInsertError(err)
		}
		err = tx.QueryRow(ctx, `
			INSERT INTO wireguard_peers (device_id, node_id, public_key, assigned_ipv4, assigned_ipv6, status)
			VALUES ($1::uuid, $2::uuid, $3, $4::inet, $5::inet, 'pending')
			RETURNING id::text
		`, device.ID, node.id, input.PublicKey, ipv4.String(), ipv6.String()).Scan(&device.Peer.ID)
		if err != nil {
			return VPNDevice{}, mapDeviceInsertError(err)
		}

		payload, err := json.Marshal(map[string]string{
			"public_key": input.PublicKey, "ipv4": ipv4.String(), "ipv6": ipv6.String(),
		})
		if err != nil {
			return VPNDevice{}, err
		}
		commandHash := sha256.Sum256(payload)
		if _, err := tx.Exec(ctx, `
			INSERT INTO node_commands
				(node_id, created_by_operator_id, command_type, payload, request_hash, idempotency_key)
			VALUES ($1::uuid, NULLIF($2, '')::uuid, 'peer.ensure', $3::jsonb, $4, $5)
		`, node.id, actorID, string(payload), commandHash[:], newRequestID()); err != nil {
			return VPNDevice{}, err
		}
		return device, nil
}




func (s *PostgresStore) CreateDeviceEnrollmentCode(ctx context.Context, actorID, userID string, tokenHash []byte, expiresAt time.Time, requestID string, sourceIP *string) error {
	if len(tokenHash) != 32 {
		return errors.New("invalid enrollment token hash")
	}
	now := time.Now().UTC()
	if !expiresAt.After(now) || expiresAt.After(now.Add(time.Hour)) {
		return errors.New("enrollment token expiry is out of range")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var userStatus string
	if err := tx.QueryRow(ctx, `
		SELECT status FROM vpn_users WHERE id = $1::uuid FOR UPDATE
	`, userID).Scan(&userStatus); errors.Is(err, pgx.ErrNoRows) {
		return ErrVPNUserNotFound
	} else if err != nil {
		return err
	}
	if userStatus != "active" {
		return ErrUserNotActive
	}
	if _, err := tx.Exec(ctx, `
		UPDATE vpn_user_enrollment_codes
		SET revoked_at = now()
		WHERE user_id = $1::uuid
		  AND consumed_at IS NULL
		  AND revoked_at IS NULL
	`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO vpn_user_enrollment_codes
			(user_id, created_by_operator_id, token_hash, expires_at)
		VALUES ($1::uuid, $2::uuid, $3, $4)
	`, userID, actorID, tokenHash, expiresAt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events
			(actor_operator_id, subject_user_id, action, outcome, request_id, source_ip, metadata)
		VALUES ($1::uuid, $2::uuid, 'device.enrollment_code.create', 'success', $3, $4::inet, '{}'::jsonb)
	`, actorID, userID, requestID, sourceIP); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) EnrollVPNDevice(ctx context.Context, tokenHash []byte, idempotencyKey string, requestHash []byte, input CreateDeviceInput, requestID string, sourceIP *string) (VPNDevice, bool, error) {
	if len(tokenHash) != 32 || len(requestHash) != 32 ||
		!idempotencyKeyPattern.MatchString(idempotencyKey) {
		return VPNDevice{}, false, errors.New("invalid client enrollment request")
	}
	var enrollmentID, userID, actorID string
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, user_id::text, created_by_operator_id::text
		FROM vpn_user_enrollment_codes
		WHERE token_hash = $1 AND revoked_at IS NULL
	`, tokenHash).Scan(&enrollmentID, &userID, &actorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return VPNDevice{}, false, ErrEnrollmentCodeInvalid
	}
	if err != nil {
		return VPNDevice{}, false, err
	}
	route := "POST:/v1/client/enroll/" + enrollmentID
	return s.runIdempotentDeviceMutation(ctx, actorID, route, idempotencyKey, requestHash, "device.enroll", requestID, sourceIP, func(tx pgx.Tx) (VPNDevice, error) {
		var codeUserID string
		var expiresAt time.Time
		var consumedAt, revokedAt *time.Time
		var consumedDeviceID *string
		err := tx.QueryRow(ctx, `
			SELECT user_id::text, expires_at, consumed_at, revoked_at, device_id::text
			FROM vpn_user_enrollment_codes
			WHERE id = $1::uuid AND token_hash = $2
			FOR UPDATE
		`, enrollmentID, tokenHash).Scan(&codeUserID, &expiresAt, &consumedAt, &revokedAt, &consumedDeviceID)
		if errors.Is(err, pgx.ErrNoRows) || revokedAt != nil {
			return VPNDevice{}, ErrEnrollmentCodeInvalid
		}
		if err != nil {
			return VPNDevice{}, err
		}
		if codeUserID != userID {
			return VPNDevice{}, ErrEnrollmentCodeInvalid
		}
		if consumedAt != nil {
			if consumedDeviceID == nil {
				return VPNDevice{}, ErrEnrollmentCodeUsed
			}
			existing, err := getVPNDeviceTx(ctx, tx, *consumedDeviceID)
			if errors.Is(err, pgx.ErrNoRows) {
				return VPNDevice{}, ErrEnrollmentCodeUsed
			}
			if err != nil {
				return VPNDevice{}, err
			}
			if existing.UserID != userID || existing.DeviceRef != input.DeviceRef ||
				existing.Peer.PublicKey != input.PublicKey {
				return VPNDevice{}, ErrEnrollmentCodeUsed
			}
			return existing, nil
		}
		if !expiresAt.After(time.Now().UTC()) {
			return VPNDevice{}, ErrEnrollmentCodeExpired
		}

		device, err := s.createVPNDeviceTx(ctx, tx, actorID, userID, input)
		if err != nil {
			return VPNDevice{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE vpn_user_enrollment_codes
			SET consumed_at = now(), device_id = $2::uuid
			WHERE id = $1::uuid AND consumed_at IS NULL AND revoked_at IS NULL
		`, enrollmentID, device.ID); err != nil {
			return VPNDevice{}, err
		}
		return device, nil
	})
}

func (s *PostgresStore) RevokeVPNDevice(ctx context.Context, actorID, userID, deviceID, route, key string, requestHash []byte, requestID string, sourceIP *string) (VPNDevice, bool, error) {
	return s.runIdempotentDeviceMutation(ctx, actorID, route, key, requestHash, "device.revoke", requestID, sourceIP, func(tx pgx.Tx) (VPNDevice, error) {
		var device VPNDevice
		err := tx.QueryRow(ctx, `
			SELECT d.id::text, d.user_id::text, d.device_ref, d.device_name, d.platform,
			       d.status, d.created_at, COALESCE(d.last_error, ''),
			       p.id::text, p.node_id::text, p.public_key, host(p.assigned_ipv4),
			       COALESCE(host(p.assigned_ipv6), ''), n.public_key, n.endpoint_host,
			       n.endpoint_port, p.status
			FROM vpn_devices d
			JOIN wireguard_peers p ON p.device_id = d.id
			JOIN vpn_nodes n ON n.id = p.node_id
			WHERE d.id = $1::uuid AND d.user_id = $2::uuid
			FOR UPDATE OF d, p
		`, deviceID, userID).Scan(
			&device.ID, &device.UserID, &device.DeviceRef, &device.DeviceName,
			&device.Platform, &device.Status, &device.CreatedAt, &device.LastError,
			&device.Peer.ID, &device.Peer.NodeID, &device.Peer.PublicKey,
			&device.Peer.AssignedIPv4, &device.Peer.AssignedIPv6,
			&device.Peer.ServerPublicKey, &device.Peer.EndpointHost,
			&device.Peer.EndpointPort, &device.Peer.Status,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return VPNDevice{}, ErrDeviceNotFound
		}
		if err != nil {
			return VPNDevice{}, err
		}
		if device.Status == "revoked" || device.Status == "revoking" {
			return device, nil
		}
		if device.Status != "provisioning" && device.Status != "active" && device.Status != "error" {
			return VPNDevice{}, ErrDeviceConflict
		}
		payload, err := json.Marshal(map[string]string{"public_key": device.Peer.PublicKey})
		if err != nil {
			return VPNDevice{}, err
		}
		commandHash := sha256.Sum256(payload)
		if _, err := tx.Exec(ctx, `
			INSERT INTO node_commands
				(node_id, created_by_operator_id, command_type, payload, request_hash, idempotency_key)
			VALUES ($1::uuid, $2::uuid, 'peer.remove', $3::jsonb, $4, $5)
		`, device.Peer.NodeID, actorID, string(payload), commandHash[:], newRequestID()); err != nil {
			return VPNDevice{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE vpn_devices SET status = 'revoking', updated_at = now(), last_error = NULL
			WHERE id = $1::uuid
		`, device.ID); err != nil {
			return VPNDevice{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE wireguard_peers SET status = 'revoking'
			WHERE id = $1::uuid
		`, device.Peer.ID); err != nil {
			return VPNDevice{}, err
		}
		device.Status = "revoking"
		device.Peer.Status = "revoking"
		device.LastError = ""
		return device, nil
	})
}

func (s *PostgresStore) runIdempotentDeviceMutation(ctx context.Context, actorID, route, key string, requestHash []byte, action, requestID string, sourceIP *string, mutate func(pgx.Tx) (VPNDevice, error)) (VPNDevice, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return VPNDevice{}, false, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		DELETE FROM idempotency_records
		WHERE actor_operator_id = $1::uuid AND expires_at <= now()
	`, actorID); err != nil {
		return VPNDevice{}, false, err
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
			return VPNDevice{}, false, err
		}
		if !bytes.Equal(existingHash, requestHash) {
			return VPNDevice{}, false, ErrIdempotencyConflict
		}
		if len(responseBody) == 0 || responseStatus == nil {
			return VPNDevice{}, false, errors.New("idempotency record is incomplete")
		}
		var device VPNDevice
		if err := json.Unmarshal(responseBody, &device); err != nil {
			return VPNDevice{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return VPNDevice{}, false, err
		}
		return device, true, nil
	}
	if err != nil {
		return VPNDevice{}, false, err
	}
	device, err := mutate(tx)
	if err != nil {
		return VPNDevice{}, false, err
	}
	body, err := json.Marshal(device)
	if err != nil {
		return VPNDevice{}, false, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE idempotency_records SET response_status = 202, response_body = $4::jsonb
		WHERE actor_operator_id = $1::uuid AND route = $2 AND idempotency_key = $3
	`, actorID, route, key, string(body)); err != nil {
		return VPNDevice{}, false, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events
			(actor_operator_id, subject_user_id, action, outcome, request_id, source_ip, metadata)
		VALUES ($1::uuid, $2::uuid, $3, 'success', $4, $5::inet, '{}'::jsonb)
	`, actorID, device.UserID, action, requestID, sourceIP); err != nil {
		return VPNDevice{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return VPNDevice{}, false, err
	}
	return device, false, nil
}

func selectProvisioningNode(ctx context.Context, tx pgx.Tx) (provisioningNode, error) {
	rows, err := tx.Query(ctx, `
		SELECT n.id::text, n.client_ipv4_pool::text, n.client_ipv6_pool::text,
		       host(n.interface_ipv4), host(n.interface_ipv6), n.public_key,
		       n.endpoint_host, n.endpoint_port
		FROM vpn_nodes n
		WHERE n.status = 'online'
		  AND n.last_seen_at > now() - interval '90 seconds'
		  AND EXISTS (
			  SELECT 1 FROM vpn_node_agent_certificates ac
			  WHERE ac.node_id = n.id
			    AND ac.revoked_at IS NULL
			    AND ac.not_before <= now()
			    AND ac.not_after > now()
		  )
		  AND n.client_ipv4_pool IS NOT NULL
		  AND n.client_ipv6_pool IS NOT NULL
		  AND n.interface_ipv4 IS NOT NULL
		  AND n.interface_ipv6 IS NOT NULL
		ORDER BY (
			SELECT count(*) FROM wireguard_peers p
			WHERE p.node_id = n.id AND p.status <> 'revoked'
		) ASC, n.last_seen_at DESC NULLS LAST
		FOR UPDATE OF n SKIP LOCKED
	`)
	if err != nil {
		return provisioningNode{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var node provisioningNode
		var ipv4PoolText, ipv6PoolText, interfaceIPv4Text, interfaceIPv6Text string
		if err := rows.Scan(
			&node.id, &ipv4PoolText, &ipv6PoolText,
			&interfaceIPv4Text, &interfaceIPv6Text, &node.serverPublicKey,
			&node.endpointHost, &node.endpointPort,
		); err != nil {
			return provisioningNode{}, err
		}
		node.ipv4Pool, err = netip.ParsePrefix(ipv4PoolText)
		if err != nil || !node.ipv4Pool.Addr().Is4() || node.ipv4Pool.Bits() < 16 || node.ipv4Pool.Bits() > 30 {
			continue
		}
		node.ipv6Pool, err = netip.ParsePrefix(ipv6PoolText)
		if err != nil || !node.ipv6Pool.Addr().Is6() || node.ipv6Pool.Bits() < 48 || node.ipv6Pool.Bits() > 120 {
			continue
		}
		node.interfaceIPv4, err = netip.ParseAddr(interfaceIPv4Text)
		if err != nil || !node.ipv4Pool.Contains(node.interfaceIPv4) {
			continue
		}
		node.interfaceIPv6, err = netip.ParseAddr(interfaceIPv6Text)
		if err != nil || !node.ipv6Pool.Contains(node.interfaceIPv6) {
			continue
		}
		if !validWireGuardPublicKey(node.serverPublicKey) || strings.TrimSpace(node.endpointHost) == "" || node.endpointPort < 1 || node.endpointPort > 65535 {
			continue
		}
		return node, nil
	}
	if err := rows.Err(); err != nil {
		return provisioningNode{}, err
	}
	return provisioningNode{}, ErrNoProvisioningNode
}

func allocateNodeAddress(prefix netip.Prefix, used map[netip.Addr]struct{}, ipv4 bool) (netip.Addr, error) {
	candidate := prefix.Masked().Addr().Next()
	for probes := 0; probes < 65536 && candidate.IsValid() && prefix.Contains(candidate); probes++ {
		_, occupied := used[candidate]
		if !occupied && candidate.IsGlobalUnicast() && !candidate.IsLinkLocalUnicast() &&
			(!ipv4 || !isIPv4Broadcast(candidate, prefix)) {
			return candidate, nil
		}
		candidate = candidate.Next()
	}
	return netip.Addr{}, ErrAddressPoolExhausted
}


func isIPv4Broadcast(address netip.Addr, prefix netip.Prefix) bool {
	if !address.Is4() || prefix.Addr().Is6() || prefix.Bits() >= 31 {
		return false
	}
	masked := prefix.Masked().Addr().As4()
	current := address.As4()
	var network, value uint32
	for _, octet := range masked {
		network = (network << 8) | uint32(octet)
	}
	for _, octet := range current {
		value = (value << 8) | uint32(octet)
	}
	hostBits := uint(32 - prefix.Bits())
	hostMask := uint32((uint64(1) << hostBits) - 1)
	return value == (network | hostMask)
}

func validWireGuardPublicKey(value string) bool {
	decoded, err := base64.StdEncoding.DecodeString(value)
	return err == nil && len(decoded) == 32 && base64.StdEncoding.EncodeToString(decoded) == value
}

func mapDeviceInsertError(err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError.Code == "23505" {
		return ErrDeviceConflict
	}
	return err
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
		SELECT n.id::text
		FROM vpn_node_agent_certificates ac
		JOIN vpn_nodes n ON n.id = ac.node_id
		WHERE ac.fingerprint_sha256 = $1
		  AND ac.revoked_at IS NULL
		  AND ac.not_before <= now()
		  AND ac.not_after > now()
		  AND n.status <> 'disabled'
		FOR UPDATE OF n
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
	if err := markExpiredNodeCommandSubjects(ctx, tx, nodeID); err != nil {
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



func markExpiredNodeCommandSubjects(ctx context.Context, tx pgx.Tx, nodeID string) error {
	if _, err := tx.Exec(ctx, `
		UPDATE wireguard_peers p
		SET status = 'error'
		FROM node_commands c
		WHERE c.node_id = $1::uuid
		  AND c.status = 'expired'
		  AND c.command_type IN ('peer.ensure', 'peer.remove')
		  AND p.node_id = c.node_id
		  AND p.public_key = c.payload->>'public_key'
		  AND p.status IN ('pending', 'active', 'revoking')
		  AND NOT EXISTS (
		      SELECT 1
		      FROM node_commands newer
		      WHERE newer.node_id = c.node_id
		        AND newer.payload->>'public_key' = c.payload->>'public_key'
		        AND (newer.created_at, newer.id) > (c.created_at, c.id)
		  )
	`, nodeID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE vpn_devices d
		SET status = 'error', updated_at = now(), last_error = 'command_expired'
		FROM wireguard_peers p
		WHERE p.device_id = d.id
		  AND p.node_id = $1::uuid
		  AND p.status = 'error'
		  AND d.status IN ('provisioning', 'active', 'revoking')
	`, nodeID)
	return err
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
	var commandType string
	var commandPayload []byte
	err = tx.QueryRow(ctx, `
		SELECT status, last_error, lease_expires_at, expires_at, command_type, payload
		FROM node_commands
		WHERE id = $1::uuid AND node_id = $2::uuid
		FOR UPDATE
	`, commandID, nodeID).Scan(&currentStatus, &currentError, &leaseExpiresAt, &expiresAt, &commandType, &commandPayload)
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
		if err := markExpiredNodeCommandSubjects(ctx, tx, nodeID); err != nil {
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

	var commandPayloadFields struct {
		PublicKey string `json:"public_key"`
	}
	if err := json.Unmarshal(commandPayload, &commandPayloadFields); err != nil || !validWireGuardPublicKey(commandPayloadFields.PublicKey) {
		return errors.New("node command payload has no valid WireGuard public key")
	}
	switch commandType {
	case "peer.ensure":
		if status == "succeeded" {
			if _, err := tx.Exec(ctx, `
				UPDATE wireguard_peers
				SET status = 'active'
				WHERE node_id = $1::uuid AND public_key = $2
				  AND status IN ('pending', 'error')
			`, nodeID, commandPayloadFields.PublicKey); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				UPDATE vpn_devices d
				SET status = 'active', updated_at = now(), last_error = NULL
				FROM wireguard_peers p
				WHERE p.device_id = d.id AND p.node_id = $1::uuid
				  AND p.public_key = $2 AND d.status = 'provisioning'
			`, nodeID, commandPayloadFields.PublicKey); err != nil {
				return err
			}
		} else {
			if _, err := tx.Exec(ctx, `
				UPDATE wireguard_peers SET status = 'error'
				WHERE node_id = $1::uuid AND public_key = $2
				  AND status IN ('pending', 'active')
			`, nodeID, commandPayloadFields.PublicKey); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				UPDATE vpn_devices d
				SET status = 'error', updated_at = now(), last_error = $3
				FROM wireguard_peers p
				WHERE p.device_id = d.id AND p.node_id = $1::uuid
				  AND p.public_key = $2 AND d.status IN ('provisioning', 'active')
			`, nodeID, commandPayloadFields.PublicKey, errorCode); err != nil {
				return err
			}
		}
	case "peer.remove":
		if status == "succeeded" {
			if _, err := tx.Exec(ctx, `
				UPDATE wireguard_peers
				SET status = 'revoked', revoked_at = COALESCE(revoked_at, now())
				WHERE node_id = $1::uuid AND public_key = $2
				  AND status <> 'revoked'
			`, nodeID, commandPayloadFields.PublicKey); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				UPDATE vpn_devices d
				SET status = 'revoked', revoked_at = COALESCE(d.revoked_at, now()),
				    updated_at = now(), last_error = NULL
				FROM wireguard_peers p
				WHERE p.device_id = d.id AND p.node_id = $1::uuid
				  AND p.public_key = $2 AND d.status <> 'revoked'
			`, nodeID, commandPayloadFields.PublicKey); err != nil {
				return err
			}
		} else {
			if _, err := tx.Exec(ctx, `
				UPDATE wireguard_peers SET status = 'error'
				WHERE node_id = $1::uuid AND public_key = $2
				  AND status IN ('pending', 'active', 'revoking')
			`, nodeID, commandPayloadFields.PublicKey); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				UPDATE vpn_devices d
				SET status = 'error', updated_at = now(), last_error = $3
				FROM wireguard_peers p
				WHERE p.device_id = d.id AND p.node_id = $1::uuid
				  AND p.public_key = $2 AND d.status IN ('provisioning', 'active', 'revoking')
			`, nodeID, commandPayloadFields.PublicKey, errorCode); err != nil {
				return err
			}
		}
	default:
		return errors.New("unknown node command type")
	}
	return tx.Commit(ctx)
}
