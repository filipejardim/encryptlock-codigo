import 'package:flutter_secure_storage/flutter_secure_storage.dart';

/// Pauses password guessing (like Padlock): 5 wrong passwords → 2 minutes,
/// 10 → 4 minutes, 15 → 8 minutes, doubling each time. The count and the
/// pause are kept in secure storage, so closing the app or restarting the
/// phone does not reset them. Only the right password resets the count.
///
/// Moving the phone's clock back does not help: the pause starts again.
class AttemptGuard {
  AttemptGuard({FlutterSecureStorage? storage, DateTime Function()? clock, String prefix = 'encryptlock.attempts'})
      : _storage = storage ?? const FlutterSecureStorage(),
        _clock = clock ?? DateTime.now,
        _failuresKey = '$prefix.failures',
        _untilKey = '$prefix.until',
        _lastKey = '$prefix.last';

  final FlutterSecureStorage _storage;
  final DateTime Function() _clock;

  final String _failuresKey;
  final String _untilKey;
  final String _lastKey;

  static const attemptsPerPause = 5;
  static const firstPause = Duration(minutes: 2);

  Future<int> _int(String key) async => int.tryParse(await _storage.read(key: key) ?? '') ?? 0;

  /// Wrong passwords since the last right one.
  Future<int> failures() => _int(_failuresKey);

  /// How long the user must still wait; zero when they may try.
  Future<Duration> remaining() async {
    final now = _clock().millisecondsSinceEpoch;
    final until = await _int(_untilKey);
    if (until == 0) return Duration.zero;
    final last = await _int(_lastKey);
    if (now < last) {
      // The clock went back: start the pause again from now.
      final pause = _pauseFor(await failures());
      await _storage.write(key: _untilKey, value: '${now + pause.inMilliseconds}');
      await _storage.write(key: _lastKey, value: '$now');
      return pause;
    }
    return until > now ? Duration(milliseconds: until - now) : Duration.zero;
  }

  /// Wrong attempts left before the next pause.
  Future<int> attemptsLeft() async => attemptsPerPause - (await failures()) % attemptsPerPause;

  static Duration _pauseFor(int failures) {
    final round = failures ~/ attemptsPerPause;
    if (round == 0) return Duration.zero;
    // Capped at 2^20 × 2 minutes (about 4 years) to stay a sane number.
    return firstPause * (1 << (round - 1).clamp(0, 20));
  }

  /// Records a wrong password; returns the pause that starts now (or zero).
  Future<Duration> recordFailure() async {
    final count = await failures() + 1;
    final now = _clock().millisecondsSinceEpoch;
    await _storage.write(key: _failuresKey, value: '$count');
    await _storage.write(key: _lastKey, value: '$now');
    if (count % attemptsPerPause != 0) return Duration.zero;
    final pause = _pauseFor(count);
    await _storage.write(key: _untilKey, value: '${now + pause.inMilliseconds}');
    return pause;
  }

  /// The right password: everything starts over.
  Future<void> reset() async {
    await _storage.delete(key: _failuresKey);
    await _storage.delete(key: _untilKey);
    await _storage.delete(key: _lastKey);
  }
}
