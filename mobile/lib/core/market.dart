import 'dart:convert';
import 'dart:io';

import 'last_answers.dart';
import 'network.dart';

/// One offer on the market (LOCK ↔ USDC on Polygon).
class MarketOffer {
  MarketOffer.fromJson(Map<String, dynamic> j)
    : id = int.tryParse('${j['id'] ?? 0}') ?? 0,
      maker = '${j['maker'] ?? ''}',
      polygon = '${j['polygon'] ?? ''}',
      sell = '${j['side']}' == 'SIDE_SELL' || '${j['side']}' == '1',
      lock = BigInt.tryParse('${j['lock'] ?? 0}') ?? BigInt.zero,
      usdc = BigInt.tryParse('${j['usdc'] ?? 0}') ?? BigInt.zero,
      expiresAt = int.tryParse('${j['expires_at'] ?? 0}') ?? 0,
      tradeId = int.tryParse('${j['trade_id'] ?? 0}') ?? 0,
      awaitingApproval = j['awaiting_approval'] == true;

  final int id;
  final String maker;

  /// The maker's Polygon address (a seller receives the USDC there).
  final String polygon;

  /// True: the maker sells LOCK. False: the maker buys LOCK.
  final bool sell;

  /// LOCK in lockunit; USDC in units of 6 decimals.
  final BigInt lock;
  final BigInt usdc;
  final int expiresAt;

  /// Reserved for a trade in progress (0 = open).
  final int tradeId;

  /// A sell offer of an account with a second device, waiting for it.
  final bool awaitingApproval;

  /// Dollars per LOCK.
  double get price => usdcToDouble(usdc) / lockToDouble(lock);
}

/// A trade in progress: the LOCK wait in the market until the USDC are paid
/// on Polygon (or go back when the time is over).
class MarketTrade {
  MarketTrade.fromJson(Map<String, dynamic> j)
    : id = int.tryParse('${j['id'] ?? 0}') ?? 0,
      offerId = int.tryParse('${j['offer_id'] ?? 0}') ?? 0,
      seller = '${j['seller'] ?? ''}',
      buyer = '${j['buyer'] ?? ''}',
      sellerPolygon = '${j['seller_polygon'] ?? ''}',
      lock = BigInt.tryParse('${j['lock'] ?? 0}') ?? BigInt.zero,
      usdc = BigInt.tryParse('${j['usdc'] ?? 0}') ?? BigInt.zero,
      hash = base64Decode('${j['hash'] ?? ''}').map((b) => b.toRadixString(16).padLeft(2, '0')).join(),
      payBy = int.tryParse('${j['pay_by'] ?? 0}') ?? 0;

  final int id;
  final int offerId;
  final String seller;
  final String buyer;
  final String sellerPolygon;
  final BigInt lock;
  final BigInt usdc;

  /// The hash of the buyer's secret, in hex.
  final String hash;

  /// The USDC must be locked on Polygon before this time (Unix seconds).
  final int payBy;
}

/// A finished trade (counts for the price).
class MarketFill {
  MarketFill.fromJson(Map<String, dynamic> j)
    : lock = BigInt.tryParse('${j['lock'] ?? 0}') ?? BigInt.zero,
      usdc = BigInt.tryParse('${j['usdc'] ?? 0}') ?? BigInt.zero,
      time = int.tryParse('${j['time'] ?? 0}') ?? 0;

  final BigInt lock;
  final BigInt usdc;
  final int time;

  double get price => usdcToDouble(usdc) / lockToDouble(lock);
}

/// The price of LOCK: the last trade and the last 24 hours.
class MarketPrice {
  const MarketPrice(this.last, this.dayAverage, this.recent, {this.dayMedianUnits = 0});

  /// Null = no trade yet ("no price yet").
  final MarketFill? last;
  final double? dayAverage;

  /// The 24-hour median weighed by volume, in billionths of a dollar per LOCK
  /// (0 = no trades in 24 hours), as the network gives it.
  final int dayMedianUnits;

  double? get dayMedian => dayMedianUnits == 0 ? null : dayMedianUnits / 1e9;

  /// The price that values LOCK, as the network computes it (billionths of a
  /// dollar per LOCK; 0 before the first trade): the 24-hour median, or the
  /// last trade when there was none in 24 hours. Many tiny trades between
  /// two accounts of the same person at a made-up price barely move it.
  BigInt get referenceUnits {
    if (dayMedianUnits != 0) return BigInt.from(dayMedianUnits);
    final l = last;
    if (l == null || l.lock == BigInt.zero) return BigInt.zero;
    return l.usdc * BigInt.from(100000000000) ~/ l.lock;
  }

