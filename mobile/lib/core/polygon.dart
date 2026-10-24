import 'dart:convert';
import 'dart:io';

import 'package:flutter/foundation.dart' show debugPrint, kIsWeb;

/// The Polygon network the wallet uses for USDC and POL: the test one in test
/// versions, the real one in the final app (fixed to the real network).
class PolygonNetwork {
  const PolygonNetwork(this.name, this.chainId, this.rpc, this.usdc, this.vault, [this.backups = const []]);

  final String name;
  final int chainId;
  final String rpc;

  /// Other public servers of the same network, asked in turn when [rpc]
  /// does not answer (down, busy or over its free limit).
  final List<String> backups;

  /// Circle's USDC contract on this network (6 decimals).
  final String usdc;

  /// The swap vault (HTLC) that holds the buyer's USDC for the seller; empty
  /// while it is not published on this network ("Buy" is then not possible).
  final String vault;

  static const amoy = PolygonNetwork(
    'Amoy',
    80002,
    'https://polygon-amoy-bor-rpc.publicnode.com',
    '0x41E94Eb019C0762f9Bfcf9Fb1E58725BfB0e7582',
    '', // published when the founder gets test POL
  );

  /// A Polygon test chain on the founder's PC (hardhat; polygon/scripts/
  /// setup-local.js), for test builds with --dart-define=POLYGON=local.
  static final local = PolygonNetwork(
    'Local',
    31337,
    !kIsWeb && Platform.isAndroid ? _onThePc : 'http://127.0.0.1:8545',
    '0x5FbDB2315678afecb367f032d93F642f64180aa3',
    '0xe7f1725E7734CE288F8367e1Bb143E90bb3F0512',
  );

  /// The real Polygon network (real money). Circle's native USDC, the one
  /// exchanges such as Binance send when the Polygon network is chosen.
  static const mainnet = PolygonNetwork(
    'Polygon',
    137,
    'https://polygon-bor-rpc.publicnode.com',
    '0x3c499c542cEF5E3811e1192ce70d8cC03d5c3359',
    '', // the vault is published there before launch
    ['https://polygon.gateway.tenderly.co', 'https://rpc-mainnet.matic.quiknode.pro'],
  );

  /// Phone: the PC of the built-in LockKeeper (same Wi-Fi); emulator: 10.0.2.2.
  static String get _onThePc {
    final pc = Uri.tryParse(const String.fromEnvironment('LOCKKEEPER'))?.host ?? '';
    return 'http://${pc.isEmpty ? '10.0.2.2' : pc}:8545';
  }

  static final _test = const String.fromEnvironment('POLYGON') == 'local' ? local : amoy;

  /// The network in use: the test network, or the real one when chosen in
  /// Settings → Advanced (AppState.setRealPolygon).
  static PolygonNetwork current = _test;

  static void useReal(bool real) => current = real ? mainnet : _test;
  static bool get isTest => current.chainId != 137;
}

/// Balances on Polygon, in the coins' smallest units.
class PolygonBalances {
  const PolygonBalances(this.pol, this.usdc);

  /// POL, 18 decimals (pays the network's "gas").
  final BigInt pol;

  /// USDC, 6 decimals.
  final BigInt usdc;
}

/// Reads public data from a Polygon node (JSON-RPC). Only public addresses
/// are ever sent; never a secret.
class PolygonClient {
  PolygonClient([PolygonNetwork? network]) : network = network ?? PolygonNetwork.current;

  final PolygonNetwork network;

  /// The server that answered last, per network: asked first next time.
  static final _working = <int, int>{};

  /// Asks the servers in turn until one answers. An error answered by the
  /// network itself (such as "nonce too low") is final; a server that is
  /// down, busy or over its free limit is skipped.
  Future<dynamic> _rpc(String method, List<Object> params) async {
    final servers = [network.rpc, ...network.backups];
    final first = _working[network.chainId] ?? 0;
    Object? last;
    for (var i = 0; i < servers.length; i++) {
      final at = (first + i) % servers.length;
      try {
        final result = await _rpcAt(servers[at], method, params);
        _working[network.chainId] = at;
        return result;
      } on PolygonError {
        rethrow;
      } catch (e) {
        last = e;
        debugPrint('polygon server ${servers[at]}: $e');
      }
    }
    throw last ?? const HttpException('Polygon timeout');
  }

  static Future<dynamic> _rpcAt(String url, String method, List<Object> params) async {
    final client = HttpClient()..connectionTimeout = const Duration(seconds: 10);
    try {
      final request = await client.postUrl(Uri.parse(url));
      request.headers.contentType = ContentType.json;
      request.write(jsonEncode({'jsonrpc': '2.0', 'id': 1, 'method': method, 'params': params}));
      final response = await request.close().timeout(const Duration(seconds: 15));
      final text = await response.transform(utf8.decoder).join().timeout(const Duration(seconds: 15));
      if (response.statusCode != 200) throw HttpException('HTTP ${response.statusCode}');
      final body = jsonDecode(text) as Map<String, dynamic>;
      final error = body['error'];
      if (error != null) {
        final message = '$error';
        // The server's own limits, not the network's answer: try another.
        if (RegExp(r'limit|rate|too many|archive|token|unauthori|capacity', caseSensitive: false).hasMatch(message)) {
          throw HttpException(message);
        }
        throw PolygonError(message);
      }
      return body['result'];
    } finally {
      client.close();
    }
  }

