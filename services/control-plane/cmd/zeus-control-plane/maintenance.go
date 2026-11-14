package main

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

const (
	sessionCleanupBatchSize    = 250
	sessionCleanupMaxBatchSize = 1000
	sessionCleanupInterval     = 15 * time.Minute
	sessionCleanupTimeout      = 5 * time.Second
	sessionRetentionPeriod     = 30 * 24 * time.Hour
)

// DeleteExpiredSessions removes only expired or revoked sessions whose retention
// window has elapsed. It works in small, lock-skipping batches so concurrent
// API workers are not blocked by a large cleanup sweep.
func (s *PostgresStore) DeleteExpiredSessions(ctx context.Context, batchSize int) (int64, error) {
	if batchSize < 1 || batchSize > sessionCleanupMaxBatchSize {
		return 0, errors.New("session cleanup batch size is out of range")
	}
	if s == nil || s.pool == nil {
		return 0, errors.New("database is not configured")
	}

	var deleted int64
	err := s.pool.QueryRow(ctx, `
		WITH stale_sessions AS (
			SELECT id
			FROM operator_sessions
			WHERE (revoked_at IS NOT NULL AND revoked_at <= now() - interval '30 days')
			   OR (revoked_at IS NULL AND expires_at <= now() - interval '30 days')
			ORDER BY CASE WHEN revoked_at IS NULL THEN expires_at ELSE revoked_at END, id
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		), deleted_sessions AS (
			DELETE FROM operator_sessions AS sessions
			USING stale_sessions
			WHERE sessions.id = stale_sessions.id
			RETURNING 1
		)
		SELECT count(*) FROM deleted_sessions
	`, batchSize).Scan(&deleted)
	return deleted, err
}

// runSessionCleanup performs one bounded cleanup at startup, then repeats at a
// fixed interval. It exits promptly when the Control Plane is shutting down.
func runSessionCleanup(ctx context.Context, store *PostgresStore, logger *slog.Logger) {
	if store == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}

	ticker := time.NewTicker(sessionCleanupInterval)
	defer ticker.Stop()

	cleanup := func() {
		cleanupCtx, cancel := context.WithTimeout(ctx, sessionCleanupTimeout)
		defer cancel()

		deleted, err := store.DeleteExpiredSessions(cleanupCtx, sessionCleanupBatchSize)
		if err != nil {
			if ctx.Err() == nil {
				logger.Warn("expired operator session cleanup failed", "error", err)
			}
			return
		}
		if deleted > 0 {
			logger.Info("expired operator sessions removed", "deleted", deleted)
		}
	}

	cleanup()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanup()
		}
	}
}
