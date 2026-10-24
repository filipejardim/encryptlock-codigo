import 'dart:math' as math;

import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart' show HardwareKeyboard;
import 'package:intl/intl.dart' show DateFormat, NumberFormat;

import '../core/market.dart';
import '../theme.dart';

/// Chart time frames (seconds per candle) and their button names.
const chartFrames = <(int, String)>[
  (60, '1m'),
  (120, '2m'),
  (180, '3m'),
  (300, '5m'),
  (600, '10m'),
  (900, '15m'),
  (3600, '1h'),
  (28800, '8h'),
  (43200, '12h'),
  (86400, '1D'),
];

/// Drawing tools of the full-screen chart.
enum ChartTool { none, measure, hline, trend, rect }

/// A drawing kept in chart values (time in seconds, price in dollars), so it
/// stays in place when the chart is moved or zoomed.
class ChartDrawing {
  ChartDrawing(this.tool, this.a, this.b);
  final ChartTool tool;
  Offset a; // (time, price)
  Offset b;
}

/// The most candles the chart holds (with older history loaded): two weeks of
/// 1-minute candles; further back, a longer time frame.
const maxChartCandles = 20000;

/// Two lists of candles as one, in time order; where both have the same
/// time, [newer] wins (the newest answer of the blockchain).
List<Candle> mergeCandles(List<Candle> older, List<Candle> newer) {
  final byTime = <int, Candle>{for (final c in older) c.time: c, for (final c in newer) c.time: c};
  return byTime.values.toList()..sort((a, b) => a.time.compareTo(b.time));
}

/// Candles with no gaps: from the network's candles (only intervals with
/// trades), each empty interval is a flat candle at the last price, up to now.
/// A traded candle opens at the previous close, so a single trade that moves
/// the price still shows as a rising or falling candle (not a flat dash).
List<Candle> fillGaps(List<Candle> candles, int interval, {int max = 500}) {
  if (candles.isEmpty) return const [];
  final now = DateTime.now().millisecondsSinceEpoch ~/ 1000;
  final end = math.max(candles.last.time, now - now % interval);
  final start = math.max(candles.first.time, end - (max - 1) * interval);
  final out = <Candle>[];
  var i = 0;
  double? last;
  // The price just before the window, if older candles exist.
  while (i < candles.length && candles[i].time < start) {
    last = candles[i].close;
    i++;
  }
  for (var t = start; t <= end; t += interval) {
    if (i < candles.length && candles[i].time == t) {
      final c = candles[i];
      out.add(last == null ? c : Candle(c.time, last, math.max(c.high, last), math.min(c.low, last), c.close, c.lock));
      last = c.close;
      i++;
    } else if (last != null) {
      out.add(Candle(t, last, last, last, last, 0));
    }
  }
  return out;
}

/// RSI (14): how strongly the price moved up against down lately (0–100;
/// above 70 often "too high", below 30 "too low"). Null where not enough data.
List<double?> rsi(List<Candle> c, [int period = 14]) {
  final out = List<double?>.filled(c.length, null);
  if (c.length <= period) return out;
  double up = 0, down = 0;
  for (var i = 1; i <= period; i++) {
    final d = c[i].close - c[i - 1].close;
    if (d > 0) {
      up += d;
    } else {
      down -= d;
    }
  }
  up /= period;
  down /= period;
  double value(double u, double d) => d == 0 ? (u == 0 ? 50 : 100) : 100 - 100 / (1 + u / d);
  out[period] = value(up, down);
  for (var i = period + 1; i < c.length; i++) {
    final d = c[i].close - c[i - 1].close;
    up = (up * (period - 1) + math.max(d, 0)) / period;
    down = (down * (period - 1) + math.max(-d, 0)) / period;
    out[i] = value(up, down);
  }
  return out;
}

