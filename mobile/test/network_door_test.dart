import 'package:encryptlock_wallet/core/network.dart';
import 'package:flutter_test/flutter_test.dart';

/// The door published on the website goes into a command on the friend's
/// computer: only plain https addresses and plain host names are accepted.
void main() {
  test('network door from the website', () {
    final d = NetworkDoor.fromJson({
      'lockkeeper': 'https://a-b.trycloudflare.com',
      'rpc': 'https://c-d.trycloudflare.com',
      'p2p': 'e-f.trycloudflare.com',
    })!;
    expect(d.lockKeeper, 'https://a-b.trycloudflare.com');
    expect(d.rpc, 'https://c-d.trycloudflare.com');
    expect(d.p2pHost, 'e-f.trycloudflare.com');

    // Older website (no rpc/p2p): still a door for phones, not for joining.
    final old = NetworkDoor.fromJson({'lockkeeper': 'https://a-b.trycloudflare.com'})!;
    expect(old.rpc, isNull);
    expect(old.p2pHost, isNull);

    // Poisoned values are dropped.
    for (final bad in [
      'x.com --url 0.0.0.0:22',
      'x.com"; calc',
      'x.com&calc',
      '-hostname',
      '-x.com',
      'x-.com',
      'nodots',
      'X.COM',
      '',
    ]) {
      expect(NetworkDoor.fromJson({'lockkeeper': 'https://a.b', 'p2p': bad})!.p2pHost, isNull, reason: bad);
    }
    for (final bad in ['http://plain.text', 'https://x.com"\nunsafe = true', 'https://x.com/path', 'https://x.com:1/']) {
      expect(NetworkDoor.fromJson({'lockkeeper': 'https://a.b', 'rpc': bad})!.rpc, isNull, reason: bad);
    }
    expect(NetworkDoor.fromJson({'lockkeeper': 'http://a.b'}), isNull);
    expect(NetworkDoor.fromJson('nope'), isNull);
  });
}
