import 'dart:convert';

import 'package:shared_preferences/shared_preferences.dart';

/// The last public answers of the blockchain (price, offers, chart, the
/// LockKeepers), kept on the device: while no LockKeeper can be reached the
/// app keeps showing them, with the time they are from. Public data only.
class LastAnswers {
  LastAnswers._();

  static const _prefix = 'encryptlock.answer.';

  static final Map<String, Map<String, dynamic>> _memory = {};

  static Future<void> put(String key, Map<String, dynamic> json) async {
    _memory[key] = json;
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString('$_prefix$key', jsonEncode(json));
  }

  static Future<Map<String, dynamic>?> get(String key) async {
    final m = _memory[key];
    if (m != null) return m;
    final raw = (await SharedPreferences.getInstance()).getString('$_prefix$key');
    if (raw == null) return null;
    try {
      return _memory[key] = jsonDecode(raw) as Map<String, dynamic>;
    } catch (_) {
      return null;
    }
  }

  /// Another network was chosen: its numbers are not these.
  static Future<void> clear() async {
    _memory.clear();
    final prefs = await SharedPreferences.getInstance();
    for (final k in prefs.getKeys().where((k) => k.startsWith(_prefix)).toList()) {
      await prefs.remove(k);
    }
  }
}