/// The candle chart: green-mint candles going up and red going down, the
/// price scale on the right from top to bottom, a line across at the current
/// price and the price written at the top. Drag to move it every way (also
/// into the empty space after the newest candle), pinch or mouse wheel to
/// stretch; with a tool chosen, drag to draw.
/// Only for looking: buying and selling stay below the chart.
class PriceChart extends StatefulWidget {
  const PriceChart({
    super.key,
    required this.candles,
    required this.interval,
    this.tool = ChartTool.none,
    this.drawings,
    this.showRsi = false,
    this.myLock = 0,
    this.title,
    this.onToolUsed,
    this.onNearStart,
  });

  /// Without gaps (fillGaps), oldest first.
  final List<Candle> candles;
  final int interval;
  final ChartTool tool;

  /// Drawings to show and add to (null: none, as on the small chart).
  final List<ChartDrawing>? drawings;
  final bool showRsi;

  /// This wallet's LOCK, for the measure: "your LOCK at that price".
  final double myLock;

  /// Written at the top inside the chart (the coin's value).
  final String? title;

  /// A drawing was finished (the screen can go back to moving the chart).
  final VoidCallback? onToolUsed;

  /// The chart was pulled back close to its first candle: time to load the
  /// older history (the view stays where it is when it arrives).
  final VoidCallback? onNearStart;

  @override
  State<PriceChart> createState() => _PriceChartState();
}

class _PriceChartState extends State<PriceChart> {
  /// How many candles fit across, and how far back from the newest (in candles).
  double _visible = 60;
  bool _sized = false;
  double _back = 0;
  double _visibleAtStart = 60;
  double _backAtStart = 0;
  Offset _startFocal = Offset.zero;
  ChartDrawing? _drawing;
  ChartDrawing? _measure;
  _Frame? _frame;

  /// The price range when set by hand on the price scale (null: it follows
  /// the visible candles by itself).
  double? _lo, _hi;
  double _loAtStart = 0, _hiAtStart = 1;

  /// The small chart is being moved with the mouse: every way, as on other
  /// trading platforms (a finger on a phone only slides it sideways, so the
  /// page around it still scrolls).
  bool _mouseDrag = false;

  /// This drag already moved the chart up or down (after a few points, so a
  /// sideways pull does not switch the automatic price scale off).
  bool _movedUpDown = false;

  /// Lowest price on the screen now (tests).
  @visibleForTesting
  double? get lowestShown => _frame?.lo;

  /// How many candles fit on the screen now (tests).
  @visibleForTesting
  double get candlesShown => _visible;

  /// How far back from the newest candle the view is (tests).
  @visibleForTesting
  double get candlesBack => _back;

  @override
  void didUpdateWidget(PriceChart old) {
    super.didUpdateWidget(old);
    // New candles while looking at older ones: the view stays where it is
    // (it does not jump forward every minute). Older history added at the
    // start changes nothing: the view counts from the newest candle.
    if (_back > 0 && old.interval == widget.interval && old.candles.isNotEmpty && widget.candles.isNotEmpty) {
      final added = (widget.candles.last.time - old.candles.last.time) ~/ widget.interval;
      if (added > 0) _back += added;
    }
    if (old.interval != widget.interval) {
      _back = 0;
      _measure = null;
      _lo = _hi = null;
    }
    if (old.tool != widget.tool && widget.tool != ChartTool.measure) _measure = null;
  }

  void _clamp() {
    final n = widget.candles.length.toDouble();
    // Up to 500 candles on the screen (thin, close together) even with little
    // history yet: the space before the first trade stays empty, as on other
    // trading charts.
    _visible = _visible.clamp(8, 500);
    // Free both ways, as on other trading charts: back into the past until
    // only the first candles are left at the right edge, forward into the
    // empty space until only the newest are left at the left edge.
    _back = _back.clamp(5 - _visible, math.max(0, n - 2));
  }

  void _start(ScaleStartDetails d) {
    _visibleAtStart = _visible;
    _backAtStart = _back;
    _startFocal = d.localFocalPoint;
    _movedUpDown = false;
    final f = _frame;
    if (f != null) {
      _loAtStart = f.lo;
      _hiAtStart = f.hi;
    }
    if (widget.tool != ChartTool.none && f != null && d.pointerCount == 1) {
      final v = f.toValue(d.localFocalPoint);
      if (widget.tool == ChartTool.hline) {
        widget.drawings?.add(ChartDrawing(ChartTool.hline, v, v));
        widget.onToolUsed?.call();
        setState(() {});
        return;
      }
      _drawing = ChartDrawing(widget.tool, v, v);
      if (widget.tool == ChartTool.measure) {
        _measure = _drawing;
      } else {
        widget.drawings?.add(_drawing!);
      }
      setState(() {});
    }
  }

