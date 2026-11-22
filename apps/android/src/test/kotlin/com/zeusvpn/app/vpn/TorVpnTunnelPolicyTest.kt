package com.zeusvpn.app.vpn

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class TorVpnTunnelPolicyTest {
    @Test
    fun usesTheSameDualStackAddressesAsHev() {
        assertEquals(HevTorTunnelConfig.TUNNEL_IPV4_ADDRESS, TorVpnTunnelPolicy.IPV4_ADDRESS)
        assertEquals(HevTorTunnelConfig.TUNNEL_IPV6_ADDRESS, TorVpnTunnelPolicy.IPV6_ADDRESS)
        assertEquals(HevTorTunnelConfig.MAP_DNS_ADDRESS, TorVpnTunnelPolicy.DNS_SERVER)
    }

    @Test
    fun routesBothAddressFamiliesIntoTheTunByDefault() {
        assertEquals("0.0.0.0", TorVpnTunnelPolicy.IPV4_DEFAULT_ROUTE)
        assertEquals(0, TorVpnTunnelPolicy.IPV4_DEFAULT_PREFIX)
        assertEquals("::", TorVpnTunnelPolicy.IPV6_DEFAULT_ROUTE)
        assertEquals(0, TorVpnTunnelPolicy.IPV6_DEFAULT_PREFIX)
    }

    @Test
    fun usesConservativeTunnelMtuAndExpectedAddressPrefixes() {
        assertEquals(1500, TorVpnTunnelPolicy.MTU)
        assertEquals(30, TorVpnTunnelPolicy.IPV4_PREFIX)
        assertEquals(126, TorVpnTunnelPolicy.IPV6_PREFIX)
        assertTrue(TorVpnTunnelPolicy.SESSION_NAME.isNotBlank())
    }
}
