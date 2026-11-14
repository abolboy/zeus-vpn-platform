package com.zeusvpn.app.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.zeusvpn.app.R
import com.zeusvpn.app.data.SettingsRepository
import com.zeusvpn.app.network.ClientEnrollmentException
import com.zeusvpn.app.network.ClientEnrollmentRepository
import com.zeusvpn.app.network.NetworkCondition
import com.zeusvpn.app.network.NetworkMonitor
import com.zeusvpn.app.vpn.InvalidWireGuardProfileException
import com.zeusvpn.app.vpn.MissingWireGuardProfileException
import com.zeusvpn.app.vpn.WireGuardEngine
import com.zeusvpn.app.vpn.WireGuardState
import android.app.Activity
import android.net.VpnService
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.launch

private val Ink = Color(0xFF070D19)
private val Panel = Color(0xFF111D31)
private val Mint = Color(0xFF62E6C2)
private val Violet = Color(0xFF9D85FF)
private val SoftText = Color(0xFF91A4B9)
private val Line = Color(0xFF263650)

@Composable
fun ZeusDashboard() {
    val context = LocalContext.current.applicationContext
    val monitor = remember(context) { NetworkMonitor(context) }
    val settings = remember(context) { SettingsRepository(context) }
    val wireGuard = remember(context) { WireGuardEngine.getInstance(context) }
    val enrollment = remember(context) { ClientEnrollmentRepository(context) }
    val wireGuardState by wireGuard.state.collectAsState()
    var profileSaved by remember { mutableStateOf(false) }
    var activationCodeDraft by remember { mutableStateOf("") }
    var enrollmentBusy by remember { mutableStateOf(false) }
    var enrollmentMessage by remember { mutableStateOf("") }
    var hasPendingEnrollment by remember { mutableStateOf(false) }
    var profileDraft by remember { mutableStateOf("") }
    var wireGuardMessage by remember { mutableStateOf("") }
    val scope = rememberCoroutineScope()
    val network by produceState(NetworkCondition.UNKNOWN, monitor) {
        monitor.observe().collect { value = it }
    }
    val autoReconnect by produceState(true, settings) {
        settings.autoReconnectEnabled.collect { value = it }
    }

    val connectingMessage = stringResource(R.string.wg_connecting)
    val connectedMessage = stringResource(R.string.wg_engine_started)
    val connectionFailedMessage = stringResource(R.string.wg_connect_failed)
    val permissionDeniedMessage = stringResource(R.string.wg_permission_denied)
    val profileSavedMessage = stringResource(R.string.wg_profile_saved_message)
    val invalidProfileMessage = stringResource(R.string.wg_invalid_profile)
    val missingProfileMessage = stringResource(R.string.wg_missing_profile)
    val disconnectedMessage = stringResource(R.string.wg_disconnected_message)
    val profileDeletedMessage = stringResource(R.string.wg_profile_deleted)

    LaunchedEffect(wireGuard, enrollment) {
        profileSaved = runCatching { wireGuard.hasProfile() }.getOrDefault(false)
        hasPendingEnrollment = runCatching { enrollment.hasPendingEnrollment() }.getOrDefault(false)
        // A crash may occur after the encrypted profile is committed but before the pending
        // enrollment marker is deleted. In that case the profile is already the successful result.
        if (profileSaved && hasPendingEnrollment) {
            runCatching { enrollment.completeEnrollment() }
            hasPendingEnrollment = runCatching { enrollment.hasPendingEnrollment() }.getOrDefault(false)
        }
    }

    val connectWireGuard: () -> Unit = {
        scope.launch {
            wireGuardMessage = connectingMessage
            try {
                wireGuard.connect()
                wireGuardMessage = connectedMessage
            } catch (_: MissingWireGuardProfileException) {
                profileSaved = false
                wireGuardMessage = missingProfileMessage
            } catch (_: Exception) {
                wireGuardMessage = connectionFailedMessage
            }
        }
    }

    val vpnPermissionLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.StartActivityForResult(),
    ) { result ->
        if (result.resultCode == Activity.RESULT_OK) {
            connectWireGuard()
        } else {
            wireGuardMessage = permissionDeniedMessage
        }
    }

    val requestWireGuardConnection: () -> Unit = {
        val permissionIntent = VpnService.prepare(context)
        if (permissionIntent == null) {
            connectWireGuard()
        } else {
            vpnPermissionLauncher.launch(permissionIntent)
        }
    }

    val saveWireGuardProfile: () -> Unit = {
        scope.launch {
            try {
                wireGuard.saveProfile(profileDraft)
                profileSaved = true
                profileDraft = ""
                wireGuardMessage = profileSavedMessage
            } catch (_: InvalidWireGuardProfileException) {
                wireGuardMessage = invalidProfileMessage
            } catch (_: Exception) {
                wireGuardMessage = invalidProfileMessage
            }
        }
    }

    val disconnectWireGuard: () -> Unit = {
        scope.launch {
            try {
                wireGuard.disconnect()
                wireGuardMessage = disconnectedMessage
            } catch (_: Exception) {
                wireGuardMessage = connectionFailedMessage
            }
        }
    }

    val forgetWireGuardProfile: () -> Unit = {
        scope.launch {
            try {
                wireGuard.forgetProfile()
                profileSaved = false
                profileDraft = ""
                wireGuardMessage = profileDeletedMessage
            } catch (_: Exception) {
                wireGuardMessage = connectionFailedMessage
            }
        }
    }

    val enrollDevice: () -> Unit = {
        if (!enrollment.isConfigured) {
            enrollmentMessage = "نشانی HTTPS کنترل‌پلین در تنظیمات ساخت اندروید تعیین نشده است."
        } else if (profileSaved) {
            enrollmentMessage = "ابتدا پروفایل فعلی را از این گوشی حذف کنید."
        } else if (!hasPendingEnrollment && activationCodeDraft.isBlank()) {
            enrollmentMessage = "کد فعال‌سازی را وارد کنید."
        } else if (!enrollmentBusy) {
            scope.launch {
                enrollmentBusy = true
                enrollmentMessage = if (hasPendingEnrollment) {
                    "در حال ادامهٔ ثبت قبلی با همان کلید امن..."
                } else {
                    "در حال ساخت کلید روی گوشی و ثبت کلید عمومی..."
                }
                try {
                    val result = enrollment.enroll(
                        activationCode = activationCodeDraft,
                        deviceName = Build.MODEL.orEmpty(),
                    )
                    wireGuard.saveProfile(result.profileText)
                    enrollment.completeEnrollment()
                    profileSaved = true
                    hasPendingEnrollment = false
                    activationCodeDraft = ""
                    enrollmentMessage = "دستگاه ثبت شد و پروفایل با رمزگذاری محلی ذخیره شد. وضعیت تونل را جداگانه بررسی کنید."
                } catch (error: ClientEnrollmentException) {
                    enrollmentMessage = error.message ?: "فعال‌سازی دستگاه انجام نشد."
                } catch (error: Exception) {
                    enrollmentMessage = error.message ?: "فعال‌سازی انجام نشد؛ تلاش امن برای ادامه روی همین گوشی نگهداری شده است."
                } finally {
                    hasPendingEnrollment = runCatching { enrollment.hasPendingEnrollment() }.getOrDefault(false)
                    enrollmentBusy = false
                }
            }
        }
    }

    Box(
        Modifier.fillMaxSize()
            .background(Brush.verticalGradient(listOf(Ink, Color(0xFF0A1729), Ink)))
    ) {
        Box(
            Modifier.align(Alignment.TopEnd).offset(x = 72.dp, y = (-70).dp)
                .size(250.dp)
                .background(
                    Brush.radialGradient(listOf(Mint.copy(alpha = 0.17f), Color.Transparent)),
                    CircleShape,
                )
        )
        Box(
            Modifier.align(Alignment.BottomStart).offset(x = (-110).dp, y = 90.dp)
                .size(260.dp)
                .background(
                    Brush.radialGradient(listOf(Violet.copy(alpha = 0.13f), Color.Transparent)),
                    CircleShape,
                )
        )

        Column(
            Modifier.fillMaxSize().verticalScroll(rememberScrollState())
                .padding(horizontal = 20.dp, vertical = 22.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            BrandHeader()
            HeroCard()
            NetworkCard(network)
            EngineCard()
            WireGuardCard(
                state = wireGuardState,
                profileSaved = profileSaved,
                pendingEnrollment = hasPendingEnrollment,
                profileDraft = profileDraft,
                message = if (hasPendingEnrollment) "ثبت دستگاه در انتظار تکمیل است؛ قبل از جایگزینی پروفایل، همان ثبت قبلی را ادامه دهید." else wireGuardMessage,
                onProfileDraftChange = { profileDraft = it },
                onSaveProfile = saveWireGuardProfile,
                onConnect = requestWireGuardConnection,
                onDisconnect = disconnectWireGuard,
                onForgetProfile = forgetWireGuardProfile,
            )
            EnrollmentCard(
                configured = enrollment.isConfigured,
                profileSaved = profileSaved,
                hasPendingEnrollment = hasPendingEnrollment,
                activationCode = activationCodeDraft,
                busy = enrollmentBusy,
                message = enrollmentMessage,
                onActivationCodeChange = { activationCodeDraft = it },
                onEnroll = enrollDevice,
            )
            OfflineCard()
            SettingsCard(
                enabled = autoReconnect,
                onEnabledChange = { enabled ->
                    scope.launch { settings.setAutoReconnectEnabled(enabled) }
                },
            )
            Text(
                stringResource(R.string.phase_footer),
                Modifier.align(Alignment.CenterHorizontally).padding(top = 3.dp),
                color = Mint,
                style = MaterialTheme.typography.labelMedium,
                letterSpacing = 1.1.sp,
            )
            Text(
                stringResource(R.string.disclaimer),
                Modifier.align(Alignment.CenterHorizontally),
                color = SoftText,
                style = MaterialTheme.typography.bodySmall,
            )
        }
    }
}