  void _update(ScaleUpdateDetails d) {
    final f = _frame;
    if (f == null) return;
    if (_drawing != null) {
      setState(() => _drawing!.b = f.toValue(d.localFocalPoint));
      return;
    }
    if (widget.tool == ChartTool.hline) return;
    setState(() {
      if (d.pointerCount > 1) _visible = _visibleAtStart / d.horizontalScale.clamp(0.2, 5);
      _back = _backAtStart + (d.localFocalPoint.dx - _startFocal.dx) / f.step;
      // Up and down too: always on the full screen, with the mouse on the
      // small chart.
      final dy = d.localFocalPoint.dy - _startFocal.dy;
      if (dy.abs() > 6) _movedUpDown = true;
      final vertical = (widget.drawings != null || _mouseDrag) && _movedUpDown;
      if (vertical && d.pointerCount == 1) {
        final shift = dy / f.plot.height * (_hiAtStart - _loAtStart);
        _setRange(_loAtStart + shift, _hiAtStart + shift);
      }
      _clamp();
    });
    // Close to the first candle: ask for the older ones.
    if (widget.candles.length - _visible - _back + 3 < _visible) widget.onNearStart?.call();
  }

  /// Moved up or down by hand: free, also below zero (as on other trading
  /// charts), so the price never stays stuck at the bottom edge and old low
  /// prices or a stop loss far below can be seen and measured.
  void _setRange(double lo, double hi) {
    _lo = lo;
    _hi = lo + math.max(hi - lo, 1e-9);
  }

  /// Price scale stretched (factor < 1: taller candles) or squeezed around its middle.
  void _scalePrice(double factor, double lo, double hi) {
    // Around the current price, so the candles never run away off the chart
    // (around the middle, once zero was at the bottom the middle was far
    // above the price and the candles could not be found again).
    final c = widget.candles;
    final last = c.isEmpty ? null : c.last.close;
    final a = last != null && last >= lo && last <= hi ? last : (lo + hi) / 2;
    final (lo2, hi2) = scaleRange(lo, hi, a, factor.clamp(0.02, 50));
    _lo = lo2;
    _hi = hi2;
  }

  void _priceDragStart(DragStartDetails d) {
    final f = _frame;
    if (f == null) return;
    _loAtStart = f.lo;
    _hiAtStart = f.hi;
    _startFocal = d.localPosition;
  }

  // Pulling the price scale down squeezes the candles, up stretches them.
  void _priceDrag(DragUpdateDetails d) =>
      setState(() => _scalePrice(math.exp((d.localPosition.dy - _startFocal.dy) / 80), _loAtStart, _hiAtStart));

  void _timeDragStart(DragStartDetails d) {
    _visibleAtStart = _visible;
    _startFocal = d.localPosition;
  }

  // Pulling the times to the left fits more candles, to the right fewer and wider.
  void _timeDrag(DragUpdateDetails d) => setState(() {
    _visible = _visibleAtStart * math.exp(-(d.localPosition.dx - _startFocal.dx) / 150);
    _clamp();
  });

  /// Mouse wheel: on the price scale it stretches the prices, on the times or
  /// with Ctrl it fits more or fewer candles. On the small chart a plain wheel
  /// keeps scrolling the page.
  void _signal(PointerSignalEvent e, String where) {
    if (e is! PointerScrollEvent) return;
    final full = widget.drawings != null;
    if (where == 'plot' && !full && !HardwareKeyboard.instance.isControlPressed) return;
    GestureBinding.instance.pointerSignalResolver.register(e, (ev) {
      final down = (ev as PointerScrollEvent).scrollDelta.dy > 0;
      final f = _frame;
      setState(() {
        if (where == 'price' && f != null) {
          _scalePrice(down ? 1.15 : 1 / 1.15, f.lo, f.hi);
        } else {
          _visible *= down ? 1.12 : 1 / 1.12;
          _clamp();
        }
      });
    });
  }

