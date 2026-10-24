import 'package:encryptlock_wallet/core/core.dart';
import 'package:encryptlock_wallet/core/wallet_store.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';

/// Stands in for the Go core + security chip: "hw1:" records, password checked.
class FakeCore extends PreviewCore {
  FakeCore({required this.preview});

  final bool preview;
  int seals = 0;

  @override
  bool get isPreview => preview;

  @override
  Future<String?> sealWords(String words, String password) async {
    if (preview) return null;
    seals++;
    return 'hw1:$password|$words';
  }

  @override
  Future<String?> openWords(String record, String password) async {
    final parts = record.substring(4).split('|');
    return parts[0] == password ? parts[1] : null;
  }
}

void main() {
  const words = 'tide quarter nerve olympic';

  setUp(() => FlutterSecureStorage.setMockInitialValues({}));

  test('new wallets use the security-chip format', () async {
    final store = WalletStore(core: FakeCore(preview: false));
    await store.save(words, 'pass1234');
    expect(await const FlutterSecureStorage().read(key: 'encryptlock.wallet.v1'), startsWith('hw1:'));
    expect(await store.unlock('pass1234'), words);
    expect(await store.unlock('wrong'), isNull);
  });

  test('a wallet from the first test build is upgraded at the right password', () async {
    // Format 1 written as the first build did (preview path = Dart PBKDF2).
    await WalletStore(core: FakeCore(preview: true)).save(words, 'pass1234');
    final old = await const FlutterSecureStorage().read(key: 'encryptlock.wallet.v1');
    expect(old, startsWith('{'));

    final core = FakeCore(preview: false);
    final store = WalletStore(core: core);
    expect(await store.unlock('wrong'), isNull);
    expect(core.seals, 0, reason: 'wrong password must not touch the wallet');
    expect(await store.unlock('pass1234'), words);
    expect(core.seals, 1);
    expect(await const FlutterSecureStorage().read(key: 'encryptlock.wallet.v1'), startsWith('hw1:'));
    expect(await store.unlock('pass1234'), words);
  });
}
