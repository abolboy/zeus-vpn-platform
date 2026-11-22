package com.zeusvpn.app.vpn

import android.content.Context
import android.os.ParcelFileDescriptor
import hev.htproxy.TProxyService
import kotlinx.coroutines.Dispatchers
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
 * Owns the TUN ParcelFileDescriptor only after native Hev startup succeeds.
 *
 * Hev's Android JNI receives a borrowed descriptor: upstream tunnel_fini()
 * explicitly leaves externally supplied descriptors to the caller. Therefore
 * this runtime keeps the descriptor open until TProxyStopService() has joined
 * the native worker successfully, then closes it.
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

    /**
     * Transfers descriptor lifecycle ownership to this object only on success.
     * If this method throws, the caller remains responsible for closing [tun].
     */
    suspend fun start(
        context: Context,
        tun: ParcelFileDescriptor,
        endpoint: TorSocksEndpoint,
    ) = withContext(Dispatchers.IO) {
        mutex.withLock {
            check(activeTun == null) { "A Hev TUN session is already owned by ZEUS" }
            check(tun.fileDescriptor.valid()) { "The supplied TUN descriptor is invalid" }
            check(!TProxyService.TProxyIsRunning()) {
                "The process-wide Hev native tunnel is already running"
            }

            val config = writeConfig(context.applicationContext, endpoint)
            val started = try {
                TProxyService.TProxyStartService(config.absolutePath, tun.fd)
            } catch (error: Throwable) {
                config.delete()
                throw error
            }

            if (!started) {
                config.delete()
                throw IllegalStateException("Hev refused to start its native worker")
            }

            activeTun = tun
            activeConfig = config
        }
    }

    /**
     * Stops and joins the native worker before releasing the caller-owned TUN fd.
     * On failure, the descriptor reference is retained so a later stop can retry.
     */
    suspend fun stop() = withContext(Dispatchers.IO) {
        mutex.withLock {
            val tun = activeTun
            if (tun == null) {
                check(!TProxyService.TProxyIsRunning()) {
                    "Hev is running without a TUN descriptor owned by ZEUS"
                }
                return@withLock
            }

            check(TProxyService.TProxyStopService()) {
                "Hev did not confirm native worker shutdown; TUN descriptor retained"
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