  void _end(ScaleEndDetails d) {
    if (_drawing != null) {
      _drawing = null;
      if (widget.tool != ChartTool.measure) widget.onToolUsed?.call();
    }
  }

  @override
  Widget build(BuildContext context) {
    final locale = Localizations.localeOf(context).toString();
    return LayoutBuilder(
      builder: (context, box) {
        if (!_sized) {
          // About 12 points per candle: wide candles on a phone, more on a computer.
          _visible = ((box.maxWidth - 62) / 12).clamp(20, 90);
          _sized = true;
        }
        _clamp();
        final frame = _Frame.make(
          widget.candles,
          widget.interval,
          Size(box.maxWidth, box.maxHeight),
          _visible,
          _back,
          widget.showRsi,
          _lo,
          _hi,
        );
        _frame = frame;
        // Full screen (with drawings): pinch, mouse wheel and tools. The small
        // chart only slides sideways, so the page around it still scrolls.
        final full = widget.drawings != null;
        final plot = frame.plot;
        return Stack(
          children: [
            Listener(
              onPointerSignal: (e) => _signal(e, 'plot'),
              child: RawGestureDetector(
                gestures: full
                    ? {
                        ScaleGestureRecognizer: GestureRecognizerFactoryWithHandlers<ScaleGestureRecognizer>(
                          () => ScaleGestureRecognizer(allowedButtonsFilter: _leftOrMiddle),
                          (g) => g
                            ..onStart = _start
                            ..onUpdate = _update
                            ..onEnd = _end,
                        ),
                      }
                    : {
                        // A finger: sideways only, so the page still scrolls...
                        _SidewaysDrag: GestureRecognizerFactoryWithHandlers<_SidewaysDrag>(
                          () => _SidewaysDrag(),
                          (g) => g
                            ..onStart = (d) {
                              _mouseDrag = false;
                              _start(ScaleStartDetails(localFocalPoint: d.localPosition));
                            }
                            ..onUpdate = (d) => _update(ScaleUpdateDetails(localFocalPoint: d.localPosition, pointerCount: 1)),
                        ),
                        // ...the mouse: every way (the page scrolls with the wheel).
                        _MouseDrag: GestureRecognizerFactoryWithHandlers<_MouseDrag>(
                          () => _MouseDrag(allowedButtonsFilter: _leftOrMiddle),
                          (g) => g
                            ..onStart = (d) {
                              _mouseDrag = true;
                              _start(ScaleStartDetails(localFocalPoint: d.localPosition));
                            }
                            ..onUpdate = (d) => _update(ScaleUpdateDetails(localFocalPoint: d.localPosition, pointerCount: 1)),
                        ),
                      },
                child: CustomPaint(
                  size: Size(box.maxWidth, box.maxHeight),
                  painter: _ChartPainter(
                    frame: frame,
                    drawings: [...?widget.drawings, ?_measure],
                    title: widget.title,
                    showRsi: widget.showRsi,
                    myLock: widget.myLock,
                    locale: locale,
                    font: DefaultTextStyle.of(context).style.fontFamily,
                  ),
                ),
              ),
            ),
            // The price scale: pull up or down (or mouse wheel) to stretch the
            // candles; double tap goes back to automatic.
            Positioned(
              left: plot.right,
              top: 0,
              width: box.maxWidth - plot.right,
              height: plot.bottom,
              child: Listener(
                onPointerSignal: (e) => _signal(e, 'price'),
                child: GestureDetector(
                  behavior: HitTestBehavior.opaque,
                  onVerticalDragStart: _priceDragStart,
                  onVerticalDragUpdate: _priceDrag,
                  onDoubleTap: () => setState(() => _lo = _hi = null),
                  child: const MouseRegion(cursor: SystemMouseCursors.resizeUpDown),
                ),
              ),
            ),
            // The times: pull sideways (or mouse wheel) for more or fewer candles.
            Positioned(
              left: 0,
              bottom: 0,
              width: plot.width,
              height: 22,
              child: Listener(
                onPointerSignal: (e) => _signal(e, 'time'),
                child: GestureDetector(
                  behavior: HitTestBehavior.opaque,
                  onHorizontalDragStart: _timeDragStart,
                  onHorizontalDragUpdate: _timeDrag,
                  child: const MouseRegion(cursor: SystemMouseCursors.resizeLeftRight),
                ),
              ),
            ),
          ],
        );
      },
    );
  }
}

