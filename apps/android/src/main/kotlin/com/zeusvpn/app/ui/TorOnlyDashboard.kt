package com.zeusvpn.app.ui

import android.app.Activity
import android.content.Intent
import android.net.VpnService
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import com.zeusvpn.app.data.SettingsRepository
import com.zeusvpn.app.network.CustomerIdentityRepository
import com.zeusvpn.app.network.TorClientEnrollmentException
import com.zeusvpn.app.network.TorClientEnrollmentRepository
import com.zeusvpn.app.vpn.TorVpnService
import com.zeusvpn.app.vpn.TorVpnServiceState
import com.zeusvpn.app.vpn.TorVpnServiceStatus
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

@Composable
fun TorOnlyDashboard() {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val identity = remember(context) { CustomerIdentityRepository(context) }
    val enrollment = remember(context) { TorClientEnrollmentRepository(context) }
    val settings = remember(context) { SettingsRepository(context) }
    val serviceState by TorVpnServiceStatus.state.collectAsState()

    var signedIn by remember { mutableStateOf(identity.hasStoredSession()) }
    var deviceId by remember { mutableStateOf<String?>(null) }
    var invitationCode by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var message by remember { mutableStateOf("") }
    var accountDeviceStatus by remember { mutableStateOf("") }

    val signInLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.StartActivityForResult(),
    ) { result ->
        scope.launch {
            busy = true
            try {
                identity.completeAuthorization(result.data)
                signedIn = true
                message = "ورود حساب ZEUS با موفقیت انجام شد."
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                message = error.message ?: "ورود انجام نشد."
            } finally {
                busy = false
            }
        }
    }

    val vpnConsentLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.StartActivityForResult(),
    ) { result ->
        if (result.resultCode == Activity.RESULT_OK) {
            TorVpnService.start(context)
            message = "درخواست راه‌اندازی سرویس Tor ارسال شد."
        } else {
            message = "مجوز VPN داده نشد؛ تونل راه‌اندازی نشد."
        }
    }

    LaunchedEffect(settings) {
        deviceId = settings.loadTorDeviceId()
        signedIn = identity.hasStoredSession()
    }

    fun runBusy(block: suspend () -> Unit) {
        if (busy) return
        scope.launch {
            busy = true
            message = ""
            try {
                block()
            } catch (error: CancellationException) {
                throw error
            } catch (error: TorClientEnrollmentException) {
                message = error.message ?: error.errorCode
            } catch (error: Exception) {
                message = error.message ?: "عملیات انجام نشد."
            } finally {
                busy = false
            }
        }
    }

    Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(20.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            Text("ZEUS VPN", style = MaterialTheme.typography.headlineLarge)
            Text("اتصال اختصاصی Tor", style = MaterialTheme.typography.titleMedium)
            Text(
                "نسخهٔ اندروید ZEUS در حال مهاجرت به موتور Tor است. این صفحه فقط وضعیت واقعی اجزای موجود را نمایش می‌دهد و شروع موتور به‌تنهایی به معنی اتصال تأییدشده نیست.",
                style = MaterialTheme.typography.bodyMedium,
            )

            Card(modifier = Modifier.fillMaxWidth()) {
                Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    Text("حساب ZEUS", style = MaterialTheme.typography.titleMedium)
                    Text(if (signedIn) "حساب وارد شده است." else "برای ثبت دستگاه با حساب، وارد شوید.")
                    Button(
                        onClick = {
                            runBusy {
                                signInLauncher.launch(identity.createAuthorizationIntent())
                            }
                        },
                        enabled = !busy && identity.isConfigured && !signedIn,
                        modifier = Modifier.fillMaxWidth(),
                    ) {
                        Text("ورود امن با OIDC")
                    }
                    if (!identity.isConfigured) {
                        Text("تنظیمات OIDC در این Build تعریف نشده است؛ ثبت دستگاه با دعوت‌نامهٔ مدیر همچنان قابل استفاده است.")
                    }
                }
            }

            Card(modifier = Modifier.fillMaxWidth()) {
                Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    Text("ثبت دستگاه Tor", style = MaterialTheme.typography.titleMedium)
                    OutlinedTextField(
                        value = invitationCode,
                        onValueChange = { invitationCode = it.trim() },
                        label = { Text("کد دعوت مدیر (اختیاری)") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                    Text(
                        if (deviceId == null) "این نصب هنوز به حساب ZEUS متصل نشده است."
                        else "شناسه دستگاه: $deviceId",
                        style = MaterialTheme.typography.bodySmall,
                    )
                    Button(
                        onClick = {
                            runBusy {
                                val result = enrollment.enroll(
                                    invitationCode = invitationCode.takeIf { it.isNotBlank() },
                                    deviceName = Build.MODEL.orEmpty(),
                                )
                                settings.saveTorDeviceId(result.deviceId)
                                deviceId = result.deviceId
                                invitationCode = ""
                                message = "دستگاه Tor با موفقیت به حساب متصل شد."
                            }
                        },
                        enabled = !busy && deviceId == null &&
                            (signedIn || invitationCode.isNotBlank() || enrollment.hasPendingEnrollment()),
                        modifier = Modifier.fillMaxWidth(),
                    ) {
                        Text(if (enrollment.hasPendingEnrollment()) "ادامه ثبت نیمه‌تمام" else "ثبت دستگاه")
                    }
                    if (deviceId != null) {
                        OutlinedButton(
                            onClick = {
                                runBusy {
                                    if (!signedIn) throw IllegalStateException("برای بررسی یا لغو دستگاه ابتدا وارد حساب شوید.")
                                    val status = enrollment.getDeviceStatus(deviceId!!)
                                    accountDeviceStatus = status.optString("status", "نامشخص")
                                    message = "وضعیت دستگاه از سرور دریافت شد."
                                }
                            },
                            enabled = !busy && signedIn,
                            modifier = Modifier.fillMaxWidth(),
                        ) { Text("بررسی وضعیت دستگاه") }

                        OutlinedButton(
                            onClick = {
                                runBusy {
                                    if (!signedIn) throw IllegalStateException("برای لغو دستگاه ابتدا وارد حساب شوید.")
                                    enrollment.revokeDevice(deviceId!!)
                                    TorVpnService.stop(context)
                                    settings.clearTorDeviceId()
                                    deviceId = null
                                    accountDeviceStatus = "revoked"
                                    message = "دستگاه در سرور لغو شد."
                                }
                            },
                            enabled = !busy && signedIn,
                            modifier = Modifier.fillMaxWidth(),
                        ) { Text("لغو ثبت دستگاه") }
                    }
                    if (accountDeviceStatus.isNotBlank()) {
                        Text("وضعیت ثبت‌شده در سرور: $accountDeviceStatus")
                    }
                }
            }

            Card(modifier = Modifier.fillMaxWidth()) {
                Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    Text("موتور VPN", style = MaterialTheme.typography.titleMedium)
                    Text(
                        when (serviceState) {
                            TorVpnServiceState.STOPPED -> "متوقف"
                            TorVpnServiceState.STARTING -> "در حال راه‌اندازی Tor و TUN"
                            TorVpnServiceState.HEV_WORKER_STARTED_UNVERIFIED ->
                                "موتور Hev شروع شده؛ عبور واقعی ترافیک هنوز تأیید نشده است."
                            TorVpnServiceState.STOPPING -> "در حال توقف"
                            TorVpnServiceState.FAILED -> "خطا یا توقف تأییدنشده؛ اتصال امن اعلام نمی‌شود."
                        },
                    )
                    Button(
                        onClick = {
                            val consent = VpnService.prepare(context)
                            if (consent == null) TorVpnService.start(context)
                            else vpnConsentLauncher.launch(consent)
                        },
                        enabled = !busy && deviceId != null &&
                            serviceState in setOf(TorVpnServiceState.STOPPED, TorVpnServiceState.FAILED),
                        modifier = Modifier.fillMaxWidth(),
                    ) { Text("راه‌اندازی موتور Tor") }
                    OutlinedButton(
                        onClick = { TorVpnService.stop(context) },
                        enabled = serviceState != TorVpnServiceState.STOPPED,
                        modifier = Modifier.fillMaxWidth(),
                    ) { Text("توقف موتور") }
                    Text(
                        "محدودیت فعلی: ترافیک خود برنامهٔ ZEUS از TUN مستثنا است تا سوکت‌های Tor به حلقهٔ تونل وارد نشوند. بنابراین هنوز نمی‌توان ادعای پوشش کامل ترافیک خود برنامه یا اتصال کاملاً تأییدشده داشت.",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.error,
                    )
                }
            }

            if (busy) {
                CircularProgressIndicator()
            }
            if (message.isNotBlank()) {
                Text(message, style = MaterialTheme.typography.bodyMedium)
            }
            Spacer(Modifier.height(12.dp))
        }
    }
}
