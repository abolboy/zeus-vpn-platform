package main

import (
	"testing"
	"time"
)

func TestEffectiveNodeStatus(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-30 * time.Second)
	stale := now.Add(-91 * time.Second)
	future := now.Add(time.Minute)
	cutoff := now.Add(-nodeHeartbeatStaleAfter)

	tests := []struct {
		name       string
		status     string
		lastSeen   *time.Time
		staleAfter time.Duration
		want       string
	}{
		{name: "fresh online heartbeat", status: "online", lastSeen: &fresh, staleAfter: nodeHeartbeatStaleAfter, want: "online"},
		{name: "stale online heartbeat", status: "online", lastSeen: &stale, staleAfter: nodeHeartbeatStaleAfter, want: "offline"},
		{name: "exact heartbeat cutoff remains online", status: "online", lastSeen: &cutoff, staleAfter: nodeHeartbeatStaleAfter, want: "online"},
		{name: "future heartbeat remains online", status: "online", lastSeen: &future, staleAfter: nodeHeartbeatStaleAfter, want: "online"},
		{name: "never seen pending node remains pending", status: "pending", lastSeen: nil, staleAfter: nodeHeartbeatStaleAfter, want: "pending"},
		{name: "disabled node remains disabled", status: "disabled", lastSeen: &stale, staleAfter: nodeHeartbeatStaleAfter, want: "disabled"},
		{name: "degraded node remains degraded", status: "degraded", lastSeen: &stale, staleAfter: nodeHeartbeatStaleAfter, want: "degraded"},
		{name: "invalid timeout preserves stored state", status: "online", lastSeen: &stale, staleAfter: 0, want: "online"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := effectiveNodeStatus(test.status, test.lastSeen, now, test.staleAfter); got != test.want {
				t.Fatalf("effectiveNodeStatus() = %q, want %q", got, test.want)
			}
		})
	}
}
