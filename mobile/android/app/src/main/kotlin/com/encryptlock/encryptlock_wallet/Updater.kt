package com.encryptlock.encryptlock_wallet

import android.app.Activity
import android.content.Intent
import android.os.Build
import androidx.core.content.FileProvider
import io.flutter.plugin.common.MethodChannel
import java.io.File

/**
 * "Update" inside the app (installed from the website): where the app came
 * from (Google Play updates it by itself), where to keep the download, and
 * the phone's own "Install this update?" screen for the checked file.
 */
object Updater {
    fun handle(activity: Activity, method: String, path: String?, result: MethodChannel.Result) {
        when (method) {
            "installer" -> result.success(installer(activity))
            "cacheDir" -> result.success(activity.cacheDir.absolutePath)
            "install" -> {
                val file = File(path ?: "")
                // Only a file the app downloaded into its own cache.
                if (!file.exists() || file.parentFile?.canonicalPath != activity.cacheDir.canonicalPath) {
                    result.error("update", "no update file", null)
                    return
                }
                val uri = FileProvider.getUriForFile(activity, activity.packageName + ".update", file)
                val intent = Intent(Intent.ACTION_VIEW).apply {
                    setDataAndType(uri, "application/vnd.android.package-archive")
                    addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK)
                }
                activity.startActivity(intent)
                result.success(null)
            }
            else -> result.notImplemented()
        }
    }

    private fun installer(activity: Activity): String? = try {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            activity.packageManager.getInstallSourceInfo(activity.packageName).installingPackageName
        } else {
            @Suppress("DEPRECATION")
            activity.packageManager.getInstallerPackageName(activity.packageName)
        }
    } catch (e: Exception) {
        null
    }
}