@Composable
private fun EnrollmentCard(
    configured: Boolean,
    profileSaved: Boolean,
    hasPendingEnrollment: Boolean,
    activationCode: String,
    busy: Boolean,
    message: String,
    onActivationCodeChange: (String) -> Unit,
    onEnroll: () -> Unit,
) {
    PanelCard {
        Text(
            "DEVICE ENROLLMENT",
            color = Mint,
            style = MaterialTheme.typography.labelMedium,
            letterSpacing = 1.0.sp,
        )
        Text(
            "فعال‌سازی امن دستگاه",
            color = Color.White,
            style = MaterialTheme.typography.titleMedium,
            fontWeight = FontWeight.Bold,
        )
        Text(
            when {
                profileSaved -> "یک پروفایل روی این گوشی ذخیره شده است. برای جلوگیری از جایگزینی ناخواسته، ثبت دستگاه جدید غیرفعال است."
                hasPendingEnrollment -> "یک درخواست ثبت نیمه‌تمام به‌صورت رمزگذاری‌شده روی این گوشی وجود دارد. ادامه، همان کلید و شناسه را دوباره استفاده می‌کند."
                else -> "کد ۱۵ دقیقه‌ای را از مدیر بگیرید. کلید خصوصی همین‌جا ساخته می‌شود و فقط کلید عمومی به سرور می‌رود."
            },
            color = SoftText,
            style = MaterialTheme.typography.bodySmall,
            lineHeight = 20.sp,
        )
        if (configured && !profileSaved && !hasPendingEnrollment) {
            OutlinedTextField(
                value = activationCode,
                onValueChange = onActivationCodeChange,
                modifier = Modifier.fillMaxWidth(),
                label = { Text("کد فعال‌سازی") },
                placeholder = { Text("کد یک‌بارمصرف ۴۳ نویسه‌ای") },
                singleLine = true,
                textStyle = MaterialTheme.typography.bodyMedium,
            )
        }
        if (!configured) {
            Text(
                "این نسخه هنوز با ZEUS_API_BASE_URL ساخته نشده است. تا زمان انتشار HTTPS کنترل‌پلین، فعال‌سازی آنلاین در دسترس نیست.",
                color = Color(0xFFFFC879),
                style = MaterialTheme.typography.bodySmall,
                lineHeight = 19.sp,
            )
        }
        Button(
            onClick = onEnroll,
            enabled = configured && !profileSaved && !busy &&
                (hasPendingEnrollment || activationCode.isNotBlank()),
            modifier = Modifier.fillMaxWidth(),
            colors = ButtonDefaults.buttonColors(containerColor = Mint, contentColor = Ink),
        ) {
            Text(
                when {
                    busy -> "در حال انجام..."
                    hasPendingEnrollment -> "ادامهٔ ثبت قبلی"
                    else -> "ساخت کلید و فعال‌سازی"
                },
                fontWeight = FontWeight.SemiBold,
            )
        }
        if (message.isNotBlank()) {
            Text(
                message,
                color = if (message.contains("ثبت شد")) Mint else Color(0xFFFFC879),
                style = MaterialTheme.typography.bodySmall,
                lineHeight = 19.sp,
            )
        }
        Text(
            "کلید خصوصی در فایل رمزگذاری‌شدهٔ Android Keystore باقی می‌ماند؛ هیچ توکن مدیریتی در اپ کاربر استفاده نمی‌شود.",
            color = SoftText,
            style = MaterialTheme.typography.bodySmall,
            lineHeight = 19.sp,
        )
    }
}