/// A price range stretched (k < 1) or squeezed (k > 1) around the price [a],
/// never below zero: [a] stays at the same height, so squeezing and then
/// stretching again comes back to where it was.
(double, double) scaleRange(double lo, double hi, double a, double k) {
  final lo2 = math.max(0.0, a - (a - lo) * k);
  return (lo2, math.max(lo2 + 1e-9, a + (hi - a) * k));
}

/// Drag sideways on the small chart with a finger.
class _SidewaysDrag extends HorizontalDragGestureRecognizer {
  @override
  bool isPointerAllowed(PointerEvent event) => event.kind != PointerDeviceKind.mouse && super.isPointerAllowed(event);
}

/// Drag in any direction on the small chart with the mouse (left button or
/// the middle one, wheel pressed).
class _MouseDrag extends PanGestureRecognizer {
  _MouseDrag({super.allowedButtonsFilter});

  @override
  bool isPointerAllowed(PointerEvent event) => event.kind == PointerDeviceKind.mouse && super.isPointerAllowed(event);
}

/// The left button (or a finger) and the mouse's middle button.
bool _leftOrMiddle(int buttons) => buttons & (kPrimaryButton | kMiddleMouseButton) != 0;

/// Where everything goes: which candles are visible, the price range and the
/// conversions between screen points and (time, price).
class _Frame {
  _Frame(
    this.candles,
    this.interval,
    this.size,
    this.plot,
    this.rsiBox,
    this.first,
    this.step,
    this.lo,
    this.hi,
    this.rsiValues,
  );

  factory _Frame.make(
    List<Candle> all,
    int interval,
    Size size,
    double visible,
    double back,
    bool withRsi,
    double? manualLo,
    double? manualHi,
  ) {
    const axis = 62.0, timeAxis = 18.0;
    final chartH = size.height - timeAxis;
    final rsiH = withRsi ? (chartH * 0.24).clamp(48.0, 120.0) : 0.0;
    final plot = Rect.fromLTWH(0, 0, size.width - axis, chartH - rsiH - (withRsi ? 6 : 0));
    final rsiBox = Rect.fromLTWH(0, plot.bottom + 6, plot.width, rsiH);
    final step = plot.width / visible;
    // Index (can be fractional) of the candle at the left edge.
    final first = all.length - visible - back + 3;
    final from = math.max(0, first.floor()), to = math.min(all.length, (first + visible).ceil() + 1);
    var lo = double.infinity, hi = 0.0;
    for (var i = from; i < to; i++) {
      lo = math.min(lo, all[i].low);
      hi = math.max(hi, all[i].high);
    }
    if (lo == double.infinity) {
      lo = 0;
      hi = 1;
    }
    // Some room above and below; a flat price still gets a scale.
    final pad = hi - lo == 0 ? math.max(hi * 0.1, 0.0001) : (hi - lo) * 0.12;
    if (manualLo != null && manualHi != null) {
      lo = manualLo;
      hi = manualHi;
    } else {
      lo = math.max(0, lo - pad);
      hi = hi + pad;
    }
    return _Frame(all, interval, size, plot, rsiBox, first, step, lo, hi, withRsi ? rsi(all) : const []);
  }

  final List<Candle> candles;
  final int interval;
  final Size size;
  final Rect plot, rsiBox;
  final double first, step, lo, hi;
  final List<double?> rsiValues;

