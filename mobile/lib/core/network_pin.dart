import 'dart:convert';

import 'package:cryptography/cryptography.dart';

/// The real network, fixed inside the final app (set when it is built:
/// --dart-define=CHAIN_ID=... --dart-define=GENESIS_HASH=...). Empty in test
/// versions, which follow whatever network their LockKeeper is on.
///
/// With them set, a LockKeeper (or the address on the website) that points to
/// another network is refused: the app never shows its numbers, never signs
/// for it, and a computer never joins it as a LockKeeper.
class NetworkPin {
  static const chainId = String.fromEnvironment('CHAIN_ID');
  static const genesisHash = String.fromEnvironment('GENESIS_HASH');

  /// Throws [WrongNetwork] if a network name is not the real network's.
  static void checkChainId(String network) {
    if (chainId.isNotEmpty && network != chainId) throw WrongNetwork(network);
  }

  /// Throws [WrongNetwork] if a genesis (block 1) is not the real network's.
  static Future<void> checkGenesis(Object? genesis) async {
    if (genesisHash.isEmpty) return;
    final h = await hashGenesis(genesis);
    if (h != genesisHash.toLowerCase()) throw WrongNetwork('genesis $h');
  }

  /// SHA-256 (hex) of the genesis written with its keys in order and no
  /// spaces, so the same genesis always gives the same code whoever sends it.
  static Future<String> hashGenesis(Object? genesis) async {
    final bytes = utf8.encode(jsonEncode(_sorted(genesis)));
    final hash = await Sha256().hash(bytes);
    return hash.bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
  }

  static Object? _sorted(Object? v) {
    if (v is Map) {
      final keys = v.keys.map((k) => '$k').toList()..sort();
      return {for (final k in keys) k: _sorted(v[k])};
    }
    if (v is List) return [for (final x in v) _sorted(x)];
    return v;
  }
}

/// A LockKeeper of another network than the one fixed in this app.
class WrongNetwork implements Exception {
  const WrongNetwork(this.what);
  final String what;
  @override
  String toString() => 'not the EncryptLock network ($what)';
}