@Composable
private fun BrandHeader() {
    Row(
        Modifier.fillMaxWidth().padding(top = 4.dp, bottom = 2.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text("ZEUS", color = Color.White, fontSize = 31.sp,
                fontWeight = FontWeight.Black, letterSpacing = 3.sp)
            Text(stringResource(R.string.brand_subtitle), color = SoftText,
                style = MaterialTheme.typography.labelSmall, letterSpacing = 1.4.sp)
        }
        Surface(
            shape = RoundedCornerShape(17.dp),
            color = Mint.copy(alpha = 0.10f),
            border = BorderStroke(1.dp, Mint.copy(alpha = 0.42f)),
        ) {
            Box(Modifier.size(52.dp), contentAlignment = Alignment.Center) {
                Text("Z", color = Mint, fontSize = 26.sp, fontWeight = FontWeight.Black)
            }
        }
    }
}

@Composable
private fun HeroCard() {
    Card(
        modifier = Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(26.dp),
        colors = CardDefaults.cardColors(containerColor = Panel),
        elevation = CardDefaults.cardElevation(defaultElevation = 8.dp),
        border = BorderStroke(1.dp, Mint.copy(alpha = 0.20f)),
    ) {
        Box(
            Modifier.fillMaxWidth()
                .background(
                    Brush.linearGradient(
                        listOf(Color(0xFF172C42), Color(0xFF122039), Color(0xFF1A2140))
                    )
                )
                .padding(21.dp)
        ) {
            Column(
                Modifier.fillMaxWidth(0.83f),
                verticalArrangement = Arrangement.spacedBy(9.dp),
            ) {
                Text(stringResource(R.string.hero_kicker), color = Mint,
                    style = MaterialTheme.typography.labelSmall, letterSpacing = 1.2.sp)
                Text(stringResource(R.string.hero_heading), color = Color.White,
                    style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold)
                Text(stringResource(R.string.hero_description), color = SoftText,
                    style = MaterialTheme.typography.bodySmall, lineHeight = 20.sp)
                Surface(
                    shape = RoundedCornerShape(8.dp),
                    color = Color(0xFFF2C879).copy(alpha = 0.10f),
                    border = BorderStroke(1.dp, Color(0xFFF2C879).copy(alpha = 0.22f)),
                ) {
                    Text(stringResource(R.string.preview_badge),
                        Modifier.padding(horizontal = 10.dp, vertical = 6.dp),
                        color = Color(0xFFF2C879), style = MaterialTheme.typography.labelSmall)
                }
            }
            Surface(
                modifier = Modifier.align(Alignment.CenterEnd).size(54.dp),
                shape = RoundedCornerShape(18.dp),
                color = Mint.copy(alpha = 0.11f),
                border = BorderStroke(1.dp, Mint.copy(alpha = 0.35f)),
            ) {
                Box(contentAlignment = Alignment.Center) {
                    Text("Z", color = Mint, fontSize = 28.sp, fontWeight = FontWeight.Black)
                }
            }
        }
    }
}