  double xOfIndex(num i) => (i - first + 0.5) * step;
  double yOf(double price) => plot.bottom - (price - lo) / (hi - lo) * plot.height;
  double priceAt(double y) => lo + (plot.bottom - y) / plot.height * (hi - lo);

  /// Chart values of a screen point: (time in seconds, price).
  Offset toValue(Offset p) {
    final index = p.dx / step + first - 0.5;
    final t0 = candles.isEmpty ? 0 : candles.first.time;
    return Offset(t0 + index * interval, priceAt(p.dy));
  }

  Offset toScreen(Offset v) {
    final t0 = candles.isEmpty ? 0 : candles.first.time;
    return Offset(xOfIndex((v.dx - t0) / interval), yOf(v.dy));
  }
}

class _ChartPainter extends CustomPainter {
  _ChartPainter({
    required this.frame,
    required this.drawings,
    required this.title,
    required this.showRsi,
    required this.myLock,
    required this.locale,
    this.font,
  });

  final _Frame frame;

  /// The app's letters (the painter does not inherit them by itself).
  final String? font;
  final List<ChartDrawing> drawings;
  final String? title;
  final bool showRsi;
  final double myLock;
  final String locale;

  static const up = Brand.neonGreen;
  static const down = Brand.danger;
  static const grid = Color(0x221C3134);
  static const label = Brand.pearl;

  String _price(double v) {
    // Below zero (chart moved up by hand) the same digits as above it, and
    // never "-0".
    final a = v.abs();
    if (a < 1e-12) v = 0;
    final digits = a >= 100
        ? 2
        : a >= 1
        ? 3
        : a >= 0.01 || v == 0
        ? 4
        : 6;
    return NumberFormat.decimalPatternDigits(locale: locale, decimalDigits: digits).format(v);
  }

  void _text(
    Canvas canvas,
    String s,
    Offset at, {
    Color color = label,
    double size = 11,
    FontWeight weight = FontWeight.w500,
    bool right = false,
    Color? box,
  }) {
    final tp = TextPainter(
      text: TextSpan(
        text: s,
        style: TextStyle(color: color, fontSize: size, fontWeight: weight, fontFamily: font),
      ),
      textDirection: TextDirection.ltr,
    )..layout();
    var o = right ? at - Offset(tp.width, 0) : at;
    if (box != null) {
      final r = Rect.fromLTWH(o.dx - 4, o.dy - 2, tp.width + 8, tp.height + 4);
      canvas.drawRRect(RRect.fromRectAndRadius(r, const Radius.circular(3)), Paint()..color = box);
    }
    tp.paint(canvas, o);
  }

