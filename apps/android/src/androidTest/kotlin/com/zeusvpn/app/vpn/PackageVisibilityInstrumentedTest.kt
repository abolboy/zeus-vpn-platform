package com.zeusvpn.app.vpn

import android.content.Intent
import android.content.pm.PackageManager
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import com.zeusvpn.app.network.SplitTunnelMode
import com.zeusvpn.app.network.SplitTunnelPolicy
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class PackageVisibilityInstrumentedTest {
    @Test
    fun installedNonLauncherAppIsHiddenAndRejectedBySplitTunnelValidation() {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val packageManager = context.packageManager

        // CI installs this fixture APK and verifies it with adb before instrumentation.
        // It has no launcher activity, uses a different signing certificate, and is not
        // declared in the target app's <queries> section.
        val launcherIntent = Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER)
        val visibleLauncherPackages = packageManager
            .queryIntentActivities(launcherIntent, 0)
            .map { it.activityInfo.packageName }
            .toSet()

        assertTrue(
            "The declared launcher query should expose the Android Settings app",
            "com.android.settings" in visibleLauncherPackages,
        )
        assertTrue(
            "The target app must remain visible to its own PackageManager",
            runCatching { packageManager.getApplicationInfo(context.packageName, 0) }.isSuccess,
        )
        assertFalse(
            "The non-launcher fixture must not appear in launcher query results",
            FIXTURE_PACKAGE in visibleLauncherPackages,
        )

        val lookupError = assertThrows(PackageManager.NameNotFoundException::class.java) {
            packageManager.getApplicationInfo(FIXTURE_PACKAGE, 0)
        }
        assertNotNull(lookupError)

        val policy = SplitTunnelPolicy(
            mode = SplitTunnelMode.INCLUDE_SELECTED,
            packageNames = setOf(FIXTURE_PACKAGE),
        )
        val validationError = assertThrows(IllegalArgumentException::class.java) {
            InstalledApplicationPolicyValidator.validate(policy) { packageName ->
                runCatching { packageManager.getApplicationInfo(packageName, 0) }.isSuccess
            }
        }

        assertTrue(validationError.message.orEmpty().contains(FIXTURE_PACKAGE))
        assertTrue(validationError.message.orEmpty().contains("قابل‌مشاهده نیست"))
    }

    private companion object {
        const val FIXTURE_PACKAGE = "com.zeusvpn.visibilityfixture"
    }
}