@Composable
private fun NetworkCard(network: NetworkCondition) {
    val (networkText, networkColor, networkDetail) = when (network) {
        NetworkCondition.ONLINE -> Triple(stringResource(R.string.online), Mint, stringResource(R.string.online_detail))
        NetworkCondition.LIMITED -> Triple(stringResource(R.string.limited), Color(0xFFFFC46B), stringResource(R.string.limited_detail))
        NetworkCondition.OFFLINE -> Triple(stringResource(R.string.offline), Color(0xFFFF7D88), stringResource(R.string.offline_detail))
        NetworkCondition.UNKNOWN -> Triple(stringResource(R.string.checking), SoftText, stringResource(R.string.checking_detail))
    }
    PanelCard {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(stringResource(R.string.network_title), color = SoftText,
                    style = MaterialTheme.typography.labelMedium)
                Text(networkText, color = networkColor,
                    style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold)
            }
            StatusPill(networkColor, networkText)
        }
        Text(networkDetail, color = Color.White, style = MaterialTheme.typography.bodyMedium)
        HorizontalDivider(color = Line.copy(alpha = 0.75f))
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text("ⓘ", color = Mint, fontSize = 14.sp)
            Spacer(Modifier.width(8.dp))
            Text(stringResource(R.string.network_note), color = SoftText,
                style = MaterialTheme.typography.bodySmall)
        }
    }
}

