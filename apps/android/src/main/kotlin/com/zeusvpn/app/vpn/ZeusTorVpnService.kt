package com.zeusvpn.app.vpn

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.VpnService
import android.os.Build
import android.os.IBinder
import androidx.core.app.NotificationCompat
import com.zeusvpn.app.MainActivity
import com.zeusvpn.app.R
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

enum class TorVpnServiceState {
    STOPPED,
    STARTING,
    HEV_WORKER_STARTED_UNVERIFIED,
    STOPPING,
    FAILED,
}

/**
 * Process-level state for the ZEUS-owned Tor VPN host.
 * HEV_WORKER_STARTED_UNVERIFIED is deliberately not named CONNECTED:
 * native worker creation does not prove packet forwarding or Tor exit reachability.
 */
object TorVpnServiceStatus {
    private val mutableState = MutableStateFlow(TorVpnServiceState.STOPPED)
    val state: StateFlow<TorVpnServiceState> = mutableState

    internal fun set(value: TorVpnServiceState) {
        mutableState.value = value
    }
}

/**
 * Owns Android's TUN descriptor and the Tor -> Hev runtime sequence.
 *
 * The ZEUS app UID is excluded by TorVpnTunnelPolicy so Tor's upstream sockets
 * do not loop into their own TUN. Consequently, this is not full-device coverage
 * for ZEUS's own app traffic. A later SOCKS-based data-plane probe is required
 * before the UI may report a verified Tor connection.
 */
class ZeusTorVpnService : VpnService() {
    private val serviceScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val lifecycleMutex = Mutex()

    @Volatile private var torController: TorDaemonController? = null
    @Volatile private var startJob: Job? = null

