import java.util.Properties

// The EncryptLock signing key (android/key.properties, never in git; the
// key itself in %USERPROFILE%.encryptlock-assinatura). Every version must
// be signed with it, or phones refuse to install it over the old one.
val keyProps = Properties().apply {
    val f = rootProject.file("key.properties")
    if (f.exists()) f.inputStream().use { load(it) }
}

plugins {
    id("com.android.application")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

android {
    namespace = "com.encryptlock.encryptlock_wallet"
    compileSdk = flutter.compileSdkVersion
    ndkVersion = flutter.ndkVersion

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
        // Needed by the notifications package (flutter_local_notifications).
        isCoreLibraryDesugaringEnabled = true
    }

    defaultConfig {
        // TODO: Specify your own unique Application ID (https://developer.android.com/studio/build/application-id.html).
        applicationId = "com.encryptlock.encryptlock_wallet"
        // You can update the following values to match your application needs.
        // For more information, see: https://flutter.dev/to/review-gradle-config.
        // Android 7.0+ (the Go wallet core is built for API 24).
        minSdk = 24
        targetSdk = flutter.targetSdkVersion
        versionCode = flutter.versionCode
        versionName = flutter.versionName
    }

    signingConfigs {
        create("release") {
            if (keyProps.isNotEmpty()) {
                storeFile = file(keyProps.getProperty("storeFile"))
                storePassword = keyProps.getProperty("storePassword")
                keyAlias = keyProps.getProperty("keyAlias")
                keyPassword = keyProps.getProperty("keyPassword")
            }
        }
    }

    buildTypes {
        release {
            // The EncryptLock key; without key.properties (another computer)
            // the debug key, which phones will not accept as an update.
            signingConfig = signingConfigs.getByName(if (keyProps.isNotEmpty()) "release" else "debug")
        }
    }
}

kotlin {
    compilerOptions {
        jvmTarget = org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17
    }
}

flutter {
    source = "../.."
}

dependencies {
    // Go wallet core (package mobilecore), built by scripts/mobilecore.sh.
    implementation(files("libs/mobilecore.aar"))
    // FileProvider: hands the checked update file to the phone's installer.
    implementation("androidx.core:core:1.13.1")
    coreLibraryDesugaring("com.android.tools:desugar_jdk_libs:2.1.5")
}