  double? get reference => referenceUnits == BigInt.zero ? null : referenceUnits.toDouble() / 1e9;

  /// Newest first.
  final List<MarketFill> recent;
}

/// A trade has the LOCK, USDC and seller's Polygon address of the sell offer
/// it came from (the network copies them; anything else is not paid).
bool sameTerms(MarketOffer offer, MarketTrade trade) =>
    trade.offerId == offer.id &&
    trade.lock == offer.lock &&
    trade.usdc == offer.usdc &&
    trade.sellerPolygon.toLowerCase() == offer.polygon.toLowerCase();

double lockToDouble(BigInt units) => units.toDouble() / LockKeeperClient.unitsPerLock.toDouble();
double usdcToDouble(BigInt units) => units.toDouble() / 1e6;

extension MarketQueries on LockKeeperClient {
  Future<Map<String, dynamic>> _json(String path) async {
    final client = HttpClient()..connectionTimeout = const Duration(seconds: 8);
    try {
      final req = await client.getUrl(Uri.parse('$baseUrl$path')).timeout(const Duration(seconds: 10));
      final res = await req.close().timeout(const Duration(seconds: 10));
      if (res.statusCode != 200) throw HttpException('HTTP ${res.statusCode}');
      final json = jsonDecode(await res.transform(utf8.decoder).join()) as Map<String, dynamic>;
      // Public numbers, kept for when no LockKeeper can be reached.
      // (Only the newest candles: older pages are asked for again.)
      if (_kept.any(path.startsWith) && !path.contains('before=')) await LastAnswers.put(path, json);
      return json;
    } finally {
      client.close();
    }
  }

  static const _kept = ['/encryptlock/market/v1/offers', '/encryptlock/market/v1/price', '/encryptlock/market/v1/candles'];

  Future<List<MarketOffer>> offers() async => parseOffers(await _json('/encryptlock/market/v1/offers'));

  /// The trades in progress of an account: (trade id, offer id, hash in
  /// hex, pay-by time).
  /// Where this network watches the USDC payments (null: trades not
  /// possible yet). The app pays exactly there.
  Future<({int chainId, String vault, String usdc})?> polygonConfig() async {
    final res = await _json('/encryptlock/market/v1/polygon');
    final chainId = int.tryParse('${res['chain_id'] ?? 0}') ?? 0;
    final vault = '${res['vault'] ?? ''}';
    if (chainId == 0 || vault.isEmpty) return null;
    return (chainId: chainId, vault: vault, usdc: '${res['usdc'] ?? ''}');
  }

  Future<List<MarketTrade>> trades(String address) async {
    final res = await _json('/encryptlock/market/v1/trades/$address');
    return [for (final t in (res['trades'] as List<dynamic>? ?? const [])) MarketTrade.fromJson(t as Map<String, dynamic>)];
  }

  Future<MarketPrice> price() async => parsePrice(await _json('/encryptlock/market/v1/price'));
}

/// The last offers and price the blockchain gave (null: none kept).
Future<List<MarketOffer>?> lastOffers() async {
  final j = await LastAnswers.get('/encryptlock/market/v1/offers');
  return j == null ? null : parseOffers(j);
}

Future<MarketPrice?> lastPrice() async {
  final j = await LastAnswers.get('/encryptlock/market/v1/price');
  return j == null ? null : parsePrice(j);
}

List<MarketOffer> parseOffers(Map<String, dynamic> res) => [
  for (final o in (res['offers'] as List<dynamic>? ?? const [])) MarketOffer.fromJson(o as Map<String, dynamic>),
];

MarketPrice parsePrice(Map<String, dynamic> res) {
    final last = res['last'] == null ? null : MarketFill.fromJson(res['last'] as Map<String, dynamic>);
    final dayLock = BigInt.tryParse('${res['day_lock'] ?? 0}') ?? BigInt.zero;
    final dayUsdc = BigInt.tryParse('${res['day_usdc'] ?? 0}') ?? BigInt.zero;
    return MarketPrice(
      last,
      dayLock == BigInt.zero ? null : usdcToDouble(dayUsdc) / lockToDouble(dayLock),
      [for (final f in (res['recent'] as List<dynamic>? ?? const [])) MarketFill.fromJson(f as Map<String, dynamic>)],
      dayMedianUnits: int.tryParse('${res['day_median'] ?? 0}') ?? 0,
    );
}

