package com.zeusvpn.app.ui

import android.app.Activity
import android.content.ContentResolver
import android.net.Uri
import android.net.VpnService
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
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
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.ByteArrayOutputStream
import java.io.IOException
import java.util.Locale

private val AppBackground = Color(0xFF0B1220)
private val Panel = Color(0xFF151F30)
private val PanelRaised = Color(0xFF1B293D)
private val Primary = Color(0xFF4E9DDB)
private val Positive = Color(0xFF42C69A)
private val Warning = Color(0xFFE6B566)
private val Danger = Color(0xFFE17E88)
private val MainText = Color(0xFFF2F5F9)
private val SecondaryText = Color(0xFFA7B5C8)
private val MutedText = Color(0xFF74849B)
private val DividerColor = Color(0xFF2A394E)

@Composable
fun ZeusDashboard() {
    val context = LocalContext.current.applicationContext
    val monitor = remember(context) { NetworkMonitor(context) }
    val settings = remember(context) { SettingsRepository(context) }
    val wireGuard = remember(context) { WireGuardEngine.getInstance(context) }
    val enrollment = remember(context) { ClientEnrollmentRepository(context) }
    val wireGuardState by wireGuard.state.collectAsState()
    val scope = rememberCoroutineScope()

    var profileSaved by remember { mutableStateOf(false) }
    var activationCodeDraft by remember { mutableStateOf("") }
    var enrollmentBusy by remember { mutableStateOf(false) }
    var enrollmentMessage by remember { mutableStateOf("") }
    var hasPendingEnrollment by remember { mutableStateOf(false) }
    var profileDraft by remember { mutableStateOf("") }
    var wireGuardMessage by remember { mutableStateOf("") }
    var profileImportBusy by remember { mutableStateOf(false) }
    var showAdvancedImport by remember { mutableStateOf(false) }

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

    val saveWireGuardProfile: () -> Unit = {
        scope.launch {
            try {
                wireGuard.saveProfile(profileDraft)
                profileSaved = true
                profileDraft = ""
                showAdvancedImport = false
                wireGuardMessage = profileSavedMessage
            } catch (_: InvalidWireGuardProfileException) {
                wireGuardMessage = invalidProfileMessage
            } catch (_: Exception) {
                wireGuardMessage = invalidProfileMessage
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
            enrollmentMessage = "دریافت خودکار هنوز به نشانی امن Control Plane متصل نشده است."
        } else if (profileSaved) {
            enrollmentMessage = "پروفایل فعلی روی این گوشی وجود دارد."
        } else if (!hasPendingEnrollment && activationCodeDraft.isBlank()) {
            enrollmentMessage = "کد دعوت یک‌بارمصرف را وارد کنید."
        } else if (!enrollmentBusy) {
            scope.launch {
                enrollmentBusy = true
                enrollmentMessage = if (hasPendingEnrollment) {
                    "در حال ادامهٔ ثبت قبلی با همان کلید امن..."
                } else {
                    "در حال ساخت کلید روی گوشی و ثبت دستگاه..."
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
                    enrollmentMessage = "دستگاه ثبت شد؛ پروفایل به‌صورت رمزگذاری‌شده ذخیره شد."
                } catch (error: ClientEnrollmentException) {
                    enrollmentMessage = error.message ?: "ثبت دستگاه انجام نشد."
                } catch (error: Exception) {
                    enrollmentMessage = error.message ?: "ثبت انجام نشد؛ تلاش امن برای ادامه روی همین گوشی نگهداری شده است."
                } finally {
                    hasPendingEnrollment = runCatching { enrollment.hasPendingEnrollment() }.getOrDefault(false)
                    enrollmentBusy = false
                }
            }
        }
    }

    val profilePicker = rememberLauncherForActivityResult(
        ActivityResultContracts.OpenDocument(),
    ) { uri: Uri? ->
        if (uri != null) {
            scope.launch {
                profileImportBusy = true
                wireGuardMessage = "در حال خواندن و اعتبارسنجی فایل..."
                try {
                    val profileText = readProfileDocument(context.contentResolver, uri)
                    wireGuard.saveProfile(profileText)
                    profileSaved = true
                    profileDraft = ""
                    showAdvancedImport = false
                    wireGuardMessage = profileSavedMessage
                } catch (_: InvalidWireGuardProfileException) {
                    wireGuardMessage = invalidProfileMessage
                } catch (_: Exception) {
                    wireGuardMessage = "فایل انتخاب‌شده قابل خواندن یا معتبر نیست."
                } finally {
                    profileImportBusy = false
                }
            }
        }
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(AppBackground)
            .verticalScroll(rememberScrollState())
            .imePadding()
            .padding(horizontal = 20.dp, vertical = 18.dp),
        verticalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        BrandHeader()

        ConnectionCard(
            state = wireGuardState,
            profileSaved = profileSaved,
            message = wireGuardMessage,
            onConnect = requestWireGuardConnection,
            onDisconnect = disconnectWireGuard,
        )

        if (wireGuardState == WireGuardState.ENGINE_UP) {
            TrafficStatsCard(trafficStats)
        }

        NetworkSummaryCard(network)

        ProfileSetupCard(
            configured = enrollment.isConfigured,
            profileSaved = profileSaved,
            hasPendingEnrollment = hasPendingEnrollment,
            activationCode = activationCodeDraft,
            enrollmentBusy = enrollmentBusy,
            importBusy = profileImportBusy,
            enrollmentMessage = enrollmentMessage,
            profileMessage = wireGuardMessage,
            profileDraft = profileDraft,
            showAdvancedImport = showAdvancedImport,
            canForgetProfile = wireGuardState == WireGuardState.DISCONNECTED,
            onActivationCodeChange = { value ->
                if (value.length <= 43 && value.all {
                    it in 'A'..'Z' || it in 'a'..'z' || it in '0'..'9' || it == '-' || it == '_'
                }) {
                    activationCodeDraft = value
                }
            },
            onEnroll = enrollDevice,
            onChooseFile = {
                if (!profileImportBusy && wireGuardState == WireGuardState.DISCONNECTED) {
                    profilePicker.launch(arrayOf("*/*"))
                }
            },
            onToggleAdvanced = { showAdvancedImport = !showAdvancedImport },
            onProfileDraftChange = { profileDraft = it },
            onSaveProfile = saveWireGuardProfile,
            onForgetProfile = forgetWireGuardProfile,
        )

        SettingsCard(
            enabled = autoReconnect,
            onEnabledChange = { enabled ->
                scope.launch { settings.setAutoReconnectEnabled(enabled) }
            },
        )

        Text(
            stringResource(R.string.disclaimer),
            modifier = Modifier.fillMaxWidth().padding(top = 3.dp, bottom = 8.dp),
            color = MutedText,
            style = MaterialTheme.typography.bodySmall,
            lineHeight = 18.sp,
        )
    }
}

@Composable
private fun BrandHeader() {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 3.dp, bottom = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Surface(
            modifier = Modifier.size(44.dp),
            shape = RoundedCornerShape(13.dp),
            color = Primary,
        ) {
            Box(contentAlignment = Alignment.Center) {
                Text(
                    "Z",
                    color = Color.White,
                    fontSize = 25.sp,
                    fontWeight = FontWeight.ExtraBold,
                )
            }
        }
        Spacer(Modifier.width(12.dp))
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(
                "ZEUS VPN",
                color = MainText,
                fontSize = 20.sp,
                fontWeight = FontWeight.Bold,
                letterSpacing = 0.7.sp,
            )
            Text(
                "اتصال امن و ساده",
                color = SecondaryText,
                style = MaterialTheme.typography.bodySmall,
            )
        }
        Text(
            "ANDROID",
            color = MutedText,
            style = MaterialTheme.typography.labelSmall,
            letterSpacing = 1.1.sp,
        )
    }
}

