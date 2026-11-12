package com.zeusvpn.app.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
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

private val Ink = Color(0xFF071321)
private val Panel = Color(0xFF122238)
private val Mint = Color(0xFF62E6C2)
private val SoftText = Color(0xFF91A4B9)

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

    Column(
        Modifier.fillMaxSize()
            .background(Brush.verticalGradient(listOf(Ink, Color(0xFF0A1B2D))))
            .verticalScroll(rememberScrollState())
            .padding(20.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text("ZEUS", color = Color.White, fontSize = 32.sp, fontWeight = FontWeight.Black, letterSpacing = 2.sp)
                Text(stringResource(R.string.brand_subtitle), color = SoftText, style = MaterialTheme.typography.labelSmall)
            }
            Surface(shape = RoundedCornerShape(16.dp), color = Mint.copy(alpha = 0.12f)) {
                Text("Z", Modifier.padding(horizontal = 18.dp, vertical = 10.dp), color = Mint, fontSize = 22.sp, fontWeight = FontWeight.Bold)
            }
        }

        val (networkText, networkColor, networkDetail) = when (network) {
            NetworkCondition.ONLINE -> Triple(stringResource(R.string.online), Mint, stringResource(R.string.online_detail))
            NetworkCondition.LIMITED -> Triple(stringResource(R.string.limited), Color(0xFFFFC46B), stringResource(R.string.limited_detail))
            NetworkCondition.OFFLINE -> Triple(stringResource(R.string.offline), Color(0xFFFF7D88), stringResource(R.string.offline_detail))
            NetworkCondition.UNKNOWN -> Triple(stringResource(R.string.checking), SoftText, stringResource(R.string.checking_detail))
        }
        PanelCard {
            Text(stringResource(R.string.network_title), color = SoftText, style = MaterialTheme.typography.labelLarge)
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(networkText, color = networkColor, style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold)
                Spacer(Modifier.weight(1f))
                Text("●", color = networkColor)
            }
            Text(networkDetail, color = Color.White, style = MaterialTheme.typography.bodyMedium)
            Text(stringResource(R.string.network_note), color = SoftText, style = MaterialTheme.typography.bodySmall)
        }

        PanelCard {
            Text(stringResource(R.string.engine_title), color = Mint, style = MaterialTheme.typography.labelLarge)
            Text(stringResource(R.string.engine_heading), color = Color.White, style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold)
            Text(stringResource(R.string.engine_body), color = SoftText, style = MaterialTheme.typography.bodyMedium)
            Surface(shape = RoundedCornerShape(12.dp), color = Color.White.copy(alpha = 0.07f)) {
                Text(stringResource(R.string.engine_status), Modifier.padding(12.dp), color = Color.White)
            }
        }

        PanelCard {
            Text(stringResource(R.string.offline_title), color = Color.White, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
            Text(stringResource(R.string.offline_body), color = SoftText, style = MaterialTheme.typography.bodyMedium)
        }

        PanelCard {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(5.dp)) {
                    Text(stringResource(R.string.settings_title), color = Color.White, fontWeight = FontWeight.SemiBold)
                    Text(stringResource(R.string.autoreconnect), color = SoftText, style = MaterialTheme.typography.bodyMedium)
                    Text(stringResource(R.string.autoreconnect_note), color = SoftText, style = MaterialTheme.typography.bodySmall)
                }
                Switch(
                    checked = autoReconnect,
                    onCheckedChange = { enabled -> scope.launch { settings.setAutoReconnectEnabled(enabled) } },
                    colors = SwitchDefaults.colors(checkedThumbColor = Ink, checkedTrackColor = Mint),
                )
            }
        }

        Text(stringResource(R.string.phase_footer), Modifier.align(Alignment.CenterHorizontally), color = Mint, style = MaterialTheme.typography.labelMedium)
        Text(stringResource(R.string.disclaimer), Modifier.align(Alignment.CenterHorizontally), color = SoftText, style = MaterialTheme.typography.bodySmall)
    }
}

@Composable
private fun PanelCard(content: @Composable ColumnScope.() -> Unit) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(22.dp),
        colors = CardDefaults.cardColors(containerColor = Panel),
    ) {
        Column(Modifier.padding(18.dp), verticalArrangement = Arrangement.spacedBy(10.dp), content = content)
    }
}
