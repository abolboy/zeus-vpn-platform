package main

import "time"

const nodeHeartbeatStaleAfter = 90 * time.Second

// effectiveNodeStatus marks a previously online node offline when its mTLS
// heartbeat is stale. Pending, degraded, disabled, and never-seen states are
// preserved rather than inferred from missing data.
func effectiveNodeStatus(status string, lastSeenAt *time.Time, now time.Time, staleAfter time.Duration) string {
	if status != "online" || lastSeenAt == nil || staleAfter <= 0 {
		return status
	}
	if now.Sub(*lastSeenAt) > staleAfter {
		return "offline"
	}
	return status
}
