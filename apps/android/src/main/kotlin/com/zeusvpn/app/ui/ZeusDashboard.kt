package com.zeusvpn.app.ui

import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.draw.scale
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.PasswordVisualTransformation
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
import com.zeusvpn.app.vpn.WireGuardTrafficStats
import android.app.Activity
import android.net.VpnService
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.launch

private val Ink = Color(0xFF070D19)
private val Panel = Color(0xFF111D31)
private val Mint = Color(0xFF62E6C2)
private val Violet = Color(0xFF9D85FF)
private val ElectricBlue = Color(0xFF67C8FF)
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

    val trafficStats by produceState(WireGuardTrafficStats.EMPTY, wireGuard, wireGuardState) {
        if (wireGuardState != WireGuardState.ENGINE_UP) {
            value = WireGuardTrafficStats.EMPTY
        } else {
            while (true) {
                runCatching { wireGuard.trafficStats() }
                    .onSuccess { value = it }
                delay(3_000L)
            }
        }
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
            .background(Brush.verticalGradient(listOf(Color(0xFF050A14), Color(0xFF0A1729), Ink)))
    ) {
        AnimatedAuroraBackground()

        Column(
            Modifier.fillMaxSize().verticalScroll(rememberScrollState())
                .padding(horizontal = 20.dp, vertical = 22.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            BrandHeader()
            HeroCard(
                state = wireGuardState,
                profileSaved = profileSaved,
                onConnect = requestWireGuardConnection,
                onDisconnect = disconnectWireGuard,
            )
            if (wireGuardState == WireGuardState.ENGINE_UP) {
                TrafficStatsCard(trafficStats)
            }
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
                visualTransformation = PasswordVisualTransformation(),
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
private fun AnimatedAuroraBackground() {
    val transition = rememberInfiniteTransition(label = "zeus-aurora")
    val driftX by transition.animateFloat(
        initialValue = -20f,
        targetValue = 24f,
        animationSpec = infiniteRepeatable(
            animation = tween(durationMillis = 13_000, easing = FastOutSlowInEasing),
            repeatMode = RepeatMode.Reverse,
        ),
        label = "aurora-drift-x",
    )
    val driftY by transition.animateFloat(
        initialValue = 18f,
        targetValue = -18f,
        animationSpec = infiniteRepeatable(
            animation = tween(durationMillis = 16_000, easing = FastOutSlowInEasing),
            repeatMode = RepeatMode.Reverse,
        ),
        label = "aurora-drift-y",
    )

    Box(Modifier.fillMaxSize()) {
        Box(
            Modifier
                .align(Alignment.TopEnd)
                .offset(x = (48f + driftX).dp, y = (-82f + driftY).dp)
                .size(340.dp)
                .background(
                    Brush.radialGradient(
                        listOf(Mint.copy(alpha = 0.19f), ElectricBlue.copy(alpha = 0.045f), Color.Transparent)
                    ),
                    CircleShape,
                )
        )
        Box(
            Modifier
                .align(Alignment.BottomStart)
                .offset(x = (-118f - driftY).dp, y = (82f + driftX).dp)
                .size(310.dp)
                .background(
                    Brush.radialGradient(
                        listOf(Violet.copy(alpha = 0.17f), ElectricBlue.copy(alpha = 0.035f), Color.Transparent)
                    ),
                    CircleShape,
                )
        )
    }
}

@Composable
private fun BrandHeader() {
    val transition = rememberInfiniteTransition(label = "zeus-brand")
    val logoPulse by transition.animateFloat(
        initialValue = 0.97f,
        targetValue = 1.035f,
        animationSpec = infiniteRepeatable(
            animation = tween(durationMillis = 3_600, easing = FastOutSlowInEasing),
            repeatMode = RepeatMode.Reverse,
        ),
        label = "brand-pulse",
    )

    Row(
        Modifier.fillMaxWidth().padding(top = 4.dp, bottom = 2.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Text(
                "ZEUS",
                color = Color.White,
                fontSize = 32.sp,
                fontWeight = FontWeight.Black,
                letterSpacing = 3.5.sp,
            )
            Text(
                stringResource(R.string.brand_subtitle).uppercase(),
                color = SoftText,
                style = MaterialTheme.typography.labelSmall,
                letterSpacing = 1.6.sp,
            )
        }
        Box(
            Modifier
                .size(58.dp)
                .scale(logoPulse)
                .clip(RoundedCornerShape(19.dp))
                .background(Brush.linearGradient(listOf(Mint.copy(alpha = 0.20f), Violet.copy(alpha = 0.18f))))
                .border(
                    BorderStroke(1.dp, Brush.linearGradient(listOf(Mint, Violet, ElectricBlue))),
                    RoundedCornerShape(19.dp),
                ),
            contentAlignment = Alignment.Center,
        ) {
            Text(
                "Z",
                color = Mint,
                fontSize = 29.sp,
                fontWeight = FontWeight.Black,
            )
        }
    }
}

@Composable
private fun HeroCard(
    state: WireGuardState,
    profileSaved: Boolean,
    onConnect: () -> Unit,
    onDisconnect: () -> Unit,
) {
    val transition = rememberInfiniteTransition(label = "zeus-connection-orbit")
    val orbitRotation by transition.animateFloat(
        initialValue = 0f,
        targetValue = 360f,
        animationSpec = infiniteRepeatable(
            animation = tween(durationMillis = 18_000, easing = LinearEasing),
            repeatMode = RepeatMode.Restart,
        ),
        label = "connection-orbit-rotation",
    )
    val connectedPulse by transition.animateFloat(
        initialValue = 0.96f,
        targetValue = 1.045f,
        animationSpec = infiniteRepeatable(
            animation = tween(durationMillis = 2_400, easing = FastOutSlowInEasing),
            repeatMode = RepeatMode.Reverse,
        ),
        label = "connection-pulse",
    )

    val isConnected = state == WireGuardState.ENGINE_UP
    val isBusy = state == WireGuardState.CONNECTING || state == WireGuardState.DISCONNECTING
    val canToggle = isConnected || (state == WireGuardState.DISCONNECTED && profileSaved)
    val (statusLabel, statusColor, helperText) = when (state) {
        WireGuardState.DISCONNECTED -> Triple(
            stringResource(R.string.wg_disconnected),
            SoftText,
            if (profileSaved) stringResource(R.string.wg_profile_is_saved)
            else stringResource(R.string.wg_profile_required),
        )
        WireGuardState.CONNECTING -> Triple(
            stringResource(R.string.wg_connecting),
            Color(0xFFFFC46B),
            stringResource(R.string.wg_connecting_message),
        )
        WireGuardState.ENGINE_UP -> Triple(
            stringResource(R.string.wg_engine_up),
            Mint,
            stringResource(R.string.wg_engine_started),
        )
        WireGuardState.DISCONNECTING -> Triple(
            stringResource(R.string.wg_disconnecting),
            Color(0xFFFFC46B),
            stringResource(R.string.wg_disconnecting),
        )
    }
    val actionLabel = when {
        isConnected -> stringResource(R.string.wg_disconnect)
        isBusy -> statusLabel
        else -> stringResource(R.string.wg_connect)
    }

    Card(
        modifier = Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(28.dp),
        colors = CardDefaults.cardColors(containerColor = Panel),
        elevation = CardDefaults.cardElevation(defaultElevation = 10.dp),
        border = BorderStroke(
            1.dp,
            Brush.linearGradient(listOf(Mint.copy(alpha = 0.50f), Violet.copy(alpha = 0.34f), ElectricBlue.copy(alpha = 0.18f))),
        ),
    ) {
        Box(
            Modifier.fillMaxWidth()
                .background(
                    Brush.linearGradient(
                        listOf(Color(0xFF142C40), Color(0xFF111D35), Color(0xFF1B1B3A))
                    )
                )
                .padding(18.dp)
        ) {
            Column(verticalArrangement = Arrangement.spacedBy(14.dp)) {
                Row(
                    Modifier.fillMaxWidth(),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Column(
                        Modifier.weight(1f),
                        verticalArrangement = Arrangement.spacedBy(9.dp),
                    ) {
                        Text(
                            stringResource(R.string.hero_kicker),
                            color = Mint,
                            style = MaterialTheme.typography.labelSmall,
                            letterSpacing = 1.25.sp,
                        )
                        Text(
                            stringResource(R.string.hero_heading),
                            color = Color.White,
                            fontSize = 21.sp,
                            lineHeight = 27.sp,
                            fontWeight = FontWeight.Bold,
                        )
                        Text(
                            stringResource(R.string.hero_description),
                            color = SoftText,
                            style = MaterialTheme.typography.bodySmall,
                            lineHeight = 19.sp,
                        )
                    }

                    Spacer(Modifier.width(8.dp))

                    Box(Modifier.size(126.dp), contentAlignment = Alignment.Center) {
                        Box(
                            Modifier
                                .fillMaxSize()
                                .border(
                                    BorderStroke(
                                        1.dp,
                                        Brush.sweepGradient(listOf(Mint.copy(alpha = 0.82f), Violet.copy(alpha = 0.72f), ElectricBlue.copy(alpha = 0.82f), Mint.copy(alpha = 0.82f))),
                                    ),
                                    CircleShape,
                                )
                        )
                        Box(
                            Modifier.fillMaxSize().rotate(orbitRotation),
                            contentAlignment = Alignment.TopCenter,
                        ) {
                            Box(
                                Modifier
                                    .size(9.dp)
                                    .background(if (isConnected) Mint else ElectricBlue, CircleShape)
                            )
                        }
                        Box(
                            Modifier
                                .size(88.dp)
                                .scale(if (isConnected) connectedPulse else 1f)
                                .clip(CircleShape)
                                .background(
                                    Brush.linearGradient(
                                        if (isConnected) listOf(Mint, ElectricBlue)
                                        else listOf(Color(0xFF8E86FF), Mint),
                                    )
                                )
                                .border(BorderStroke(1.dp, Color.White.copy(alpha = 0.34f)), CircleShape)
                                .clickable(enabled = canToggle && !isBusy) {
                                    if (isConnected) onDisconnect() else onConnect()
                                }
                                .semantics {
                                    contentDescription = actionLabel
                                    role = Role.Button
                                },
                            contentAlignment = Alignment.Center,
                        ) {
                            Text(
                                "⏻",
                                color = Ink,
                                fontSize = 37.sp,
                                fontWeight = FontWeight.Bold,
                            )
                        }
                    }
                }

                Row(
                    Modifier.fillMaxWidth(),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    StatusPill(statusColor, statusLabel)
                    Text(
                        actionLabel,
                        color = if (canToggle) Mint else SoftText,
                        style = MaterialTheme.typography.labelMedium,
                        fontWeight = FontWeight.SemiBold,
                    )
                    Spacer(Modifier.weight(1f))
                    Text(
                        stringResource(R.string.preview_badge),
                        color = Color(0xFFFFC879),
                        style = MaterialTheme.typography.labelSmall,
                    )
                }

                Text(
                    helperText,
                    color = SoftText,
                    style = MaterialTheme.typography.bodySmall,
                    lineHeight = 18.sp,
                )
            }
        }
    }
}


@Composable
private fun TrafficStatsCard(stats: WireGuardTrafficStats) {
    PanelCard {
        Row(
            Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text("LIVE TUNNEL TRAFFIC", color = Mint,
                    style = MaterialTheme.typography.labelSmall, letterSpacing = 1.05.sp)
                Text("آمار ترافیک تونل", color = Color.White,
                    style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
            }
            StatusPill(Mint, "WireGuard")
        }
        Row(
            Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            TrafficValue(
                modifier = Modifier.weight(1f),
                label = "دریافت‌شده",
                value = formatTrafficBytes(stats.receivedBytes),
                accent = ElectricBlue,
                symbol = "↓",
            )
            TrafficValue(
                modifier = Modifier.weight(1f),
                label = "ارسال‌شده",
                value = formatTrafficBytes(stats.sentBytes),
                accent = Mint,
                symbol = "↑",
            )
        }
        HorizontalDivider(color = Line.copy(alpha = 0.75f))
        Row(verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.size(7.dp).background(
                if (stats.latestHandshakeEpochMillis > 0L) Mint else Color(0xFFFFC46B),
                CircleShape,
            ))
            Spacer(Modifier.width(9.dp))
            Text(
                formatLatestHandshake(stats.latestHandshakeEpochMillis),
                color = if (stats.latestHandshakeEpochMillis > 0L) Mint else Color(0xFFFFC879),
                style = MaterialTheme.typography.bodySmall,
            )
        }
        Text(
            "شمارنده‌ها از موتور WireGuard خوانده می‌شوند؛ فعال بودن موتور به‌تنهایی تضمین‌کنندهٔ عبور ترافیک نیست.",
            color = SoftText,
            style = MaterialTheme.typography.labelSmall,
            lineHeight = 17.sp,
        )
    }
}

@Composable
private fun TrafficValue(
    modifier: Modifier,
    label: String,
    value: String,
    accent: Color,
    symbol: String,
) {
    Surface(
        modifier = modifier,
        shape = RoundedCornerShape(15.dp),
        color = accent.copy(alpha = 0.07f),
        border = BorderStroke(1.dp, accent.copy(alpha = 0.18f)),
    ) {
        Column(
            Modifier.padding(horizontal = 12.dp, vertical = 13.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(symbol, color = accent, fontSize = 16.sp, fontWeight = FontWeight.Bold)
                Spacer(Modifier.width(6.dp))
                Text(label, color = SoftText, style = MaterialTheme.typography.labelSmall)
            }
            Text(
                value,
                color = Color.White,
                fontSize = 16.sp,
                fontWeight = FontWeight.Bold,
                maxLines = 1,
            )
        }
    }
}

private fun formatTrafficBytes(bytes: Long): String {
    val safeBytes = bytes.coerceAtLeast(0L).toDouble()
    val units = arrayOf("B", "KB", "MB", "GB", "TB")
    var value = safeBytes
    var unitIndex = 0
    while (value >= 1024.0 && unitIndex < units.lastIndex) {
        value /= 1024.0
        unitIndex++
    }
    return if (unitIndex == 0) {
        "${value.toLong()} ${units[unitIndex]}"
    } else {
        String.format(java.util.Locale.US, "%.1f %s", value, units[unitIndex])
    }
}

private fun formatLatestHandshake(epochMillis: Long): String {
    if (epochMillis <= 0L) return "هنوز Handshake موفقی ثبت نشده است"
    val seconds = ((System.currentTimeMillis() - epochMillis).coerceAtLeast(0L)) / 1_000L
    val elapsed = when {
        seconds < 60L -> "${seconds} ثانیه قبل"
        seconds < 3_600L -> "${seconds / 60L} دقیقه قبل"
        seconds < 86_400L -> "${seconds / 3_600L} ساعت قبل"
        else -> "${seconds / 86_400L} روز قبل"
    }
    return "آخرین Handshake: $elapsed"
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

        Button(
            onClick = onSaveProfile,
            enabled = profileDraft.isNotBlank() && state != WireGuardState.ENGINE_UP && !pendingEnrollment,
            modifier = Modifier.fillMaxWidth(),
            colors = ButtonDefaults.buttonColors(containerColor = Mint, contentColor = Ink),
        ) {
            Text(
                if (profileSaved) stringResource(R.string.wg_replace_profile)
                else stringResource(R.string.wg_save_profile),
                fontWeight = FontWeight.SemiBold,
            )
        }
        Text(
            "کنترل اتصال از دکمهٔ مرکزی بالای صفحه انجام می‌شود.",
            color = SoftText,
            style = MaterialTheme.typography.labelSmall,
        )

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
        border = BorderStroke(
            1.dp,
            Brush.linearGradient(
                listOf(Mint.copy(alpha = 0.18f), Violet.copy(alpha = 0.12f), Color.White.copy(alpha = 0.045f))
            ),
        ),
    ) {
        Column(
            Modifier.padding(19.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
            content = content,
        )
    }
}
