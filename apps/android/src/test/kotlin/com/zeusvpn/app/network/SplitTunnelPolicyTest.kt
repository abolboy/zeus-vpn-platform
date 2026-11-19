package com.zeusvpn.app.network

import com.wireguard.crypto.KeyPair
import com.zeusvpn.app.vpn.WireGuardProfileValidator
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Test

class SplitTunnelPolicyTest {
    @Test
    fun includesOnlySelectedApplicationsInWireGuardConfig() {
        val policy = SplitTunnelPolicy(
            mode = SplitTunnelMode.INCLUDE_SELECTED,
            packageNames = setOf("org.telegram.messenger", "com.instagram.android"),
        )

        val parsed = WireGuardProfileValidator.parse(
            SplitTunnelProfilePolicy.apply(profile(), policy),
        )

        assertEquals(
            setOf("com.instagram.android", "org.telegram.messenger"),
            parsed.getInterface().getIncludedApplications(),
        )
        assertEquals(emptySet<String>(), parsed.getInterface().getExcludedApplications())
    }

    @Test
    fun excludesSelectedApplicationsFromWireGuardConfig() {
        val policy = SplitTunnelPolicy(
            mode = SplitTunnelMode.EXCLUDE_SELECTED,
            packageNames = setOf("com.example.bank"),
        )

        val parsed = WireGuardProfileValidator.parse(
            SplitTunnelProfilePolicy.apply(profile(), policy),
        )

        assertEquals(setOf("com.example.bank"), parsed.getInterface().getExcludedApplications())
        assertEquals(emptySet<String>(), parsed.getInterface().getIncludedApplications())
    }

    @Test
    fun allModeRemovesExistingApplicationRoutingDirectives() {
        val imported = profile().replace(
            "[Interface]",
            "[Interface]\nIncludedApplications = com.example.app\nExcludedApplications = com.example.old",
        )

        val parsed = WireGuardProfileValidator.parse(
            SplitTunnelProfilePolicy.apply(imported, SplitTunnelPolicy.DEFAULT),
        )

        assertEquals(emptySet<String>(), parsed.getInterface().getIncludedApplications())
        assertEquals(emptySet<String>(), parsed.getInterface().getExcludedApplications())
    }

    @Test
    fun removesOldRoutingDirectivesFromCrLfProfiles() {
        val imported = profile()
            .replace("\n", "\r\n")
            .replace(
                "[Interface]\r\n",
                "[Interface]\r\nIncludedApplications = com.example.old\r\nExcludedApplications = com.example.older\r\n",
            )
        val routed = SplitTunnelProfilePolicy.apply(
            imported,
            SplitTunnelPolicy(SplitTunnelMode.EXCLUDE_SELECTED, setOf("com.example.new")),
        )
        val parsed = WireGuardProfileValidator.parse(routed)

        assertEquals(setOf("com.example.new"), parsed.getInterface().getExcludedApplications())
        assertEquals(emptySet<String>(), parsed.getInterface().getIncludedApplications())
    }

    @Test
    fun normalizesLoneCarriageReturnProfileLineEndings() {
        val imported = profile()
            .replace("\n", "\r")
            .replace(
                "[Interface]\r",
                "[Interface]\rIncludedApplications = com.example.old\r",
            )

        val routed = SplitTunnelProfilePolicy.apply(imported, SplitTunnelPolicy.DEFAULT)

        assertFalse(routed.contains("IncludedApplications", ignoreCase = true))
    }

    @Test
    fun changingModeReplacesOldDirectiveInsteadOfCombiningLists() {
        val imported = profile().replace(
            "[Interface]",
            "[Interface]\nIncludedApplications = com.example.old",
        )

        val parsed = WireGuardProfileValidator.parse(
            SplitTunnelProfilePolicy.apply(
                imported,
                SplitTunnelPolicy(SplitTunnelMode.EXCLUDE_SELECTED, setOf("com.example.new")),
            ),
        )

        assertEquals(setOf("com.example.new"), parsed.getInterface().getExcludedApplications())
        assertEquals(emptySet<String>(), parsed.getInterface().getIncludedApplications())
    }

    @Test
    fun rejectsInvalidPackageNames() {
        assertThrows(IllegalArgumentException::class.java) {
            SplitTunnelProfilePolicy.apply(
                profile(),
                SplitTunnelPolicy(SplitTunnelMode.EXCLUDE_SELECTED, setOf("com.example.app,AllowedIPs=0.0.0.0/0")),
            )
        }
    }

    @Test
    fun rejectsEmptyIncludeOnlySelection() {
        assertThrows(IllegalArgumentException::class.java) {
            SplitTunnelPolicy(SplitTunnelMode.INCLUDE_SELECTED, emptySet()).validate()
        }
    }

    @Test
    fun rejectsMoreThanOneHundredSelectedApplications() {
        val selected = (1..101).map { "com.example.app$it" }.toSet()
        assertThrows(IllegalArgumentException::class.java) {
            SplitTunnelPolicy(SplitTunnelMode.EXCLUDE_SELECTED, selected).validate()
        }
    }

    @Test
    fun rejectsProfilesWithoutInterfaceSection() {
        assertThrows(IllegalArgumentException::class.java) {
            SplitTunnelProfilePolicy.apply("[Peer]\nPublicKey = invalid", SplitTunnelPolicy.DEFAULT)
        }
    }

    private fun profile(): String {
        val clientKeys = KeyPair()
        val serverKeys = KeyPair()
        return buildString {
            appendLine("[Interface]")
            appendLine("PrivateKey = ${clientKeys.getPrivateKey().toBase64()}")
            appendLine("Address = 10.66.0.2/32")
            appendLine("DNS = 1.1.1.1")
            appendLine()
            appendLine("[Peer]")
            appendLine("PublicKey = ${serverKeys.getPublicKey().toBase64()}")
            appendLine("Endpoint = 127.0.0.1:51820")
            appendLine("AllowedIPs = 0.0.0.0/0")
        }
    }
}
