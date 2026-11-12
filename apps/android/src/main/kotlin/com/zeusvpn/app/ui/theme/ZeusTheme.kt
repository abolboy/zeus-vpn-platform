package com.zeusvpn.app.ui.theme

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

@Composable
fun ZeusTheme(content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = darkColorScheme(
            primary = Color(0xFF62E6C2),
            onPrimary = Color(0xFF071321),
            secondary = Color(0xFF91A4B9),
            background = Color(0xFF071321),
            surface = Color(0xFF122238),
            onSurface = Color(0xFFF3F7FC),
        ),
        content = content,
    )
}
