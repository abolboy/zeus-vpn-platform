package com.zeusvpn.app.data

import android.content.Context
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.core.stringSetPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import com.zeusvpn.app.network.SplitTunnelMode
import com.zeusvpn.app.network.SplitTunnelPolicy
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map

private val Context.zeusSettings by preferencesDataStore(name = "zeus_local_settings")

/** Non-secret preferences persist locally and remain readable offline. */
class SettingsRepository(context: Context) {
    private val store = context.applicationContext.zeusSettings

    val autoReconnectEnabled: Flow<Boolean> = store.data.map { it[AUTO_RECONNECT] ?: true }

    /**
     * Records an explicit request to connect and remain connected.
     * A saved profile alone never starts the VPN automatically; reconnect only
     * follows a user-initiated connection intent.
     */
    val tunnelDesired: Flow<Boolean> = store.data.map { it[TUNNEL_DESIRED] ?: false }

    val splitTunnelPolicy: Flow<SplitTunnelPolicy> = store.data.map { it.toSplitTunnelPolicy() }

    suspend fun loadSplitTunnelPolicy(): SplitTunnelPolicy =
        store.data.first().toSplitTunnelPolicy()

    suspend fun setSplitTunnelPolicy(policy: SplitTunnelPolicy) {
        policy.validate()
        store.edit { preferences ->
            preferences[SPLIT_TUNNEL_MODE] = policy.mode.name
            preferences[SPLIT_TUNNEL_PACKAGES] = policy.packageNames
        }
    }

    suspend fun setAutoReconnectEnabled(enabled: Boolean) {
        store.edit { it[AUTO_RECONNECT] = enabled }
    }

    suspend fun setTunnelDesired(desired: Boolean) {
        store.edit { it[TUNNEL_DESIRED] = desired }
    }

    /** Device UUID is an identifier, not a credential; account tokens remain Keystore-encrypted. */
    suspend fun saveTorDeviceId(deviceId: String) {
        require(deviceId.matches(Regex("^[0-9a-fA-F-]{36}$"))) { "Invalid Tor device ID" }
        store.edit { it[TOR_DEVICE_ID] = deviceId }
    }

    suspend fun loadTorDeviceId(): String? =
        store.data.first()[TOR_DEVICE_ID]?.takeIf { it.matches(Regex("^[0-9a-fA-F-]{36}$")) }

    suspend fun clearTorDeviceId() {
        store.edit { it.remove(TOR_DEVICE_ID) }
    }

    private fun Preferences.toSplitTunnelPolicy(): SplitTunnelPolicy {
        val mode = runCatching {
            SplitTunnelMode.valueOf(this[SPLIT_TUNNEL_MODE] ?: SplitTunnelMode.ALL.name)
        }.getOrDefault(SplitTunnelMode.ALL)
        return SplitTunnelPolicy(
            mode = mode,
            packageNames = this[SPLIT_TUNNEL_PACKAGES] ?: emptySet(),
        )
    }

    private companion object {
        val AUTO_RECONNECT = booleanPreferencesKey("auto_reconnect_enabled")
        val TUNNEL_DESIRED = booleanPreferencesKey("tunnel_desired")
        val TOR_DEVICE_ID = stringPreferencesKey("tor_device_id")
        val SPLIT_TUNNEL_MODE = stringPreferencesKey("split_tunnel_mode")
        val SPLIT_TUNNEL_PACKAGES = stringSetPreferencesKey("split_tunnel_packages")
    }
}
