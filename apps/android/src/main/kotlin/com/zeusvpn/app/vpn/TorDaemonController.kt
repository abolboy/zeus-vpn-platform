package com.zeusvpn.app.vpn

import android.content.BroadcastReceiver
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.ServiceConnection
import android.os.Build
import android.os.IBinder
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import org.torproject.jni.TorService

data class TorSocksEndpoint(
    val host: String,
    val port: Int,
)

enum class TorDaemonState {
    STOPPED,
    STARTING,
    READY,
    STOPPING,
    FAILED,
}

class TorStartupException(message: String, cause: Throwable? = null) : Exception(message, cause)

/**
 * Owns the embedded TorService binding and reports readiness only after Tor
 * announces that its first circuit is established and a valid SOCKS port exists.
 *
 * This class exposes a local SOCKS endpoint. It does not route Android device
 * traffic through Tor and must not be presented as a connected VPN by itself.
 */
internal fun parseTorSocksEndpoint(value: String?): TorSocksEndpoint? {
    val listener = value?.trim()?.trim('"') ?: return null
    val separator = listener.lastIndexOf(':')
    if (separator <= 0) return null

    val host = listener.substring(0, separator).removeSurrounding("[", "]")
    val loopbackHost = when (host) {
        "127.0.0.1" -> "127.0.0.1"
        "::1" -> "::1"
        else -> return null
    }
    val port = listener.substring(separator + 1).toIntOrNull()
        ?.takeIf { it in 1..65_535 }
        ?: return null

    return TorSocksEndpoint(loopbackHost, port)
}

class TorDaemonController(context: Context) {
    private val appContext = context.applicationContext
    private val operationMutex = Mutex()
    private val mutableState = MutableStateFlow(TorDaemonState.STOPPED)

    val state: StateFlow<TorDaemonState> = mutableState

    @Volatile private var torService: TorService? = null
    private var serviceConnection: ServiceConnection? = null
    private var statusReceiver: BroadcastReceiver? = null
    private var isBound = false
    @Volatile private var circuitReadyReceived = false
    @Volatile private var startup: CompletableDeferred<Unit>? = null

    suspend fun start(timeoutMillis: Long = DEFAULT_START_TIMEOUT_MILLIS): TorSocksEndpoint =
        withContext(Dispatchers.IO) {
            require(timeoutMillis in MIN_START_TIMEOUT_MILLIS..MAX_START_TIMEOUT_MILLIS) {
                "Tor startup timeout is outside the supported range"
            }

            operationMutex.withLock {
                val existingEndpoint = torService?.let(::readLiveSocksEndpoint)
                if (mutableState.value == TorDaemonState.READY && existingEndpoint != null) {
                    return@withLock existingEndpoint
                }
                if (isBound || statusReceiver != null) {
                    withContext(Dispatchers.Main.immediate) { cleanupBinding() }
                }

                mutableState.value = TorDaemonState.STARTING
                circuitReadyReceived = false
                val ready = CompletableDeferred<Unit>()
                startup = ready

                try {
                    withContext(Dispatchers.Main.immediate) {
                        registerStatusReceiver()
                        bindTorService()
                    }

                    val endpoint = withTimeoutOrNull(timeoutMillis) {
                        ready.await()
                        while (true) {
                            if (mutableState.value == TorDaemonState.FAILED) {
                                throw TorStartupException("Tor failed before its SOCKS endpoint became available")
                            }
                            val endpoint = torService?.let(::readLiveSocksEndpoint)
                            if (endpoint != null) {
                                return@withTimeoutOrNull endpoint
                            }
                            delay(SOCKS_PORT_POLL_INTERVAL_MILLIS)
                        }
                        @Suppress("UNREACHABLE_CODE")
                        throw TorStartupException("Tor SOCKS endpoint polling stopped unexpectedly")
                    } ?: throw TorStartupException("Tor did not establish a usable SOCKS endpoint before the timeout")

                    mutableState.value = TorDaemonState.READY
                    endpoint
                } catch (error: Exception) {
                    mutableState.value = TorDaemonState.FAILED
                    withContext(Dispatchers.Main.immediate) { cleanupBinding() }
                    throw error
                } finally {
                    startup = null
                }
            }
        }

    /**
     * Unbinds the app from TorService. TorService.onDestroy() requests native
     * Tor shutdown when this was the final binding. The library does not expose
     * a completion future for native process exit, so STOPPED means the Android
     * service binding was released, not that native shutdown was independently
     * observed to completion.
     */
    suspend fun stop() {
        // Complete outside the mutex so stop can interrupt a start() waiting for
        // the first circuit instead of waiting for the startup timeout.
        startup?.completeExceptionally(TorStartupException("Tor startup was stopped"))
        withContext(Dispatchers.IO) {
            operationMutex.withLock {
                mutableState.value = TorDaemonState.STOPPING
                withContext(Dispatchers.Main.immediate) { cleanupBinding() }
                mutableState.value = TorDaemonState.STOPPED
            }
        }
    }