@Composable
private fun ConnectionCard(
    state: WireGuardState,
    profileSaved: Boolean,
    message: String,
    onConnect: () -> Unit,
    onDisconnect: () -> Unit,
) {
    val connected = state == WireGuardState.ENGINE_UP
    val busy = state == WireGuardState.CONNECTING || state == WireGuardState.DISCONNECTING
    val statusColor = when (state) {
        WireGuardState.ENGINE_UP -> Positive
        WireGuardState.CONNECTING, WireGuardState.DISCONNECTING -> Warning
        WireGuardState.DISCONNECTED -> MutedText
    }
    val title = when (state) {
        WireGuardState.ENGINE_UP -> "تونل فعال است"
        WireGuardState.CONNECTING -> "در حال اتصال"
        WireGuardState.DISCONNECTING -> "در حال قطع اتصال"
        WireGuardState.DISCONNECTED -> "اتصال برقرار نیست"
    }
    val detail = when (state) {
        WireGuardState.ENGINE_UP -> "موتور WireGuard فعال است. وضعیت Handshake و ترافیک را پایین‌تر ببینید."
        WireGuardState.CONNECTING -> "در حال راه‌اندازی تونل امن..."
        WireGuardState.DISCONNECTING -> "در حال بستن تونل..."
        WireGuardState.DISCONNECTED -> if (profileSaved) {
            "پروفایل این دستگاه آمادهٔ اتصال است."
        } else {
            "برای شروع، ابتدا اتصال را به‌صورت خودکار دریافت کنید."
        }
    }

    PanelCard {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(5.dp)) {
                Text("وضعیت اتصال", color = SecondaryText, style = MaterialTheme.typography.labelMedium)
                Text(title, color = MainText, style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold)
            }
            StatusPill(statusColor, title)
        }
        Text(detail, color = SecondaryText, style = MaterialTheme.typography.bodyMedium, lineHeight = 20.sp)
        Button(
            onClick = if (connected) onDisconnect else onConnect,
            enabled = !busy && (connected || profileSaved),
            modifier = Modifier.fillMaxWidth().height(50.dp),
            shape = RoundedCornerShape(13.dp),
            colors = ButtonDefaults.buttonColors(
                containerColor = if (connected) Danger else Primary,
                contentColor = Color.White,
                disabledContainerColor = PanelRaised,
                disabledContentColor = MutedText,
            ),
        ) {
            Text(
                when {
                    busy -> "لطفاً صبر کنید..."
                    connected -> "قطع اتصال"
                    profileSaved -> "اتصال امن"
                    else -> "ابتدا دریافت اتصال"
                },
                fontWeight = FontWeight.SemiBold,
                fontSize = 15.sp,
            )
        }
        if (message.isNotBlank()) {
            Text(
                message,
                color = if (connected || message.contains("ذخیره شد") || message.contains("معتبر")) Positive else Warning,
                style = MaterialTheme.typography.bodySmall,
                lineHeight = 18.sp,
            )
        }
    }
}

