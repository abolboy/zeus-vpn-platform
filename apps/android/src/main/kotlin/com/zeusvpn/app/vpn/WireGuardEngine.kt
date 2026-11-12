package com.zeusvpn.app.vpn

import android.content.Context
import com.wireguard.android.backend.GoBackend
import com.wireguard.android.backend.Tunnel
import com.wireguard.config.Config
import com.zeusvpn.app.data.EncryptedWireGuardConfigStore
import java.io.ByteArrayInputStream
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

class InvalidWireGuardProfileException : Exception()
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
            mutableState.value = if (newState == Tunnel.State.UP) {
                WireGuardState.ENGINE_UP
            } else {
                WireGuardState.DISCONNECTED
            }
        }
    }

    suspend fun hasProfile(): Boolean = withContext(Dispatchers.IO) {
        profileStore.hasProfile()
    }

    suspend fun saveProfile(profileText: String) = withContext(Dispatchers.IO) {
        operationMutex.withLock {
            if (mutableState.value == WireGuardState.ENGINE_UP) {
                throw IllegalStateException("Disconnect before replacing the profile")
            }
            val config = parseAndValidate(profileText)
            // Parsing happens before writing. The private key never enters logs or DataStore.
            if (config.getPeers().isEmpty()) throw InvalidWireGuardProfileException()
            profileStore.save(profileText)
        }
    }

    suspend fun connect() = withContext(Dispatchers.IO) {
        operationMutex.withLock {
            val profileText = profileStore.read() ?: throw MissingWireGuardProfileException()
            val config = parseAndValidate(profileText)
            mutableState.value = WireGuardState.CONNECTING
            try {
                val stateAfterConnect = backend.setState(tunnel, Tunnel.State.UP, config)
                if (stateAfterConnect != Tunnel.State.UP) {
                    throw IllegalStateException("WireGuard backend did not enter UP state")
                }
                mutableState.value = WireGuardState.ENGINE_UP
            } catch (error: Exception) {
                mutableState.value = WireGuardState.DISCONNECTED
                throw error
            }
        }
    }

    suspend fun disconnect() = withContext(Dispatchers.IO) {
        operationMutex.withLock {
            if (mutableState.value == WireGuardState.ENGINE_UP) {
                mutableState.value = WireGuardState.DISCONNECTING
                try {
                    backend.setState(tunnel, Tunnel.State.DOWN, null)
                } finally {
                    mutableState.value = WireGuardState.DISCONNECTED
                }
            } else {
                mutableState.value = WireGuardState.DISCONNECTED
            }
        }
    }

    suspend fun forgetProfile() = withContext(Dispatchers.IO) {
        operationMutex.withLock {
            if (mutableState.value == WireGuardState.ENGINE_UP) {
                mutableState.value = WireGuardState.DISCONNECTING
                backend.setState(tunnel, Tunnel.State.DOWN, null)
                mutableState.value = WireGuardState.DISCONNECTED
            }
            profileStore.clear()
        }
    }

    private fun parseAndValidate(text: String): Config {
        if (text.isBlank() || text.toByteArray(Charsets.UTF_8).size > MAX_PROFILE_BYTES) {
            throw InvalidWireGuardProfileException()
        }
        return try {
            val config = Config.parse(ByteArrayInputStream(text.toByteArray(Charsets.UTF_8)))
            val interfaceSection = config.getInterface()
            val peers = config.getPeers()
            val hasEndpoint = peers.any { it.getEndpoint().isPresent }
            val hasAllowedIps = peers.any { it.getAllowedIps().isNotEmpty() }
            if (interfaceSection.getAddresses().isEmpty() || peers.isEmpty() || !hasEndpoint || !hasAllowedIps) {
                throw InvalidWireGuardProfileException()
            }
            config
        } catch (_: InvalidWireGuardProfileException) {
            throw InvalidWireGuardProfileException()
        } catch (_: Exception) {
            throw InvalidWireGuardProfileException()
        }
    }

    companion object {
        private const val TUNNEL_NAME = "zeus-vpn"
        private const val MAX_PROFILE_BYTES = 64 * 1024

        @Volatile
        private var instance: WireGuardEngine? = null

        fun getInstance(context: Context): WireGuardEngine {
            return instance ?: synchronized(this) {
                instance ?: WireGuardEngine(context).also { instance = it }
            }
        }
    }
}
