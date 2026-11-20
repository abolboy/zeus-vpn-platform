package com.zeusvpn.app.vpn

import com.zeusvpn.app.network.SplitTunnelMode
import com.zeusvpn.app.network.SplitTunnelPolicy

/**
 * Validates selected packages before the WireGuard backend is asked to start.
 *
 * [isPackageAvailable] is supplied by the Android PackageManager adapter in
 * [WireGuardEngine], and can be replaced by a deterministic lookup in unit tests.
 */
internal object InstalledApplicationPolicyValidator {
    fun validate(
        policy: SplitTunnelPolicy,
        isPackageAvailable: (packageName: String) -> Boolean,
    ): SplitTunnelPolicy {
        policy.validate()
        if (policy.mode == SplitTunnelMode.ALL) return SplitTunnelPolicy.DEFAULT

        val unavailablePackages = policy.packageNames
            .filterNot(isPackageAvailable)
            .sorted()

        if (unavailablePackages.isNotEmpty()) {
            val shownPackages = unavailablePackages.take(5).joinToString(", ")
            val remainingCount = unavailablePackages.size - minOf(unavailablePackages.size, 5)
            val remainingMessage = if (remainingCount > 0) " و $remainingCount مورد دیگر" else ""
            throw IllegalArgumentException(
                "برخی برنامه‌های انتخاب‌شده نصب نیستند یا در Android قابل‌مشاهده نیستند: " +
                    "$shownPackages$remainingMessage. تنظیم مسیریابی را بازبینی کنید.",
            )
        }

        return policy
    }
}
