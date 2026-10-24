import 'package:encryptlock_wallet/core/updater.dart';
import 'package:encryptlock_wallet/core/version.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  const win = 'aa00000000000000000000000000000000000000000000000000000000000000';
  const apk = 'BB00000000000000000000000000000000000000000000000000000000000000';

  test('the website version file gives the version and both checksums', () {
    final u = UpdateInfo.fromJson({
      'version': '9.0.1',
      'windows': {'sha256': win},
      'android': {'sha256': apk},
    })!;
    expect(u.version, '9.0.1');
    expect(u.windowsSha256, win);
    expect(u.androidSha256, apk.toLowerCase());
    expect(isNewer(u.version, appVersion), isTrue);
    // Always the project's own downloads page, for that exact version.
    expect(u.fileUrl.toString(), startsWith('https://github.com/filipejardim/encryptlock-downloads/releases/download/v9.0.1/EncryptLock-'));
  });

  test('a broken or missing checksum is never accepted (no update without it)', () {
    final u = UpdateInfo.fromJson({
      'version': '1.0.2',
      'windows': {'sha256': 'not-a-hash'},
    })!;
    expect(u.windowsSha256, '');
    expect(u.androidSha256, '');
    expect(UpdateInfo.fromJson({'version': '1.0.2; rm -rf'}), isNull);
    expect(UpdateInfo.fromJson('nonsense'), isNull);
    // An older site file (only the version) still shows the new version.
    expect(UpdateInfo.fromJson({'version': '1.0.3'})!.version, '1.0.3');
  });

  test('a download without a published checksum is refused before it starts', () async {
    final u = UpdateInfo.fromJson({'version': '9.9.9'})!;
    await expectLater(Updater.download(u, (_) {}), throwsA(isA<UpdateFailed>()));
  });
}
