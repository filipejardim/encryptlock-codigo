import 'dart:convert';
import 'dart:math';

import 'package:cryptography/cryptography.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

import 'core.dart';

/// Keeps the 24 recovery words on the phone, always encrypted with the
/// owner's password (white paper, section 7.3.1: "Decrypt your wallet").
///
/// The password never leaves the phone and is never stored. On Android
/// (format 2) the Go core derives a key from it with Argon2id (64 MiB, so
/// graphics cards cannot guess fast) and encrypts the words with AES-256-GCM;
/// the result is encrypted again with a key inside the phone's security chip
/// (Android Keystore / StrongBox), so the files are useless on another device.
/// Everything is then kept in flutter_secure_storage.
///
/// Format 1 (PBKDF2 in Dart) was used by the first test build: it is still
/// read, and replaced by format 2 at the first correct password. The browser
/// preview, which has no Go core, also uses it.
class WalletStore {
  WalletStore({required this.core, FlutterSecureStorage? storage})
      : _storage = storage ?? const FlutterSecureStorage();

  final WalletCore core;
  final FlutterSecureStorage _storage;

  static const _key = 'encryptlock.wallet.v1';
  static const _bioKey = 'encryptlock.wallet.biometric';
  static const _sendCodeKey = 'encryptlock.sendcode';
  static const _v2Prefix = 'hw1:';
  static const _iterations = 210000;

  static final _aes = AesGcm.with256bits();

  static Pbkdf2 _kdf(int iterations) =>
      Pbkdf2(macAlgorithm: Hmac.sha256(), iterations: iterations, bits: 256);

  static List<int> _random(int length) {
    final rng = Random.secure();
    return List<int>.generate(length, (_) => rng.nextInt(256));
  }

  Future<bool> hasWallet() async => (await _storage.read(key: _key)) != null;

  /// Encrypts and saves the words. Replaces any wallet already saved.
  Future<void> save(String words, String password) async {
    final sealed = await core.sealWords(words, password);
    await _storage.write(key: _key, value: sealed ?? await _sealV1(words, password));
  }

  /// Returns the words, or null if the password is wrong.
  Future<String?> unlock(String password) async {
    final raw = await _storage.read(key: _key);
    if (raw == null) return null;
    // hw1: phone (security chip); pc1: computer (Windows protects the file).
    if (raw.startsWith(_v2Prefix) || raw.startsWith('pc1:')) return core.openWords(raw, password);
    final words = await _openV1(raw, password);
    if (words != null && !core.isPreview) await save(words, password);
    return words;
  }

  /// Removes the wallet from this phone (it can be recovered with the words).
  Future<void> delete() async {
    await _storage.delete(key: _key);
    await _storage.delete(key: _bioKey);
    await _storage.delete(key: _sendCodeKey);
  }

  /// The words sealed with the fingerprint/face key (see Biometric), if on.
  Future<String?> biometricRecord() => _storage.read(key: _bioKey);

  Future<void> saveBiometricRecord(String? record) =>
      record == null ? _storage.delete(key: _bioKey) : _storage.write(key: _bioKey, value: record);

  /// The send code, sealed with Argon2id; null when not set.
  Future<String?> sendCodeRecord() => _storage.read(key: _sendCodeKey);

  Future<void> saveSendCodeRecord(String? record) =>
      record == null ? _storage.delete(key: _sendCodeKey) : _storage.write(key: _sendCodeKey, value: record);

  Future<String> _sealV1(String words, String password) async {
    final salt = _random(16);
    final key = await _kdf(_iterations).deriveKeyFromPassword(password: password, nonce: salt);
    final box = await _aes.encrypt(utf8.encode(words), secretKey: key);
    return jsonEncode({
      'v': 1,
      'kdf': 'pbkdf2-sha256',
      'iterations': _iterations,
      'salt': base64Encode(salt),
      'nonce': base64Encode(box.nonce),
      'cipher': base64Encode(box.cipherText),
      'mac': base64Encode(box.mac.bytes),
    });
  }

  Future<String?> _openV1(String raw, String password) async {
    final record = jsonDecode(raw) as Map<String, dynamic>;
    final key = await _kdf(record['iterations'] as int)
        .deriveKeyFromPassword(password: password, nonce: base64Decode(record['salt'] as String));
    final box = SecretBox(
      base64Decode(record['cipher'] as String),
      nonce: base64Decode(record['nonce'] as String),
      mac: Mac(base64Decode(record['mac'] as String)),
    );
    try {
      return utf8.decode(await _aes.decrypt(box, secretKey: key));
    } on SecretBoxAuthenticationError {
      return null;
    }
  }
}
