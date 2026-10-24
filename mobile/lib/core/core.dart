import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

/// The wallet core: 24 recovery words, addresses and (later) signing.
///
/// On Android it calls the same Go code as the network and the command-line
/// wallet (package mobilecore, built with gomobile), so the app never
/// reimplements cryptography. In the browser there is no Go code: a preview
/// with example data is used only to look at the screens.
abstract class WalletCore {
  static WalletCore create() {
    if (kIsWeb) return PreviewCore();
    // Phone: Go core inside the app. Computer: the same Go core as a small
    // program next to the app (encryptlock-core.exe).
    return NativeCore(Platform.isAndroid ? const ChannelTransport() : ProcessTransport());
  }

  /// True when running with example data (browser preview).
  bool get isPreview;

  Future<String> newMnemonic();

  Future<bool> validateMnemonic(String words);

  Future<String> address(String words);

  /// The 0x… address of the same wallet on Polygon (USDC and POL).
  Future<String> polygonAddress(String words);

  /// A new trade secret (hex) and its sha256 (hex).
  Future<String> newSwapSecret();
  Future<String> swapHash(String secret);

  /// The lockvaloper1... address this account would have as a LockKeeper.
  Future<String> validatorAddress(String address);

  /// A small secret (the send code) checked with Argon2id, like the password.
  Future<String> sealSecret(String text, String password);

  /// The text, or null if the password is wrong.
  Future<String?> openSecret(String record, String password);

  Future<bool> isAddress(String address);

  /// Amount typed by the user ("1,5") in lockunit; null if not valid.
  Future<int?> parseLock(String text);

  /// Fee the sender pays for sending this many lockunit (network rule).
  Future<int> transferFee(int units);

  /// Signed transactions (base64), with both keys of the wallet.
  Future<String> signRegister(String words, String chainId, String accountNumber, int sequence);
  Future<String> signSend(String words, String chainId, String accountNumber, int sequence, String to, int units);
  Future<String> signAccept(String words, String chainId, String accountNumber, int sequence, int id);
  Future<String> signCancel(String words, String chainId, String accountNumber, int sequence, int id);
  Future<String> signFreeze(String words, String chainId, String accountNumber, int sequence);

  /// Other signed messages (plan, "I'm here", vault…): method name of the Go
  /// core and its extra arguments.
  Future<String> sign(String method, String words, String chainId, String accountNumber, int sequence,
      [Map<String, Object> extra = const {}]);
  Future<String> signFreezeFor(String words, String chainId, String accountNumber, int sequence, String account);
  Future<String> signSetDevice(String words, String chainId, String accountNumber, int sequence, String device);
  Future<String> signSetLimits(
      String words, String chainId, String accountNumber, int sequence, int dailyLimit, int approvalAbove);
  Future<String> signApprove(String words, String chainId, String accountNumber, int sequence, int id);
  Future<String> signUnfreeze(String words, String chainId, String accountNumber, int sequence);

  /// Encrypts the words with the password (Argon2id + AES-256-GCM in Go) and
  /// again with the phone's security chip. Null in the browser preview.
  Future<String?> sealWords(String words, String password);

  /// The words, or null if the password is wrong.
  Future<String?> openWords(String record, String password);
}

/// The real wallet core (Go), reached through a transport.
class NativeCore implements WalletCore {
  NativeCore(this._channel);

  final CoreTransport _channel;

  @override
  bool get isPreview => false;

  @override
  Future<String> newMnemonic() async => (await _channel.invokeMethod<String>('newMnemonic'))!;

  @override
  Future<bool> validateMnemonic(String words) async =>
      (await _channel.invokeMethod<bool>('validateMnemonic', {'words': words}))!;

  @override
  Future<String> address(String words) async =>
      (await _channel.invokeMethod<String>('address', {'words': words}))!;

  @override
  Future<String> polygonAddress(String words) async =>
      (await _channel.invokeMethod<String>('polygonAddress', {'words': words}))!;

  @override
  Future<String> newSwapSecret() async => (await _channel.invokeMethod<String>('newSwapSecret'))!;

  @override
  Future<String> swapHash(String secret) async =>
      (await _channel.invokeMethod<String>('swapHash', {'secret': secret}))!;

  @override
  Future<String> validatorAddress(String address) async =>
      (await _channel.invokeMethod<String>('validatorAddress', {'address': address}))!;

  @override
  Future<bool> isAddress(String address) async =>
      (await _channel.invokeMethod<bool>('isAddress', {'address': address.trim()}))!;

  @override
  Future<String> sealSecret(String text, String password) async =>
      (await _channel.invokeMethod<String>('sealSecret', {'text': text, 'password': password}))!;

  @override
  Future<String?> openSecret(String record, String password) async {
    final text = await _channel.invokeMethod<String>('openSecret', {'record': record, 'password': password});
    return (text == null || text.isEmpty) ? null : text;
  }

