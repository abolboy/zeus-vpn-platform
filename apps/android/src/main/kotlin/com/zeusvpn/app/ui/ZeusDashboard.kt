package com.zeusvpn.app.ui

import android.app.Activity
import android.content.ContentResolver
import android.content.Intent
import android.net.Uri
import android.net.VpnService
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.Home
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material.icons.outlined.PowerSettingsNew
import androidx.compose.material.icons.outlined.Shield
import androidx.compose.material.icons.outlined.Tune
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.graphicsLayer
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
import com.zeusvpn.app.network.CustomerIdentityRepository
import com.zeusvpn.app.network.NetworkCondition
import com.zeusvpn.app.network.NetworkMonitor
import com.zeusvpn.app.network.TunnelConnectivityChecker
import com.zeusvpn.app.network.TunnelConnectivityResult
import com.zeusvpn.app.network.TunnelProbeFailure
import com.zeusvpn.app.network.TunnelProbeStatus
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

private val AppBackground = Color(0xFF080F1C)
private val Panel = Color(0xFF111C2D)
private val PanelRaised = Color(0xFF1A2A42)
private val Primary = Color(0xFF638DFF)
private val Positive = Color(0xFF47D8B0)
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
    val identity = remember(context) { CustomerIdentityRepository(context) }
    val tunnelChecker = remember { TunnelConnectivityChecker() }
    val wireGuardState by wireGuard.state.collectAsState()
    val scope = rememberCoroutineScope()

    var profileSaved by remember { mutableStateOf(false) }
    var activationCodeDraft by remember { mutableStateOf("") }
    var enrollmentBusy by remember { mutableStateOf(false) }
    var enrollmentMessage by remember { mutableStateOf("") }
    var hasPendingEnrollment by remember { mutableStateOf(false) }
    var profileDraft by remember { mutableStateOf("") }
    var wireGuardMessage by remember { mutableStateOf("") }
    var profileSetupMessage by remember { mutableStateOf("") }
    var profileImportBusy by remember { mutableStateOf(false) }
    var showAdvancedImport by remember { mutableStateOf(false) }
    var showInviteFallback by remember { mutableStateOf(false) }
    var customerSignedIn by remember { mutableStateOf(identity.hasStoredSession()) }
    var authBusy by remember { mutableStateOf(false) }
    var authMessage by remember { mutableStateOf("") }
    var selectedTab by remember { mutableIntStateOf(0) }
    var manualProbeRequest by remember { mutableIntStateOf(0) }

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
                runCatching { wireGuard.trafficStats() }.onSuccess { value = it }
                delay(3_000L)
            }
        }
    }
    val tunnelReachability by produceState(
        TunnelConnectivityResult.IDLE,
        tunnelChecker,
        wireGuardState,
        manualProbeRequest,
    ) {
        if (wireGuardState != WireGuardState.ENGINE_UP) {
            value = TunnelConnectivityResult.IDLE
        } else {
            while (true) {
                value = TunnelConnectivityResult.CHECKING
                value = tunnelChecker.check()
                delay(30_000L)
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

    LaunchedEffect(wireGuard, enrollment, identity) {
        profileSaved = runCatching { wireGuard.hasProfile() }.getOrDefault(false)
        hasPendingEnrollment = runCatching { enrollment.hasPendingEnrollment() }.getOrDefault(false)
        customerSignedIn = identity.hasStoredSession()
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
        if (result.resultCode == Activity.RESULT_OK) connectWireGuard()
        else wireGuardMessage = permissionDeniedMessage
    }

    val requestWireGuardConnection: () -> Unit = {
        val permissionIntent = VpnService.prepare(context)
        if (permissionIntent == null) connectWireGuard()
        else vpnPermissionLauncher.launch(permissionIntent)
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
                profileSetupMessage = profileSavedMessage
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
                profileSetupMessage = profileDeletedMessage
            } catch (_: Exception) {
                wireGuardMessage = connectionFailedMessage
            }
        }
    }

    val performEnrollment: suspend (String?, String) -> Unit = { idToken, activationCode ->
        if (!enrollment.isConfigured) {
            enrollmentMessage = "نشانی HTTPS کنترل‌پلین در این نسخه تنظیم نشده است."
        } else if (profileSaved) {
            enrollmentMessage = "پروفایل فعلی روی این گوشی وجود دارد."
        } else if (enrollmentBusy) {
            enrollmentMessage = "درخواست ثبت دستگاه در حال اجراست."
        } else {
            enrollmentBusy = true
            enrollmentMessage = "در حال ساخت کلید روی گوشی و دریافت خودکار پروفایل..."
            try {
                val result = enrollment.enroll(
                    activationCode = activationCode,
                    deviceName = Build.MODEL.orEmpty(),
                    idToken = idToken,
                )
                wireGuard.saveProfile(result.profileText)
                enrollment.completeEnrollment()
                profileSaved = true
                hasPendingEnrollment = false
                activationCodeDraft = ""
                enrollmentMessage = "دستگاه ثبت شد؛ پروفایل اتصال به‌صورت رمزگذاری‌شده ذخیره شد."
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

    val customerSignInLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.StartActivityForResult(),
    ) { result ->
        scope.launch {
            authBusy = false
            if (result.resultCode != Activity.RESULT_OK) {
                authMessage = "ورود لغو شد؛ هر زمان آماده بودید دوباره تلاش کنید."
            } else {
                try {
                    val idToken = identity.completeAuthorization(result.data)
                    customerSignedIn = true
                    authMessage = "حساب شما با موفقیت تأیید شد."
                    if (!profileSaved && enrollment.isConfigured) {
                        performEnrollment(idToken, "")
                    }
                } catch (error: Exception) {
                    authMessage = error.message ?: "ورود امن انجام نشد."
                }
            }
        }
    }

    val startCustomerSignIn: () -> Unit = {
        if (!identity.isConfigured) {
            authMessage = "تنظیمات ارائه‌دهندهٔ هویت OIDC هنوز برای این نسخه وارد نشده است."
        } else if (!authBusy) {
            scope.launch {
                authBusy = true
                authMessage = "در حال آماده‌سازی ورود امن..."
                try {
                    customerSignInLauncher.launch(identity.createAuthorizationIntent())
                } catch (error: Exception) {
                    authBusy = false
                    authMessage = error.message ?: "صفحهٔ ورود در دسترس نیست."
                }
            }
        }
    }

    val signOutCustomer: () -> Unit = {
        identity.signOutLocally()
        customerSignedIn = false
        authMessage = "از حساب خارج شدید؛ پروفایل ذخیره‌شده روی این گوشی حذف نشد."
    }

    val enrollDevice: () -> Unit = {
        if (!enrollment.isConfigured) {
            enrollmentMessage = "دریافت خودکار تا زمان تنظیم نشانی HTTPS کنترل‌پلین فعال نمی‌شود."
        } else if (profileSaved) {
            enrollmentMessage = "پروفایل فعلی روی این گوشی وجود دارد."
        } else if (customerSignedIn) {
            scope.launch {
                try {
                    val idToken = identity.freshIdToken()
                    performEnrollment(idToken, "")
                } catch (error: Exception) {
                    customerSignedIn = identity.hasStoredSession()
                    authMessage = error.message ?: "نشست حساب منقضی شده است؛ دوباره وارد شوید."
                }
            }
        } else if (activationCodeDraft.isBlank()) {
            showInviteFallback = true
            enrollmentMessage = "برای دریافت خودکار، وارد حساب شوید؛ یا کد دعوت یک‌بارمصرف را وارد کنید."
        } else {
            scope.launch { performEnrollment(null, activationCodeDraft) }
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
                    profileSetupMessage = profileSavedMessage
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

    Scaffold(
        containerColor = AppBackground,
        bottomBar = {
            NavigationBar(
                containerColor = Color(0xFF0D1727),
                contentColor = SecondaryText,
                tonalElevation = 0.dp,
            ) {
                NavigationBarItem(
                    selected = selectedTab == 0,
                    onClick = { selectedTab = 0 },
                    icon = { Icon(Icons.Outlined.Home, contentDescription = "خانه") },
                    label = { Text("خانه") },
                    colors = NavigationBarItemDefaults.colors(
                        selectedIconColor = Primary,
                        selectedTextColor = MainText,
                        indicatorColor = Primary.copy(alpha = 0.13f),
                        unselectedIconColor = MutedText,
                        unselectedTextColor = MutedText,
                    ),
                )
                NavigationBarItem(
                    selected = selectedTab == 1,
                    onClick = { selectedTab = 1 },
                    icon = { Icon(Icons.Outlined.Dns, contentDescription = "مسیر سرور") },
                    label = { Text("سرورها") },
                    colors = NavigationBarItemDefaults.colors(
                        selectedIconColor = Primary,
                        selectedTextColor = MainText,
                        indicatorColor = Primary.copy(alpha = 0.13f),
                        unselectedIconColor = MutedText,
                        unselectedTextColor = MutedText,
                    ),
                )
                NavigationBarItem(
                    selected = selectedTab == 2,
                    onClick = { selectedTab = 2 },
                    icon = { Icon(Icons.Outlined.Person, contentDescription = "حساب") },
                    label = { Text("حساب") },
                    colors = NavigationBarItemDefaults.colors(
                        selectedIconColor = Primary,
                        selectedTextColor = MainText,
                        indicatorColor = Primary.copy(alpha = 0.13f),
                        unselectedIconColor = MutedText,
                        unselectedTextColor = MutedText,
                    ),
                )
            }
        },
    ) { insets ->
        Box(
            modifier = Modifier
                .fillMaxSize()
                .background(
                    Brush.verticalGradient(
                        listOf(Color(0xFF101C30), AppBackground, Color(0xFF080D17)),
                    ),
                )
                .padding(insets),
        ) {
            Column(
                modifier = Modifier
                    .fillMaxSize()
                    .verticalScroll(rememberScrollState())
                    .imePadding()
                    .padding(horizontal = 18.dp, vertical = 16.dp),
                verticalArrangement = Arrangement.spacedBy(16.dp),
            ) {
                BrandHeader(customerSignedIn)

                when (selectedTab) {
                    0 -> {
                        ConnectionCard(
                            state = wireGuardState,
                            profileSaved = profileSaved,
                            message = wireGuardMessage,
                            stats = trafficStats,
                            reachability = tunnelReachability,
                            onConnect = requestWireGuardConnection,
                            onDisconnect = disconnectWireGuard,
                        )
                        if (wireGuardState == WireGuardState.ENGINE_UP) {
                            TrafficStatsCard(trafficStats, tunnelReachability) { manualProbeRequest += 1 }
                        }
                        NetworkSummaryCard(network)
                        ProfileSetupCard(
                            configured = enrollment.isConfigured,
                            identityConfigured = identity.isConfigured,
                            customerSignedIn = customerSignedIn,
                            authBusy = authBusy,
                            authMessage = authMessage,
                            onSignIn = startCustomerSignIn,
                            onSignOut = signOutCustomer,
                            profileSaved = profileSaved,
                            hasPendingEnrollment = hasPendingEnrollment,
                            activationCode = activationCodeDraft,
                            showInviteFallback = showInviteFallback,
                            enrollmentBusy = enrollmentBusy,
                            importBusy = profileImportBusy,
                            enrollmentMessage = enrollmentMessage,
                            profileMessage = profileSetupMessage,
                            profileDraft = profileDraft,
                            showAdvancedImport = showAdvancedImport,
                            canForgetProfile = wireGuardState == WireGuardState.DISCONNECTED,
                            onActivationCodeChange = { value ->
                                if (value.length <= 43 && value.all {
                                    it in 'A'..'Z' || it in 'a'..'z' || it in '0'..'9' || it == '-' || it == '_'
                                }) activationCodeDraft = value
                            },
                            onToggleInviteFallback = { showInviteFallback = !showInviteFallback },
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
                        Text(
                            stringResource(R.string.disclaimer),
                            modifier = Modifier.fillMaxWidth().padding(bottom = 8.dp),
                            color = MutedText,
                            style = MaterialTheme.typography.bodySmall,
                            lineHeight = 18.sp,
                        )
                    }
                    1 -> {
                        SmartRoutingCard()
                        NetworkSummaryCard(network)
                        if (wireGuardState == WireGuardState.ENGINE_UP) {
                            TrafficStatsCard(trafficStats, tunnelReachability) { manualProbeRequest += 1 }
                        } else {
                            PanelCard {
                                Text("آمار زنده پس از فعال‌شدن تونل نمایش داده می‌شود.", color = SecondaryText,
                                    style = MaterialTheme.typography.bodyMedium, lineHeight = 20.sp)
                            }
                        }
                    }
                    else -> {
                        CustomerAccountCard(
                            identityConfigured = identity.isConfigured,
                            signedIn = customerSignedIn,
                            authBusy = authBusy,
                            authMessage = authMessage,
                            onSignIn = startCustomerSignIn,
                            onSignOut = signOutCustomer,
                            profileSaved = profileSaved,
                            onGoHome = { selectedTab = 0 },
                        )
                        SettingsCard(
                            enabled = autoReconnect,
                            onEnabledChange = { enabled ->
                                scope.launch { settings.setAutoReconnectEnabled(enabled) }
                            },
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun BrandHeader(customerSignedIn: Boolean) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(top = 4.dp, bottom = 2.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Surface(
            modifier = Modifier.size(48.dp),
            shape = RoundedCornerShape(15.dp),
            color = Primary.copy(alpha = 0.17f),
            border = androidx.compose.foundation.BorderStroke(1.dp, Primary.copy(alpha = 0.42f)),
        ) {
            Box(contentAlignment = Alignment.Center) {
                Icon(Icons.Outlined.Shield, contentDescription = null, tint = Primary, modifier = Modifier.size(28.dp))
            }
        }
        Spacer(Modifier.width(12.dp))
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text("ZEUS VPN", color = MainText, fontSize = 21.sp, fontWeight = FontWeight.ExtraBold, letterSpacing = 1.1.sp)
            Text("SECURE PRIVATE NETWORK", color = MutedText, style = MaterialTheme.typography.labelSmall, letterSpacing = 1.25.sp)
        }
        Column(horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(4.dp)) {
            StatusPill(if (customerSignedIn) Positive else Primary, if (customerSignedIn) "ACCOUNT" else "GUEST")
            Text("ANDROID", color = MutedText, style = MaterialTheme.typography.labelSmall, letterSpacing = 1.sp)
        }
    }
}

@Composable
private fun ConnectionCard(
    state: WireGuardState,
    profileSaved: Boolean,
    message: String,
    stats: WireGuardTrafficStats,
    reachability: TunnelConnectivityResult,
    onConnect: () -> Unit,
    onDisconnect: () -> Unit,
) {
    val connected = state == WireGuardState.ENGINE_UP
    val busy = state == WireGuardState.CONNECTING || state == WireGuardState.DISCONNECTING
    val handshakeFresh = stats.latestHandshakeEpochMillis > 0L &&
        System.currentTimeMillis() - stats.latestHandshakeEpochMillis <= 180_000L
    val internetVerified = connected && reachability.status == TunnelProbeStatus.REACHABLE
    val statusColor = when (state) {
        WireGuardState.ENGINE_UP -> if (internetVerified) Positive else Warning
        WireGuardState.CONNECTING, WireGuardState.DISCONNECTING -> Warning
        WireGuardState.DISCONNECTED -> MutedText
    }
    val title = when (state) {
        WireGuardState.ENGINE_UP -> when {
            internetVerified -> "اینترنت از مسیر VPN پاسخ می‌دهد"
            handshakeFresh -> "تونل فعال است؛ اینترنت تأیید نشده"
            else -> "موتور فعال است؛ Handshake تأیید نشده"
        }
        WireGuardState.CONNECTING -> "در حال اتصال"
        WireGuardState.DISCONNECTING -> "در حال قطع اتصال"
        WireGuardState.DISCONNECTED -> "آمادهٔ اتصال"
    }
    val detail = when (state) {
        WireGuardState.ENGINE_UP -> when {
            internetVerified -> "آزمون HTTPS از داخل برنامه پاسخ معتبر دریافت کرد؛ دسترسی همهٔ سایت‌ها و برنامه‌ها جداگانه تضمین نمی‌شود."
            reachability.status == TunnelProbeStatus.CHECKING -> "موتور تونل روشن است؛ اکنون مسیر اینترنت از داخل برنامه بررسی می‌شود."
            handshakeFresh -> "Handshake تازه ثبت شده، اما آزمون HTTPS موفق نبود. " + probeFailureMessage(reachability.failure)
            else -> "فقط موتور محلی فعال است؛ هنوز Handshake معتبر و دسترسی اینترنت تأیید نشده است."
        }
        WireGuardState.CONNECTING -> "در حال ایجاد تونل رمزگذاری‌شده..."
        WireGuardState.DISCONNECTING -> "در حال بستن تونل امن..."
        WireGuardState.DISCONNECTED -> if (profileSaved) "پروفایل این دستگاه آمادهٔ استفاده است." else "برای شروع، حساب بسازید یا اتصال را با کد دعوت دریافت کنید."
    }

    PanelCard {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(5.dp)) {
                Text("CONTROL CENTER", color = MutedText, style = MaterialTheme.typography.labelSmall, letterSpacing = 1.4.sp)
                Text(title, color = MainText, style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold)
            }
            StatusPill(statusColor, when (state) {
                WireGuardState.ENGINE_UP -> if (internetVerified) "ONLINE" else "VERIFY"
                WireGuardState.CONNECTING, WireGuardState.DISCONNECTING -> "BUSY"
                WireGuardState.DISCONNECTED -> "READY"
            })
        }
        Box(modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp), contentAlignment = Alignment.Center) {
            ConnectionOrb(statusColor, connected, busy)
        }
        Text(detail, modifier = Modifier.fillMaxWidth(), color = SecondaryText, style = MaterialTheme.typography.bodyMedium, lineHeight = 21.sp)
        Button(
            onClick = if (connected) onDisconnect else onConnect,
            enabled = !busy && (connected || profileSaved),
            modifier = Modifier.fillMaxWidth().height(54.dp),
            shape = RoundedCornerShape(16.dp),
            colors = ButtonDefaults.buttonColors(
                containerColor = if (connected) Danger else Primary,
                contentColor = Color.White,
                disabledContainerColor = PanelRaised,
                disabledContentColor = MutedText,
            ),
        ) {
            Icon(if (connected) Icons.Outlined.PowerSettingsNew else Icons.Outlined.Shield, contentDescription = null)
            Spacer(Modifier.width(9.dp))
            Text(
                when {
                    busy -> "لطفاً صبر کنید..."
                    connected -> "قطع اتصال امن"
                    profileSaved -> "اتصال به ZEUS"
                    else -> "ابتدا دریافت اتصال"
                },
                fontWeight = FontWeight.Bold,
                fontSize = 15.sp,
            )
        }
        if (message.isNotBlank()) {
            InlineNotice(
                text = message,
                color = if (internetVerified || message.contains("ذخیره شد") || message.contains("معتبر")) Positive else Warning,
            )
        }
    }
}

@Composable
private fun probeFailureMessage(failure: TunnelProbeFailure?): String = when (failure) {
    TunnelProbeFailure.DNS -> "نام دامنه resolve نشد؛ DNS پروفایل و دسترسی DNS از مسیر VPN را بررسی کنید."
    TunnelProbeFailure.TIMEOUT -> "پاسخ در مهلت مقرر نرسید؛ مسیر خروجی، فایروال، NAT و MTU نود را بررسی کنید."
    TunnelProbeFailure.ROUTE -> "مسیر شبکه برقرار نشد؛ IP forwarding، NAT و مسیر پیش‌فرض سرور را بررسی کنید."
    TunnelProbeFailure.TLS -> "ارتباط HTTPS در مرحله TLS شکست خورد؛ ساعت دستگاه و فیلتر یا بازرسی TLS را بررسی کنید."
    TunnelProbeFailure.HTTP_STATUS -> "مقصد آزمون پاسخ ۲۰۴ مورد انتظار را نداد؛ ممکن است مقصد فیلتر یا پاسخ آن تغییر کرده باشد."
    TunnelProbeFailure.UNKNOWN, null -> "DNS، IP forwarding، NAT، مسیر خروجی و فایروال VPS را بررسی کنید."
}

@Composable
private fun ConnectionOrb(statusColor: Color, connected: Boolean, busy: Boolean) {
    val pulse = rememberInfiniteTransition(label = "zeus-connection-pulse")
    val scale by pulse.animateFloat(
        initialValue = 0.97f,
        targetValue = 1.035f,
        animationSpec = infiniteRepeatable(
            animation = tween(durationMillis = 2_200, easing = FastOutSlowInEasing),
            repeatMode = RepeatMode.Reverse,
        ),
        label = "zeus-pulse-scale",
    )
    Box(modifier = Modifier.size(186.dp), contentAlignment = Alignment.Center) {
        Canvas(Modifier.fillMaxSize()) {
            val radius = size.minDimension * 0.47f
            drawCircle(statusColor.copy(alpha = 0.055f), radius = radius, center = Offset(size.width / 2f, size.height / 2f))
            drawCircle(statusColor.copy(alpha = 0.16f), radius = radius * 0.87f, center = Offset(size.width / 2f, size.height / 2f), style = Stroke(1.dp.toPx()))
            drawArc(
                brush = Brush.sweepGradient(
                    listOf(statusColor.copy(alpha = 0.12f), statusColor, Primary.copy(alpha = 0.2f), statusColor.copy(alpha = 0.12f)),
                ),
                startAngle = -90f,
                sweepAngle = if (connected) 292f else 236f,
                useCenter = false,
                topLeft = Offset((size.width - radius * 1.62f) / 2f, (size.height - radius * 1.62f) / 2f),
                size = Size(radius * 1.62f, radius * 1.62f),
                style = Stroke(width = 5.dp.toPx(), cap = StrokeCap.Round),
            )
        }
        Surface(
            modifier = Modifier.size(128.dp).graphicsLayer {
                scaleX = if (busy) 1f else scale
                scaleY = if (busy) 1f else scale
            },
            shape = CircleShape,
            color = PanelRaised,
            border = androidx.compose.foundation.BorderStroke(1.dp, statusColor.copy(alpha = 0.35f)),
            shadowElevation = 12.dp,
        ) {
            Column(horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center) {
                Icon(
                    imageVector = if (connected) Icons.Outlined.Shield else Icons.Outlined.PowerSettingsNew,
                    contentDescription = null,
                    tint = statusColor,
                    modifier = Modifier.size(39.dp),
                )
                Spacer(Modifier.height(7.dp))
                Text(if (connected) "ACTIVE" else if (busy) "WORKING" else "ZEUS", color = MainText, fontWeight = FontWeight.ExtraBold, letterSpacing = 1.3.sp)
                Text(if (connected) "ENGINE ON" else "TUNNEL OFF", color = MutedText, style = MaterialTheme.typography.labelSmall, letterSpacing = 0.7.sp)
            }
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
private fun TrafficStatsCard(
    stats: WireGuardTrafficStats,
    reachability: TunnelConnectivityResult,
    onRefresh: () -> Unit,
) {
    PanelCard {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
                Text("ترافیک تونل", color = MainText, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
                Text("به‌روزرسانی خودکار هر ۳ ثانیه", color = MutedText, style = MaterialTheme.typography.labelSmall)
            }
            TextButton(onClick = onRefresh, enabled = reachability.status != TunnelProbeStatus.CHECKING) {
                Text("آزمون مجدد")
            }
            StatusPill(
                when (reachability.status) {
                    TunnelProbeStatus.REACHABLE -> Positive
                    TunnelProbeStatus.UNREACHABLE -> Danger
                    TunnelProbeStatus.CHECKING -> Warning
                    TunnelProbeStatus.IDLE -> MutedText
                },
                when (reachability.status) {
                    TunnelProbeStatus.REACHABLE -> "ONLINE"
                    TunnelProbeStatus.UNREACHABLE -> "NO NET"
                    TunnelProbeStatus.CHECKING -> "CHECK"
                    TunnelProbeStatus.IDLE -> "IDLE"
                },
            )
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
        val probeMessage = when (reachability.status) {
            TunnelProbeStatus.REACHABLE ->
                "آزمون HTTPS از داخل برنامه موفق بود (${reachability.successfulProvider ?: "مقصد تأییدشده"})."
            TunnelProbeStatus.CHECKING ->
                "در حال آزمون HTTPS از داخل نشست برنامه..."
            TunnelProbeStatus.UNREACHABLE ->
                "آزمون HTTPS از داخل برنامه شکست خورد. " + probeFailureMessage(reachability.failure)
            TunnelProbeStatus.IDLE ->
                "آزمون اینترنت پس از فعال‌شدن موتور اجرا می‌شود."
        }
        Text(
            probeMessage,
            color = when (reachability.status) {
                TunnelProbeStatus.REACHABLE -> Positive
                TunnelProbeStatus.UNREACHABLE -> Danger
                TunnelProbeStatus.CHECKING -> Warning
                TunnelProbeStatus.IDLE -> MutedText
            },
            style = MaterialTheme.typography.bodySmall,
            lineHeight = 18.sp,
        )
        Text(
            "شمارنده‌ها و Handshake از موتور WireGuard خوانده می‌شوند. آزمون HTTPS فقط دسترسی همان مقصدها را می‌سنجد و سلامت تمام سایت‌ها را تضمین نمی‌کند.",
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
    identityConfigured: Boolean,
    customerSignedIn: Boolean,
    authBusy: Boolean,
    authMessage: String,
    onSignIn: () -> Unit,
    onSignOut: () -> Unit,
    profileSaved: Boolean,
    hasPendingEnrollment: Boolean,
    activationCode: String,
    showInviteFallback: Boolean,
    enrollmentBusy: Boolean,
    importBusy: Boolean,
    enrollmentMessage: String,
    profileMessage: String,
    profileDraft: String,
    showAdvancedImport: Boolean,
    canForgetProfile: Boolean,
    onActivationCodeChange: (String) -> Unit,
    onToggleInviteFallback: () -> Unit,
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
                "ثبت دستگاه و دریافت پروفایل به‌صورت خودکار؛ بدون فایل کانفیگ و بدون نمایش کلید خصوصی.",
                color = SecondaryText,
                style = MaterialTheme.typography.bodySmall,
                lineHeight = 19.sp,
            )
        }

        if (profileSaved) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                StatusPill(Positive, "READY")
                Spacer(Modifier.width(9.dp))
                Text("پروفایل رمزگذاری‌شده روی این گوشی ذخیره است.", color = SecondaryText, style = MaterialTheme.typography.bodySmall)
            }
            Text(
                "کلید خصوصی در دستگاه ساخته و با Android Keystore محافظت شده است؛ برای ثبت دستگاه فقط کلید عمومی ارسال می‌شود.",
                color = SecondaryText,
                style = MaterialTheme.typography.bodySmall,
                lineHeight = 18.sp,
            )
            OutlinedButton(
                onClick = onChooseFile,
                enabled = canForgetProfile && !importBusy,
                modifier = Modifier.fillMaxWidth().height(46.dp),
                shape = RoundedCornerShape(13.dp),
                border = androidx.compose.foundation.BorderStroke(1.dp, DividerColor),
            ) {
                Text(if (importBusy) "در حال خواندن فایل..." else "جایگزینی با فایل کانفیگ")
            }
            TextButton(onClick = onForgetProfile, enabled = canForgetProfile, modifier = Modifier.align(Alignment.End)) {
                Text("حذف پروفایل از این گوشی", color = Danger)
            }
        } else {
            if (hasPendingEnrollment) {
                InlineNotice(
                    text = "ثبت قبلی به‌صورت امن ذخیره شده است. ادامهٔ فرآیند از همان کلید دستگاه استفاده می‌کند.",
                    color = Warning,
                )
            }

            if (identityConfigured) {
                if (customerSignedIn) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        StatusPill(Positive, "ACCOUNT VERIFIED")
                        Spacer(Modifier.width(8.dp))
                        Text("حساب برای ثبت دستگاه آماده است.", color = SecondaryText, style = MaterialTheme.typography.bodySmall)
                    }
                    Button(
                        onClick = onEnroll,
                        enabled = configured && !enrollmentBusy && !profileSaved,
                        modifier = Modifier.fillMaxWidth().height(52.dp),
                        shape = RoundedCornerShape(15.dp),
                        colors = ButtonDefaults.buttonColors(containerColor = Primary, contentColor = Color.White),
                    ) {
                        Text(if (enrollmentBusy) "در حال دریافت کلید..." else "ثبت دستگاه و دریافت خودکار کلید", fontWeight = FontWeight.Bold)
                    }
                    TextButton(onClick = onSignOut, modifier = Modifier.align(Alignment.End)) {
                        Text("خروج از حساب", color = SecondaryText)
                    }
                } else {
                    Text(
                        "برای ساخت حساب جدید یا ورود، تأیید هویت در صفحهٔ رسمی سرویس انجام می‌شود.",
                        color = SecondaryText,
                        style = MaterialTheme.typography.bodySmall,
                        lineHeight = 19.sp,
                    )
                    Button(
                        onClick = onSignIn,
                        enabled = configured && !authBusy,
                        modifier = Modifier.fillMaxWidth().height(52.dp),
                        shape = RoundedCornerShape(15.dp),
                        colors = ButtonDefaults.buttonColors(containerColor = Primary, contentColor = Color.White),
                    ) {
                        Icon(Icons.Outlined.Person, contentDescription = null)
                        Spacer(Modifier.width(8.dp))
                        Text(if (authBusy) "در حال بازکردن ورود امن..." else "ورود / ساخت حساب ZEUS", fontWeight = FontWeight.Bold)
                    }
                }
            } else {
                InlineNotice(
                    text = "ورود و ثبت‌نام خودکار پس از تنظیم OIDC فعال می‌شود. تا آن زمان می‌توانید از کد دعوت مدیر استفاده کنید.",
                    color = Warning,
                )
            }

            if (!configured) {
                InlineNotice(
                    text = "آدرس HTTPS کنترل‌پلین در این نسخه تنظیم نشده است؛ هیچ درخواست ثبت‌نامی به سرویس ارسال نمی‌شود.",
                    color = Warning,
                )
            }

            if (!customerSignedIn && !showInviteFallback) {
                TextButton(onClick = onToggleInviteFallback, modifier = Modifier.align(Alignment.Start)) {
                    Text("کد دعوت یک‌بارمصرف دارم", color = SecondaryText)
                }
            }

            if (showInviteFallback && !customerSignedIn) {
                OutlinedTextField(
                    value = activationCode,
                    onValueChange = onActivationCodeChange,
                    modifier = Modifier.fillMaxWidth(),
                    label = { Text("کد دعوت") },
                    placeholder = { Text("کد یک‌بارمصرف ۴۳ نویسه‌ای") },
                    singleLine = true,
                    visualTransformation = PasswordVisualTransformation(),
                    textStyle = MaterialTheme.typography.bodyMedium,
                    shape = RoundedCornerShape(13.dp),
                    colors = OutlinedTextFieldDefaults.colors(
                        focusedBorderColor = Primary,
                        unfocusedBorderColor = DividerColor,
                        focusedLabelColor = Primary,
                        unfocusedLabelColor = MutedText,
                        cursorColor = Primary,
                    ),
                )
                Button(
                    onClick = onEnroll,
                    enabled = configured && activationCode.isNotBlank() && !enrollmentBusy,
                    modifier = Modifier.fillMaxWidth().height(48.dp),
                    shape = RoundedCornerShape(13.dp),
                    colors = ButtonDefaults.buttonColors(containerColor = PanelRaised, contentColor = MainText),
                ) {
                    Text(if (enrollmentBusy) "در حال ثبت..." else "ثبت دستگاه با کد دعوت")
                }
            }

            if (authMessage.isNotBlank()) {
                InlineNotice(
                    text = authMessage,
                    color = if (customerSignedIn) Positive else Warning,
                )
            }

            OutlinedButton(
                onClick = onChooseFile,
                enabled = canForgetProfile && !importBusy,
                modifier = Modifier.fillMaxWidth().height(46.dp),
                shape = RoundedCornerShape(13.dp),
                border = androidx.compose.foundation.BorderStroke(1.dp, DividerColor),
            ) {
                Text(if (importBusy) "در حال خواندن فایل..." else "واردکردن فایل کانفیگ (.conf)")
            }

            TextButton(onClick = onToggleAdvanced, modifier = Modifier.align(Alignment.Start)) {
                Text(if (showAdvancedImport) "بستن ورود دستی پیشرفته" else "ورود دستی متن کانفیگ (پیشرفته)", color = MutedText)
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
                    shape = RoundedCornerShape(13.dp),
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
                    shape = RoundedCornerShape(13.dp),
                    colors = ButtonDefaults.buttonColors(containerColor = PanelRaised, contentColor = MainText),
                ) { Text("اعتبارسنجی و ذخیرهٔ امن") }
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
private fun SmartRoutingCard() {
    PanelCard {
        Text("مسیر هوشمند سرور", color = MainText, style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold)
        Text(
            "برای هر دستگاه، کنترل‌پلین یک نود سالم و آنلاین را بر اساس ظرفیت آزاد انتخاب می‌کند. این نسخه هنوز فهرست عمومی منطقه‌ها و انتخاب دستی سرور را منتشر نمی‌کند؛ بنابراین موقعیت ساختگی نمایش داده نمی‌شود.",
            color = SecondaryText,
            style = MaterialTheme.typography.bodyMedium,
            lineHeight = 21.sp,
        )
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            StatusPill(Positive, "AUTO ROUTE")
            Text("انتخاب بر مبنای سلامت و ظرفیت نود", color = SecondaryText, style = MaterialTheme.typography.bodySmall)
        }
        HorizontalDivider(color = DividerColor)
        Text("پروفایل این دستگاه", color = MainText, fontWeight = FontWeight.SemiBold)
        Text(
            "آدرس، کلید عمومی سرور و محدوده‌های IPv4/IPv6 پس از ثبت دستگاه از سرویس دریافت می‌شوند؛ نیازی به واردکردن فایل کانفیگ نیست.",
            color = SecondaryText,
            style = MaterialTheme.typography.bodySmall,
            lineHeight = 19.sp,
        )
    }
}

@Composable
private fun CustomerAccountCard(
    identityConfigured: Boolean,
    signedIn: Boolean,
    authBusy: Boolean,
    authMessage: String,
    onSignIn: () -> Unit,
    onSignOut: () -> Unit,
    profileSaved: Boolean,
    onGoHome: () -> Unit,
) {
    PanelCard {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Surface(shape = RoundedCornerShape(14.dp), color = Primary.copy(alpha = 0.15f)) {
                Icon(Icons.Outlined.Person, contentDescription = null, tint = Primary, modifier = Modifier.padding(13.dp).size(25.dp))
            }
            Spacer(Modifier.width(12.dp))
            Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
                Text("حساب ZEUS", color = MainText, style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold)
                Text(if (signedIn) "نشست تأییدشده روی این دستگاه" else "برای دریافت و مدیریت اتصال وارد شوید", color = SecondaryText, style = MaterialTheme.typography.bodySmall)
            }
            StatusPill(if (signedIn) Positive else MutedText, if (signedIn) "ACTIVE" else "GUEST")
        }
        if (signedIn) {
            Text(
                "حساب با ارائه‌دهندهٔ هویت تأیید شده است. نشست روی دستگاه به‌صورت رمزگذاری‌شده نگهداری می‌شود.",
                color = SecondaryText,
                style = MaterialTheme.typography.bodySmall,
                lineHeight = 19.sp,
            )
            Button(onClick = onSignOut, modifier = Modifier.fillMaxWidth(), shape = RoundedCornerShape(13.dp),
                colors = ButtonDefaults.buttonColors(containerColor = PanelRaised, contentColor = MainText)) {
                Text("خروج امن از حساب")
            }
        } else {
            Text(
                if (identityConfigured) "ورود و ساخت حساب در مرورگر امن ارائه‌دهندهٔ هویت انجام می‌شود." else "برای فعال‌کردن ثبت‌نام عمومی، OIDC باید در نسخهٔ ساخت و کنترل‌پلین تنظیم شود.",
                color = SecondaryText,
                style = MaterialTheme.typography.bodySmall,
                lineHeight = 19.sp,
            )
            Button(
                onClick = onSignIn,
                enabled = identityConfigured && !authBusy,
                modifier = Modifier.fillMaxWidth().height(50.dp),
                shape = RoundedCornerShape(13.dp),
                colors = ButtonDefaults.buttonColors(containerColor = Primary, contentColor = Color.White),
            ) {
                Text(if (authBusy) "در حال آماده‌سازی..." else "ورود / ساخت حساب", fontWeight = FontWeight.Bold)
            }
        }
        if (authMessage.isNotBlank()) InlineNotice(authMessage, if (signedIn) Positive else Warning)
        Row(verticalAlignment = Alignment.CenterVertically) {
            StatusPill(if (profileSaved) Positive else Warning, if (profileSaved) "PROFILE READY" else "NO PROFILE")
            Spacer(Modifier.width(9.dp))
            Text(if (profileSaved) "پروفایل روی گوشی ذخیره شده است." else "پس از ورود، به خانه بروید و دستگاه را ثبت کنید.", color = SecondaryText, style = MaterialTheme.typography.bodySmall)
        }
        if (!profileSaved) {
            TextButton(onClick = onGoHome, modifier = Modifier.align(Alignment.End)) {
                Text("رفتن به راه‌اندازی اتصال", color = Primary)
            }
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
