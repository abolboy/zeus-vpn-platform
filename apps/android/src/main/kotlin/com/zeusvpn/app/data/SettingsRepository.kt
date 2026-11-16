package com.zeusvpn.app.data

import android.content.Context
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.core.stringSetPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import com.zeusvpn.app.network.SplitTunnelMode
import com.zeusvpn.app.network.SplitTunnelPolicy

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

    val splitTunnelPolicy: Flow<SplitTunnelPolicy> = store.data.map { preferences ->
        val mode = runCatching {
            SplitTunnelMode.valueOf(preferences[SPLIT_TUNNEL_MODE] ?: SplitTunnelMode.ALL.name)
        }.getOrDefault(SplitTunnelMode.ALL)
        SplitTunnelPolicy(
            mode = mode,
            packageNames = preferences[SPLIT_TUNNEL_PACKAGES] ?: emptySet(),
        )
    }

    suspend fun loadSplitTunnelPolicy(): SplitTunnelPolicy {
        val preferences = store.data.first()
        val mode = runCatching {
            SplitTunnelMode.valueOf(preferences[SPLIT_TUNNEL_MODE] ?: SplitTunnelMode.ALL.name)
        }.getOrDefault(SplitTunnelMode.ALL)
        return SplitTunnelPolicy(
            mode = mode,
            packageNames = preferences[SPLIT_TUNNEL_PACKAGES] ?: emptySet(),
        )
    }

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

    private companion object {
        val AUTO_RECONNECT = booleanPreferencesKey("auto_reconnect_enabled")
        val TUNNEL_DESIRED = booleanPreferencesKey("tunnel_desired")
        val SPLIT_TUNNEL_MODE = stringPreferencesKey("split_tunnel_mode")
        val SPLIT_TUNNEL_PACKAGES = stringSetPreferencesKey("split_tunnel_packages")
    }
}
