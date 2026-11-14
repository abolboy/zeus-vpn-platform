package com.zeusvpn.app.network

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.IOException
import java.net.ConnectException
import java.net.HttpURLConnection
import java.net.NoRouteToHostException
import java.net.Proxy
import java.net.SocketTimeoutException
import java.net.URL
import java.net.UnknownHostException
import javax.net.ssl.SSLException

enum class TunnelProbeStatus { IDLE, CHECKING, REACHABLE, UNREACHABLE }

/** A non-sensitive failure category suitable for showing to the user. */
enum class TunnelProbeFailure {
    DNS,
    TIMEOUT,
    ROUTE,
    TLS,
    HTTP_STATUS,
    UNKNOWN,
}

data class TunnelConnectivityResult(
    val status: TunnelProbeStatus,
    val successfulProvider: String? = null,
    val attempts: Int = 0,
    val failure: TunnelProbeFailure? = null,
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
        var lastFailure = TunnelProbeFailure.UNKNOWN
        for (endpoint in ENDPOINTS) {
            attempts += 1
            try {
                if (probe(endpoint.url)) {
                    return@withContext TunnelConnectivityResult(
                        status = TunnelProbeStatus.REACHABLE,
                        successfulProvider = endpoint.label,
                        attempts = attempts,
                    )
                }
                lastFailure = TunnelProbeFailure.HTTP_STATUS
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                lastFailure = classifyFailure(error)
            }
        }
        TunnelConnectivityResult(
            status = TunnelProbeStatus.UNREACHABLE,
            attempts = attempts,
            failure = lastFailure,
        )
    }

    private data class Endpoint(val label: String, val url: String)

    private class UnexpectedProbeStatusException(val statusCode: Int) :
        IOException("Connectivity endpoint returned HTTP $statusCode")

    private companion object {
        val ENDPOINTS = listOf(
            Endpoint("Google", "https://connectivitycheck.gstatic.com/generate_204"),
            Endpoint("Cloudflare", "https://cp.cloudflare.com/generate_204"),
        )

        fun classifyFailure(error: Throwable): TunnelProbeFailure {
            val causes = generateSequence(error) { it.cause }.take(8).toList()
            return when {
                causes.any { it is UnknownHostException } -> TunnelProbeFailure.DNS
                causes.any { it is SocketTimeoutException } -> TunnelProbeFailure.TIMEOUT
                causes.any { it is NoRouteToHostException || it is ConnectException } ->
                    TunnelProbeFailure.ROUTE
                causes.any { it is SSLException } -> TunnelProbeFailure.TLS
                causes.any { it is UnexpectedProbeStatusException } -> TunnelProbeFailure.HTTP_STATUS
                else -> TunnelProbeFailure.UNKNOWN
            }
        }

        fun probeEndpoint(endpoint: String): Boolean {
            val connection = URL(endpoint).openConnection(Proxy.NO_PROXY) as HttpURLConnection
            return try {
                connection.requestMethod = "GET"
                connection.connectTimeout = 4_000
                connection.readTimeout = 4_000
                connection.instanceFollowRedirects = false
                connection.useCaches = false
                connection.setRequestProperty("Cache-Control", "no-cache, no-store")
                val status = connection.responseCode
                if (status != HttpURLConnection.HTTP_NO_CONTENT) {
                    throw UnexpectedProbeStatusException(status)
                }
                true
            } finally {
                connection.disconnect()
            }
        }
    }
}
