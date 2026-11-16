package com.zeusvpn.app.network

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class TunnelVerificationPolicyTest {
    @Test
    fun verifiesOnlyWhenEngineHandshakeAndHttpsAreAllHealthy() {
        assertTrue(
            isTunnelConnectionVerified(
                engineRunning = true,
                latestHandshakeEpochMillis = 1_000_000L,
                nowEpochMillis = 1_120_000L,
                httpsProbeSucceeded = true,
            ),
        )
    }

    @Test
    fun doesNotTreatAnUpEngineAsAWorkingTunnel() {
        assertFalse(
            isTunnelConnectionVerified(
                engineRunning = true,
                latestHandshakeEpochMillis = 0L,
                nowEpochMillis = 1_120_000L,
                httpsProbeSucceeded = true,
            ),
        )
    }

    @Test
    fun requiresHttpsProbeToSucceed() {
        assertFalse(
            isTunnelConnectionVerified(
                engineRunning = true,
                latestHandshakeEpochMillis = 1_100_000L,
                nowEpochMillis = 1_120_000L,
                httpsProbeSucceeded = false,
            ),
        )
    }

    @Test
    fun rejectsStaleOrFutureHandshakes() {
        assertFalse(
            isTunnelConnectionVerified(
                engineRunning = true,
                latestHandshakeEpochMillis = 800_000L,
                nowEpochMillis = 1_120_001L,
                httpsProbeSucceeded = true,
            ),
        )
        assertFalse(
            isTunnelConnectionVerified(
                engineRunning = true,
                latestHandshakeEpochMillis = 1_130_000L,
                nowEpochMillis = 1_120_000L,
                httpsProbeSucceeded = true,
            ),
        )
    }

    @Test
    fun requiresEngineToBeRunning() {
        assertFalse(
            isTunnelConnectionVerified(
                engineRunning = false,
                latestHandshakeEpochMillis = 1_100_000L,
                nowEpochMillis = 1_120_000L,
                httpsProbeSucceeded = true,
            ),
        )
    }
}