  static BigInt _hex(Object? v) {
    final s = '$v'.replaceFirst('0x', '');
    return s.isEmpty ? BigInt.zero : BigInt.parse(s, radix: 16);
  }

  Future<PolygonBalances> balances(String address) async {
    final pol = await _rpc('eth_getBalance', [address, 'latest']);
    // balanceOf(address): selector 70a08231 + the address padded to 32 bytes.
    final data = '0x70a08231${address.replaceFirst('0x', '').toLowerCase().padLeft(64, '0')}';
    final usdc = await _rpc('eth_call', [
      {'to': network.usdc, 'data': data},
      'latest',
    ]);
    return PolygonBalances(_hex(pol), _hex(usdc));
  }

  /// The wallet's next transaction number.
  Future<int> nonce(String address) async => _hex(await _rpc('eth_getTransactionCount', [address, 'pending'])).toInt();

  /// EIP-1559 fees in wei: the tip and the most the wallet pays per gas.
  Future<({BigInt tip, BigInt feeCap})> fees() async {
    final block = await _rpc('eth_getBlockByNumber', ['latest', false]) as Map<String, dynamic>;
    final base = _hex(block['baseFeePerGas']);
    var tip = _hex(await _rpc('eth_maxPriorityFeePerGas', []));
    // Polygon wants at least 25 gwei of tip on its main and test networks.
    final minTip = network.chainId == 31337 ? BigInt.from(1000000000) : BigInt.from(25000000000);
    if (tip < minTip) tip = minTip;
    return (tip: tip, feeCap: base * BigInt.two + tip);
  }

  /// Sends a signed transaction (0x…) and returns its hash.
  Future<String> send(String rawTx) async => '${await _rpc('eth_sendRawTransaction', [rawTx])}';

  /// How many transactions of [address] are already in blocks.
  Future<int> doneCount(String address) async => _hex(await _rpc('eth_getTransactionCount', [address, 'latest'])).toInt();

  /// Sends transaction number [nonce] of [from] once the network has all
  /// the earlier ones, and tries again a few times when a server turns it
  /// away: a public Polygon server can answer from a node still a block
  /// behind, which refuses a transaction that comes right after one it has
  /// not seen yet. Returns its hash, or null when the network already had
  /// it from an earlier try (then it is in once [doneCount] passes [nonce]).
  Future<String?> sendInOrder(String rawTx, String from, int nonce) async {
    Object? refusal;
    for (var i = 0; i < 12; i++) {
      if (i > 0) await Future<void>.delayed(const Duration(seconds: 3));
      try {
        if (await doneCount(from) < nonce) continue; // the earlier ones not seen yet
        return await send(rawTx);
      } catch (e) {
        final text = '$e'.toLowerCase();
        if (text.contains('already known') || text.contains('nonce too low')) return null;
        refusal = e;
        debugPrint('polygon send refused, trying again: $e');
      }
    }
    throw refusal ?? const HttpException('Polygon timeout');
  }

  /// Waits until transaction number [nonce] of [from] is in a block.
  Future<void> included(String from, int nonce, {Duration timeout = const Duration(minutes: 3)}) async {
    final end = DateTime.now().add(timeout);
    while (DateTime.now().isBefore(end)) {
      try {
        if (await doneCount(from) > nonce) return;
      } catch (_) {} // a server that did not answer: ask again
      await Future<void>.delayed(const Duration(seconds: 2));
    }
    throw const HttpException('Polygon timeout');
  }

  /// Waits until the transaction is in a block; true when it succeeded.
  Future<bool> confirmed(String hash, {Duration timeout = const Duration(minutes: 3)}) async {
    final end = DateTime.now().add(timeout);
    while (DateTime.now().isBefore(end)) {
      try {
        final r = await _rpc('eth_getTransactionReceipt', [hash]);
        if (r is Map<String, dynamic>) return r['status'] == '0x1';
      } catch (_) {} // a server that did not answer: ask again
      await Future<void>.delayed(const Duration(seconds: 2));
    }
    throw const HttpException('Polygon timeout');
  }
}

/// An error answered by the Polygon network itself (the same from any
/// server), such as "nonce too low" or "already known".
class PolygonError extends HttpException {
  const PolygonError(super.message);
}

enum BuyProblemKind { noUsdc, noPol, noPolAmount, notReady, wrongNetwork }

/// Why "Buy" cannot start (shown to the person; nothing was sent).
class BuyProblem implements Exception {
  const BuyProblem(this.kind);

  final BuyProblemKind kind;
}
