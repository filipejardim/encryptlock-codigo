import 'dart:async';

import 'package:flutter/material.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../app_state.dart';
import '../core/market.dart';
import '../core/network.dart';
import '../l10n/gen/app_localizations.dart';
import '../theme.dart';
import '../widgets/brand.dart';
import '../widgets/price_chart.dart';
import 'market.dart' show formatUsd;

/// Last time frame chosen and RSI (kept on this device, so the chart opens
/// as it was left) and the drawings of the full-screen chart.
int _frame = 900;
bool _rsiOn = false;
bool _prefsRead = false;
final _drawings = <ChartDrawing>[];
const _frameKey = 'encryptlock.chart.frame', _rsiKey = 'encryptlock.chart.rsi';

Future<void> _readChartPrefs() async {
  if (_prefsRead) return;
  _prefsRead = true;
  try {
    final p = await SharedPreferences.getInstance();
    final f = p.getInt(_frameKey);
    if (f != null && chartFrames.any((c) => c.$1 == f)) _frame = f;
    _rsiOn = p.getBool(_rsiKey) ?? _rsiOn;
  } catch (_) {
    // Not kept: the chart opens with the usual time frame.
  }
}

Future<void> _saveChartPrefs() async {
  try {
    final p = await SharedPreferences.getInstance();
    await p.setInt(_frameKey, _frame);
    await p.setBool(_rsiKey, _rsiOn);
  } catch (_) {}
}

/// Loads the candles of a time frame from the LockKeeper, again every 10 s,
/// and the older history page by page when the chart is pulled back to its
/// first candles (trades are kept forever on the blockchain).
mixin _Candles<T extends StatefulWidget> on State<T> {
  List<Candle> candles = const [];

  /// As the blockchain gave them (only intervals with trades).
  List<Candle> _raw = const [];
  bool loaded = false;
  bool _olderDone = false, _loadingOlder = false;
  Timer? _timer;

  Future<void> startLoading() async {
    await _readChartPrefs();
    if (!mounted) return;
    setState(() {});
    _load();
    _timer = Timer.periodic(const Duration(seconds: 10), (_) => _load());
  }

  void _show(List<Candle> raw, int frame) {
    _raw = raw;
    candles = fillGaps(raw, frame, max: maxChartCandles);
    loaded = true;
  }

  Future<void> _load() async {
    final frame = _frame;
    try {
      final c = await LockKeeperClient(AppScope.of(context).lockKeeperUrl).candles(frame);
      if (mounted && frame == _frame) setState(() => _show(mergeCandles(_raw, c), frame));
    } catch (_) {
      // No LockKeeper answers: the last candles of this time frame stay.
      final c = candles.isEmpty ? await lastCandles(frame) : null;
      if (mounted) {
        setState(() {
          if (c != null && frame == _frame) _show(c, frame);
          loaded = true;
        });
      }
    }
  }

  /// The history before the first candle shown.
  Future<void> loadOlder() async {
    if (_olderDone || _loadingOlder || _raw.isEmpty || candles.length >= maxChartCandles) return;
    _loadingOlder = true;
    final frame = _frame;
    try {
      final c = await LockKeeperClient(AppScope.of(context).lockKeeperUrl).candles(frame, before: _raw.first.time);
      if (mounted && frame == _frame) {
        setState(() {
          if (c.isEmpty) _olderDone = true;
          _show(mergeCandles(c, _raw), frame);
        });
      }
    } catch (_) {
      // Tried again on the next pull.
    } finally {
      _loadingOlder = false;
    }
  }

  void setFrame(int f) {
    setState(() {
      _frame = f;
      candles = const [];
      _raw = const [];
      _olderDone = false;
    });
    _saveChartPrefs();
    _load();
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }
}

/// The coin's value written at the top inside the chart.
String chartTitle(AppState state, BuildContext context) {
  final last = state.price?.last;
  final locale = Localizations.localeOf(context).toString();
  return last == null ? 'LOCK' : '1 LOCK = ${formatUsd(last.price, locale)}';
}

/// Row of time-frame buttons (slides sideways on a phone).
class _Frames extends StatelessWidget {
  const _Frames({required this.selected, required this.onChanged, this.small = false});

  final int selected;
  final ValueChanged<int> onChanged;
  final bool small;

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      height: small ? 30 : 36,
      child: ListView(
        scrollDirection: Axis.horizontal,
        children: [
          for (final (secs, name) in chartFrames)
            Padding(
              padding: const EdgeInsets.only(right: 6),
              child: InkWell(
                borderRadius: BorderRadius.circular(6),
                onTap: () => onChanged(secs),
                child: Container(
                  alignment: Alignment.center,
                  padding: EdgeInsets.symmetric(horizontal: small ? 9 : 12),
                  decoration: BoxDecoration(
                    color: secs == selected ? Brand.neonGreen.withValues(alpha: 0.18) : const Color(0x66060B0D),
                    border: Border.all(color: secs == selected ? Brand.neonGreen : Brand.cardBorder),
                    borderRadius: BorderRadius.circular(6),
                  ),
                  child: Text(
                    name,
                    style: TextStyle(
                      color: secs == selected ? Brand.neonGreen : Brand.pearl,
                      fontWeight: FontWeight.w700,
                      fontSize: small ? 12 : 13,
                    ),
                  ),
                ),
              ),
            ),
        ],
      ),
    );
  }
}

/// The chart inside the market screen: time frames, candles and a button
/// that opens it full screen. Smaller on a phone.
class MarketChart extends StatefulWidget {
  const MarketChart({super.key, this.fill = false});

