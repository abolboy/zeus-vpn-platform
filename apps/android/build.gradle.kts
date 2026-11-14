plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.plugin.compose")
}

val zeusApiBaseUrl = providers.gradleProperty("ZEUS_API_BASE_URL").orElse("").get()
val zeusOidcIssuer = providers.gradleProperty("ZEUS_OIDC_ISSUER").orElse("").get()
val zeusOidcClientId = providers.gradleProperty("ZEUS_OIDC_CLIENT_ID").orElse("").get()
val zeusOidcRedirectUri = providers.gradleProperty("ZEUS_OIDC_REDIRECT_URI").orElse("com.zeusvpn.app:/oauth2redirect").get()

android {
    namespace = "com.zeusvpn.app"
    compileSdk = 36
    defaultConfig {
        applicationId = "com.zeusvpn.app"
        minSdk = 24
        targetSdk = 36
        versionCode = 1
        versionName = "0.1.0"
        manifestPlaceholders["appAuthRedirectScheme"] = "com.zeusvpn.app"
        buildConfigField("String", "ZEUS_API_BASE_URL", "\"$zeusApiBaseUrl\"")
        buildConfigField("String", "ZEUS_OIDC_ISSUER", "\"$zeusOidcIssuer\"")
        buildConfigField("String", "ZEUS_OIDC_CLIENT_ID", "\"$zeusOidcClientId\"")
        buildConfigField("String", "ZEUS_OIDC_REDIRECT_URI", "\"$zeusOidcRedirectUri\"")
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }
    buildTypes {
        // Keep this diagnostics APK installable beside an earlier .debug build.
        debug { applicationIdSuffix = ".diagnostics"; versionNameSuffix = "-diagnostics" }
        release {
            isMinifyEnabled = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
        isCoreLibraryDesugaringEnabled = true
    }
    buildFeatures {
        compose = true
        buildConfig = true
    }
    packaging { resources { excludes += "/META-INF/{AL2.0,LGPL2.1}" } }
}
dependencies {
    // Stable Compose 1.11.4 line, compatible with the stable Android API 36 build target.
    val composeBom = platform("androidx.compose:compose-bom:2026.06.01")
    implementation(composeBom)
    androidTestImplementation(composeBom)
    implementation("androidx.activity:activity-compose:1.13.0")
    implementation("net.openid:appauth:0.11.1")
    implementation("androidx.compose.material:material-icons-extended")
    // Official embeddable WireGuard userspace tunnel backend.
    implementation("com.wireguard.android:tunnel:1.0.20260102")
    coreLibraryDesugaring("com.android.tools:desugar_jdk_libs:2.0.3")
    implementation("androidx.compose.foundation:foundation")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.ui:ui-tooling-preview")
    implementation("androidx.datastore:datastore-preferences:1.3.0")
    debugImplementation("androidx.compose.ui:ui-tooling")
    debugImplementation("androidx.compose.ui:ui-test-manifest")
    testImplementation("junit:junit:4.13.2")
    androidTestImplementation("androidx.test.ext:junit:1.2.1")
    androidTestImplementation("androidx.test.espresso:espresso-core:3.6.1")
    androidTestImplementation("androidx.compose.ui:ui-test-junit4")
}
