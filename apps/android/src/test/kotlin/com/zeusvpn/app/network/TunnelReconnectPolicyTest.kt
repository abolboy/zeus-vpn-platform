package com.zeusvpn.app.network

import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test

class TunnelReconnectPolicyTest {
    @Test
    fun usesExponentialBackoff() {
        assertEquals(2_000L, TunnelReconnectPolicy.delayAfterFailure(0))
        assertEquals(4_000L, TunnelReconnectPolicy.delayAfterFailure(1))
        assertEquals(8_000L, TunnelReconnectPolicy.delayAfterFailure(2))
        assertEquals(16_000L, TunnelReconnectPolicy.delayAfterFailure(3))
    }

    @Test
    fun capsBackoffAtSixtySeconds() {
        assertEquals(60_000L, TunnelReconnectPolicy.delayAfterFailure(5))
        assertEquals(60_000L, TunnelReconnectPolicy.delayAfterFailure(20))
    }

    @Test
    fun rejectsNegativeFailureCount() {
        assertThrows(IllegalArgumentException::class.java) {
            TunnelReconnectPolicy.delayAfterFailure(-1)
        }
    }
}
