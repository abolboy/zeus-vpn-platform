package com.zeusvpn.app.network

import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class TunnelConnectivityCheckerTest {
    @Test
    fun succeedsOnFirstReachableEndpoint() = runBlocking {
        var calls = 0
        val result = TunnelConnectivityChecker { calls += 1; true }.check()
        assertEquals(TunnelProbeStatus.REACHABLE, result.status)
        assertEquals("Google", result.successfulProvider)
        assertEquals(1, result.attempts)
        assertEquals(1, calls)
    }

    @Test
    fun fallsBackToSecondEndpointWhenFirstFails() = runBlocking {
        var calls = 0
        val result = TunnelConnectivityChecker { calls += 1; calls == 2 }.check()
        assertEquals(TunnelProbeStatus.REACHABLE, result.status)
        assertEquals("Cloudflare", result.successfulProvider)
        assertEquals(2, result.attempts)
        assertEquals(2, calls)
    }

    @Test
    fun reportsUnreachableWhenBothEndpointsFail() = runBlocking {
        var calls = 0
        val result = TunnelConnectivityChecker { calls += 1; false }.check()
        assertEquals(TunnelProbeStatus.UNREACHABLE, result.status)
        assertEquals(2, result.attempts)
        assertEquals(2, calls)
        assertEquals(null, result.successfulProvider)
    }

    @Test
    fun continuesFallbackAfterProbeException() = runBlocking {
        var calls = 0
        val result = TunnelConnectivityChecker {
            calls += 1
            if (calls == 1) error("simulated DNS failure")
            true
        }.check()
        assertTrue(result.status == TunnelProbeStatus.REACHABLE)
        assertEquals("Cloudflare", result.successfulProvider)
        assertEquals(2, result.attempts)
    }
}
