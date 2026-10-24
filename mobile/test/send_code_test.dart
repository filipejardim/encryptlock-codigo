import 'package:encryptlock_wallet/app_state.dart';
import 'package:encryptlock_wallet/core/core.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  setUp(() {
    FlutterSecureStorage.setMockInitialValues({});
    SharedPreferences.setMockInitialValues({});
  });

  test('send code: off by default, right and wrong codes, removal', () async {
    final state = AppState(core: PreviewCore());
    await state.load();
    expect(state.sendCodeOn, isFalse);
    expect(await state.checkSendCode('anything'), isTrue, reason: 'no code set: nothing to check');

    await state.setSendCode('2468');
    expect(state.sendCodeOn, isTrue);
    expect(await state.checkSendCode('1111'), isFalse);
    expect(await state.sendCodeGuard.failures(), 1);
    expect(await state.guard.failures(), 0, reason: 'separate from the password count');
    expect(await state.checkSendCode('2468'), isTrue);
    expect(await state.sendCodeGuard.failures(), 0);

    // Survives reopening the app.
    final reopened = AppState(core: PreviewCore());
    await reopened.load();
    expect(reopened.sendCodeOn, isTrue);

    await reopened.removeSendCode();
    expect(reopened.sendCodeOn, isFalse);
  });
}
