package com.zeusvpn.app.network

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.net.HttpURLConnection
import java.net.Proxy
import java.net.URL

enum class TunnelProbeStatus { IDLE, CHECKING, REACHABLE, UNREACHABLE }

data class TunnelConnectivityResult(
    val status: TunnelProbeStatus,
    val successfulProvider: String? = null,
    val attempts: Int = 0,
) {
    companion object {
        val IDLE = TunnelConnectivityResult(TunnelProbeStatus.IDLE)
        val CHECKING = TunnelConnectivityResult(TunnelProbeStatus.CHECKING)
    }
}

/**
 * Makes ordinary HTTPS requests from the app process while the VPN is active.
 * This is a reachability signal, not proof that every app or destination works.
 */
class TunnelConnectivityChecker(
    private val probe: (String) -> Boolean = ::probeEndpoint,
) {
    suspend fun check(): TunnelConnectivityResult = withContext(Dispatchers.IO) {
        var attempts = 0
        for (endpoint in ENDPOINTS) {
            attempts += 1
            val reachable = runCatching { probe(endpoint.url) }.getOrDefault(false)
            if (reachable) {
                return@withContext TunnelConnectivityResult(
                    status = TunnelProbeStatus.REACHABLE,
                    successfulProvider = endpoint.label,
                    attempts = attempts,
                )
            }
        }
        TunnelConnectivityResult(TunnelProbeStatus.UNREACHABLE, attempts = attempts)
    }

    private data class Endpoint(val label: String, val url: String)

    private companion object {
        val ENDPOINTS = listOf(
            Endpoint("Google", "https://connectivitycheck.gstatic.com/generate_204"),
            Endpoint("Cloudflare", "https://cp.cloudflare.com/generate_204"),
        )

        fun probeEndpoint(endpoint: String): Boolean {
            val connection = URL(endpoint).openConnection(Proxy.NO_PROXY) as HttpURLConnection
            return try {
                connection.requestMethod = "GET"
                connection.connectTimeout = 4_000
                connection.readTimeout = 4_000
                connection.instanceFollowRedirects = false
                connection.useCaches = false
                connection.setRequestProperty("Cache-Control", "no-cache, no-store")
                connection.responseCode == HttpURLConnection.HTTP_NO_CONTENT
            } finally {
                connection.disconnect()
            }
        }
    }
}
