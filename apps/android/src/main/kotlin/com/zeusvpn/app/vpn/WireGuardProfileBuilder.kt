package com.zeusvpn.app.vpn

/**
 * Builds the client profile from server-assigned public connection details and a
 * private key that was generated locally on this device.
 */
object WireGuardProfileBuilder {
    fun build(
        clientPrivateKey: String,
        clientIPv4: String,
        clientIPv6: String,
        serverPublicKey: String,
        endpointHost: String,
        endpointPort: Int,
    ): String {
        require(endpointHost.isNotBlank() && endpointHost.length <= 253) {
            "Endpoint host is invalid"
        }
        require(!endpointHost.any { it.isWhitespace() } && !endpointHost.contains('/')) {
            "Endpoint host is invalid"
        }
        require(endpointPort in 1..65535) { "Endpoint port is invalid" }

        val formattedHost = if (endpointHost.contains(':') && !endpointHost.startsWith("[")) {
            "[$endpointHost]"
        } else {
            endpointHost
        }
        val profile = """
            [Interface]
            PrivateKey = $clientPrivateKey
            Address = $clientIPv4/32, $clientIPv6/128
            DNS = 1.1.1.1, 1.0.0.1
            MTU = 1380

            [Peer]
            PublicKey = $serverPublicKey
            AllowedIPs = 0.0.0.0/0, ::/0
            Endpoint = $formattedHost:$endpointPort
            PersistentKeepalive = 25
        """.trimIndent() + "\n"

        WireGuardProfileValidator.parse(profile)
        return profile
    }
}
