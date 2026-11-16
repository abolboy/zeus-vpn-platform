package com.zeusvpn.app.network

/**
 * Android per-application routing policy for the WireGuard VpnService.
 *
 * ALL routes all apps through the tunnel. INCLUDE_SELECTED only routes the
 * selected launchable packages through it. EXCLUDE_SELECTED routes every app
 * through the tunnel except the selected packages.
 */
enum class SplitTunnelMode {
    ALL,
    INCLUDE_SELECTED,
    EXCLUDE_SELECTED,
}

data class SplitTunnelPolicy(
    val mode: SplitTunnelMode = SplitTunnelMode.ALL,
    val packageNames: Set<String> = emptySet(),
) {
    fun validate() {
        if (packageNames.size > MAX_APPLICATIONS) {
            throw IllegalArgumentException("Too many applications selected for split tunneling.")
        }
        if (packageNames.any { it.length > MAX_PACKAGE_NAME_LENGTH || !PACKAGE_NAME.matches(it) }) {
            throw IllegalArgumentException("A selected application package name is invalid.")
        }
        if (mode == SplitTunnelMode.INCLUDE_SELECTED && packageNames.isEmpty()) {
            throw IllegalArgumentException("Select at least one application to send through the VPN.")
        }
    }

    companion object {
        const val MAX_APPLICATIONS = 100
        const val MAX_PACKAGE_NAME_LENGTH = 255
        private val PACKAGE_NAME = Regex("^[A-Za-z][A-Za-z0-9_]*(\\.[A-Za-z0-9_]+)+$")
        val DEFAULT = SplitTunnelPolicy()
    }
}

/**
 * Rewrites only the Android-specific IncludedApplications / ExcludedApplications
 * directives inside [Interface]. The WireGuard parser validates the result
 * before the backend receives it. Existing routing directives are always removed
 * first so a user change cannot leave conflicting allow/deny lists behind.
 */
object SplitTunnelProfilePolicy {
    private val routingDirective = Regex("^\\s*(IncludedApplications|ExcludedApplications)\\s*=.*$", RegexOption.IGNORE_CASE)
    private val sectionHeader = Regex("^\\s*\\[[^]]+\\]\\s*$")

    fun apply(profileText: String, policy: SplitTunnelPolicy): String {
        policy.validate()
        require(profileText.isNotBlank()) { "WireGuard profile is empty." }

        val lines = profileText.split('\n')
        val interfaceIndex = lines.indexOfFirst { it.trim().equals("[Interface]", ignoreCase = true) }
        require(interfaceIndex >= 0) { "WireGuard profile has no Interface section." }

        val directives = when (policy.mode) {
            SplitTunnelMode.ALL -> emptyList()
            SplitTunnelMode.INCLUDE_SELECTED -> listOf(
                "IncludedApplications = " + policy.packageNames.sorted().joinToString(", "),
            )
            SplitTunnelMode.EXCLUDE_SELECTED -> if (policy.packageNames.isEmpty()) {
                emptyList()
            } else {
                listOf("ExcludedApplications = " + policy.packageNames.sorted().joinToString(", "))
            }
        }

        return buildString(profileText.length + directives.sumOf { it.length + 1 }) {
            var inInterface = false
            var inserted = false
            lines.forEach { line ->
                if (line.trim().equals("[Interface]", ignoreCase = true)) {
                    inInterface = true
                } else if (inInterface && sectionHeader.matches(line)) {
                    directives.forEach { append(it).append('\n') }
                    inserted = true
                    inInterface = false
                }

                if (!(inInterface && routingDirective.matches(line))) {
                    append(line).append('\n')
                }
            }
            if (inInterface && !inserted) {
                directives.forEach { append(it).append('\n') }
            }
            if (!profileText.endsWith('\n')) {
                val result = toString()
                if (result.endsWith("\n\n")) {
                    // Preserve the original single-line ending behavior without adding blank lines.
                }
            }
        }.let { output ->
            if (profileText.endsWith('\n')) output else output.removeSuffix("\n")
        }
    }
}
