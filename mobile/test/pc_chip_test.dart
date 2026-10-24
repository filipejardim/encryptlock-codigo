import 'package:encryptlock_wallet/core/pc_lockkeeper.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('only plain addresses go into the administrator command', () {
    expect(PcLockKeeper.isSafeChecker('http://127.0.0.1:1317'), isTrue);
    expect(PcLockKeeper.isSafeChecker('https://abc-def.trycloudflare.com'), isTrue);
    expect(PcLockKeeper.isSafeChecker("http://x.com';calc;'"), isFalse);
    expect(PcLockKeeper.isSafeChecker(r'http://x.com/$(calc)'), isFalse);
    expect(PcLockKeeper.isSafeChecker('http://x.com" -c calc'), isFalse);
    expect(PcLockKeeper.isSafeChecker('file:///c:/x'), isFalse);
  });
}
