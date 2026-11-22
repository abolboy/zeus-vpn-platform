package com.zeusvpn.app.vpn

import android.content.Context
import android.os.ParcelFileDescriptor
import hev.htproxy.TProxyService
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import java.io.File
import java.io.FileOutputStream
import java.io.IOException

/**
 * Generates the Hev configuration for the embedded Tor SOCKS endpoint.
 *
 * The fake-DNS address and synthetic range must also be routed into the same
 * Android TUN by the eventual VpnService host. This class does not create or
 * configure a VpnService and is not, by itself, a complete Tor VPN.
 */
internal object HevTorTunnelConfig {
    const val MAP_DNS_ADDRESS = "198.18.0.2"
    const val MAP_DNS_NETWORK = "100.64.0.0"
    const val MAP_DNS_NETMASK = "255.192.0.0"
    const val MAP_DNS_PREFIX_LENGTH = 10

    fun render(endpoint: TorSocksEndpoint): String {
        require(endpoint.host == "127.0.0.1" || endpoint.host == "::1") {
            "Hev Tor upstream must be a loopback SOCKS endpoint"
        }
        require(endpoint.port in 1..65_535) {
            "Hev Tor upstream port is invalid"
        }

        // Tor does not implement SOCKS5 UDP ASSOCIATE. Hev must use the
        // standard UDP relay mode so a rejected association terminates the
        // UDP session; never enable Hev's UDP-over-TCP extension here.
        return """
            tunnel:
              mtu: 1500
              icmp: 'off'
            socks5:
              address: '${endpoint.host}'
              port: ${endpoint.port}
              udp: 'udp'
            mapdns:
              address: $MAP_DNS_ADDRESS
              port: 53
              network: $MAP_DNS_NETWORK
              netmask: $MAP_DNS_NETMASK
              cache-size: 10000
            misc:
              connect-timeout: 10000
              tcp-read-write-timeout: 300000
              udp-read-write-timeout: 60000
              max-session-count: 4096
              log-level: warn
        """.trimIndent() + "\n"
    }
}

/**
 * Owns the TUN ParcelFileDescriptor from the moment start() is invoked.
 *
 * Hev's Android JNI receives a borrowed descriptor: upstream tunnel_fini()
 * explicitly leaves externally supplied descriptors to the caller. Therefore
 * this runtime keeps the descriptor open until TProxyStopService() has joined
 * the native worker successfully, then closes it. If JNI start throws after
 * entry, native state is uncertain and the descriptor/config are retained
 * fail-closed until process exit.
 *
 * A successful start means only that Hev created its worker thread. It does
 * not prove that configuration parsing succeeded or traffic is forwarding.
 * The shared VpnService owner, routes, lockdown policy, and data-plane tests
 * remain separate integration gates.
 */
object HevTun2SocksRuntime {
    private val mutex = Mutex()

    private var activeTun: ParcelFileDescriptor? = null
    private var activeConfig: File? = null
    private var shutdownUnconfirmed = false

    /**
     * Takes ownership of [tun] immediately when called. The caller must not close or
     * reuse it after invocation, including when this suspend call is cancelled.
     *
     * On a failure before entering JNI, this runtime closes the descriptor. If the
     * JNI call throws before returning, native startup state is unknown: retain both
     * the descriptor and config until process exit rather than risk closing resources
     * still referenced by a native worker. On a normal false return, JNI confirms
     * that no worker was created, so the descriptor can be closed safely.
     */
    suspend fun start(
        context: Context,
        tun: ParcelFileDescriptor,
        endpoint: TorSocksEndpoint,
    ) = withContext(NonCancellable + Dispatchers.IO) {
        mutex.withLock {
            val descriptorAlreadyOwned = activeTun === tun
            var config: File? = null
            var nativeCallEntered = false
            var nativeCallReturned = false

            try {
                check(!shutdownUnconfirmed) {
                    "A previous Hev shutdown was not confirmed; restart the app before another TUN session"
                }
                check(activeTun == null) { "A Hev TUN session is already owned by ZEUS" }
                check(tun.fileDescriptor.valid()) { "The supplied TUN descriptor is invalid" }
                check(!TProxyService.TProxyIsRunning()) {
                    "The process-wide Hev native tunnel is already running"
                }

                val createdConfig = writeConfig(context.applicationContext, endpoint)
                config = createdConfig
                val configPath = createdConfig.absolutePath
                val tunFd = tun.fd
                nativeCallEntered = true
                val started = TProxyService.TProxyStartService(configPath, tunFd)
                nativeCallReturned = true

                if (!started) {
                    throw IllegalStateException("Hev refused to start its native worker")
                }

                activeTun = tun
                activeConfig = createdConfig
            } catch (error: Throwable) {
                if (shouldRetainTunDescriptorAfterStartFailure(nativeCallEntered, nativeCallReturned)) {
                    // JNI may have created a worker before throwing. Keep every resource
                    // it could reference and block further starts until process exit.
                    activeTun = tun
                    activeConfig = config
                    shutdownUnconfirmed = true
                } else {
                    config?.delete()
                    if (!descriptorAlreadyOwned) {
                        runCatching { tun.close() }.onFailure(error::addSuppressed)
                    }
                }
                throw error
            }
        }
    }