  @override
  Future<int?> parseLock(String text) async {
    try {
      return await _channel.invokeMethod<int>('parseLock', {'text': text});
    } on PlatformException {
      return null;
    }
  }

  @override
  Future<int> transferFee(int units) async => (await _channel.invokeMethod<int>('transferFee', {'units': units}))!;

  @override
  Future<String> signRegister(String words, String chainId, String accountNumber, int sequence) async =>
      (await _channel.invokeMethod<String>('signRegister', {
        'words': words,
        'chainId': chainId,
        'accountNumber': accountNumber,
        'sequence': sequence,
      }))!;

  @override
  Future<String> signSend(String words, String chainId, String accountNumber, int sequence, String to, int units) async =>
      (await _channel.invokeMethod<String>('signSend', {
        'words': words,
        'chainId': chainId,
        'accountNumber': accountNumber,
        'sequence': sequence,
        'to': to.trim(),
        'units': units,
      }))!;

  @override
  Future<String> signAccept(String words, String chainId, String accountNumber, int sequence, int id) async =>
      (await _channel.invokeMethod<String>('signAccept', {
        'words': words,
        'chainId': chainId,
        'accountNumber': accountNumber,
        'sequence': sequence,
        'id': id,
      }))!;

  @override
  Future<String> signFreeze(String words, String chainId, String accountNumber, int sequence) async =>
      (await _channel.invokeMethod<String>('signFreeze', {
        'words': words,
        'chainId': chainId,
        'accountNumber': accountNumber,
        'sequence': sequence,
      }))!;

  @override
  Future<String> signFreezeFor(String words, String chainId, String accountNumber, int sequence, String account) async =>
      (await _channel.invokeMethod<String>('signFreezeFor', {
        'words': words,
        'chainId': chainId,
        'accountNumber': accountNumber,
        'sequence': sequence,
        'account': account,
      }))!;

  @override
  Future<String> signSetDevice(String words, String chainId, String accountNumber, int sequence, String device) async =>
      (await _channel.invokeMethod<String>('signSetDevice', {
        'words': words,
        'chainId': chainId,
        'accountNumber': accountNumber,
        'sequence': sequence,
        'device': device,
      }))!;

  @override
  Future<String> signSetLimits(String words, String chainId, String accountNumber, int sequence, int dailyLimit, int approvalAbove) async =>
      (await _channel.invokeMethod<String>('signSetLimits', {
        'words': words,
        'chainId': chainId,
        'accountNumber': accountNumber,
        'sequence': sequence,
        'dailyLimit': dailyLimit,
        'approvalAbove': approvalAbove,
      }))!;

  @override
  Future<String> signApprove(String words, String chainId, String accountNumber, int sequence, int id) async =>
      (await _channel.invokeMethod<String>('signApprove', {
        'words': words,
        'chainId': chainId,
        'accountNumber': accountNumber,
        'sequence': sequence,
        'id': id,
      }))!;

  @override
  Future<String> sign(String method, String words, String chainId, String accountNumber, int sequence,
          [Map<String, Object> extra = const {}]) async =>
      (await _channel.invokeMethod<String>(method, {
        'words': words,
        'chainId': chainId,
        'accountNumber': accountNumber,
        'sequence': sequence,
        ...extra,
      }))!;

  @override
  Future<String> signUnfreeze(String words, String chainId, String accountNumber, int sequence) async =>
      (await _channel.invokeMethod<String>('signUnfreeze', {
        'words': words,
        'chainId': chainId,
        'accountNumber': accountNumber,
        'sequence': sequence,
      }))!;

  @override
  Future<String> signCancel(String words, String chainId, String accountNumber, int sequence, int id) async =>
      (await _channel.invokeMethod<String>('signCancel', {
        'words': words,
        'chainId': chainId,
        'accountNumber': accountNumber,
        'sequence': sequence,
        'id': id,
      }))!;

  @override
  Future<String?> sealWords(String words, String password) =>
      _channel.invokeMethod<String>('sealWords', {'words': words, 'password': password});

  @override
  Future<String?> openWords(String record, String password) async {
    final words = await _channel.invokeMethod<String>('openWords', {'record': record, 'password': password});
    return (words == null || words.isEmpty) ? null : words;
  }
}

/// Example data for the browser preview only. Never used on a phone.
class PreviewCore implements WalletCore {
  static const _exampleWords =
      'tide quarter nerve olympic awake ladder magnet crane ripple velvet anchor '
      'sunset orbit cabin honey wrist gentle oxygen pencil cluster mango fossil dwarf bridge';

  @override
  bool get isPreview => true;

  @override
  Future<String> newMnemonic() async => _exampleWords;

  @override
  Future<bool> validateMnemonic(String words) async => words.trim().split(RegExp(r'\s+')).length == 24;

  @override
  Future<String> address(String words) async => 'lock1preview0example0address0not0real0000000';

