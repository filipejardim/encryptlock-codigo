import 'dart:convert';
import 'dart:io';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

import '../app_state.dart';
import '../core/network.dart';
import '../l10n/gen/app_localizations.dart';
import '../theme.dart';
import '../widgets/brand.dart';

/// The release of new LOCK (white paper, section 4): at genesis 1.000 to start
/// the LockKeepers and 400.000 (1%) to the creator; then 38.799.000 over 360 months of 30 days, each month a little less, halving
/// every 60 months; then the LockKeepers' everlasting reserve of 800.000, half
/// of what is left every 30 years (the last LOCK about 1.440 years after the
/// start). Same formula as the network (x/emission); the network's
/// table is exact, these figures are for reading.
class EmissionSchedule {
  static const months = 360;
  static const monthSeconds = 30 * 24 * 3600;
  static const startLock = 1000.0;
  static const founderLock = 400000.0;
  static const genesisLock = startLock + founderLock;
  static const releasedLock = 38799000.0;
  static const reserveLock = 800000.0;

  /// Every 30-year period of the reserve, shown one by one down to the last
  /// LOCK: 47 periods, the last ending 1.440 years after the start.
  static const reserveRows = 47;
  static const lastLockYears = 30 + reserveRows * 30;

  /// The reserve in lockunit (1 LOCK = 100.000.000), as the network counts it.
  static const reserveUnits = 80000000000000;

  /// Lockunit of the reserve released in its period k (0 = years 31–60):
  /// what is left halves (rounded down), exactly as on the network.
  static int reserveUnitsIn(int k) => (reserveUnits >> k) - (reserveUnits >> (k + 1));

  /// LOCK of the reserve released in its period k (0 = years 31–60).
  static double reservePeriod(int k) => reserveUnitsIn(k) / 1e8;
  static final double _r = math.pow(0.5, 1 / 60).toDouble();
  static final double _a = releasedLock * (1 - _r) / (1 - math.pow(_r, months));

  /// LOCK released in month n (1…360; after that, from the reserve).
  static double month(int n) =>
      n < 1 ? 0 : n <= months ? _a * math.pow(_r, n - 1) : reservePeriod((n - months - 1) ~/ months) / months;

  /// LOCK released in years from…to (inclusive, 1-based).
  static double years(int from, int to) {
    var sum = 0.0;
    for (var n = (from - 1) * 12 + 1; n <= to * 12; n++) {
      sum += month(n);
    }
    return sum;
  }
}

/// "Coin release": live numbers from the blockchain and the 30-year table.
class EmissionScreen extends StatefulWidget {
  const EmissionScreen({super.key});

  @override
  State<EmissionScreen> createState() => _EmissionScreenState();
}

