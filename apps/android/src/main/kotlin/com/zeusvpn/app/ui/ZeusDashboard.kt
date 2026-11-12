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
import com.zeusvpn.app.network.NetworkCondition
import com.zeusvpn.app.network.NetworkMonitor
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
    val scope = rememberCoroutineScope()
    val network by produceState(NetworkCondition.UNKNOWN, monitor) {
        monitor.observe().collect { value = it }
    }
    val autoReconnect by produceState(true, settings) {
        settings.autoReconnectEnabled.collect { value = it }
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
