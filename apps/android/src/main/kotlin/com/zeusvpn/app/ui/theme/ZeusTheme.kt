package com.zeusvpn.app.ui.theme

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

private val ZeusColors = darkColorScheme(
    primary = Color(0xFF62E6C2),
    onPrimary = Color(0xFF071321),
    secondary = Color(0xFF9D85FF),
    onSecondary = Color(0xFF111126),
    tertiary = Color(0xFF7BD9F8),
    background = Color(0xFF070D19),
    onBackground = Color(0xFFF3F7FC),
    surface = Color(0xFF111D31),
    onSurface = Color(0xFFF3F7FC),
    surfaceVariant = Color(0xFF1B2A40),
    onSurfaceVariant = Color(0xFFB1C0D4),
    outline = Color(0xFF34445D),
)

@Composable
fun ZeusTheme(content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = ZeusColors,
        content = content,
    )
}
