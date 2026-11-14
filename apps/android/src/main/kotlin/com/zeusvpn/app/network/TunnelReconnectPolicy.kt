package com.zeusvpn.app.network

/** Bounded exponential delay used by the in-app tunnel recovery loop. */
object TunnelReconnectPolicy {
    private const val INITIAL_DELAY_MS = 2_000L
    private const val MAX_DELAY_MS = 60_000L

    /**
     * Number of consecutive failures before the next attempt.
     * 0 -> 2s, 1 -> 4s, 2 -> 8s, capped at 60s.
     */
    fun delayAfterFailure(consecutiveFailures: Int): Long {
        require(consecutiveFailures >= 0) { "Failure count cannot be negative" }
        val shift = consecutiveFailures.coerceAtMost(10)
        return (INITIAL_DELAY_MS * (1L shl shift)).coerceAtMost(MAX_DELAY_MS)
    }
}
