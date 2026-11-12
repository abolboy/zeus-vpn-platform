package com.zeusvpn.app

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import com.zeusvpn.app.ui.ZeusDashboard
import com.zeusvpn.app.ui.theme.ZeusTheme

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent { ZeusTheme { ZeusDashboard() } }
    }
}
