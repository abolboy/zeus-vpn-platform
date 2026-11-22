package com.zeusvpn.app.vpn

import org.junit.Assert.assertTrue
import org.junit.Assert.assertThrows
import org.junit.Assert.assertFalse
import org.junit.Test

class HevTorTunnelConfigTest {
    @Test
    fun rendersLoopbackTorSocksAndFakeDnsConfiguration() {
        val config = HevTorTunnelConfig.render(TorSocksEndpoint("127.0.0.1", 9050))

        assertTrue(config.contains("address: '127.0.0.1'"))
        assertTrue(config.contains("port: 9050"))
        assertTrue(config.contains("udp: 'udp'"))
        assertTrue(config.contains("address: ${HevTorTunnelConfig.MAP_DNS_ADDRESS}"))
        assertTrue(config.contains("network: ${HevTorTunnelConfig.MAP_DNS_NETWORK}"))
        assertTrue(config.contains("netmask: ${HevTorTunnelConfig.MAP_DNS_NETMASK}"))
        assertTrue(config.contains("max-session-count: 4096"))
        assertTrue(config.endsWith("\n"))
    }

    @Test
    fun supportsOnlyLoopbackTorSocksEndpoints() {
        assertThrows(IllegalArgumentException::class.java) {
            HevTorTunnelConfig.render(TorSocksEndpoint("0.0.0.0", 9050))
        }
        assertThrows(IllegalArgumentException::class.java) {
            HevTorTunnelConfig.render(TorSocksEndpoint("192.168.1.10", 9050))
        }
    }

    @Test
    fun rejectsInvalidTorSocksPorts() {
        assertThrows(IllegalArgumentException::class.java) {
            HevTorTunnelConfig.render(TorSocksEndpoint("127.0.0.1", 0))
        }
        assertThrows(IllegalArgumentException::class.java) {
            HevTorTunnelConfig.render(TorSocksEndpoint("127.0.0.1", 65_536))
        }
    }

    @Test
    fun supportsBracketFreeIpv6LoopbackConfigValue() {
        val config = HevTorTunnelConfig.render(TorSocksEndpoint("::1", 9150))

        assertTrue(config.contains("address: '::1'"))
        assertTrue(config.contains("port: 9150"))
    }
}


class HevTunOwnershipDecisionTest {
    @Test
    fun retainsTunWhenJniStartThrowsAfterEntry() {
        assertTrue(shouldRetainTunDescriptorAfterStartFailure(nativeCallEntered = true, nativeCallReturned = false))
    }

    @Test
    fun releasesTunWhenFailureHappensBeforeJniEntry() {
        assertFalse(shouldRetainTunDescriptorAfterStartFailure(nativeCallEntered = false, nativeCallReturned = false))
    }

    @Test
    fun releasesTunWhenJniReturnsDefinitiveResult() {
        assertFalse(shouldRetainTunDescriptorAfterStartFailure(nativeCallEntered = true, nativeCallReturned = true))
    }
}