    /**
     * Stops and joins the native worker before releasing the caller-owned TUN fd.
     * If the native join fails or JNI throws, descriptor ownership is retained
 * permanently for this process: upstream JNI clears its joinable flag even when
 * pthread_join fails, and an exception leaves worker termination unknown.
     */
    suspend fun stop() = withContext(NonCancellable + Dispatchers.IO) {
        mutex.withLock {
            check(!shutdownUnconfirmed) {
                "Hev shutdown is unconfirmed; the TUN descriptor must remain open until process exit"
            }
            val tun = activeTun
            if (tun == null) {
                check(!TProxyService.TProxyIsRunning()) {
                    "Hev is running without a TUN descriptor owned by ZEUS"
                }
                return@withLock
            }

            val stopAttempt = attemptHevNativeStop {
                TProxyService.TProxyStopService()
            }
            if (!stopAttempt.confirmed) {
                // A false result or a JNI/linkage exception leaves worker termination
                // unconfirmed. Never retry or close the borrowed TUN descriptor.
                shutdownUnconfirmed = true
                throw IllegalStateException(
                    "Hev native shutdown could not be confirmed; TUN descriptor retained until process exit",
                    stopAttempt.failure,
                )
            }

            tun.close()
            activeTun = null
            activeConfig?.delete()
            activeConfig = null
        }
    }

    /** Reports native worker state only; it is not proof of end-to-end forwarding. */
    suspend fun isRunning(): Boolean = withContext(Dispatchers.IO) {
        mutex.withLock { TProxyService.TProxyIsRunning() }
    }

    private fun writeConfig(context: Context, endpoint: TorSocksEndpoint): File {
        val directory = context.noBackupFilesDir
        if (!directory.exists() && !directory.mkdirs()) {
            throw IOException("Could not create ZEUS private config directory")
        }

        val file = File.createTempFile("hev-tor-", ".yml", directory)
        try {
            FileOutputStream(file).use { output ->
                output.write(HevTorTunnelConfig.render(endpoint).toByteArray(Charsets.UTF_8))
                output.fd.sync()
            }
        } catch (error: Throwable) {
            file.delete()
            throw error
        }
        return file
    }
}


/** True only when JNI was entered but did not return a definitive start result. */
internal fun shouldRetainTunDescriptorAfterStartFailure(
    nativeCallEntered: Boolean,
    nativeCallReturned: Boolean,
): Boolean = nativeCallEntered && !nativeCallReturned


internal data class HevStopAttempt(
    val confirmed: Boolean,
    val failure: Throwable? = null,
)

/** Treat both a negative JNI result and a JNI/linkage exception as unconfirmed stop. */
internal fun attemptHevNativeStop(stop: () -> Boolean): HevStopAttempt =
    try {
        HevStopAttempt(confirmed = stop())
    } catch (error: Exception) {
        HevStopAttempt(confirmed = false, failure = error)
    } catch (error: LinkageError) {
        HevStopAttempt(confirmed = false, failure = error)
    }
