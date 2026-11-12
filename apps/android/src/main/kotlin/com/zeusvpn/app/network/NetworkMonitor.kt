package com.zeusvpn.app.network

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.flow.distinctUntilChanged

enum class NetworkCondition { UNKNOWN, ONLINE, LIMITED, OFFLINE }

/** Observes device internet; it deliberately does not report VPN tunnel state. */
class NetworkMonitor(context: Context) {
    private val manager = context.applicationContext
        .getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager

    fun observe(): Flow<NetworkCondition> = callbackFlow {
        fun publish() {
            val active = manager.activeNetwork
            val capabilities = active?.let(manager::getNetworkCapabilities)
            trySend(capabilities.toCondition())
        }
        val callback = object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) = publish()
            override fun onLost(network: Network) = publish()
            override fun onCapabilitiesChanged(network: Network, caps: NetworkCapabilities) {
                trySend(caps.toCondition())
            }
        }
        try {
            manager.registerDefaultNetworkCallback(callback)
            publish()
        } catch (_: SecurityException) {
            trySend(NetworkCondition.UNKNOWN)
        } catch (_: RuntimeException) {
            trySend(NetworkCondition.UNKNOWN)
        }
        awaitClose { runCatching { manager.unregisterNetworkCallback(callback) } }
    }.catch {
        emit(NetworkCondition.UNKNOWN)
    }.distinctUntilChanged()

    private fun NetworkCapabilities?.toCondition(): NetworkCondition = when {
        this == null || !hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) ->
            NetworkCondition.OFFLINE
        hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED) -> NetworkCondition.ONLINE
        else -> NetworkCondition.LIMITED
    }
}