  @override
  Future<String> polygonAddress(String words) async => '0x0000000000000000000000000000000000000000';

  @override
  Future<String> newSwapSecret() async => '00' * 32;

  @override
  Future<String> swapHash(String secret) async => '00' * 32;

  @override
  Future<String> validatorAddress(String address) async => 'lockvaloper1preview';

  @override
  Future<bool> isAddress(String address) async => address.trim().startsWith('lock1');

  // Preview only (never on a phone): not secret.
  @override
  Future<String> sealSecret(String text, String password) async => 'preview:$password:$text';

  @override
  Future<String?> openSecret(String record, String password) async {
    final parts = record.split(':');
    return parts.length == 3 && parts[1] == password ? parts[2] : null;
  }

  @override
  Future<int?> parseLock(String text) async {
    final v = double.tryParse(text.trim().replaceAll(',', '.'));
    return v == null || v <= 0 ? null : (v * 100000000).round();
  }

  @override
  Future<int> transferFee(int units) async => (units ~/ 1000).clamp(20000, 1000000);

  @override
  Future<String> signRegister(String words, String chainId, String accountNumber, int sequence) =>
      throw UnsupportedError('preview');

  @override
  Future<String> signSend(String words, String chainId, String accountNumber, int sequence, String to, int units) =>
      throw UnsupportedError('preview');

  @override
  Future<String> signAccept(String words, String chainId, String accountNumber, int sequence, int id) =>
      throw UnsupportedError('preview');

  @override
  Future<String> signCancel(String words, String chainId, String accountNumber, int sequence, int id) =>
      throw UnsupportedError('preview');

  @override
  Future<String> signFreeze(String words, String chainId, String accountNumber, int sequence) =>
      throw UnsupportedError('preview');

  @override
  Future<String> signFreezeFor(String words, String chainId, String accountNumber, int sequence, String account) =>
      throw UnsupportedError('preview');

  @override
  Future<String> signSetDevice(String words, String chainId, String accountNumber, int sequence, String device) =>
      throw UnsupportedError('preview');

  @override
  Future<String> signSetLimits(String words, String chainId, String accountNumber, int sequence, int dailyLimit, int approvalAbove) =>
      throw UnsupportedError('preview');

  @override
  Future<String> signApprove(String words, String chainId, String accountNumber, int sequence, int id) =>
      throw UnsupportedError('preview');

  @override
  Future<String> sign(String method, String words, String chainId, String accountNumber, int sequence,
          [Map<String, Object> extra = const {}]) =>
      throw UnsupportedError('preview');

  @override
  Future<String> signUnfreeze(String words, String chainId, String accountNumber, int sequence) =>
      throw UnsupportedError('preview');

  @override
  Future<String?> sealWords(String words, String password) async => null;

  @override
  Future<String?> openWords(String record, String password) async => null;
}

/// How NativeCore reaches the Go code.
abstract class CoreTransport {
  Future<T?> invokeMethod<T>(String method, [Map<String, Object?>? args]);
}

/// Android: the Go library inside the app (MainActivity.kt).
class ChannelTransport implements CoreTransport {
  const ChannelTransport();

  static const _channel = MethodChannel('encryptlock/core');

  @override
  Future<T?> invokeMethod<T>(String method, [Map<String, Object?>? args]) => _channel.invokeMethod<T>(method, args);
}

/// Computer: encryptlock-core.exe next to the app, one JSON line per call.
class ProcessTransport implements CoreTransport {
  Process? _process;
  int _next = 0;
  final _waiting = <int, Completer<Object?>>{};

  Future<Process> _start() async {
    if (_process != null) return _process!;
    final dir = File(Platform.resolvedExecutable).parent.path;
    final exe = Platform.isWindows ? 'encryptlock-core.exe' : 'encryptlock-core';
    final p = await Process.start('$dir${Platform.pathSeparator}$exe', const []);
    p.stdout.transform(utf8.decoder).transform(const LineSplitter()).listen((line) {
      final msg = jsonDecode(line) as Map<String, dynamic>;
      final c = _waiting.remove(msg['id']);
      if (c == null) return;
      if (msg['error'] != null) {
        c.completeError(PlatformException(code: 'core', message: msg['error'] as String));
      } else {
        c.complete(msg['result']);
      }
    });
    unawaited(p.stderr.drain<void>()); // only technical warnings
    unawaited(p.exitCode.then((_) => _process = null));
    return _process = p;
  }

  @override
  Future<T?> invokeMethod<T>(String method, [Map<String, Object?>? args]) async {
    final p = await _start();
    final id = ++_next;
    final c = Completer<Object?>();
    _waiting[id] = c;
    p.stdin.writeln(jsonEncode({'id': id, 'method': method, 'args': args ?? const {}}));
    final result = await c.future;
    // The core leaves out false and empty results.
    if (result == null && T == bool) return false as T;
    if (result == null && T == String) return '' as T;
    return result as T?;
  }
}
