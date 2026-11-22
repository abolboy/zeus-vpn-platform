package com.zeusvpn.app.vpn

import android.net.VpnService

/**
 * Single source of truth for the addresses and routes shared by Android's TUN
 * and Hev. This policy is applied by the ZEUS-owned VpnService host.
 *
 * Both IP families are routed into TUN. If Tor/Hev cannot carry a destination,
 * the flow must fail inside the tunnel; the OS must not receive a direct route
 * fallback. Tor SOCKS IPv6 capability still requires runtime verification.
 */
internal object TorVpnTunnelPolicy {
    const val SESSION_NAME = "ZEUS Tor"
    const val MTU = 1500

    const val IPV4_ADDRESS = HevTorTunnelConfig.TUNNEL_IPV4_ADDRESS
    const val IPV4_PREFIX = 30
    const val IPV4_DEFAULT_ROUTE = "0.0.0.0"
    const val IPV4_DEFAULT_PREFIX = 0

    const val IPV6_ADDRESS = HevTorTunnelConfig.TUNNEL_IPV6_ADDRESS
    const val IPV6_PREFIX = 126
    const val IPV6_DEFAULT_ROUTE = "::"
    const val IPV6_DEFAULT_PREFIX = 0

    const val DNS_SERVER = HevTorTunnelConfig.MAP_DNS_ADDRESS

    /**
     * Configure a full-device dual-stack TUN and exclude ZEUS's own UID.
     * The exclusion is required so embedded Tor's outbound sockets do not
     * loop back into their own TUN. Consequently, ZEUS app traffic is outside
     * the Tor tunnel and this limitation must be disclosed in the UI.
     */
    @Throws(android.content.pm.PackageManager.NameNotFoundException::class)
    fun applyTo(builder: VpnService.Builder, packageName: String) {
        require(packageName.isNotBlank()) { "ZEUS package name must not be blank" }

        builder
            .setSession(SESSION_NAME)
            .setMtu(MTU)
            .addAddress(IPV4_ADDRESS, IPV4_PREFIX)
            .addRoute(IPV4_DEFAULT_ROUTE, IPV4_DEFAULT_PREFIX)
            .addAddress(IPV6_ADDRESS, IPV6_PREFIX)
            .addRoute(IPV6_DEFAULT_ROUTE, IPV6_DEFAULT_PREFIX)
            .addDnsServer(DNS_SERVER)
            .addDisallowedApplication(packageName)
    }
}
