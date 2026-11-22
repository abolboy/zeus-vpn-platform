package com.zeusvpn.app.vpn

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class TorDaemonControllerTest {
    @Test
    fun parsesQuotedIpv4LoopbackListener() {
        assertEquals(
            TorSocksEndpoint("127.0.0.1", 9050),
            parseTorSocksEndpoint("\"127.0.0.1:9050\""),
        )
    }

    @Test
    fun parsesBracketedIpv6LoopbackListener() {
        assertEquals(
            TorSocksEndpoint("::1", 9150),
            parseTorSocksEndpoint("\"[::1]:9150\""),
        )
    }

    @Test
    fun rejectsNonLoopbackListenerAddresses() {
        assertNull(parseTorSocksEndpoint("\"0.0.0.0:9050\""))
        assertNull(parseTorSocksEndpoint("\"192.168.1.20:9050\""))
    }

    @Test
    fun rejectsMissingMalformedAndOutOfRangePorts() {
        assertNull(parseTorSocksEndpoint(null))
        assertNull(parseTorSocksEndpoint(""))
        assertNull(parseTorSocksEndpoint("\"127.0.0.1\""))
        assertNull(parseTorSocksEndpoint("\"127.0.0.1:0\""))
        assertNull(parseTorSocksEndpoint("\"127.0.0.1:65536\""))
        assertNull(parseTorSocksEndpoint("\"127.0.0.1:not-a-port\""))
    }
}