    override fun onBind(intent: Intent?): IBinder? = super.onBind(intent)

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_START -> {
                if (prepare(this) != null) {
                    TorVpnServiceStatus.set(TorVpnServiceState.FAILED)
                    stopSelf(startId)
                    return START_NOT_STICKY
                }
                startForegroundCompat(buildNotification("در حال آماده‌سازی اتصال Tor"))
                if (startJob?.isActive != true) {
                    startJob = serviceScope.launch { startTunnel() }
                }
            }
            ACTION_STOP -> serviceScope.launch { stopTunnel(startId) }
            else -> {
                stopSelf(startId)
                return START_NOT_STICKY
            }
        }
        return START_NOT_STICKY
    }

    override fun onRevoke() {
        serviceScope.launch { stopTunnel(startId = 0) }
        super.onRevoke()
    }

    private suspend fun startTunnel() = lifecycleMutex.withLock {
        if (TorVpnServiceStatus.state.value == TorVpnServiceState.HEV_WORKER_STARTED_UNVERIFIED) {
            return@withLock
        }

        TorVpnServiceStatus.set(TorVpnServiceState.STARTING)
        updateNotification("در حال راه‌اندازی Tor و تونل شبکه")

        val controller = TorDaemonController(applicationContext)
        torController = controller
        try {
            val endpoint = controller.start()
            val tun = Builder().apply {
                TorVpnTunnelPolicy.applyTo(this, packageName)
            }.establish() ?: throw IllegalStateException("Android did not establish the ZEUS TUN interface")

            HevTun2SocksRuntime.start(applicationContext, tun, endpoint)
            TorVpnServiceStatus.set(TorVpnServiceState.HEV_WORKER_STARTED_UNVERIFIED)
            updateNotification("موتور تونل شروع شده؛ بررسی عبور واقعی ترافیک هنوز لازم است")
        } catch (cancelled: CancellationException) {
            cleanupAfterFailedStart(controller)
            TorVpnServiceStatus.set(TorVpnServiceState.STOPPED)
            throw cancelled
        } catch (error: Exception) {
            val cleanupConfirmed = cleanupAfterFailedStart(controller)
            TorVpnServiceStatus.set(TorVpnServiceState.FAILED)
            if (cleanupConfirmed) {
                stopForeground(STOP_FOREGROUND_REMOVE)
                stopSelf()
            } else {
                updateNotification("خطای توقف موتور؛ منابع شبکه برای جلوگیری از نشت حفظ شده‌اند")
            }
        }
    }

    private suspend fun cleanupAfterFailedStart(controller: TorDaemonController): Boolean {
        val hevStopped = runCatching { HevTun2SocksRuntime.stop() }.isSuccess
        if (!hevStopped) return false
        val torStopped = runCatching { controller.stop() }.isSuccess
        if (torStopped) torController = null
        return torStopped
    }

    private suspend fun stopTunnel(startId: Int) {
        TorVpnServiceStatus.set(TorVpnServiceState.STOPPING)
        updateNotification("در حال توقف اتصال Tor")

        // Cancel an in-flight Tor bootstrap before taking the lifecycle lock.
        startJob?.takeIf { it.isActive }?.cancelAndJoin()

        lifecycleMutex.withLock {
            val hevStopped = runCatching { HevTun2SocksRuntime.stop() }
            if (hevStopped.isFailure) {
                TorVpnServiceStatus.set(TorVpnServiceState.FAILED)
                updateNotification("توقف موتور تأیید نشد؛ منابع برای جلوگیری از نشت حفظ شده‌اند")
                return
            }

            val torStopped = runCatching { torController?.stop() }
            if (torStopped.isFailure) {
                TorVpnServiceStatus.set(TorVpnServiceState.FAILED)
                updateNotification("تونل بسته شد؛ آزادسازی سرویس Tor نیازمند بازیابی است")
                stopForeground(STOP_FOREGROUND_REMOVE)
                stopSelf(startId)
                return
            }

            torController = null
            TorVpnServiceStatus.set(TorVpnServiceState.STOPPED)
            stopForeground(STOP_FOREGROUND_REMOVE)
            stopSelf(startId)
        }
    }

    private fun startForegroundCompat(notification: Notification) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startForeground(
                NOTIFICATION_ID,
                notification,
                ServiceInfo.FOREGROUND_SERVICE_TYPE_SYSTEM_EXEMPTED,
            )
        } else {
            startForeground(NOTIFICATION_ID, notification)
        }
    }

    private fun updateNotification(message: String) {
        val manager = getSystemService(NotificationManager::class.java)
        manager.notify(NOTIFICATION_ID, buildNotification(message))
    }

    private fun buildNotification(message: String): Notification {
        ensureNotificationChannel()
        val openApp = PendingIntent.getActivity(
            this,
            0,
            Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        return NotificationCompat.Builder(this, NOTIFICATION_CHANNEL_ID)
            .setSmallIcon(android.R.drawable.stat_sys_warning)
            .setContentTitle("ZEUS VPN · Tor")
            .setContentText(message)
            .setContentIntent(openApp)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setCategory(NotificationCompat.CATEGORY_SERVICE)
            .build()
    }

    private fun ensureNotificationChannel() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        val manager = getSystemService(NotificationManager::class.java)
        if (manager.getNotificationChannel(NOTIFICATION_CHANNEL_ID) == null) {
            manager.createNotificationChannel(
                NotificationChannel(
                    NOTIFICATION_CHANNEL_ID,
                    "ZEUS Tor VPN",
                    NotificationManager.IMPORTANCE_LOW,
                ),
            )
        }
    }

    override fun onDestroy() {
        serviceScope.cancel()
        super.onDestroy()
    }

    companion object {
        const val ACTION_START = "com.zeusvpn.app.vpn.action.START_TOR"
        const val ACTION_STOP = "com.zeusvpn.app.vpn.action.STOP_TOR"
        private const val NOTIFICATION_CHANNEL_ID = "zeus_tor_vpn"
        private const val NOTIFICATION_ID = 7301

        fun start(context: android.content.Context) {
            val intent = Intent(context, ZeusTorVpnService::class.java).setAction(ACTION_START)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                context.startForegroundService(intent)
            } else {
                context.startService(intent)
            }
        }

        fun stop(context: android.content.Context) {
            context.startService(
                Intent(context, ZeusTorVpnService::class.java).setAction(ACTION_STOP),
            )
        }
    }
}
