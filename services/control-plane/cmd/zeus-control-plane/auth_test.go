package main

import (
	"fmt"
	"testing"
	"time"
)

func TestLoginLimiterBoundsDistinctKeys(t *testing.T) {
	limiter := newLoginLimiter()
	now := time.Now()

	for index := 0; index < loginLimiterMaxEntries; index++ {
		key := fmt.Sprintf("client-%d", index)
		if !limiter.allowWindow(key, now, 2, time.Minute) {
			t.Fatalf("new key %q was rejected before the limiter reached capacity", key)
		}
	}
	if got := len(limiter.entries); got != loginLimiterMaxEntries {
		t.Fatalf("limiter entries=%d, want hard cap %d", got, loginLimiterMaxEntries)
	}

	if limiter.allowWindow("overflow-client", now, 2, time.Minute) {
		t.Fatal("new key was accepted after all active slots were consumed")
	}
	if got := len(limiter.entries); got != loginLimiterMaxEntries {
		t.Fatalf("limiter entries grew beyond cap: got %d, cap %d", got, loginLimiterMaxEntries)
	}

	// Existing clients retain their own bucket while the map is at capacity.
	if !limiter.allowWindow("client-0", now, 2, time.Minute) {
		t.Fatal("existing client was rejected before reaching its per-key limit")
	}
	if limiter.allowWindow("client-0", now, 2, time.Minute) {
		t.Fatal("existing client exceeded its per-key request limit")
	}

	// Once the window expires, stale entries are reclaimed and a new client can enter.
	if !limiter.allowWindow("new-client-after-expiry", now.Add(time.Minute), 2, time.Minute) {
		t.Fatal("new client remained blocked after old limiter entries expired")
	}
	if got := len(limiter.entries); got != 1 {
		t.Fatalf("expired entries were not reclaimed: got %d entries, want 1", got)
	}
}
