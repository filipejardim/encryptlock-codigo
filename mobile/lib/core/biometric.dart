import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

/// Why fingerprint/face did not give the words back.
enum BiometricFailure {
  /// The owner closed the dialog.
  canceled,

  /// A fingerprint was added or removed on the phone: the key is gone, so
  /// the password is needed (and biometrics must be turned on again).
  invalidated,

  /// No biometrics on this phone, or another error.
  unavailable,
}

class BiometricException implements Exception {
  const BiometricException(this.failure);

  final BiometricFailure failure;
}

/// Fingerprint / face with a key in the phone's security chip (see
/// BiometricKey.kt). Only on Android; the browser preview has none.
class Biometric {
  static const _channel = MethodChannel('encryptlock/biometric');

  static Future<bool> available() async {
    if (kIsWeb || !Platform.isAndroid) return false;
    try {
      return await _channel.invokeMethod<bool>('available') ?? false;
    } on PlatformException {
      return false;
    }
  }

  static BiometricException _error(PlatformException e) => BiometricException(switch (e.code) {
        'canceled' => BiometricFailure.canceled,
        'invalidated' => BiometricFailure.invalidated,
        _ => BiometricFailure.unavailable,
      });

  /// Encrypts text so that only finger/face can open it.
  static Future<String> seal(String text, {required String title, required String cancel}) async {
    try {
      return (await _channel.invokeMethod<String>('seal', {'text': text, 'title': title, 'cancel': cancel}))!;
    } on PlatformException catch (e) {
      throw _error(e);
    }
  }

  static Future<String> open(String record, {required String title, required String cancel}) async {
    try {
      return (await _channel.invokeMethod<String>('open', {'record': record, 'title': title, 'cancel': cancel}))!;
    } on PlatformException catch (e) {
      throw _error(e);
    }
  }

  static Future<void> delete() async {
    if (kIsWeb || !Platform.isAndroid) return;
    await _channel.invokeMethod<void>('delete');
  }
}
