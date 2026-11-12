package com.zeusvpn.app.vpn

import com.wireguard.crypto.KeyPair
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test

class WireGuardProfileValidatorTest {
    @Test
    fun acceptsWellFormedClientProfile() {
        val parsed = WireGuardProfileValidator.parse(profile())
        assertEquals(1, parsed.getPeers().size)
        assertEquals(1, parsed.getInterface().getAddresses().size)
        assertEquals(1, parsed.getPeers().first().getAllowedIps().size)
    }

    @Test
    fun rejectsProfileWithoutPeerEndpoint() {
        assertThrows(InvalidWireGuardProfileException::class.java) {
            WireGuardProfileValidator.parse(profile(endpoint = null))
        }
    }

    @Test
    fun rejectsProfileWithoutInterfaceAddress() {
        assertThrows(InvalidWireGuardProfileException::class.java) {
            WireGuardProfileValidator.parse(profile(address = null))
        }
    }

    @Test
    fun rejectsProfileWithoutAllowedIps() {
        assertThrows(InvalidWireGuardProfileException::class.java) {
            WireGuardProfileValidator.parse(profile(allowedIps = null))
        }
    }

    @Test
    fun rejectsMalformedPrivateKey() {
        val malformed = profile().replace(Regex("PrivateKey = [^\\n]+"), "PrivateKey = not-a-key")
        assertThrows(InvalidWireGuardProfileException::class.java) {
            WireGuardProfileValidator.parse(malformed)
        }
    }

    @Test
    fun rejectsOversizedProfile() {
        assertThrows(InvalidWireGuardProfileException::class.java) {
            WireGuardProfileValidator.parse(ByteArray(64 * 1024 + 1) { 'x'.code.toByte() })
        }
    }

    private fun profile(
        address: String? = "10.66.0.2/32",
        endpoint: String? = "127.0.0.1:51820",
        allowedIps: String? = "0.0.0.0/0",
    ): String {
        // The keys are ephemeral test material generated for each test run, never committed.
        val clientKeys = KeyPair()
        val serverKeys = KeyPair()
        return buildString {
            appendLine("[Interface]")
            appendLine("PrivateKey = ${clientKeys.getPrivateKey().toBase64()}")
            if (address != null) appendLine("Address = $address")
            appendLine()
            appendLine("[Peer]")
            appendLine("PublicKey = ${serverKeys.getPublicKey().toBase64()}")
            if (endpoint != null) appendLine("Endpoint = $endpoint")
            if (allowedIps != null) appendLine("AllowedIPs = $allowedIps")
        }
    }
}
