import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../theme.dart';

/// Trading candles drawn as decoration (not real prices): they tell people
/// at a glance that this is where LOCK is traded.
class Candle {
  const Candle(this.open, this.close, this.high, this.low);

  final double open, close, high, low;
  bool get up => close >= open;
}

/// Three green, two red, then green after green: a market going up.
List<Candle> risingCandles() {
  const moves = [0.9, 1.1, 0.8, -0.7, -0.5, 1.0, 1.3, 0.9, 1.2, 1.1];
  return _walk(moves);
}

/// Up and down and up again, ending where it started, so it can run
/// behind a title without a jump.
List<Candle> waveCandles() {
  const moves = [
    0.8, 1.0, -0.4, 0.9, 0.7, -0.6, -0.9, -0.5, 0.6, -0.8, -0.7, 0.5, //
    1.1, 0.6, 0.9, -0.5, 0.8, -1.0, -0.6, -0.7, 0.4, -0.9, -0.4,
  ];
  final sum = moves.fold<double>(0, (a, b) => a + b);
  return _walk([...moves, -sum]);
}

List<Candle> _walk(List<double> moves) {
  var p = 0.0;
  return [
    for (var i = 0; i < moves.length; i++)
      () {
        // Each candle opens a little back inside the one before, as on a
        // real chart; the line moves on by 60 % of the candle.
        final open = p - 0.4 * moves[i], close = open + moves[i];
        p += 0.6 * moves[i];
        // Wicks of different lengths so it looks like a real chart.
        final wick = 0.25 + 0.2 * ((i * 7) % 3);
        return Candle(open, close, math.max(open, close) + wick, math.min(open, close) - wick * 0.8);
      }(),
  ];
}

/// Paints [candles] with a neon glow. [shift] (0..1) slides them to the
/// left and starts again, for the running version.
class CandlesPainter extends CustomPainter {
  CandlesPainter(this.candles, {this.opacity = 1, this.shift = 0, this.repeat = false, this.body = 0.5});

  final List<Candle> candles;

  /// Width of a candle body as a part of its space (0.72 = side by side).
  final double body;
  final double opacity;
  final double shift;
  final bool repeat;

  @override
  void paint(Canvas canvas, Size size) {
    if (candles.isEmpty || size.isEmpty) return;
    final lo = candles.map((c) => c.low).reduce(math.min);
    final hi = candles.map((c) => c.high).reduce(math.max);
    final span = hi - lo == 0 ? 1.0 : hi - lo;
    final pad = size.height * (repeat ? 0.12 : 0.04);
    double y(double v) => size.height - pad - (v - lo) / span * (size.height - 2 * pad);

    // Not too big, not too small: about 19 px per candle, at most the width.
    final step = repeat ? 19.0 : size.width / candles.length;
    final width = math.min(step * body, repeat ? 11.0 : 16.0);
    final glow = math.max(1.5, width * 0.4);
    final total = step * candles.length;
    final start = repeat ? -shift * total : 0.0;
    final copies = repeat ? (size.width / total).ceil() + 1 : 1;

    canvas.save();
    canvas.clipRect(Offset.zero & size);
    for (var k = 0; k < copies; k++) {
      for (var i = 0; i < candles.length; i++) {
        final c = candles[i];
        final x = start + k * total + step * i + step / 2;
        if (x < -step || x > size.width + step) continue;
        final color = (c.up ? Brand.neonGreen : Brand.danger).withValues(alpha: opacity);
        final top = y(math.max(c.open, c.close)), bottom = y(math.min(c.open, c.close));
        final rect = Rect.fromLTRB(x - width / 2, top, x + width / 2, math.max(bottom, top + 2));
        canvas.drawRect(
          rect.inflate(glow * 0.3),
          Paint()
            ..color = color.withValues(alpha: opacity * 0.45)
            ..maskFilter = MaskFilter.blur(BlurStyle.normal, glow),
        );
        canvas.drawLine(
          Offset(x, y(c.high)),
          Offset(x, y(c.low)),
          Paint()
            ..color = color
            ..strokeWidth = repeat ? 1.3 : 1.0,
        );
        canvas.drawRect(rect, Paint()..color = color);
      }
    }
    canvas.restore();
  }

  @override
  bool shouldRepaint(CandlesPainter old) =>
      old.shift != shift || old.opacity != opacity || old.candles != candles || old.body != body;
}

/// Candles running slowly from right to left (behind the market's title).
class RunningCandles extends StatefulWidget {
  const RunningCandles({super.key, this.opacity = 0.35});

  final double opacity;

  @override
  State<RunningCandles> createState() => _RunningCandlesState();
}

class _RunningCandlesState extends State<RunningCandles> with SingleTickerProviderStateMixin {
  late final AnimationController _c = AnimationController(vsync: this, duration: const Duration(seconds: 30))..repeat();
  final _candles = waveCandles();

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return RepaintBoundary(
      child: AnimatedBuilder(
        animation: _c,
        builder: (_, _) => CustomPaint(
          painter: CandlesPainter(_candles, opacity: widget.opacity, shift: _c.value, repeat: true),
          size: Size.infinite,
        ),
      ),
    );
  }
}
