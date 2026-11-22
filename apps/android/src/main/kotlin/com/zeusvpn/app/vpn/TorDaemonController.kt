package com.zeusvpn.app.vpn

import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.ServiceConnection
import android.os.IBinder
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.NonCancellable
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

internal interface TorServiceApi {
    fun getInfo(key: String): String?
}

internal interface TorServiceBinding {
    interface Listener {
        fun onConnected(service: TorServiceApi)
        fun onDisconnected()
        fun onBindingDied()
        fun onNullBinding()
    }

    fun bind(listener: Listener): Boolean
    fun unbind()
}

private class AndroidTorServiceBinding(context: Context) : TorServiceBinding {
    private val appContext = context.applicationContext
    private var serviceConnection: ServiceConnection? = null

    override fun bind(listener: TorServiceBinding.Listener): Boolean {
        check(serviceConnection == null) { "Tor service is already bound" }

        val connection = object : ServiceConnection {
            override fun onServiceConnected(name: ComponentName, binder: IBinder) {
                val service = (binder as? TorService.LocalBinder)?.service
                if (service == null) {
                    listener.onNullBinding()
                } else {
                    listener.onConnected(object : TorServiceApi {
                        override fun getInfo(key: String): String? = service.getInfo(key)
                    })
                }
            }

            override fun onServiceDisconnected(name: ComponentName) {
                listener.onDisconnected()
            }

            override fun onBindingDied(name: ComponentName) {
                listener.onBindingDied()
            }

            override fun onNullBinding(name: ComponentName) {
                listener.onNullBinding()
            }
        }

        serviceConnection = connection
        return try {
            appContext.bindService(
                Intent(appContext, TorService::class.java),
                connection,
                Context.BIND_AUTO_CREATE,
            ).also { bound ->
                if (!bound) serviceConnection = null
            }
        } catch (error: Exception) {
            serviceConnection = null
            throw error
        }
    }

    override fun unbind() {
        val connection = serviceConnection ?: return
        serviceConnection = null
        appContext.unbindService(connection)
    }
}

/**
 * Owns the embedded TorService binding and reports readiness only after Tor
 * reports an established circuit and a valid SOCKS port through its live ControlPort.
 *
 * This class exposes a local SOCKS endpoint. It does not route Android device
 * traffic through Tor and must not be presented as a connected VPN by itself.
 */
internal fun isTorCircuitEstablished(value: String?): Boolean =
    value?.trim()?.trim('"') == "1"

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

class TorDaemonController internal constructor(
    private val serviceBinding: TorServiceBinding,
    private val mainDispatcher: CoroutineDispatcher,
) {
    constructor(context: Context) : this(
        AndroidTorServiceBinding(context.applicationContext),
        Dispatchers.Main.immediate,
    )

    private val operationMutex = Mutex()
    private val mutableState = MutableStateFlow(TorDaemonState.STOPPED)

    val state: StateFlow<TorDaemonState> = mutableState

    @Volatile private var torService: TorServiceApi? = null
    private var isBound = false
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
                if (isBound) {
                    withContext(mainDispatcher) { cleanupBinding() }
                }

                mutableState.value = TorDaemonState.STARTING
                val ready = CompletableDeferred<Unit>()
                startup = ready

                try {
                    withContext(mainDispatcher) {
                        bindTorService()
                    }

                    val endpoint = withTimeoutOrNull(timeoutMillis) {
                        // Readiness comes exclusively from live ControlPort responses.
                        while (!ready.isCompleted) {
                            if (mutableState.value == TorDaemonState.FAILED) {
                                throw TorStartupException("Tor failed before establishing its first circuit")
                            }
                            val service = torService
                            if (service != null &&
                                isTorCircuitEstablished(service.getInfo("status/circuit-established"))
                            ) {
                                ready.complete(Unit)
                            } else {
                                delay(CIRCUIT_STATUS_POLL_INTERVAL_MILLIS)
                            }
                        }
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
                } catch (cancelled: CancellationException) {
                    withContext(NonCancellable + mainDispatcher) { cleanupBinding() }
                    mutableState.value = TorDaemonState.STOPPED
                    throw cancelled
                } catch (error: Exception) {
                    mutableState.value = TorDaemonState.FAILED
                    withContext(NonCancellable + mainDispatcher) { cleanupBinding() }
                    throw error
                } finally {
                    startup = null
                }
            }
        }

    /**
     * Releases the Android service binding. TorService.onDestroy() requests native
     * Tor shutdown when this was the final binding. The library exposes no completion
     * future for native process exit, so STOPPED means only that the binding was released.
     */
    suspend fun stop() {
        // Complete outside the mutex so stop can interrupt start() while it waits for a circuit.
        startup?.completeExceptionally(TorStartupException("Tor startup was stopped"))
        withContext(Dispatchers.IO) {
            operationMutex.withLock {
                mutableState.value = TorDaemonState.STOPPING
                withContext(mainDispatcher) { cleanupBinding() }
                mutableState.value = TorDaemonState.STOPPED
            }
        }
    }

    private fun bindTorService() {
        isBound = serviceBinding.bind(object : TorServiceBinding.Listener {
            override fun onConnected(service: TorServiceApi) {
                torService = service
            }

            override fun onDisconnected() {
                torService = null
                when (mutableState.value) {
                    TorDaemonState.STARTING -> {
                        mutableState.value = TorDaemonState.FAILED
                        startup?.completeExceptionally(
                            TorStartupException("Tor service disconnected during startup"),
                        )
                    }
                    TorDaemonState.READY -> mutableState.value = TorDaemonState.FAILED
                    else -> Unit
                }
            }

            override fun onBindingDied() {
                torService = null
                if (mutableState.value == TorDaemonState.STARTING || mutableState.value == TorDaemonState.READY) {
                    mutableState.value = TorDaemonState.FAILED
                }
                startup?.completeExceptionally(TorStartupException("Tor service binding died"))
            }

            override fun onNullBinding() {
                if (mutableState.value == TorDaemonState.STARTING || mutableState.value == TorDaemonState.READY) {
                    mutableState.value = TorDaemonState.FAILED
                }
                startup?.completeExceptionally(TorStartupException("Tor service returned a null binding"))
            }
        })
        if (!isBound) {
            throw TorStartupException("Android refused to bind the embedded Tor service")
        }
    }

    private fun readLiveSocksEndpoint(service: TorServiceApi): TorSocksEndpoint? =
        parseTorSocksEndpoint(service.getInfo("net/listeners/socks"))

    private fun cleanupBinding() {
        if (isBound) {
            isBound = false
            runCatching { serviceBinding.unbind() }
        }
        torService = null
    }

    private companion object {
        const val DEFAULT_START_TIMEOUT_MILLIS = 180_000L
        const val MIN_START_TIMEOUT_MILLIS = 15_000L
        const val MAX_START_TIMEOUT_MILLIS = 300_000L
        const val CIRCUIT_STATUS_POLL_INTERVAL_MILLIS = 500L
        const val SOCKS_PORT_POLL_INTERVAL_MILLIS = 100L
    }
}