@Composable
private fun NetworkSummaryCard(network: NetworkCondition) {
    val (label, detail, color) = when (network) {
        NetworkCondition.ONLINE -> Triple("اینترنت در دسترس است", "اتصال اینترنت دستگاه توسط اندروید تأیید شده است.", Positive)
        NetworkCondition.LIMITED -> Triple("اینترنت محدود است", "شبکه وجود دارد اما دسترسی اینترنت تأیید نشده است.", Warning)
        NetworkCondition.OFFLINE -> Triple("دستگاه آفلاین است", "اتصال شبکه‌ای با دسترسی اینترنت پیدا نشد.", Danger)
        NetworkCondition.UNKNOWN -> Triple("در حال بررسی شبکه", "وضعیت شبکه دستگاه هنوز مشخص نیست.", MutedText)
    }
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .background(Panel, RoundedCornerShape(14.dp))
            .border(1.dp, DividerColor, RoundedCornerShape(14.dp))
            .padding(horizontal = 15.dp, vertical = 13.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            Modifier.size(9.dp).background(color, CircleShape)
        )
        Spacer(Modifier.width(11.dp))
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Text(label, color = MainText, style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.SemiBold)
            Text(detail, color = SecondaryText, style = MaterialTheme.typography.bodySmall, lineHeight = 17.sp)
        }
    }
}

