package com.encryptlock.encryptlock_wallet

import android.content.pm.ApplicationInfo
import android.view.WindowManager
import com.encryptlock.mobilecore.Mobilecore
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import java.util.concurrent.Executors

/**
 * Connects the Flutter screens to the Go wallet core (package mobilecore):
 * the same code as the EncryptLock network and command-line wallet.
 */
class MainActivity : FlutterActivity() {
    private var links: MethodChannel? = null

    override fun onNewIntent(intent: android.content.Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        intent.dataString?.let { links?.invokeMethod("link", it) }
    }

    // Key work (ML-DSA-65) is slow-ish: never on the screen thread.
    private val worker = Executors.newSingleThreadExecutor()

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        // The 24 recovery words appear on screen: block screenshots and the
        // app preview in "recent apps". (Test builds allow screenshots.)
        val debuggable = applicationInfo.flags and ApplicationInfo.FLAG_DEBUGGABLE != 0
        if (!debuggable) {
            window.addFlags(WindowManager.LayoutParams.FLAG_SECURE)
        }

        // encryptlock:lock1... links (QR codes): the app asks for the one it
        // was opened with; later ones are pushed while it runs.
        links = MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "encryptlock/link")
        links!!.setMethodCallHandler { call, result ->
            if (call.method == "initial") result.success(intent?.dataString) else result.notImplemented()
        }

        // Fingerprint / face: runs on the screen thread (it shows a dialog).
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "encryptlock/biometric").setMethodCallHandler { call, result ->
            val title = call.argument<String>("title") ?: ""
            val cancel = call.argument<String>("cancel") ?: ""
            val done = BiometricKey.Done { value, error ->
                if (error == null) result.success(value) else result.error(error, error, null)
            }
            when (call.method) {
                "available" -> result.success(BiometricKey.available(this))
                "seal" -> BiometricKey.seal(this, call.argument<String>("text")!!, title, cancel, done)
                "open" -> BiometricKey.open(this, call.argument<String>("record")!!, title, cancel, done)
                "delete" -> {
                    BiometricKey.delete()
                    result.success(null)
                }
                else -> result.notImplemented()
            }
        }

        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "encryptlock/update").setMethodCallHandler { call, result ->
            Updater.handle(this, call.method, call.argument<String>("path"), result)
        }

        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "encryptlock/core").setMethodCallHandler { call, result ->
            worker.execute {
                try {
                    val value: Any = when (call.method) {
                        "newMnemonic" -> Mobilecore.newMnemonic()
                        "validateMnemonic" -> Mobilecore.validateMnemonic(call.argument<String>("words")!!)
                        "address" -> Mobilecore.address(call.argument<String>("words")!!)
                        "polygonAddress" -> Mobilecore.polygonAddress(call.argument<String>("words")!!)
                        "validatorAddress" -> Mobilecore.validatorAddress(call.argument<String>("address")!!)
                        // Password-style secrets (also the send code): Argon2id in Go.
                        "sealSecret" -> Mobilecore.sealWords(call.argument<String>("text")!!, call.argument<String>("password")!!)
                        "openSecret" -> Mobilecore.openWords(call.argument<String>("record")!!, call.argument<String>("password")!!)
                        "isAddress" -> Mobilecore.isAddress(call.argument<String>("address")!!)
                        "parseLock" -> Mobilecore.parseLock(call.argument<String>("text")!!)
                        "transferFee" -> Mobilecore.transferFee(long(call, "units"))
                        // Signing: both keys (Ed25519 + ML-DSA-65), in Go.
                        "signRegister" -> Mobilecore.signRegister(
                            call.argument<String>("words")!!, call.argument<String>("chainId")!!,
                            call.argument<String>("accountNumber")!!, long(call, "sequence")
                        )
                        "signSend" -> Mobilecore.signSend(
                            call.argument<String>("words")!!, call.argument<String>("chainId")!!,
                            call.argument<String>("accountNumber")!!, long(call, "sequence"),
                            call.argument<String>("to")!!, long(call, "units")
                        )
                        "signAccept" -> Mobilecore.signAccept(
                            call.argument<String>("words")!!, call.argument<String>("chainId")!!,
                            call.argument<String>("accountNumber")!!, long(call, "sequence"), long(call, "id")
                        )
                        "signFreeze" -> Mobilecore.signFreeze(
                            call.argument<String>("words")!!, call.argument<String>("chainId")!!,
                            call.argument<String>("accountNumber")!!, long(call, "sequence")
                        )
                        "signUnfreeze" -> Mobilecore.signUnfreeze(
                            call.argument<String>("words")!!, call.argument<String>("chainId")!!,
                            call.argument<String>("accountNumber")!!, long(call, "sequence")
                        )
                        "signFreezeFor" -> Mobilecore.signFreezeFor(
                            call.argument<String>("words")!!, call.argument<String>("chainId")!!,
                            call.argument<String>("accountNumber")!!, long(call, "sequence"), call.argument<String>("account")!!
                        )
                        "signSetDevice" -> Mobilecore.signSetDevice(
                            call.argument<String>("words")!!, call.argument<String>("chainId")!!,
                            call.argument<String>("accountNumber")!!, long(call, "sequence"), call.argument<String>("device")!!
                        )
                        "signSetLimits" -> Mobilecore.signSetLimits(
                            call.argument<String>("words")!!, call.argument<String>("chainId")!!,
                            call.argument<String>("accountNumber")!!, long(call, "sequence"), long(call, "dailyLimit"), long(call, "approvalAbove")
                        )
                        "signApprove" -> Mobilecore.signApprove(
                            call.argument<String>("words")!!, call.argument<String>("chainId")!!,
                            call.argument<String>("accountNumber")!!, long(call, "sequence"), long(call, "id")
                        )
                        "signSetPlan" -> Mobilecore.signSetPlan(
                            call.argument<String>("words")!!, call.argument<String>("chainId")!!,
                            call.argument<String>("accountNumber")!!, long(call, "sequence"), call.argument<String>("plan")!!
                        )
                        "signAlive" -> Mobilecore.signAlive(
                            call.argument<String>("words")!!, call.argument<String>("chainId")!!,
                            call.argument<String>("accountNumber")!!, long(call, "sequence")
                        )
                        "signCancelClaims" -> Mobilecore.signCancelClaims(
                            call.argument<String>("words")!!, call.argument<String>("chainId")!!,
                            call.argument<String>("accountNumber")!!, long(call, "sequence")
                        )
                        "signClaimInheritance" -> Mobilecore.signClaimInheritance(
                            call.argument<String>("words")!!, call.argument<String>("chainId")!!,
                            call.argument<String>("accountNumber")!!, long(call, "sequence"), call.argument<String>("account")!!
                        )
                        "signApproveOffer" -> Mobilecore.signApproveOffer(
                            call.argument<String>("words")!!, call.argument<String>("chainId")!!,
                            call.argument<String>("accountNumber")!!, long(call, "sequence"), long(call, "id")
                        )
                        "signApproveRecovery" -> Mobilecore.signApproveRecovery(
                            call.argument<String>("words")!!, call.argument<String>("chainId")!!,
                            call.argument<String>("accountNumber")!!, long(call, "sequence"), call.argument<String>("account")!!, call.argument<String>("newAddress")!!
                        )
                        "signSetVault" -> Mobilecore.signSetVault(
                            call.argument<String>("words")!!, call.argument<String>("chainId")!!,
                            call.argument<String>("accountNumber")!!, long(call, "sequence"), long(call, "delayDays")
                        )
                        "newSwapSecret" -> Mobilecore.newSwapSecret()
                        "swapHash" -> Mobilecore.swapHash(call.argument<String>("secret")!!)
                        "signPlaceOffer" -> Mobilecore.signPlaceOffer(
                            call.argument<String>("words")!!, call.argument<String>("chainId")!!,
                            call.argument<String>("accountNumber")!!, long(call, "sequence"), long(call, "side"),
                            long(call, "lock"), long(call, "usdc"), call.argument<String>("polygon")!!,
                            call.argument<String>("hash") ?: "", long(call, "expiresAt")
                        )
                        "signCancelOffer" -> Mobilecore.signCancelOffer(
                            call.argument<String>("words")!!, call.argument<String>("chainId")!!,
                            call.argument<String>("accountNumber")!!, long(call, "sequence"), long(call, "id")
                        )
                        "signTakeOffer" -> Mobilecore.signTakeOffer(
                            call.argument<String>("words")!!, call.argument<String>("chainId")!!,
                            call.argument<String>("accountNumber")!!, long(call, "sequence"), long(call, "id"),
                            call.argument<String>("polygon")!!, call.argument<String>("hash") ?: ""
                        )
                        "signLock" -> Mobilecore.signLock(
                            call.argument<String>("words")!!, call.argument<String>("chainId")!!,
                            call.argument<String>("accountNumber")!!, long(call, "sequence"), long(call, "units"), long(call, "until")
                        )
                        // A LockKeeper wallet moves to another computer.
                        "signMoveLockKeeper" -> Mobilecore.signMoveLockKeeper(
                            call.argument<String>("words")!!, call.argument<String>("chainId")!!,
                            call.argument<String>("accountNumber")!!, long(call, "sequence"),
                            call.argument<String>("consPubKey") ?: "", call.argument<String>("helper")!!
                        )
                        // Link a computer by QR code: guarantee + helper.
                        "signBecomeLockKeeper" -> Mobilecore.signBecomeLockKeeper(
                            call.argument<String>("words")!!, call.argument<String>("chainId")!!,
                            call.argument<String>("accountNumber")!!, long(call, "sequence"),
                            call.argument<String>("consPubKey")!!, long(call, "units"),
                            call.argument<String>("moniker")!!, call.argument<String>("helper")!!
                        )
                        // "Send" with POL or USDC (token "" = POL).
                        "signPolygonSend" -> Mobilecore.signPolygonSend(
                            call.argument<String>("words")!!, long(call, "polygonChainId"), long(call, "nonce"),
                            call.argument<String>("tipWei")!!, call.argument<String>("feeCapWei")!!,
                            call.argument<String>("token") ?: "", call.argument<String>("to")!!,
                            call.argument<String>("amount")!!
                        )
                        // "Buy": the three Polygon transactions that pay a trade.
                        "signPolygonPayment" -> Mobilecore.signPolygonPayment(
                            call.argument<String>("words")!!, long(call, "polygonChainId"), long(call, "nonce"),
                            call.argument<String>("tipWei")!!, call.argument<String>("feeCapWei")!!,
                            call.argument<String>("usdc")!!, call.argument<String>("vault")!!,
                            call.argument<String>("seller")!!, call.argument<String>("secret")!!,
                            long(call, "amount"), long(call, "timelock")
                        )
                        "signPolygonRefund" -> Mobilecore.signPolygonRefund(
                            call.argument<String>("words")!!, long(call, "polygonChainId"), long(call, "nonce"),
                            call.argument<String>("tipWei")!!, call.argument<String>("feeCapWei")!!,
                            call.argument<String>("usdc")!!, call.argument<String>("vault")!!,
                            call.argument<String>("seller")!!, call.argument<String>("hash")!!,
                            long(call, "amount"), long(call, "timelock")
                        )
                        "signCollectRewards" -> Mobilecore.signCollectRewards(
                            call.argument<String>("words")!!, call.argument<String>("chainId")!!,
                            call.argument<String>("accountNumber")!!, long(call, "sequence")
                        )
                        "signCancel" -> Mobilecore.signCancel(
                            call.argument<String>("words")!!, call.argument<String>("chainId")!!,
                            call.argument<String>("accountNumber")!!, long(call, "sequence"), long(call, "id")
                        )
                        // Password (Argon2id + AES-256-GCM, in Go), then the security chip.
                        "sealWords" -> HardwareKey.seal(
                            Mobilecore.sealWords(call.argument<String>("words")!!, call.argument<String>("password")!!)
                        )
                        // "" = wrong password.
                        "openWords" -> Mobilecore.openWords(
                            HardwareKey.open(call.argument<String>("record")!!), call.argument<String>("password")!!
                        )
                        else -> {
                            runOnUiThread { result.notImplemented() }
                            return@execute
                        }
                    }
                    runOnUiThread { result.success(value) }
                } catch (e: Exception) {
                    runOnUiThread { result.error("core", e.message, null) }
                }
            }
        }
    }
}

/** Dart ints arrive as Int or Long. */
private fun long(call: io.flutter.plugin.common.MethodCall, name: String): Long =
    (call.argument<Number>(name)!!).toLong()