class _EmissionScreenState extends State<EmissionScreen> {
  double? _supply;
  DateTime? _genesis;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
  }

  Future<Map<String, dynamic>> _get(String url) async {
    final client = HttpClient()..connectionTimeout = const Duration(seconds: 8);
    try {
      final res = await (await client.getUrl(Uri.parse(url))).close().timeout(const Duration(seconds: 10));
      return jsonDecode(await res.transform(utf8.decoder).join()) as Map<String, dynamic>;
    } finally {
      client.close();
    }
  }

  Future<void> _load() async {
    final base = AppScope.of(context).lockKeeperUrl;
    try {
      final supply = await _get('$base/cosmos/bank/v1beta1/supply/by_denom?denom=lockunit');
      final amount = double.parse('${(supply['amount'] as Map)['amount']}') / 1e8;
      // The first block is deleted after a while: the start date comes with
      // the network's address on the website; from block 1 while it exists.
      final time = await networkDoorFromSite().then((d) => d?.genesis).catchError((_) => null) ??
          DateTime.parse('${((await _get('$base/cosmos/base/tendermint/v1beta1/blocks/1'))['block'] as Map)['header']['time']}');
      if (mounted) {
        setState(() {
          _supply = amount;
          _genesis = time;
        });
      }
    } catch (_) {
      // No network: the table below is still shown.
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final locale = Localizations.localeOf(context).toString();
    final n = NumberFormat.decimalPattern(locale);
    String lock(double v) => n.format(v.round());
    // Small amounts of the reserve keep their decimals (down to 0,00000001).
    final tiny = NumberFormat('#,##0.########', locale);
    String smallLock(double v) => v >= 100 ? lock(v) : tiny.format(v);
    final date = DateFormat.yMMMMd(locale);
    final supply = _supply;
    final genesis = _genesis;
    // Year of the last LOCK: from the real start when known (2026 until then).
    final lastYear = (genesis?.year ?? 2026) + EmissionSchedule.lastLockYears;

    final live = <Widget>[];
    if (supply != null && genesis != null) {
      final elapsed = DateTime.now().difference(genesis).inSeconds;
      final monthNow = elapsed ~/ EmissionSchedule.monthSeconds + 1;
      final perMonth = EmissionSchedule.month(monthNow);
      final nextHalf = genesis.add(Duration(seconds: ((monthNow - 1) ~/ 60 + 1) * 60 * EmissionSchedule.monthSeconds));
      final end = genesis.add(const Duration(seconds: EmissionSchedule.months * EmissionSchedule.monthSeconds));
      live.addAll([
        _row('💎', t.emExisting, '${lock(supply)} LOCK'),
        _row('⏳', t.emRemaining, '${lock(math.max(0, 40000000 - supply))} LOCK'),
        _row('⛏️', t.emNow, t.emRate(lock(perMonth / 720), lock(perMonth / 30), lock(perMonth * 7 / 30))),
        if (monthNow <= EmissionSchedule.months) _row('📉', t.emNextHalf, date.format(nextHalf)),
        if (monthNow <= EmissionSchedule.months) _row('🏁', t.emEnd, date.format(end)),
        _row('♾️', t.emLastLock, '~ $lastYear'),
      ]);
    }
    return Scaffold(
      appBar: BrandBar(title: t.emTitle),
      body: ChipBackground(
        child: Align(
          alignment: Alignment.topCenter,
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 820),
            child: ListView(
              padding: const EdgeInsets.all(20),
              children: [
                Text(
                  t.emIntro,
                  textAlign: TextAlign.center,
                  style: const TextStyle(fontSize: 16, height: 1.45, color: Brand.pearl),
                ),
                const SizedBox(height: 16),
                if (live.isNotEmpty) NeonCard(child: Column(children: live)),
                const SizedBox(height: 16),
                NeonCard(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      Text(t.emPeriods, style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w700, color: Brand.neonGreen)),
                      const SizedBox(height: 8),
                      _row('🌱', t.emStart, '${lock(EmissionSchedule.startLock)} LOCK'),
                      _row('🛠️', t.emFounder, '${lock(EmissionSchedule.founderLock)} LOCK'),
                      for (var p = 0; p < 6; p++)
                        _row('📅', t.emYears('${p * 5 + 1}', '${p * 5 + 5}'), '${lock(EmissionSchedule.years(p * 5 + 1, p * 5 + 5))} LOCK'),
                      const Divider(color: Brand.cardBorder),
                      const SizedBox(height: 4),
                      Text(
                        t.emReserveTitle,
                        style: const TextStyle(fontWeight: FontWeight.w700, color: Brand.neonGreen, height: 1.35),
                      ),
                      const SizedBox(height: 4),
                      // Every period down to the last LOCK (the last one says the year).
                      for (var k = 0; k < EmissionSchedule.reserveRows; k++)
                        _row(
                          k == EmissionSchedule.reserveRows - 1 ? '🏁' : '♾️',
                          k == EmissionSchedule.reserveRows - 1
                              ? t.emReserveRest(n.format(31 + k * 30), n.format(60 + k * 30), '$lastYear')
                              : t.emYears(n.format(31 + k * 30), n.format(60 + k * 30)),
                          '${smallLock(EmissionSchedule.reservePeriod(k))} LOCK',
                        ),
                      const Divider(color: Brand.cardBorder),
                      _row('🔒', t.emMax, '${n.format(40000000)} LOCK'),
                    ],
                  ),
                ),
                const SizedBox(height: 12),
                Text(t.emNote, style: const TextStyle(color: Brand.pearl, height: 1.45)),
                const SizedBox(height: 8),
                Text(t.emReserveNote('$lastYear'), style: const TextStyle(color: Brand.pearl, height: 1.45)),
              ],
            ),
          ),
        ),
      ),
    );
  }

  Widget _row(String icon, String label, String value) => Padding(
    padding: const EdgeInsets.symmetric(vertical: 6),
    child: Row(
      children: [
        SizedBox(width: 30, child: Text(icon, style: const TextStyle(fontSize: 18))),
        Expanded(child: Text(label)),
        const SizedBox(width: 10),
        Text(value, style: const TextStyle(fontWeight: FontWeight.w700)),
      ],
    ),
  );
}
