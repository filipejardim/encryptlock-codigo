package com.encryptlock.encryptlock_wallet

import android.app.Activity
import android.hardware.biometrics.BiometricManager
import android.hardware.biometrics.BiometricPrompt
import android.os.Build
import android.os.CancellationSignal
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyPermanentlyInvalidatedException
import android.security.keystore.KeyProperties
import android.util.Base64
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/**
 * Unlocking with fingerprint or face, done the strong way: the 24 words are
 * encrypted with a key inside the phone's security chip that can only be used
 * right after the owner's finger or face is recognised (a CryptoObject, not
 * just a yes/no answer). Adding a new fingerprint to the phone destroys the
 * key, so the password is needed again.
 *
 * Android 10+ (API 29). Older phones keep using the password only.
 */
object BiometricKey {
    private const val ALIAS = "encryptlock.wallet.biometric"
    private const val PREFIX = "bio1:"

    /** Result for Dart: the text, or an error code ("canceled", "invalidated", "unavailable", …). */
    fun interface Done {
        fun call(value: String?, error: String?)
    }

    fun available(activity: Activity): Boolean {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.Q) return false
        val manager = activity.getSystemService(BiometricManager::class.java) ?: return false
        val result = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            manager.canAuthenticate(BiometricManager.Authenticators.BIOMETRIC_STRONG)
        } else {
            @Suppress("DEPRECATION") manager.canAuthenticate()
        }
        return result == BiometricManager.BIOMETRIC_SUCCESS
    }

    private fun store() = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }

    fun delete() {
        store().deleteEntry(ALIAS)
    }

    private fun newKey(): SecretKey {
        delete()
        val spec = KeyGenParameterSpec.Builder(ALIAS, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
            .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
            .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
            .setKeySize(256)
            .setUserAuthenticationRequired(true)
            .setInvalidatedByBiometricEnrollment(true)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            // Every single use needs a strong biometric (no time window).
            spec.setUserAuthenticationParameters(0, KeyProperties.AUTH_BIOMETRIC_STRONG)
        }
        return KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore")
            .apply { init(spec.build()) }
            .generateKey()
    }

    private fun prompt(activity: Activity, title: String, cancel: String, cipher: Cipher, done: Done, use: (Cipher) -> String) {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.Q) {
            done.call(null, "unavailable")
            return
        }
        val builder = BiometricPrompt.Builder(activity)
            .setTitle(title)
            .setNegativeButton(cancel, activity.mainExecutor) { _, _ -> done.call(null, "canceled") }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            builder.setAllowedAuthenticators(BiometricManager.Authenticators.BIOMETRIC_STRONG)
        }
        builder.build().authenticate(
            BiometricPrompt.CryptoObject(cipher),
            CancellationSignal(),
            activity.mainExecutor,
            object : BiometricPrompt.AuthenticationCallback() {
                override fun onAuthenticationSucceeded(result: BiometricPrompt.AuthenticationResult) {
                    try {
                        done.call(use(result.cryptoObject!!.cipher!!), null)
                    } catch (e: Exception) {
                        done.call(null, "failed")
                    }
                }

                override fun onAuthenticationError(errorCode: Int, errString: CharSequence) {
                    done.call(null, if (errorCode == BiometricPrompt.BIOMETRIC_ERROR_USER_CANCELED ||
                        errorCode == BiometricPrompt.BIOMETRIC_ERROR_CANCELED) "canceled" else "error")
                }
            },
        )
    }

    /** Encrypts text with a new biometric key (asks for finger/face once). */
    fun seal(activity: Activity, text: String, title: String, cancel: String, done: Done) {
        val cipher = try {
            Cipher.getInstance("AES/GCM/NoPadding").apply { init(Cipher.ENCRYPT_MODE, newKey()) }
        } catch (e: Exception) {
            done.call(null, "unavailable")
            return
        }
        prompt(activity, title, cancel, cipher, done) { c ->
            val out = c.doFinal(text.toByteArray(Charsets.UTF_8))
            val b64 = { b: ByteArray -> Base64.encodeToString(b, Base64.NO_WRAP) }
            PREFIX + b64(c.iv) + ":" + b64(out)
        }
    }

    /** Decrypts what seal made (asks for finger/face). */
    fun open(activity: Activity, record: String, title: String, cancel: String, done: Done) {
        val cipher = try {
            require(record.startsWith(PREFIX))
            val (iv, _) = record.removePrefix(PREFIX).split(":")
            val key = store().getKey(ALIAS, null) as SecretKey? ?: throw KeyPermanentlyInvalidatedException()
            Cipher.getInstance("AES/GCM/NoPadding").apply {
                init(Cipher.DECRYPT_MODE, key, GCMParameterSpec(128, Base64.decode(iv, Base64.NO_WRAP)))
            }
        } catch (e: KeyPermanentlyInvalidatedException) {
            done.call(null, "invalidated")
            return
        } catch (e: Exception) {
            done.call(null, "unavailable")
            return
        }
        prompt(activity, title, cancel, cipher, done) { c ->
            val data = Base64.decode(record.removePrefix(PREFIX).split(":")[1], Base64.NO_WRAP)
            String(c.doFinal(data), Charsets.UTF_8)
        }
    }
}
