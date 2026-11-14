package com.zeusvpn.app.vpn

import android.content.Context
import com.wireguard.android.backend.GoBackend
import com.wireguard.android.backend.Tunnel
import com.zeusvpn.app.data.EncryptedWireGuardConfigStore
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext

enum class WireGuardState {
    DISCONNECTED,
    CONNECTING,
    ENGINE_UP,
    DISCONNECTING,
}

/** Byte counters and handshake timestamp reported by the official WireGuard backend. */
data class WireGuardTrafficStats(
    val receivedBytes: Long,
    val sentBytes: Long,
    val latestHandshakeEpochMillis: Long,
) {
    companion object {
        val EMPTY = WireGuardTrafficStats(0L, 0L, 0L)
    }
}

class MissingWireGuardProfileException : Exception()

/**
 * Single-process owner for the official WireGuard userspace backend.
 * The library's merged manifest contributes GoBackend.VpnService.
 */
class WireGuardEngine private constructor(context: Context) {
    private val appContext = context.applicationContext
    private val profileStore = EncryptedWireGuardConfigStore(appContext)
    private val operationMutex = Mutex()
    private val mutableState = MutableStateFlow(WireGuardState.DISCONNECTED)

    val state: StateFlow<WireGuardState> = mutableState

    private val backend: GoBackend by lazy(LazyThreadSafetyMode.SYNCHRONIZED) {
        GoBackend(appContext)
    }

    private val tunnel = object : Tunnel {
        override fun getName(): String = TUNNEL_NAME

        override fun onStateChange(newState: Tunnel.State) {
            mutableState.value = newState.toAppState()
        }
    }

    /**
     * Reconcile the UI's cached state with the WireGuard backend.
     * This avoids treating the in-memory StateFlow as the source of truth.
     */
    suspend fun refreshState(): WireGuardState = withContext(Dispatchers.IO) {
        operationMutex.withLock {
            syncBackendStateLocked()
        }
    }

    suspend fun hasProfile(): Boolean = withContext(Dispatchers.IO) {
        profileStore.hasProfile()
    }

    /**
     * Read transfer counters from the official WireGuard backend.
     * The mutex prevents statistics reads from racing with connect/disconnect/profile changes.
     */
    suspend fun trafficStats(): WireGuardTrafficStats = withContext(Dispatchers.IO) {
        operationMutex.withLock {
            if (syncBackendStateLocked() != WireGuardState.ENGINE_UP) {
                return@withLock WireGuardTrafficStats.EMPTY
            }

            val snapshot = backend.getStatistics(tunnel)
            val latestHandshake = snapshot.peers()
                .mapNotNull { peerKey ->
                    snapshot.peer(peerKey)?.latestHandshakeEpochMillis()?.takeIf { it > 0L }
                }
                .maxOrNull() ?: 0L

            WireGuardTrafficStats(
                receivedBytes = snapshot.totalRx().coerceAtLeast(0L),
                sentBytes = snapshot.totalTx().coerceAtLeast(0L),
                latestHandshakeEpochMillis = latestHandshake,
            )
        }
    }

    suspend fun saveProfile(profileText: String) = withContext(Dispatchers.IO) {
        operationMutex.withLock {
            if (syncBackendStateLocked() != WireGuardState.DISCONNECTED) {
                throw IllegalStateException("Stop the tunnel before replacing the profile")
            }
            WireGuardProfileValidator.parse(profileText)
            // The plaintext is validated before it is encrypted and written.
            profileStore.save(profileText)
        }
    }

    suspend fun connect() = withContext(Dispatchers.IO) {
        operationMutex.withLock {
            val profileBytes = profileStore.read() ?: throw MissingWireGuardProfileException()
            val config = try {
                WireGuardProfileValidator.parse(profileBytes)
            } finally {
                profileBytes.fill(0)
            }

            mutableState.value = WireGuardState.CONNECTING
            try {
                val stateAfterConnect = backend.setState(tunnel, Tunnel.State.UP, config)
                if (stateAfterConnect != Tunnel.State.UP) {
                    throw IllegalStateException("WireGuard backend did not enter UP state")
                }
                mutableState.value = WireGuardState.ENGINE_UP
            } catch (error: Exception) {
                mutableState.value = runCatching { syncBackendStateLocked() }
                    .getOrDefault(WireGuardState.DISCONNECTED)
                throw error
            }
        }
    }

    suspend fun disconnect() = withContext(Dispatchers.IO) {
        operationMutex.withLock {
            if (syncBackendStateLocked() == WireGuardState.ENGINE_UP) {
                mutableState.value = WireGuardState.DISCONNECTING
                try {
                    val finalState = backend.setState(tunnel, Tunnel.State.DOWN, null)
                    if (finalState != Tunnel.State.DOWN) {
                        throw IllegalStateException("WireGuard backend did not enter DOWN state")
                    }
                    mutableState.value = WireGuardState.DISCONNECTED
                } catch (error: Exception) {
                    mutableState.value = runCatching { syncBackendStateLocked() }
                        .getOrDefault(WireGuardState.ENGINE_UP)
                    throw error
                }
            } else {
                mutableState.value = WireGuardState.DISCONNECTED
            }
        }
    }

    suspend fun forgetProfile() = withContext(Dispatchers.IO) {
        operationMutex.withLock {
            if (syncBackendStateLocked() == WireGuardState.ENGINE_UP) {
                mutableState.value = WireGuardState.DISCONNECTING
                try {
                    val finalState = backend.setState(tunnel, Tunnel.State.DOWN, null)
                    if (finalState != Tunnel.State.DOWN) {
                        throw IllegalStateException("WireGuard backend did not enter DOWN state")
                    }
                    mutableState.value = WireGuardState.DISCONNECTED
                } catch (error: Exception) {
                    mutableState.value = runCatching { syncBackendStateLocked() }
                        .getOrDefault(WireGuardState.ENGINE_UP)
                    throw error
                }
            } else if (mutableState.value != WireGuardState.DISCONNECTED) {
                throw IllegalStateException("Wait for the tunnel operation to finish")
            }
            profileStore.clear()
        }
    }

    private fun syncBackendStateLocked(): WireGuardState {
        val actualState = backend.getState(tunnel).toAppState()
        mutableState.value = actualState
        return actualState
    }

    private fun Tunnel.State.toAppState(): WireGuardState =
        if (this == Tunnel.State.UP) WireGuardState.ENGINE_UP else WireGuardState.DISCONNECTED

    companion object {
        private const val TUNNEL_NAME = "zeus-vpn"

        @Volatile
        private var instance: WireGuardEngine? = null

        fun getInstance(context: Context): WireGuardEngine {
            return instance ?: synchronized(this) {
                instance ?: WireGuardEngine(context).also { instance = it }
            }
        }
    }
}
