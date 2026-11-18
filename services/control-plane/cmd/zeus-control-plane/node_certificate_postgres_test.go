package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
	"time"
)

func TestListNodeAgentCertificatesSupportsLegacyInfinityExpiry(t *testing.T) {
	dsn := os.Getenv("ZEUS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ZEUS_TEST_DATABASE_URL is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := OpenPostgres(ctx, dsn, false)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	nodeName := "legacy-expiry-test-" + newRequestID()
	var nodeID string
	err = store.pool.QueryRow(ctx, `
		INSERT INTO vpn_nodes (name, region, endpoint_host, endpoint_port, public_key)
		VALUES ($1, 'test', '127.0.0.1', 51820, 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=')
		RETURNING id::text
	`, nodeName).Scan(&nodeID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = store.pool.Exec(cleanupCtx, "DELETE FROM vpn_node_agent_certificates WHERE node_id = $1::uuid", nodeID)
		_, _ = store.pool.Exec(cleanupCtx, "DELETE FROM vpn_nodes WHERE id = $1::uuid", nodeID)
	}()

	legacyFingerprint := sha256.Sum256([]byte("legacy-" + nodeID))
	finiteFingerprint := sha256.Sum256([]byte("finite-" + nodeID))
	_, err = store.pool.Exec(ctx, `
		INSERT INTO vpn_node_agent_certificates
			(node_id, fingerprint_sha256, not_before, not_after)
		VALUES
			($1::uuid, $2, now() - interval '5 minutes', 'infinity'::timestamptz),
			($1::uuid, $3, now() - interval '5 minutes', now() + interval '24 hours')
	`, nodeID, legacyFingerprint[:], finiteFingerprint[:])
	if err != nil {
		t.Fatal(err)
	}

	certificates, err := store.ListNodeAgentCertificates(ctx, nodeID)
	if err != nil {
		t.Fatalf("list certificates with a legacy infinity expiry: %v", err)
	}
	byFingerprint := make(map[string]NodeAgentCertificate, len(certificates))
	for _, certificate := range certificates {
		byFingerprint[certificate.FingerprintSHA256] = certificate
	}

	legacy, ok := byFingerprint[hex.EncodeToString(legacyFingerprint[:])]
	if !ok {
		t.Fatal("legacy certificate was not returned")
	}
	if legacy.NotAfter != nil {
		t.Fatalf("legacy expiry should be unknown, got %s", legacy.NotAfter)
	}

	finite, ok := byFingerprint[hex.EncodeToString(finiteFingerprint[:])]
	if !ok {
		t.Fatal("finite certificate was not returned")
	}
	if finite.NotAfter == nil || !finite.NotAfter.After(time.Now()) {
		t.Fatalf("finite certificate expiry was not preserved: %+v", finite.NotAfter)
	}
}