@Composable
private fun EngineCard() {
    PanelCard {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(stringResource(R.string.engine_title), color = Mint,
                style = MaterialTheme.typography.labelMedium, letterSpacing = 1.1.sp)
            Spacer(Modifier.weight(1f))
            StatusPill(Color(0xFFFFC46B), stringResource(R.string.foundation_label))
        }
        Text(stringResource(R.string.engine_heading), color = Color.White,
            style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold)
        Text(stringResource(R.string.engine_body), color = SoftText,
            style = MaterialTheme.typography.bodyMedium, lineHeight = 22.sp)
        StatusPill(Color(0xFFFFC46B), stringResource(R.string.engine_status))
    }
}

@Composable
private fun WireGuardCard(
    state: WireGuardState,
    profileSaved: Boolean,
    pendingEnrollment: Boolean,
    profileDraft: String,
    message: String,
    onProfileDraftChange: (String) -> Unit,
    onSaveProfile: () -> Unit,
    onConnect: () -> Unit,
    onDisconnect: () -> Unit,
    onForgetProfile: () -> Unit,
) {
    val (statusLabel, statusColor) = when (state) {
        WireGuardState.DISCONNECTED -> stringResource(R.string.wg_disconnected) to SoftText
        WireGuardState.CONNECTING -> stringResource(R.string.wg_connecting) to Color(0xFFFFC46B)
        WireGuardState.ENGINE_UP -> stringResource(R.string.wg_engine_up) to Mint
        WireGuardState.DISCONNECTING -> stringResource(R.string.wg_disconnecting) to Color(0xFFFFC46B)
    }

    PanelCard {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(
                    stringResource(R.string.wg_title),
                    color = Mint,
                    style = MaterialTheme.typography.labelMedium,
                    letterSpacing = 1.0.sp,
                )
                Text(
                    stringResource(R.string.wg_subtitle),
                    color = Color.White,
                    style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.Bold,
                )
            }
            StatusPill(statusColor, statusLabel)
        }

        Text(
            if (profileSaved) stringResource(R.string.wg_profile_is_saved)
            else stringResource(R.string.wg_profile_required),
            color = SoftText,
            style = MaterialTheme.typography.bodySmall,
            lineHeight = 20.sp,
        )

        OutlinedTextField(
            value = profileDraft,
            onValueChange = onProfileDraftChange,
            modifier = Modifier.fillMaxWidth(),
            label = { Text(stringResource(R.string.wg_profile_label)) },
            placeholder = { Text(stringResource(R.string.wg_profile_placeholder)) },
            minLines = 6,
            maxLines = 10,
            textStyle = MaterialTheme.typography.bodySmall,
        )

        Text(
            stringResource(R.string.wg_profile_security_note),
            color = SoftText,
            style = MaterialTheme.typography.bodySmall,
            lineHeight = 19.sp,
        )

        Row(
            Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(9.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Button(
                onClick = onSaveProfile,
                enabled = profileDraft.isNotBlank() && state != WireGuardState.ENGINE_UP && !pendingEnrollment,
                modifier = Modifier.weight(1f),
                colors = ButtonDefaults.buttonColors(containerColor = Mint, contentColor = Ink),
            ) {
                Text(
                    if (profileSaved) stringResource(R.string.wg_replace_profile)
                    else stringResource(R.string.wg_save_profile),
                    fontWeight = FontWeight.SemiBold,
                )
            }
            OutlinedButton(
                onClick = if (state == WireGuardState.ENGINE_UP) onDisconnect else onConnect,
                enabled = when (state) {
                    WireGuardState.ENGINE_UP -> true
                    WireGuardState.CONNECTING, WireGuardState.DISCONNECTING -> false
                    WireGuardState.DISCONNECTED -> profileSaved
                },
                modifier = Modifier.weight(1f),
                border = BorderStroke(1.dp, statusColor.copy(alpha = 0.55f)),
            ) {
                Text(
                    if (state == WireGuardState.ENGINE_UP) stringResource(R.string.wg_disconnect)
                    else stringResource(R.string.wg_connect),
                    color = Color.White,
                )
            }
        }

        if (profileSaved) {
            TextButton(
                onClick = onForgetProfile,
                enabled = state == WireGuardState.DISCONNECTED,
                modifier = Modifier.align(Alignment.End),
            ) {
                Text(stringResource(R.string.wg_delete_profile), color = Color(0xFFFF9BA8))
            }
        }

        if (message.isNotBlank()) {
            Text(
                message,
                color = if (state == WireGuardState.ENGINE_UP) Mint else Color(0xFFFFC879),
                style = MaterialTheme.typography.bodySmall,
                lineHeight = 19.sp,
            )
        }

        Text(
            stringResource(R.string.wg_handshake_note),
            color = SoftText,
            style = MaterialTheme.typography.bodySmall,
            lineHeight = 19.sp,
        )
    }
}

