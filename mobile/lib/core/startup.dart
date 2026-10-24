import 'dart:io';

import 'package:flutter/services.dart' show MethodChannel;

/// "Start with Windows": a line in the user's Run key of the registry (no
/// administrator needed). Removing the line turns it off.
///
/// From the Microsoft Store package (MSIX) the app's path changes with every
/// version and a Run line cannot start it: the package's startup task
/// "AppStart" does (windows/runner/startup_task.cpp).
class WindowsStartup {
  static const _key = r'HKCU\Software\Microsoft\Windows\CurrentVersion\Run';
  static const _name = 'EncryptLock';
  static const _startup = MethodChannel('encryptlock/startup');

  /// The package's task state, or null outside the package.
  static Future<String?> _task(String method) async {
    try {
      final s = await _startup.invokeMethod<String>(method, {'task': 'app'});
      return s == null || s == 'unpackaged' || s == 'error' ? null : s;
    } catch (_) {
      return null; // no channel (tests)
    }
  }

  static Future<bool> isOn() async {
    if (!Platform.isWindows) return false;
    final task = await _task('state');
    if (task != null) return task.startsWith('enabled');
    final r = await Process.run('reg', ['query', _key, '/v', _name]);
    return r.exitCode == 0;
  }

  static Future<void> set(bool on) async {
    if (!Platform.isWindows) return;
    if (await _task(on ? 'enable' : 'disable') != null) return;
    if (on) {
      await Process.run('reg', ['add', _key, '/v', _name, '/t', 'REG_SZ', '/d', '"${Platform.resolvedExecutable}"', '/f']);
    } else {
      await Process.run('reg', ['delete', _key, '/v', _name, '/f']);
    }
  }
}
