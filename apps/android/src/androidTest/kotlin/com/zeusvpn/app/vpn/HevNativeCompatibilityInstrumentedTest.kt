package com.zeusvpn.app.vpn

import androidx.test.ext.junit.runners.AndroidJUnit4
import hev.htproxy.TProxyService
import org.junit.Assert.assertFalse
import org.junit.Test
import org.junit.runner.RunWith

/**
 * Loads the packaged Hev JNI library on the device's actual Android API level.
 *
 * This deliberately does not start a tunnel: the test has no VpnService TUN
 * descriptor and validates only native-library loading and the idle JNI call.
 */
@RunWith(AndroidJUnit4::class)
class HevNativeCompatibilityInstrumentedTest {
    @Test
    fun nativeLibraryLoadsAndReportsIdleState() {
        assertFalse(
            "A newly installed app must not report an already-running Hev tunnel",
            TProxyService.TProxyIsRunning(),
        )
    }
}
