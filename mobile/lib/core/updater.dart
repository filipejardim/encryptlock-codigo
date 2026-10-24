import 'dart:convert';
import 'dart:io';

import 'package:cryptography/cryptography.dart';
import 'package:flutter/services.dart';

import 'version.dart';

/// "Update" inside the app, for apps installed from the website: the newest
/// version is downloaded from the public downloads page on GitHub and only
/// installed if its SHA-256 is the one the website publishes (versao.json),
/// so a file changed on the way is never installed. Apps installed from a
/// store are updated by the store.
class UpdateInfo {
  const UpdateInfo(this.version, this.windowsSha256, this.androidSha256);

  final String version;
  final String windowsSha256;
  final String androidSha256;

  /// The website's versao.json: {"version": "1.0.1", "windows": {"sha256":
  /// "…"}, "android": {"sha256": "…"}}. Null if it is not valid.
  static UpdateInfo? fromJson(Object? j) {
    if (j is! Map) return null;
    final v = j['version'];
    if (v is! String || !RegExp(r'^\d+(\.\d+){0,3}$').hasMatch(v)) return null;
    String sha(Object? o) {
      final s = o is Map ? '${o['sha256'] ?? ''}'.toLowerCase() : '';
      return RegExp(r'^[0-9a-f]{64}$').hasMatch(s) ? s : '';
    }

    return UpdateInfo(v, sha(j['windows']), sha(j['android']));
  }

  /// The file of this version for this computer or phone (fixed address:
  /// only the project's own downloads page).
  Uri get fileUrl => Uri.parse(
    'https://github.com/filipejardim/encryptlock-downloads/releases/download/v$version/'
    '${Platform.isAndroid ? 'EncryptLock-Android.apk' : 'EncryptLock-Windows.exe'}',
  );

  String get sha256 => Platform.isAndroid ? androidSha256 : windowsSha256;
}

/// The newest version on the website, or null (offline, not valid).
Future<UpdateInfo?> latestUpdate() async {
  final client = HttpClient()..connectionTimeout = const Duration(seconds: 5);
  try {
    final req = await client.getUrl(versionUrl);
    final res = await req.close().timeout(const Duration(seconds: 10));
    if (res.statusCode != 200) return null;
    return UpdateInfo.fromJson(jsonDecode(await res.transform(utf8.decoder).join()));
  } catch (_) {
    return null;
  } finally {
    client.close();
  }
}

/// The update did not happen (nothing was installed).
class UpdateFailed implements Exception {
  const UpdateFailed(this.why);
  final String why;
  @override
  String toString() => 'update failed: $why';
}

class Updater {
  static const _channel = MethodChannel('encryptlock/update');

  /// This app can update itself here: the Windows app installed by the
  /// installer (in the user's Programs folder), or an Android app that did
  /// not come from Google Play. Elsewhere (store, a copy in another folder)
  /// the website or the store is opened instead.
  static Future<bool> canUpdateItself() async {
    if (Platform.isWindows) {
      final programs = '${Platform.environment['LOCALAPPDATA'] ?? ''}\\Programs\\EncryptLock\\'.toLowerCase();
      return Platform.resolvedExecutable.toLowerCase().startsWith(programs);
    }
    if (Platform.isAndroid) {
      try {
        return await _channel.invokeMethod<String>('installer') != 'com.android.vending';
      } catch (_) {
        return false;
      }
    }
    return false;
  }

  /// Downloads the new version and checks it; [progress] gets 0..1.
  static Future<File> download(UpdateInfo info, void Function(double) progress) async {
    if (info.sha256.isEmpty) throw const UpdateFailed('no checksum published');
    final dir = Platform.isAndroid
        ? Directory(await _channel.invokeMethod<String>('cacheDir') ?? Directory.systemTemp.path)
        : Directory.systemTemp;
    final file = File('${dir.path}${Platform.pathSeparator}${info.fileUrl.pathSegments.last}');
    final client = HttpClient()..connectionTimeout = const Duration(seconds: 15);
    try {
      final req = await client.getUrl(info.fileUrl);
      final res = await req.close();
      if (res.statusCode != 200) throw UpdateFailed('HTTP ${res.statusCode}');
      final total = res.contentLength;
      final sink = Sha256().newHashSink();
      final out = file.openWrite();
      var got = 0;
      await for (final chunk in res) {
        sink.add(chunk);
        out.add(chunk);
        got += chunk.length;
        if (total > 0) progress(got / total);
      }
      await out.close();
      sink.close();
      final hex = (await sink.hash()).bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
      if (hex != info.sha256) {
        await file.delete();
        throw const UpdateFailed('the file is not the published one');
      }
      return file;
    } finally {
      client.close();
    }
  }

  /// Installs a checked file. Windows: the installer runs without questions,
  /// closes this app and opens the new one (the LockKeeper switched on comes
  /// back by itself). Android: the phone asks "Install this update?".
  static Future<void> install(File file) async {
    if (Platform.isWindows) {
      await Process.start(file.path, ['/SILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/CLOSEAPPLICATIONS'],
          mode: ProcessStartMode.detached);
      exit(0);
    }
    await _channel.invokeMethod('install', {'path': file.path});
  }
}