@Composable
private fun TrafficStatsCard(stats: WireGuardTrafficStats) {
    PanelCard {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
                Text("ترافیک تونل", color = MainText, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
                Text("به‌روزرسانی خودکار هر ۳ ثانیه", color = MutedText, style = MaterialTheme.typography.labelSmall)
            }
            StatusPill(Positive, "LIVE")
        }
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            TrafficValue(
                modifier = Modifier.weight(1f),
                label = "دریافت",
                value = formatTrafficBytes(stats.receivedBytes),
                accent = Primary,
                symbol = "↓",
            )
            TrafficValue(
                modifier = Modifier.weight(1f),
                label = "ارسال",
                value = formatTrafficBytes(stats.sentBytes),
                accent = Positive,
                symbol = "↑",
            )
        }
        HorizontalDivider(color = DividerColor)
        val handshakeLabel = if (stats.latestHandshakeEpochMillis > 0L) {
            "آخرین Handshake: " + elapsedSince(stats.latestHandshakeEpochMillis)
        } else {
            "هنوز Handshake موفقی ثبت نشده است"
        }
        Row(verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.size(7.dp).background(
                if (stats.latestHandshakeEpochMillis > 0L) Positive else Warning,
                CircleShape,
            ))
            Spacer(Modifier.width(9.dp))
            Text(
                handshakeLabel,
                color = if (stats.latestHandshakeEpochMillis > 0L) Positive else Warning,
                style = MaterialTheme.typography.bodySmall,
            )
        }
        Text(
            "این شمارنده‌ها از موتور WireGuard خوانده می‌شوند؛ فعال بودن موتور به‌تنهایی عبور موفق ترافیک را تضمین نمی‌کند.",
            color = MutedText,
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
    Column(
        modifier
            .background(PanelRaised, RoundedCornerShape(12.dp))
            .border(1.dp, DividerColor, RoundedCornerShape(12.dp))
            .padding(horizontal = 12.dp, vertical = 12.dp),
        verticalArrangement = Arrangement.spacedBy(7.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(symbol, color = accent, fontWeight = FontWeight.Bold, fontSize = 16.sp)
            Spacer(Modifier.width(6.dp))
            Text(label, color = SecondaryText, style = MaterialTheme.typography.labelMedium)
        }
        Text(
            value,
            color = MainText,
            fontSize = 17.sp,
            fontWeight = FontWeight.Bold,
            maxLines = 1,
        )
    }
}