@Composable
private fun OfflineCard() {
    PanelCard {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Surface(shape = RoundedCornerShape(11.dp), color = Violet.copy(alpha = 0.13f)) {
                Text("⌁", Modifier.padding(horizontal = 11.dp, vertical = 7.dp),
                    color = Violet, fontSize = 19.sp)
            }
            Spacer(Modifier.width(10.dp))
            Text(stringResource(R.string.offline_title), color = Color.White,
                style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
        }
        Text(stringResource(R.string.offline_body), color = SoftText,
            style = MaterialTheme.typography.bodyMedium, lineHeight = 22.sp)
    }
}

@Composable
private fun SettingsCard(enabled: Boolean, onEnabledChange: (Boolean) -> Unit) {
    PanelCard {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(5.dp)) {
                Text(stringResource(R.string.settings_title), color = Color.White,
                    fontWeight = FontWeight.SemiBold, style = MaterialTheme.typography.titleMedium)
                Text(stringResource(R.string.autoreconnect), color = SoftText,
                    style = MaterialTheme.typography.bodyMedium)
                Text(stringResource(R.string.autoreconnect_note), color = SoftText,
                    style = MaterialTheme.typography.bodySmall, lineHeight = 19.sp)
            }
            Switch(
                checked = enabled,
                onCheckedChange = onEnabledChange,
                colors = SwitchDefaults.colors(
                    checkedThumbColor = Ink, checkedTrackColor = Mint,
                    uncheckedThumbColor = SoftText,
                ),
            )
        }
    }
}

@Composable
private fun StatusPill(color: Color, label: String) {
    Surface(
        shape = RoundedCornerShape(50),
        color = color.copy(alpha = 0.10f),
        border = BorderStroke(1.dp, color.copy(alpha = 0.24f)),
    ) {
        Row(
            Modifier.padding(horizontal = 10.dp, vertical = 7.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Box(Modifier.size(6.dp).background(color, CircleShape))
            Text(label, color = color, style = MaterialTheme.typography.labelSmall)
        }
    }
}

@Composable
private fun PanelCard(content: @Composable ColumnScope.() -> Unit) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(22.dp),
        colors = CardDefaults.cardColors(containerColor = Panel.copy(alpha = 0.96f)),
        elevation = CardDefaults.cardElevation(defaultElevation = 5.dp),
        border = BorderStroke(1.dp, Color.White.copy(alpha = 0.075f)),
    ) {
        Column(
            Modifier.padding(19.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
            content = content,
        )
    }
}
