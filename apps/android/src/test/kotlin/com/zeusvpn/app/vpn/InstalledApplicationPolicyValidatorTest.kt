package com.zeusvpn.app.vpn

import com.zeusvpn.app.network.SplitTunnelMode
import com.zeusvpn.app.network.SplitTunnelPolicy
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.assertThrows
import org.junit.Test

class InstalledApplicationPolicyValidatorTest {
    @Test
    fun rejectsSelectedPackageWhenPackageManagerCannotResolveIt() {
        val availablePackage = "com.example.visible"
        val unavailablePackage = "com.example.hidden"
        val policy = SplitTunnelPolicy(
            mode = SplitTunnelMode.EXCLUDE_SELECTED,
            packageNames = setOf(availablePackage, unavailablePackage),
        )

        // Models PackageManager.getApplicationInfo succeeding for one app and
        // failing to resolve the other (not installed or hidden by package visibility).
        val error = assertThrows(IllegalArgumentException::class.java) {
            InstalledApplicationPolicyValidator.validate(policy) { packageName ->
                packageName == availablePackage
            }
        }

        assertTrue(error.message.orEmpty().contains(unavailablePackage))
        assertTrue(error.message.orEmpty().contains("قابل‌مشاهده نیست"))
    }

    @Test
    fun acceptsSelectedPackagesWhenPackageManagerResolvesAllOfThem() {
        val policy = SplitTunnelPolicy(
            mode = SplitTunnelMode.INCLUDE_SELECTED,
            packageNames = setOf("com.example.one", "com.example.two"),
        )

        val validated = InstalledApplicationPolicyValidator.validate(policy) { true }

        assertEquals(policy, validated)
    }

    @Test
    fun allModeDoesNotQueryPackageManager() {
        val validated = InstalledApplicationPolicyValidator.validate(
            SplitTunnelPolicy.DEFAULT,
        ) {
            throw AssertionError("Package lookup must not run in ALL mode")
        }

        assertEquals(SplitTunnelPolicy.DEFAULT, validated)
    }
}
