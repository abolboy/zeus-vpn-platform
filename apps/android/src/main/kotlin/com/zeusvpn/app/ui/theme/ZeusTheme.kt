package com.zeusvpn.app.ui.theme

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

private val ZeusColors = darkColorScheme(
    primary = Color(0xFF4E9DDB),
    onPrimary = Color(0xFFFFFFFF),
    secondary = Color(0xFF42C69A),
    onSecondary = Color(0xFF071A16),
    tertiary = Color(0xFFE6B566),
    background = Color(0xFF0B1220),
    onBackground = Color(0xFFF2F5F9),
    surface = Color(0xFF151F30),
    onSurface = Color(0xFFF2F5F9),
    surfaceVariant = Color(0xFF1B293D),
    onSurfaceVariant = Color(0xFFA7B5C8),
    outline = Color(0xFF2A394E),
)

@Composable
fun ZeusTheme(content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = ZeusColors,
        content = content,
    )
}