@Composable
private fun ProfileSetupCard(
    configured: Boolean,
    profileSaved: Boolean,
    hasPendingEnrollment: Boolean,
    activationCode: String,
    enrollmentBusy: Boolean,
    importBusy: Boolean,
    enrollmentMessage: String,
    profileMessage: String,
    profileDraft: String,
    showAdvancedImport: Boolean,
    canForgetProfile: Boolean,
    onActivationCodeChange: (String) -> Unit,
    onEnroll: () -> Unit,
    onChooseFile: () -> Unit,
    onToggleAdvanced: () -> Unit,
    onProfileDraftChange: (String) -> Unit,
    onSaveProfile: () -> Unit,
    onForgetProfile: () -> Unit,
) {
    PanelCard {
        Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text("راه‌اندازی اتصال", color = MainText, style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold)
            Text(
                "پروفایل را خودکار دریافت کنید؛ نیازی به دیدن یا کپی‌کردن کلید خصوصی نیست.",
                color = SecondaryText,
                style = MaterialTheme.typography.bodySmall,
                lineHeight = 19.sp,
            )
        }

        if (profileSaved) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                StatusPill(Positive, "ذخیره‌شده")
                Spacer(Modifier.width(9.dp))
                Text("پروفایل این گوشی امن ذخیره شده است.", color = SecondaryText, style = MaterialTheme.typography.bodySmall)
            }
            Text(
                "کلید خصوصی داخل Android Keystore رمزگذاری می‌شود و برای ثبت دستگاه به سرور ارسال نمی‌شود.",
                color = SecondaryText,
                style = MaterialTheme.typography.bodySmall,
                lineHeight = 18.sp,
            )
            OutlinedButton(
                onClick = onChooseFile,
                enabled = canForgetProfile && !importBusy,
                modifier = Modifier.fillMaxWidth().height(46.dp),
                shape = RoundedCornerShape(12.dp),
                border = androidx.compose.foundation.BorderStroke(1.dp, DividerColor),
            ) {
                Text(if (importBusy) "در حال خواندن فایل..." else "جایگزینی با فایل کانفیگ")
            }
            TextButton(
                onClick = onForgetProfile,
                enabled = canForgetProfile,
                modifier = Modifier.align(Alignment.End),
            ) {
                Text("حذف پروفایل از این گوشی", color = Danger)
            }
        } else {
            if (hasPendingEnrollment) {
                InlineNotice(
                    text = "یک ثبت نیمه‌تمام و رمزگذاری‌شده روی این گوشی وجود دارد. ادامهٔ آن از همان کلید قبلی استفاده می‌کند.",
                    color = Warning,
                )
            } else {
                Text(
                    "۱. کد دعوت یک‌بارمصرف را دریافت کنید.",
                    color = MainText,
                    style = MaterialTheme.typography.titleSmall,
                    fontWeight = FontWeight.SemiBold,
                )
                Text(
                    "۲. برنامه کلید WireGuard را روی همین گوشی می‌سازد، دستگاه را ثبت می‌کند و پروفایل اتصال را خودکار ذخیره می‌کند.",
                    color = SecondaryText,
                    style = MaterialTheme.typography.bodySmall,
                    lineHeight = 19.sp,
                )
            }

            if (configured && !hasPendingEnrollment) {
                OutlinedTextField(
                    value = activationCode,
                    onValueChange = onActivationCodeChange,
                    modifier = Modifier.fillMaxWidth(),
                    label = { Text("کد دعوت") },
                    placeholder = { Text("کد یک‌بارمصرف ۴۳ نویسه‌ای") },
                    singleLine = true,
                    visualTransformation = PasswordVisualTransformation(),
                    textStyle = MaterialTheme.typography.bodyMedium,
                    shape = RoundedCornerShape(12.dp),
                    colors = OutlinedTextFieldDefaults.colors(
                        focusedBorderColor = Primary,
                        unfocusedBorderColor = DividerColor,
                        focusedLabelColor = Primary,
                        unfocusedLabelColor = MutedText,
                        cursorColor = Primary,
                    ),
                )
            }

            if (!configured) {
                InlineNotice(
                    text = "دریافت خودکار تا زمان اتصال این نسخه به نشانی HTTPS کنترل‌پلین فعال نمی‌شود. این محدودیت را با واردکردن دستی کلید دور نمی‌زنیم.",
                    color = Warning,
                )
            }

            Button(
                onClick = onEnroll,
                enabled = configured && !profileSaved && !enrollmentBusy &&
                    (hasPendingEnrollment || activationCode.isNotBlank()),
                modifier = Modifier.fillMaxWidth().height(49.dp),
                shape = RoundedCornerShape(12.dp),
                colors = ButtonDefaults.buttonColors(
                    containerColor = Primary,
                    contentColor = Color.White,
                    disabledContainerColor = PanelRaised,
                    disabledContentColor = MutedText,
                ),
            ) {
                Text(
                    when {
                        enrollmentBusy -> "در حال ثبت دستگاه..."
                        hasPendingEnrollment -> "ادامهٔ دریافت اتصال"
                        else -> "دریافت خودکار و ثبت دستگاه"
                    },
                    fontWeight = FontWeight.SemiBold,
                )
            }

            OutlinedButton(
                onClick = onChooseFile,
                enabled = canForgetProfile && !importBusy,
                modifier = Modifier.fillMaxWidth().height(46.dp),
                shape = RoundedCornerShape(12.dp),
                border = androidx.compose.foundation.BorderStroke(1.dp, DividerColor),
            ) {
                Text(if (importBusy) "در حال خواندن فایل..." else "انتخاب فایل کانفیگ (.conf)")
            }

            TextButton(
                onClick = onToggleAdvanced,
                modifier = Modifier.align(Alignment.Start),
            ) {
                Text(
                    if (showAdvancedImport) "بستن ورود دستی پیشرفته" else "ورود دستی متن کانفیگ (پیشرفته)",
                    color = SecondaryText,
                )
            }

            if (showAdvancedImport) {
                OutlinedTextField(
                    value = profileDraft,
                    onValueChange = onProfileDraftChange,
                    modifier = Modifier.fillMaxWidth(),
                    label = { Text("متن کامل WireGuard") },
                    placeholder = { Text("[Interface] ... [Peer] ...") },
                    minLines = 4,
                    maxLines = 8,
                    textStyle = MaterialTheme.typography.bodySmall,
                    shape = RoundedCornerShape(12.dp),
                    colors = OutlinedTextFieldDefaults.colors(
                        focusedBorderColor = Primary,
                        unfocusedBorderColor = DividerColor,
                        focusedLabelColor = Primary,
                        unfocusedLabelColor = MutedText,
                        cursorColor = Primary,
                    ),
                )
                Button(
                    onClick = onSaveProfile,
                    enabled = profileDraft.isNotBlank() && canForgetProfile && !importBusy,
                    modifier = Modifier.fillMaxWidth(),
                    shape = RoundedCornerShape(12.dp),
                    colors = ButtonDefaults.buttonColors(containerColor = PanelRaised, contentColor = MainText),
                ) {
                    Text("اعتبارسنجی و ذخیرهٔ امن")
                }
            }
        }

        if (enrollmentMessage.isNotBlank()) {
            InlineNotice(
                text = enrollmentMessage,
                color = if (enrollmentMessage.contains("ثبت شد") || enrollmentMessage.contains("ذخیره")) Positive else Warning,
            )
        }
        if (profileMessage.isNotBlank()) {
            InlineNotice(
                text = profileMessage,
                color = if (profileMessage.contains("ذخیره") || profileMessage.contains("معتبر")) Positive else Warning,
            )
        }
    }
}

