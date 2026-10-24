import 'package:encryptlock_wallet/core/attempt_guard.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  late DateTime now;
  late AttemptGuard guard;

  setUp(() {
    FlutterSecureStorage.setMockInitialValues({});
    now = DateTime(2026, 10, 5, 12);
    guard = AttemptGuard(clock: () => now);
  });

  Future<Duration> fail(int times) async {
    var pause = Duration.zero;
    for (var i = 0; i < times; i++) {
      pause = await guard.recordFailure();
    }
    return pause;
  }

  test('4 wrong passwords: no pause, 1 attempt left', () async {
    expect(await fail(4), Duration.zero);
    expect(await guard.remaining(), Duration.zero);
    expect(await guard.attemptsLeft(), 1);
  });

  test('5 → 2 min, 10 → 4 min, 15 → 8 min', () async {
    expect(await fail(5), const Duration(minutes: 2));
    expect(await guard.remaining(), const Duration(minutes: 2));
    now = now.add(const Duration(minutes: 2));
    expect(await guard.remaining(), Duration.zero);
    expect(await fail(5), const Duration(minutes: 4));
    now = now.add(const Duration(minutes: 4));
    expect(await fail(5), const Duration(minutes: 8));
  });

  test('a new guard (app reopened) still sees the pause', () async {
    await fail(5);
    final reopened = AttemptGuard(clock: () => now.add(const Duration(seconds: 30)));
    expect(await reopened.remaining(), const Duration(seconds: 90));
  });

  test('moving the clock back restarts the pause', () async {
    await fail(5);
    now = now.subtract(const Duration(days: 1));
    expect(await guard.remaining(), const Duration(minutes: 2));
  });

  test('the right password resets everything', () async {
    await fail(5);
    await guard.reset();
    expect(await guard.remaining(), Duration.zero);
    expect(await guard.failures(), 0);
    expect(await guard.attemptsLeft(), 5);
  });
}
