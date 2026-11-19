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
 * before the backend receives it. Existing directives are removed first, so
 * changing modes cannot leave conflicting allow/deny lists behind.
 */
object SplitTunnelProfilePolicy {
    private val routingDirective = Regex("^\\s*(IncludedApplications|ExcludedApplications)\\s*=.*$", RegexOption.IGNORE_CASE)
    private val sectionHeader = Regex("^\\s*\\[[^]]+\\]\\s*$")

    fun apply(profileText: String, policy: SplitTunnelPolicy): String {
        policy.validate()
        require(profileText.isNotBlank()) { "WireGuard profile is empty." }

        val normalizedProfileText = profileText.replace("\r\n", "\n").replace('\r', '\n')
        val lines = normalizedProfileText.split('\n')
        val interfaceIndex = lines.indexOfFirst { it.trim().equals("[Interface]", ignoreCase = true) }
        require(interfaceIndex >= 0) { "WireGuard profile has no Interface section." }

        var interfaceEnd = interfaceIndex + 1
        while (interfaceEnd < lines.size && !sectionHeader.matches(lines[interfaceEnd])) {
            interfaceEnd++
        }

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

        val interfaceLines = lines.subList(interfaceIndex + 1, interfaceEnd)
            .filterNot { routingDirective.matches(it) }

        return buildList {
            addAll(lines.subList(0, interfaceIndex + 1))
            addAll(interfaceLines)
            addAll(directives)
            addAll(lines.subList(interfaceEnd, lines.size))
        }.joinToString("\n")
    }
}
