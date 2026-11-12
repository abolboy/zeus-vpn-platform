package com.zeusvpn.app.vpn

import com.wireguard.config.Config
import java.io.ByteArrayInputStream

/**
 * Parses and checks the minimum client-side fields before a profile can be stored or started.
 * This validates structure only; it cannot prove that the server key, endpoint, or handshake works.
 */
object WireGuardProfileValidator {
    private const val MAX_PROFILE_BYTES = 64 * 1024

    fun parse(profileText: String): Config {
        val profileBytes = profileText.toByteArray(Charsets.UTF_8)
        return try {
            parse(profileBytes)
        } finally {
            profileBytes.fill(0)
        }
    }

    fun parse(profileBytes: ByteArray): Config {
        if (profileBytes.isEmpty() || profileBytes.size > MAX_PROFILE_BYTES) {
            throw InvalidWireGuardProfileException()
        }

        return try {
            val config = Config.parse(ByteArrayInputStream(profileBytes))
            val interfaceSection = config.getInterface()
            val peers = config.getPeers()
            val hasEndpoint = peers.any { it.getEndpoint().isPresent }
            val hasAllowedIps = peers.any { it.getAllowedIps().isNotEmpty() }
            if (interfaceSection.getAddresses().isEmpty() || peers.isEmpty() || !hasEndpoint || !hasAllowedIps) {
                throw InvalidWireGuardProfileException()
            }
            config
        } catch (_: InvalidWireGuardProfileException) {
            throw InvalidWireGuardProfileException()
        } catch (_: Exception) {
            throw InvalidWireGuardProfileException()
        }
    }
}

class InvalidWireGuardProfileException : Exception()
