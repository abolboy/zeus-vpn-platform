package com.zeusvpn.app.data

import android.content.Context
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map

private val Context.zeusSettings by preferencesDataStore(name = "zeus_local_settings")

/** Non-secret preferences persist locally and remain readable offline. */
class SettingsRepository(context: Context) {
    private val store = context.applicationContext.zeusSettings

    val autoReconnectEnabled: Flow<Boolean> = store.data.map { it[AUTO_RECONNECT] ?: true }

    /**
     * Records explicit user intent to keep the tunnel active.
     * A saved profile alone never starts the VPN automatically.
     */
    val tunnelDesired: Flow<Boolean> = store.data.map { it[TUNNEL_DESIRED] ?: false }

    suspend fun setAutoReconnectEnabled(enabled: Boolean) {
        store.edit { it[AUTO_RECONNECT] = enabled }
    }

    suspend fun setTunnelDesired(desired: Boolean) {
        store.edit { it[TUNNEL_DESIRED] = desired }
    }

    private companion object {
        val AUTO_RECONNECT = booleanPreferencesKey("auto_reconnect_enabled")
        val TUNNEL_DESIRED = booleanPreferencesKey("tunnel_desired")
    }
}