  @override
  void paint(Canvas canvas, Size size) {
    final f = frame;
    final plot = f.plot;
    // Night background behind the chart.
    canvas.drawRect(Offset.zero & size, Paint()..color = const Color(0xCC060B0D));

    // Price scale on the right, from top to bottom, with a grid line each.
    final ticks = _ticks(f.lo, f.hi, math.max(4, (plot.height / 20).floor()));
    for (final p in ticks) {
      final y = f.yOf(p);
      if (y < plot.top - 1 || y > plot.bottom + 1) continue;
      canvas.drawLine(
        Offset(0, y),
        Offset(plot.right, y),
        Paint()
          ..color = grid
          ..strokeWidth = 1,
      );
      // Not under the current price's label.
      if (f.candles.isNotEmpty && (y - f.yOf(f.candles.last.close)).abs() < 14) continue;
      _text(canvas, _price(p), Offset(plot.right + 6, y - 7));
    }
    canvas.drawLine(
      Offset(plot.right, 0),
      Offset(plot.right, size.height - 18),
      Paint()
        ..color = Brand.cardBorder
        ..strokeWidth = 1,
    );

    canvas.save();
    canvas.clipRect(plot);
    // Candles.
    final from = math.max(0, f.first.floor()),
        to = math.min(f.candles.length, (f.first + plot.width / f.step).ceil() + 1);
    final body = math.max(1.0, f.step * 0.62);
    for (var i = from; i < to; i++) {
      final c = f.candles[i];
      final x = f.xOfIndex(i);
      final rising = c.close >= c.open;
      final color = c.lock == 0 ? (rising ? up : down).withValues(alpha: 0.35) : (rising ? up : down);
      final paint = Paint()..color = color;
      canvas.drawLine(
        Offset(x, f.yOf(c.high)),
        Offset(x, f.yOf(c.low)),
        paint..strokeWidth = math.max(1, f.step * 0.1),
      );
      final top = f.yOf(math.max(c.open, c.close)), bottom = f.yOf(math.min(c.open, c.close));
      final r = Rect.fromLTRB(x - body / 2, top, x + body / 2, math.max(bottom, top + 1.5));
      if (c.lock > 0 && body > 3) {
        canvas.drawRect(
          r.inflate(1.5),
          Paint()
            ..color = color.withValues(alpha: 0.25)
            ..maskFilter = const MaskFilter.blur(BlurStyle.normal, 3),
        );
      }
      canvas.drawRect(r, Paint()..color = color);
    }

    // Drawings.
    for (final d in drawings) {
      _drawing(canvas, d);
    }
    canvas.restore();

    // The current price: a line across the whole chart and its value on the scale.
    if (f.candles.isNotEmpty) {
      final last = f.candles.last;
      final y = f.yOf(last.close);
      if (y >= plot.top && y <= plot.bottom) {
        // Green or red as the last candle with trades (an empty minute keeps it).
        final traded = f.candles.lastWhere((c) => c.lock > 0, orElse: () => last);
        final color = traded.close >= traded.open ? up : down;
        _dashed(
          canvas,
          Offset(0, y),
          Offset(plot.right, y),
          Paint()
            ..color = color
            ..strokeWidth = 1.2,
        );
        _text(
          canvas,
          _price(last.close),
          Offset(plot.right + 6, y - 7),
          color: const Color(0xFF06231A),
          weight: FontWeight.w800,
          box: color,
        );
      }
    }

    // Times along the bottom.
    final every = math.max(1, (90 / f.step).ceil());
    final timeFmt = f.interval >= 86400
        ? DateFormat.MMMd(locale)
        : f.interval >= 3600
        ? DateFormat('d/M HH:mm')
        : DateFormat.Hm(locale);
    // Also over the empty space before the first and after the newest candle.
    final t0 = f.candles.isEmpty ? 0 : f.candles.first.time;
    for (var i = f.first.floor(); i <= (f.first + plot.width / f.step).ceil(); i++) {
      if (i % every != 0) continue;
      final x = f.xOfIndex(i);
      if (x < 20 || x > plot.right - 30) continue;
      final t = DateTime.fromMillisecondsSinceEpoch((t0 + i * f.interval) * 1000);
      _text(canvas, timeFmt.format(t), Offset(x - 18, size.height - 15), size: 10);
    }

    // RSI under the candles.
    if (showRsi) _rsi(canvas, f);

    // The coin's value at the top, inside the chart.
    if (title != null) {
      _text(
        canvas,
        title!,
        const Offset(10, 8),
        color: Colors.white,
        size: 15,
        weight: FontWeight.w800,
        box: const Color(0xAA0E1619),
      );
    }
  }

  void _rsi(Canvas canvas, _Frame f) {
    final box = f.rsiBox;
    canvas.drawRect(box, Paint()..color = const Color(0x55060B0D));
    double y(double v) => box.bottom - v / 100 * box.height;
    for (final level in [30.0, 70.0]) {
      _dashed(
        canvas,
        Offset(0, y(level)),
        Offset(box.right, y(level)),
        Paint()
          ..color = Brand.cardBorder
          ..strokeWidth = 1,
      );
      _text(canvas, level.toInt().toString(), Offset(box.right + 6, y(level) - 7), size: 10);
    }
    _text(canvas, 'RSI 14', Offset(6, box.top + 2), size: 10, color: Brand.neonBlue);
    final path = Path();
    var started = false;
    for (var i = 0; i < f.rsiValues.length; i++) {
      final v = f.rsiValues[i];
      final x = f.xOfIndex(i);
      if (v == null || x < -f.step || x > box.right + f.step) continue;
      started ? path.lineTo(x, y(v)) : path.moveTo(x, y(v));
      started = true;
    }
    canvas.save();
    canvas.clipRect(box);
    canvas.drawPath(
      path,
      Paint()
        ..color = const Color(0xFFB59CFF)
        ..style = PaintingStyle.stroke
        ..strokeWidth = 1.5,
    );
    canvas.restore();
  }

