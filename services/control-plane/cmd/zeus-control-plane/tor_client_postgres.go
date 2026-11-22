package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (s *PostgresStore) CreateTorEnrollmentCode(
	ctx context.Context, actorID, userID string, tokenHash []byte, expiresAt time.Time,
	requestID string, sourceIP *string,
) error {
	if len(tokenHash) != 32 || !uuidPattern.MatchString(actorID) || !uuidPattern.MatchString(userID) {
		return errors.New("invalid Tor enrollment invitation")
	}
	now := time.Now().UTC()
	if !expiresAt.After(now) || expiresAt.After(now.Add(24*time.Hour)) {
		return errors.New("Tor invitation expiry is out of range")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var status string
	if err := tx.QueryRow(ctx, `
		SELECT status FROM vpn_users WHERE id = $1::uuid FOR UPDATE
	`, userID).Scan(&status); errors.Is(err, pgx.ErrNoRows) {
		return ErrVPNUserNotFound
	} else if err != nil {
		return err
	}
	if status != "active" {
		return ErrUserNotActive
	}
	if _, err := tx.Exec(ctx, `
		UPDATE tor_client_enrollment_codes
		SET revoked_at = now()
		WHERE user_id = $1::uuid AND consumed_at IS NULL AND revoked_at IS NULL
	`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO tor_client_enrollment_codes
			(user_id, created_by_operator_id, token_hash, expires_at)
		VALUES ($1::uuid, $2::uuid, $3, $4)
	`, userID, actorID, tokenHash, expiresAt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events
			(actor_operator_id, subject_user_id, action, outcome, request_id, source_ip, metadata)
		VALUES ($1::uuid, $2::uuid, 'tor.device.enrollment_code.create', 'success', $3, $4::inet, '{}'::jsonb)
	`, actorID, userID, requestID, sourceIP); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) FindTorEnrollmentCodeUser(ctx context.Context, tokenHash []byte) (string, error) {
	if len(tokenHash) != 32 {
		return "", ErrEnrollmentCodeInvalid
	}
	var userID string
	err := s.pool.QueryRow(ctx, `
		SELECT user_id::text
		FROM tor_client_enrollment_codes
		WHERE token_hash = $1
	`, tokenHash).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrEnrollmentCodeInvalid
	}
	return userID, err
}

func (s *PostgresStore) EnrollTorClientDevice(
	ctx context.Context, userID string, tokenHash []byte, idempotencyKey string,
	requestHash []byte, input CreateTorClientDeviceInput, requestID string, sourceIP *string,
) (TorClientDevice, bool, error) {
	if !uuidPattern.MatchString(userID) || !idempotencyKeyPattern.MatchString(idempotencyKey) ||
		len(requestHash) != 32 || (len(tokenHash) != 0 && len(tokenHash) != 32) {
		return TorClientDevice{}, false, errors.New("invalid Tor client enrollment request")
	}
	const route = "POST:/v1/client/tor/devices"
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TorClientDevice{}, false, err
	}
	defer tx.Rollback(ctx)

	var userStatus string
	if err := tx.QueryRow(ctx, `
		SELECT status FROM vpn_users WHERE id = $1::uuid FOR UPDATE
	`, userID).Scan(&userStatus); errors.Is(err, pgx.ErrNoRows) {
		return TorClientDevice{}, false, ErrVPNUserNotFound
	} else if err != nil {
		return TorClientDevice{}, false, err
	}
	if userStatus != "active" {
		return TorClientDevice{}, false, ErrUserNotActive
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM client_device_idempotency_records
		WHERE user_id = $1::uuid AND expires_at <= now()
	`, userID); err != nil {
		return TorClientDevice{}, false, err
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
			return TorClientDevice{}, false, err
		}
		if !bytes.Equal(existingHash, requestHash) {
			return TorClientDevice{}, false, ErrIdempotencyConflict
		}
		if len(responseBody) == 0 || responseStatus == nil {
			return TorClientDevice{}, false, errors.New("Tor idempotency record is incomplete")
		}
		var device TorClientDevice
		if err := json.Unmarshal(responseBody, &device); err != nil {
			return TorClientDevice{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return TorClientDevice{}, false, err
		}
		return device, true, nil
	}
	if err != nil {
		return TorClientDevice{}, false, err
	}

	if len(tokenHash) == 32 {
		var expiresAt time.Time
		var consumedAt, revokedAt *time.Time
		err := tx.QueryRow(ctx, `
			SELECT expires_at, consumed_at, revoked_at
			FROM tor_client_enrollment_codes
			WHERE user_id = $1::uuid AND token_hash = $2
			FOR UPDATE
		`, userID, tokenHash).Scan(&expiresAt, &consumedAt, &revokedAt)
		if errors.Is(err, pgx.ErrNoRows) || revokedAt != nil {
			return TorClientDevice{}, false, ErrEnrollmentCodeInvalid
		}
		if err != nil {
			return TorClientDevice{}, false, err
		}
		if consumedAt != nil {
			return TorClientDevice{}, false, ErrEnrollmentCodeUsed
		}
		if !expiresAt.After(time.Now().UTC()) {
			return TorClientDevice{}, false, ErrEnrollmentCodeExpired
		}
	}

	var deviceRefExists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM vpn_devices WHERE user_id = $1::uuid AND device_ref = $2
		) OR EXISTS (
			SELECT 1 FROM tor_client_devices WHERE user_id = $1::uuid AND device_ref = $2
		)
	`, userID, input.DeviceRef).Scan(&deviceRefExists); err != nil {
		return TorClientDevice{}, false, err
	}
	if deviceRefExists {
		return TorClientDevice{}, false, ErrDeviceConflict
	}

	var activeDevices int
	if err := tx.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM vpn_devices
			 WHERE user_id = $1::uuid
			   AND status IN ('provisioning', 'active', 'revoking', 'error'))
			+
			(SELECT count(*) FROM tor_client_devices
			 WHERE user_id = $1::uuid AND status = 'active')
	`, userID).Scan(&activeDevices); err != nil {
		return TorClientDevice{}, false, err
	}
	var maxDevices int
	if err := tx.QueryRow(ctx, `
		SELECT max_devices FROM vpn_users WHERE id = $1::uuid
	`, userID).Scan(&maxDevices); err != nil {
		return TorClientDevice{}, false, err
	}
	if activeDevices >= maxDevices {
		return TorClientDevice{}, false, ErrDeviceLimitReached
	}

	device := TorClientDevice{
		UserID: userID, DeviceRef: input.DeviceRef, DeviceName: input.DeviceName,
		Platform: "android", ClientVersion: input.ClientVersion, Transport: "tor", Status: "active",
		Entitlement: TorClientEntitlement{Status: "active"},
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO tor_client_devices
			(user_id, device_ref, device_name, platform, client_version, status)
		VALUES ($1::uuid, $2, $3, 'android', $4, 'active')
		RETURNING id::text, created_at, updated_at
	`, userID, input.DeviceRef, input.DeviceName, input.ClientVersion).
		Scan(&device.ID, &device.CreatedAt, &device.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return TorClientDevice{}, false, ErrDeviceConflict
		}
		return TorClientDevice{}, false, err
	}

	if len(tokenHash) == 32 {
		tag, err := tx.Exec(ctx, `
			UPDATE tor_client_enrollment_codes
			SET consumed_at = now(), device_id = $3::uuid
			WHERE user_id = $1::uuid AND token_hash = $2
			  AND consumed_at IS NULL AND revoked_at IS NULL
		`, userID, tokenHash, device.ID)
		if err != nil {
			return TorClientDevice{}, false, err
		}
		if tag.RowsAffected() != 1 {
			return TorClientDevice{}, false, ErrEnrollmentCodeUsed
		}
	}

	body, err := json.Marshal(device)
	if err != nil {
		return TorClientDevice{}, false, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE client_device_idempotency_records
		SET response_status = 201, response_body = $4::jsonb
		WHERE user_id = $1::uuid AND route = $2 AND idempotency_key = $3
	`, userID, route, idempotencyKey, string(body)); err != nil {
		return TorClientDevice{}, false, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events
			(actor_operator_id, subject_user_id, action, outcome, request_id, source_ip, metadata)
		VALUES (NULL, $1::uuid, 'tor.device.enroll', 'success', $2, $3::inet, '{}'::jsonb)
	`, userID, requestID, sourceIP); err != nil {
		return TorClientDevice{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TorClientDevice{}, false, err
	}
	return device, false, nil
}

