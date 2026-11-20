plugins {
    id("com.android.application")
}

android {
    namespace = "com.zeusvpn.visibilityfixture"
    compileSdk = 36

    defaultConfig {
        applicationId = "com.zeusvpn.visibilityfixture"
        minSdk = 24
        targetSdk = 36
        versionCode = 1
        versionName = "1.0"
    }

    // CI supplies a one-off key distinct from the target app's debug key.
    // Android treats same-certificate packages as automatically visible.
    val fixtureKeyPath = providers.gradleProperty("FIXTURE_KEYSTORE_PATH")
    if (fixtureKeyPath.isPresent) {
        signingConfigs {
            create("fixture") {
                storeFile = file(fixtureKeyPath.get())
                storePassword = providers.gradleProperty("FIXTURE_KEYSTORE_PASSWORD").get()
                keyAlias = "fixture"
                keyPassword = providers.gradleProperty("FIXTURE_KEYSTORE_PASSWORD").get()
            }
        }
        buildTypes {
            getByName("debug") {
                signingConfig = signingConfigs.getByName("fixture")
            }
        }
    }
}
