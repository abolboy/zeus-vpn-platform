package com.zeusvpn.app.vpn

import com.wireguard.crypto.KeyPair
import org.junit.Assert.assertTrue
import org.junit.Assert.assertThrows
import org.junit.Test

class WireGuardProfileBuilderTest {
    @Test
    fun buildsFullTunnelProfileFromClientLocalKeyAndServerPublicDetails() {
        val clientKeys = KeyPair()
        val serverKeys = KeyPair()

        val profile = WireGuardProfileBuilder.build(
            clientPrivateKey = clientKeys.privateKey.toBase64(),
            clientIPv4 = "10.77.77.2",
            clientIPv6 = "fd77:77:77::2",
            serverPublicKey = serverKeys.publicKey.toBase64(),
            endpointHost = "vpn.example.test",
            endpointPort = 51820,
        )

        assertTrue(profile.contains("Address = 10.77.77.2/32, fd77:77:77::2/128"))
        assertTrue(profile.contains("DNS = 1.1.1.1, 1.0.0.1"))
        assertTrue(profile.contains("MTU = 1380"))
        assertTrue(profile.contains("AllowedIPs = 0.0.0.0/0, ::/0"))
        assertTrue(profile.contains("Endpoint = vpn.example.test:51820"))
        assertTrue(profile.contains("PersistentKeepalive = 25"))
        assertTrue(profile.contains("PrivateKey = \${clientKeys.privateKey.toBase64()}"))
        assertTrue(profile.contains("PublicKey = \${serverKeys.publicKey.toBase64()}"))
    }

    @Test
    fun bracketsIpv6EndpointLiterals() {
        val clientKeys = KeyPair()
        val serverKeys = KeyPair()
        val profile = WireGuardProfileBuilder.build(
            clientPrivateKey = clientKeys.privateKey.toBase64(),
            clientIPv4 = "10.77.77.3",
            clientIPv6 = "fd77:77:77::3",
            serverPublicKey = serverKeys.publicKey.toBase64(),
            endpointHost = "2001:db8::10",
            endpointPort = 51820,
        )
        assertTrue(profile.contains("Endpoint = [2001:db8::10]:51820"))
    }

    @Test
    fun rejectsInvalidEndpointPort() {
        val clientKeys = KeyPair()
        val serverKeys = KeyPair()
        assertThrows(IllegalArgumentException::class.java) {
            WireGuardProfileBuilder.build(
                clientPrivateKey = clientKeys.privateKey.toBase64(),
                clientIPv4 = "10.77.77.2",
                clientIPv6 = "fd77:77:77::2",
                serverPublicKey = serverKeys.publicKey.toBase64(),
                endpointHost = "vpn.example.test",
                endpointPort = 70000,
            )
        }
    }

    @Test
    fun rejectsNonIpAddressAllocations() {
        val clientKeys = KeyPair()
        val serverKeys = KeyPair()
        assertThrows(InvalidWireGuardProfileException::class.java) {
            WireGuardProfileBuilder.build(
                clientPrivateKey = clientKeys.privateKey.toBase64(),
                clientIPv4 = "not-an-ip",
                clientIPv6 = "fd77:77:77::2",
                serverPublicKey = serverKeys.publicKey.toBase64(),
                endpointHost = "vpn.example.test",
                endpointPort = 51820,
            )
        }
    }
}