  /// Fill the height it is given (computer window); otherwise 190 on a
  /// phone, 260 on a computer.
  final bool fill;

  @override
  State<MarketChart> createState() => _MarketChartState();
}

class _MarketChartState extends State<MarketChart> with _Candles {
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) startLoading();
    });
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final phone = MediaQuery.sizeOf(context).width < 600;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          children: [
            Expanded(child: _Frames(selected: _frame, onChanged: setFrame, small: true)),
            IconButton(
              tooltip: t.chartFullScreen,
              icon: const Icon(Icons.open_in_full, color: Brand.neonGreen),
              onPressed: () async {
                await Navigator.of(context).push(MaterialPageRoute(builder: (_) => const FullChartScreen()));
                if (mounted) setFrame(_frame);
              },
            ),
          ],
        ),
        const SizedBox(height: 6),
        _sized(
          phone,
          candles.isEmpty
              ? Center(
                  child: loaded
                      ? Text(t.chartNoTrades, style: const TextStyle(color: Brand.pearl))
                      : const CircularProgressIndicator(),
                )
              : PriceChart(
                  candles: candles,
                  interval: _frame,
                  title: chartTitle(state, context),
                  onNearStart: loadOlder,
                ),
        ),
      ],
    );
  }

  Widget _sized(bool phone, Widget chart) =>
      widget.fill ? Expanded(child: chart) : SizedBox(height: phone ? 190 : 260, child: chart);
}

/// The chart on the whole screen, with drawing tools and RSI. Only for
/// looking: buying and selling are on the market screen, below the chart.
class FullChartScreen extends StatefulWidget {
  const FullChartScreen({super.key});

  @override
  State<FullChartScreen> createState() => _FullChartScreenState();
}

class _FullChartScreenState extends State<FullChartScreen> with _Candles {
  ChartTool _tool = ChartTool.none;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) startLoading();
    });
  }

  Widget _toolButton(ChartTool tool, IconData icon, String name, bool phone) {
    final on = _tool == tool;
    return Padding(
      padding: const EdgeInsets.only(right: 6),
      child: Tooltip(
        message: name,
        child: InkWell(
          borderRadius: BorderRadius.circular(6),
          onTap: () => setState(() => _tool = on ? ChartTool.none : tool),
          child: Container(
            padding: EdgeInsets.symmetric(horizontal: phone ? 8 : 10, vertical: 6),
            decoration: BoxDecoration(
              color: on ? Brand.neonBlue.withValues(alpha: 0.2) : const Color(0x66060B0D),
              border: Border.all(color: on ? Brand.neonBlue : Brand.cardBorder),
              borderRadius: BorderRadius.circular(6),
            ),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                Icon(icon, size: 18, color: on ? Brand.neonBlue : Brand.pearl),
                if (!phone) ...[
                  const SizedBox(width: 6),
                  Text(name, style: TextStyle(color: on ? Brand.neonBlue : Brand.pearl, fontSize: 13)),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final phone = MediaQuery.sizeOf(context).width < 600;
    final myLock = state.status == null ? 0.0 : lockToDouble(state.status!.balance);
    return Scaffold(
      appBar: BrandBar(
        title: 'LOCK / USDC',
        lit: true,
        actions: [
          IconButton(
            tooltip: t.chartExitFullScreen,
            icon: const Icon(Icons.close_fullscreen, color: Brand.neonGreen),
            onPressed: () => Navigator.of(context).pop(),
          ),
        ],
      ),
      body: ChipBackground(
        child: Padding(
          padding: EdgeInsets.all(phone ? 8 : 14),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              _Frames(selected: _frame, onChanged: setFrame, small: phone),
              const SizedBox(height: 8),
              SizedBox(
                height: 36,
                child: ListView(
                  scrollDirection: Axis.horizontal,
                  children: [
                    _toolButton(ChartTool.measure, Icons.straighten, t.toolMeasure, phone),
                    _toolButton(ChartTool.hline, Icons.horizontal_rule, t.toolHLine, phone),
                    _toolButton(ChartTool.trend, Icons.show_chart, t.toolTrend, phone),
                    _toolButton(ChartTool.rect, Icons.crop_square, t.toolRect, phone),
                    Padding(
                      padding: const EdgeInsets.only(right: 6),
                      child: Tooltip(
                        message: t.toolClear,
                        child: IconButton(
                          icon: const Icon(Icons.delete_outline, color: Brand.pearl),
                          onPressed: () => setState(() {
                            _drawings.clear();
                            _tool = ChartTool.none;
                          }),
                        ),
                      ),
                    ),
                    FilterChip(
                      label: const Text('RSI'),
                      selected: _rsiOn,
                      onSelected: (v) {
                        setState(() => _rsiOn = v);
                        _saveChartPrefs();
                      },
                    ),
                  ],
                ),
              ),
              const SizedBox(height: 8),
              Expanded(
                child: candles.isEmpty
                    ? Center(
                        child: loaded
                            ? Text(t.chartNoTrades, style: const TextStyle(color: Brand.pearl))
                            : const CircularProgressIndicator(),
                      )
                    : PriceChart(
                        candles: candles,
                        interval: _frame,
                        tool: _tool,
                        drawings: _drawings,
                        showRsi: _rsiOn,
                        myLock: myLock,
                        title: chartTitle(state, context),
                        onNearStart: loadOlder,
                        onToolUsed: () => setState(() => _tool = ChartTool.none),
                      ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