  void _drawing(Canvas canvas, ChartDrawing d) {
    final a = frame.toScreen(d.a), b = frame.toScreen(d.b);
    final line = Paint()
      ..color = Brand.neonBlue
      ..strokeWidth = 1.6
      ..style = PaintingStyle.stroke;
    switch (d.tool) {
      case ChartTool.hline:
        canvas.drawLine(Offset(0, a.dy), Offset(frame.plot.right, a.dy), line..color = const Color(0xFFFFD166));
        _text(
          canvas,
          _price(d.a.dy),
          Offset(frame.plot.right - 4, a.dy - 16),
          right: true,
          color: const Color(0xFFFFD166),
          size: 11,
        );
      case ChartTool.trend:
        canvas.drawLine(a, b, line);
        canvas.drawCircle(a, 3, Paint()..color = Brand.neonBlue);
        canvas.drawCircle(b, 3, Paint()..color = Brand.neonBlue);
      case ChartTool.rect:
        final r = Rect.fromPoints(a, b);
        canvas.drawRect(r, Paint()..color = Brand.neonBlue.withValues(alpha: 0.12));
        canvas.drawRect(r, line);
      case ChartTool.measure:
        final rising = d.b.dy >= d.a.dy;
        final color = rising ? up : down;
        final r = Rect.fromPoints(a, b);
        canvas.drawRect(r, Paint()..color = color.withValues(alpha: 0.14));
        canvas.drawLine(
          a,
          b,
          Paint()
            ..color = color
            ..strokeWidth = 1.4,
        );
        final pct = d.a.dy == 0 ? 0 : (d.b.dy - d.a.dy) / d.a.dy * 100;
        final lines = [
          '${pct >= 0 ? '+' : ''}${NumberFormat.decimalPatternDigits(locale: locale, decimalDigits: 2).format(pct)} %   ${_price(d.a.dy)} → ${_price(d.b.dy)} \$',
          if (myLock > 0)
            '${NumberFormat.decimalPatternDigits(locale: locale, decimalDigits: 2).format(myLock)} LOCK = ${NumberFormat.decimalPatternDigits(locale: locale, decimalDigits: 2).format(myLock * d.b.dy)} \$',
        ];
        var y = math.min(a.dy, b.dy) - 18.0 * lines.length - 6;
        if (y < 4) y = math.max(a.dy, b.dy) + 6;
        for (final s in lines) {
          _text(
            canvas,
            s,
            Offset(r.center.dx - 60, y),
            color: const Color(0xFF06231A),
            weight: FontWeight.w700,
            box: color,
          );
          y += 18;
        }
      case ChartTool.none:
        break;
    }
  }

  void _dashed(Canvas canvas, Offset a, Offset b, Paint p) {
    final len = (b - a).distance;
    final dir = (b - a) / len;
    for (double d = 0; d < len; d += 9) {
      canvas.drawLine(a + dir * d, a + dir * math.min(d + 5, len), p);
    }
  }

  /// Round prices for the scale (1, 2 or 5 × a power of ten apart).
  static List<double> _ticks(double lo, double hi, int count) {
    final raw = (hi - lo) / count;
    if (raw <= 0) return [lo];
    final mag = math.pow(10, (math.log(raw) / math.ln10).floor()).toDouble();
    final step = [1, 2, 2.5, 5, 10].map((m) => m * mag).firstWhere((s) => s >= raw);
    final out = <double>[];
    for (var v = (lo / step).ceil() * step; v <= hi; v += step) {
      out.add(v);
    }
    return out;
  }

  @override
  bool shouldRepaint(_ChartPainter old) => true;
}
