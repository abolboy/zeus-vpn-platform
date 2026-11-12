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

    suspend fun setAutoReconnectEnabled(enabled: Boolean) {
        store.edit { it[AUTO_RECONNECT] = enabled }
    }

    private companion object {
        val AUTO_RECONNECT = booleanPreferencesKey("auto_reconnect_enabled")
    }
}