func (s *PostgresStore) GetTorClientDevice(ctx context.Context, userID, deviceID string) (TorClientDevice, error) {
	device := TorClientDevice{Transport: "tor", Entitlement: TorClientEntitlement{Status: "active"}}
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, user_id::text, device_ref, device_name, platform,
		       client_version, status, created_at, updated_at, revoked_at
		FROM tor_client_devices
		WHERE user_id = $1::uuid AND id = $2::uuid
	`, userID, deviceID).Scan(
		&device.ID, &device.UserID, &device.DeviceRef, &device.DeviceName,
		&device.Platform, &device.ClientVersion, &device.Status,
		&device.CreatedAt, &device.UpdatedAt, &device.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return TorClientDevice{}, ErrDeviceNotFound
	}
	return device, err
}

func (s *PostgresStore) RevokeTorClientDevice(ctx context.Context, userID, deviceID, requestID string, sourceIP *string) (TorClientDevice, error) {
	if !uuidPattern.MatchString(userID) || !uuidPattern.MatchString(deviceID) {
		return TorClientDevice{}, ErrDeviceNotFound
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TorClientDevice{}, err
	}
	defer tx.Rollback(ctx)
	var lockedUser string
	if err := tx.QueryRow(ctx, `
		SELECT id::text FROM vpn_users WHERE id = $1::uuid FOR UPDATE
	`, userID).Scan(&lockedUser); errors.Is(err, pgx.ErrNoRows) {
		return TorClientDevice{}, ErrDeviceNotFound
	} else if err != nil {
		return TorClientDevice{}, err
	}
	device := TorClientDevice{Transport: "tor", Entitlement: TorClientEntitlement{Status: "active"}}
	err = tx.QueryRow(ctx, `
		UPDATE tor_client_devices
		SET status = 'revoked', revoked_at = COALESCE(revoked_at, now()), updated_at = now()
		WHERE user_id = $1::uuid AND id = $2::uuid
		RETURNING id::text, user_id::text, device_ref, device_name, platform,
		          client_version, status, created_at, updated_at, revoked_at
	`, userID, deviceID).Scan(
		&device.ID, &device.UserID, &device.DeviceRef, &device.DeviceName,
		&device.Platform, &device.ClientVersion, &device.Status,
		&device.CreatedAt, &device.UpdatedAt, &device.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return TorClientDevice{}, ErrDeviceNotFound
	}
	if err != nil {
		return TorClientDevice{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events
			(actor_operator_id, subject_user_id, action, outcome, request_id, source_ip, metadata)
		VALUES (NULL, $1::uuid, 'tor.device.revoke', 'success', $2, $3::inet, '{}'::jsonb)
	`, userID, requestID, sourceIP); err != nil {
		return TorClientDevice{}, fmt.Errorf("record Tor device revocation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return TorClientDevice{}, err
	}
	return device, nil
}
