package main

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestSessionCleanupRejectsUnsafeBatchSizes(t *testing.T) {
	store := &PostgresStore{}
	for _, batchSize := range []int{0, -1, sessionCleanupMaxBatchSize + 1} {
		if _, err := store.DeleteExpiredSessions(context.Background(), batchSize); err == nil {
			t.Fatalf("batch size %d should be rejected", batchSize)
		}
	}
}

func TestPostgresExpiredSessionCleanupIntegration(t *testing.T) {
	dsn := os.Getenv("ZEUS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ZEUS_TEST_DATABASE_URL is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := OpenPostgres(ctx, dsn, false)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	login := "ci-session-cleanup-" + newRequestID()
	if err := store.EnsureBootstrapOperator(ctx, login, "ci-session-cleanup-password-123"); err != nil {
		t.Fatal(err)
	}
	operator, err := store.FindOperatorByLogin(ctx, login)
	if err != nil {
		t.Fatal(err)
	}

	_, expiredHash, err := generateSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSession(ctx, operator.ID, expiredHash, time.Now().Add(-31*24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	_, revokedHash, err := generateSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSession(ctx, operator.ID, revokedHash, time.Now().Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeSession(ctx, revokedHash); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, "UPDATE operator_sessions SET revoked_at = now() - interval '31 days' WHERE token_hash = $1", revokedHash); err != nil {
		t.Fatal(err)
	}

	_, recentRevokedHash, err := generateSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSession(ctx, operator.ID, recentRevokedHash, time.Now().Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeSession(ctx, recentRevokedHash); err != nil {
		t.Fatal(err)
	}

	_, activeHash, err := generateSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSession(ctx, operator.ID, activeHash, time.Now().Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	countRows := func(hash []byte) int {
		t.Helper()
		var count int
		if err := store.pool.QueryRow(ctx, "SELECT count(*) FROM operator_sessions WHERE token_hash = $1", hash).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}

	firstBatch, err := store.DeleteExpiredSessions(ctx, 1)
	if err != nil || firstBatch != 1 {
		t.Fatalf("first cleanup batch deleted=%d err=%v, want 1", firstBatch, err)
	}
	secondBatch, err := store.DeleteExpiredSessions(ctx, 1)
	if err != nil || secondBatch != 1 {
		t.Fatalf("second cleanup batch deleted=%d err=%v, want 1", secondBatch, err)
	}
	emptyBatch, err := store.DeleteExpiredSessions(ctx, 1)
	if err != nil || emptyBatch != 0 {
		t.Fatalf("empty cleanup batch deleted=%d err=%v, want 0", emptyBatch, err)
	}

	if got := countRows(expiredHash); got != 0 {
		t.Fatalf("expired session rows remaining=%d, want 0", got)
	}
	if got := countRows(revokedHash); got != 0 {
		t.Fatalf("old revoked session rows remaining=%d, want 0", got)
	}
	if got := countRows(recentRevokedHash); got != 1 {
		t.Fatalf("recent revoked session rows=%d, want 1 (30-day retention)", got)
	}
	if got := countRows(activeHash); got != 1 {
		t.Fatalf("active session rows=%d, want 1", got)
	}
	if _, err := store.FindOperatorForSession(ctx, activeHash); err != nil {
		t.Fatalf("active session should remain valid after cleanup: %v", err)
	}
}
