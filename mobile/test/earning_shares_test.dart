import 'package:encryptlock_wallet/core/network.dart';
import 'package:flutter_test/flutter_test.dart';

KeeperInfo keeper(double lock, {bool off = false}) =>
    KeeperInfo('pc', BigInt.from((lock * 1e8).round()), off);

void main() {
  test('below 20 LockKeepers: by guarantee, nothing for one that is off', () {
    // The network on 21 Oct 2026: founder 200 LOCK, others 5 and 3,33.
    final s = earningShares([keeper(200), keeper(5), keeper(3.33), keeper(3.33), keeper(3.33, off: true)]);
    expect(s[0], closeTo(200 / 211.66, 1e-9));
    expect(s[4], 0);
    expect(s.reduce((a, b) => a + b), closeTo(1, 1e-9));
  });

  test('20 on: the founder gets 5% like everyone, not 76%', () {
    final s = earningShares([keeper(200), for (var i = 0; i < 19; i++) keeper(3.33)]);
    for (final x in s) {
      expect(x, closeTo(0.05, 1e-6));
    }
  });

  test('19 on and one off: no cap yet', () {
    final s = earningShares([keeper(200), for (var i = 0; i < 18; i++) keeper(3.33), keeper(3.33, off: true)]);
    expect(s[0], closeTo(200 / (200 + 18 * 3.33), 1e-6));
  });

  test('cap: a few big ones capped, the small keep their guarantee', () {
    // Same numbers as x/powercap: the largest at C, C = 5% of the new total.
    final s = earningShares([keeper(100), keeper(100), for (var i = 0; i < 20; i++) keeper(1)]);
    expect(s[0], lessThanOrEqualTo(0.05 + 1e-9));
    expect(s[0], s[1]);
    expect(s[2], greaterThan(0));
    expect(s.reduce((a, b) => a + b), closeTo(1, 1e-9));
  });

  test('nobody on: all 0', () {
    expect(earningShares([keeper(5, off: true)]), [0.0]);
  });
}