    private fun registerStatusReceiver() {
        val filter = IntentFilter().apply {
            addAction(TorService.ACTION_STATUS)
            addAction(TorService.ACTION_ERROR)
        }
        val receiver = object : BroadcastReceiver() {
            override fun onReceive(context: Context, intent: Intent) {
                if (intent.getStringExtra(TorService.EXTRA_SERVICE_PACKAGE_NAME) != appContext.packageName) {
                    return
                }

                when (intent.action) {
                    TorService.ACTION_STATUS -> {
                        when (intent.getStringExtra(TorService.EXTRA_STATUS)) {
                            TorService.STATUS_ON -> {
                                circuitReadyReceived = true
                                completeStartupIfReady()
                            }
                            TorService.STATUS_STOPPING, TorService.STATUS_OFF -> {
                                when (mutableState.value) {
                                    TorDaemonState.STARTING -> {
                                        mutableState.value = TorDaemonState.FAILED
                                        startup?.completeExceptionally(
                                            TorStartupException("Tor stopped before a usable circuit was established"),
                                        )
                                    }
                                    TorDaemonState.READY -> mutableState.value = TorDaemonState.FAILED
                                    else -> Unit
                                }
                            }
                        }
                    }
                    TorService.ACTION_ERROR -> {
                        val detail = intent.getStringExtra(Intent.EXTRA_TEXT)
                            ?.take(MAX_ERROR_MESSAGE_LENGTH)
                            ?.takeIf(String::isNotBlank)
                        val message = detail ?: "Tor reported an error"
                        if (mutableState.value == TorDaemonState.STARTING || mutableState.value == TorDaemonState.READY) {
                            mutableState.value = TorDaemonState.FAILED
                        }
                        startup?.completeExceptionally(TorStartupException(message))
                    }
                }
            }
        }
        statusReceiver = receiver
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            appContext.registerReceiver(receiver, filter, Context.RECEIVER_NOT_EXPORTED)
        } else {
            @Suppress("DEPRECATION")
            appContext.registerReceiver(receiver, filter)
        }
    }

    private fun bindTorService() {
        val connection = object : ServiceConnection {
            override fun onServiceConnected(name: ComponentName, binder: IBinder) {
                val localBinder = binder as? TorService.LocalBinder
                val connectedService = localBinder?.service
                if (connectedService == null) {
                    startup?.completeExceptionally(TorStartupException("Tor service returned an unexpected binder"))
                    return
                }
                torService = connectedService
                completeStartupIfReady()
            }

            override fun onServiceDisconnected(name: ComponentName) {
                torService = null
                when (mutableState.value) {
                    TorDaemonState.STARTING -> {
                        mutableState.value = TorDaemonState.FAILED
                        startup?.completeExceptionally(TorStartupException("Tor service disconnected during startup"))
                    }
                    TorDaemonState.READY -> mutableState.value = TorDaemonState.FAILED
                    else -> Unit
                }
            }

            override fun onBindingDied(name: ComponentName) {
                torService = null
                if (mutableState.value == TorDaemonState.STARTING || mutableState.value == TorDaemonState.READY) {
                    mutableState.value = TorDaemonState.FAILED
                }
                startup?.completeExceptionally(TorStartupException("Tor service binding died"))
            }

            override fun onNullBinding(name: ComponentName) {
                if (mutableState.value == TorDaemonState.STARTING || mutableState.value == TorDaemonState.READY) {
                    mutableState.value = TorDaemonState.FAILED
                }
                startup?.completeExceptionally(TorStartupException("Tor service returned a null binding"))
            }
        }
        serviceConnection = connection
        val intent = Intent(appContext, TorService::class.java)
        isBound = appContext.bindService(intent, connection, Context.BIND_AUTO_CREATE)
        if (!isBound) {
            serviceConnection = null
            throw TorStartupException("Android refused to bind the embedded Tor service")
        }
    }

    private fun readLiveSocksPort(service: TorService): Int {
        val listener = service.getInfo("net/listeners/socks")
            ?.trim()
            ?.removeSurrounding("\"")
            ?: return -1
        return listener.substringAfterLast(':')
            .trimEnd('"', ']')
            .toIntOrNull()
            ?: -1
    }

    private fun completeStartupIfReady() {
        if (circuitReadyReceived) {
            // TorService can announce its first circuit just before its control
            // thread publishes the SOCKS port. start() polls that port under the
            // same bounded timeout instead of failing on this ordering race.
            startup?.complete(Unit)
        }
    }

    private fun cleanupBinding() {
        val receiver = statusReceiver
        statusReceiver = null
        if (receiver != null) {
            runCatching { appContext.unregisterReceiver(receiver) }
        }

        val connection = serviceConnection
        serviceConnection = null
        if (isBound && connection != null) {
            isBound = false
            runCatching { appContext.unbindService(connection) }
        } else {
            isBound = false
        }
        torService = null
        circuitReadyReceived = false
    }

    private companion object {
        const val DEFAULT_START_TIMEOUT_MILLIS = 180_000L
        const val MIN_START_TIMEOUT_MILLIS = 15_000L
        const val MAX_START_TIMEOUT_MILLIS = 300_000L
        const val MAX_ERROR_MESSAGE_LENGTH = 240
        const val SOCKS_PORT_POLL_INTERVAL_MILLIS = 100L
    }
}