@Composable
private fun SettingsCard(
    enabled: Boolean,
    onEnabledChange: (Boolean) -> Unit,
) {
    PanelCard {
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text("اتصال مجدد خودکار", color = MainText, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
                Text(
                    "ترجیح این دستگاه برای استفاده در مرحلهٔ بازیابی اتصال ذخیره می‌شود.",
                    color = SecondaryText,
                    style = MaterialTheme.typography.bodySmall,
                    lineHeight = 18.sp,
                )
            }
            Switch(
                checked = enabled,
                onCheckedChange = onEnabledChange,
                colors = SwitchDefaults.colors(
                    checkedThumbColor = Color.White,
                    checkedTrackColor = Positive,
                    uncheckedThumbColor = SecondaryText,
                    uncheckedTrackColor = PanelRaised,
                ),
            )
        }
    }
}

@Composable
private fun PanelCard(content: @Composable ColumnScope.() -> Unit) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(18.dp),
        colors = CardDefaults.cardColors(containerColor = Panel),
        elevation = CardDefaults.cardElevation(defaultElevation = 0.dp),
        border = androidx.compose.foundation.BorderStroke(1.dp, DividerColor.copy(alpha = 0.75f)),
    ) {
        Column(
            modifier = Modifier.padding(17.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
            content = content,
        )
    }
}

@Composable
private fun StatusPill(color: Color, label: String) {
    Surface(
        shape = RoundedCornerShape(50),
        color = color.copy(alpha = 0.12f),
        border = androidx.compose.foundation.BorderStroke(1.dp, color.copy(alpha = 0.28f)),
    ) {
        Row(
            modifier = Modifier.padding(horizontal = 10.dp, vertical = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Box(Modifier.size(6.dp).background(color, CircleShape))
            Text(label, color = color, style = MaterialTheme.typography.labelSmall, fontWeight = FontWeight.SemiBold)
        }
    }
}

@Composable
private fun InlineNotice(text: String, color: Color) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .background(color.copy(alpha = 0.08f), RoundedCornerShape(11.dp))
            .border(1.dp, color.copy(alpha = 0.22f), RoundedCornerShape(11.dp))
            .padding(horizontal = 12.dp, vertical = 10.dp),
        verticalAlignment = Alignment.Top,
    ) {
        Box(
            Modifier.padding(top = 5.dp).size(6.dp).background(color, CircleShape)
        )
        Spacer(Modifier.width(9.dp))
        Text(text, color = SecondaryText, style = MaterialTheme.typography.bodySmall, lineHeight = 18.sp)
    }
}

private suspend fun readProfileDocument(
    resolver: ContentResolver,
    uri: Uri,
): String = withContext(Dispatchers.IO) {
    val input = resolver.openInputStream(uri) ?: throw IOException("Selected file cannot be opened.")
    input.use { stream ->
        val output = ByteArrayOutputStream()
        val buffer = ByteArray(4_096)
        var total = 0
        try {
            while (true) {
                val count = stream.read(buffer)
                if (count < 0) break
                total += count
                if (total > 64 * 1024) throw IOException("Selected profile exceeds 64 KiB.")
                output.write(buffer, 0, count)
            }
            val bytes = output.toByteArray()
            try {
                String(bytes, Charsets.UTF_8)
            } finally {
                bytes.fill(0)
            }
        } finally {
            buffer.fill(0)
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
        value.toLong().toString() + " " + units[unitIndex]
    } else {
        String.format(Locale.US, "%.1f %s", value, units[unitIndex])
    }
}

private fun elapsedSince(epochMillis: Long): String {
    if (epochMillis <= 0L) return "نامشخص"
    val seconds = ((System.currentTimeMillis() - epochMillis).coerceAtLeast(0L)) / 1_000L
    return when {
        seconds < 60L -> seconds.toString() + " ثانیه قبل"
        seconds < 3_600L -> (seconds / 60L).toString() + " دقیقه قبل"
        seconds < 86_400L -> (seconds / 3_600L).toString() + " ساعت قبل"
        else -> (seconds / 86_400L).toString() + " روز قبل"
    }
}