/// Dollar → euro, from the European Central Bank's daily reference rates
/// (public and free); when that site does not answer (21 Oct 2026: a
/// LockKeeper's computer never reached it, so no € value), from Coinbase,
/// else from Frankfurter (the same ECB rates, another host). Null when none
/// answers.
Future<double?> usdPerEuro({String ecb = 'https://www.ecb.europa.eu/stats/eurofxref/eurofxref-daily.xml', String coinbase = 'https://api.coinbase.com/v2/exchange-rates?currency=EUR'}) async {
  final client = HttpClient()..connectionTimeout = const Duration(seconds: 8);
  Future<String?> get(String url) async {
    try {
      final req = await client.getUrl(Uri.parse(url));
      final res = await req.close().timeout(const Duration(seconds: 10));
      if (res.statusCode != 200) return null;
      return await res.transform(utf8.decoder).join();
    } catch (_) {
      return null;
    }
  }

  double? valid(Object? v) {
    final r = double.tryParse('$v');
    return r == null || r <= 0 ? null : r;
  }

  Object? field(String? json, List<String> path) {
    if (json == null) return null;
    try {
      Object? v = jsonDecode(json);
      for (final k in path) {
        v = (v as Map?)?[k];
      }
      return v;
    } catch (_) {
      return null;
    }
  }

  try {
    final xml = await get(ecb);
    final m = xml == null ? null : RegExp(r"currency='USD' rate='([0-9.]+)'").firstMatch(xml);
    return valid(m?.group(1)) ??
        valid(field(await get(coinbase), ['data', 'rates', 'USD'])) ??
        valid(field(await get('https://api.frankfurter.dev/v1/latest?base=EUR&symbols=USD'), ['rates', 'USD']));
  } finally {
    client.close();
  }
}

/// Dollars per POL, from public exchange prices (Coinbase, else CoinGecko;
/// free, no account). Null when neither answers.
Future<double?> usdPerPol() async {
  final client = HttpClient()..connectionTimeout = const Duration(seconds: 8);
  Future<Object?> get(String url) async {
    try {
      final req = await client.getUrl(Uri.parse(url));
      final res = await req.close().timeout(const Duration(seconds: 10));
      if (res.statusCode != 200) return null;
      return jsonDecode(await res.transform(utf8.decoder).join());
    } catch (_) {
      return null;
    }
  }

  try {
    final coinbase = await get('https://api.coinbase.com/v2/prices/POL-USD/spot');
    var p = double.tryParse('${((coinbase as Map?)?['data'] as Map?)?['amount']}');
    if (p == null || p <= 0) {
      final gecko = await get('https://api.coingecko.com/api/v3/simple/price?ids=polygon-ecosystem-token&vs_currencies=usd');
      p = double.tryParse('${((gecko as Map?)?['polygon-ecosystem-token'] as Map?)?['usd']}');
    }
    return p == null || p <= 0 ? null : p;
  } finally {
    client.close();
  }
}

/// One candle of the price chart (prices in dollars per LOCK).
class Candle {
  const Candle(this.time, this.open, this.high, this.low, this.close, this.lock);

  Candle.fromJson(Map<String, dynamic> j)
      : time = int.tryParse('${j['time'] ?? 0}') ?? 0,
        open = _e9(j['open']),
        high = _e9(j['high']),
        low = _e9(j['low']),
        close = _e9(j['close']),
        lock = lockToDouble(BigInt.tryParse('${j['lock'] ?? 0}') ?? BigInt.zero);

  static double _e9(Object? v) => (BigInt.tryParse('${v ?? 0}') ?? BigInt.zero).toDouble() / 1e9;

  /// Start of the candle (Unix seconds).
  final int time;
  final double open, high, low, close;

  /// LOCK traded in it (0 = no trade: a flat candle that keeps the last price).
  final double lock;
}

extension CandleQueries on LockKeeperClient {
  /// The newest [limit] candles of [interval] seconds, oldest first; with
  /// [before], the ones that start before that time (older history).
  Future<List<Candle>> candles(int interval, {int limit = 500, int before = 0}) async => parseCandles(
    await _json('/encryptlock/market/v1/candles?interval=$interval&limit=$limit${before > 0 ? '&before=$before' : ''}'),
  );
}

List<Candle> parseCandles(Map<String, dynamic> res) =>
    [for (final c in (res['candles'] as List<dynamic>? ?? const [])) Candle.fromJson(c as Map<String, dynamic>)];

/// The last candles of a time frame the blockchain gave (null: none kept).
Future<List<Candle>?> lastCandles(int interval, {int limit = 500}) async {
  final j = await LastAnswers.get('/encryptlock/market/v1/candles?interval=$interval&limit=$limit');
  return j == null ? null : parseCandles(j);
}
