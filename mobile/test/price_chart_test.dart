import 'package:encryptlock_wallet/core/market.dart';
import 'package:encryptlock_wallet/widgets/price_chart.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('empty minutes become flat candles at the last price', () {
    final now = DateTime.now().millisecondsSinceEpoch ~/ 1000;
    final t0 = now - now % 60 - 600; // 10 minutes ago
    final filled = fillGaps([
      Candle(t0, 0.2, 0.5, 0.1, 0.3, 10),
      Candle(t0 + 180, 0.3, 0.4, 0.3, 0.4, 20),
    ], 60);
    expect(filled.first.time, t0);
    expect(filled.length, 11); // up to the current minute
    expect(filled[1].close, 0.3); // gap keeps the last price
    expect(filled[1].lock, 0);
    expect(filled[3].close, 0.4);
    expect(filled.last.close, 0.4);
    for (var i = 1; i < filled.length; i++) {
      expect(filled[i].time - filled[i - 1].time, 60);
    }
  });

  test('a single trade that moves the price opens at the previous close', () {
    final now = DateTime.now().millisecondsSinceEpoch ~/ 1000;
    final t0 = now - now % 3600 - 3600 * 5;
    final filled = fillGaps([
      Candle(t0, 0.2, 0.2, 0.2, 0.2, 5),
      Candle(t0 + 3600 * 2, 0.3, 0.3, 0.3, 0.3, 5),
      Candle(t0 + 3600 * 4, 0.36, 0.36, 0.36, 0.36, 5),
    ], 3600);
    expect(filled.first.open, 0.2); // nothing before it: as the network gave it
    expect([filled[2].open, filled[2].low, filled[2].high, filled[2].close], [0.2, 0.2, 0.3, 0.3]);
    expect([filled[4].open, filled[4].low, filled[4].high, filled[4].close], [0.3, 0.3, 0.36, 0.36]);
    expect(filled[3].open, 0.3); // empty hour stays flat
  });

  test('a very old start keeps at most 500 candles', () {
    final now = DateTime.now().millisecondsSinceEpoch ~/ 1000;
    final filled = fillGaps([Candle(now - 86400 * 30, 1, 1, 1, 1, 1)], 60);
    expect(filled.length, 500);
    expect(filled.first.close, 1);
  });

  test('RSI: only rises = 100, only falls = 0, in between otherwise', () {
    List<Candle> c(List<double> closes) => [for (var i = 0; i < closes.length; i++) Candle(i * 60, closes[i], closes[i], closes[i], closes[i], 1)];
    final rising = rsi(c([for (var i = 1; i <= 20; i++) i.toDouble()]));
    expect(rising[13], isNull);
    expect(rising.last, 100);
    final falling = rsi(c([for (var i = 20; i >= 1; i--) i.toDouble()]));
    expect(falling.last, 0);
    final mixed = rsi(c([for (var i = 0; i < 30; i++) i.isEven ? 1.0 : 1.2]));
    expect(mixed.last, inInclusiveRange(30, 70));
  });

  testWidgets('the chart draws, moves and stretches without errors (phone and computer size)', (tester) async {
    final now = DateTime.now().millisecondsSinceEpoch ~/ 1000;
    final t0 = now - now % 60 - 120 * 60;
    var p = 0.2;
    final raw = <Candle>[];
    for (var i = 0; i < 120; i += 1 + i % 3) {
      final o = p;
      p = (p * (i % 5 == 0 ? 1.6 : 0.85)).clamp(0.01, 50);
      raw.add(Candle(t0 + i * 60, o, (o > p ? o : p) * 1.1, (o < p ? o : p) * 0.9, p, 5));
    }
    final drawings = <ChartDrawing>[];
    for (final size in [const Size(360, 190), const Size(1100, 600)]) {
      await tester.binding.setSurfaceSize(size);
      await tester.pumpWidget(MaterialApp(
        home: Scaffold(
          body: PriceChart(
            candles: fillGaps(raw, 60),
            interval: 60,
            tool: ChartTool.measure,
            drawings: drawings,
            showRsi: true,
            myLock: 20,
            title: 'LOCK · 0,20 \$',
          ),
        ),
      ));
      await tester.drag(find.byType(PriceChart), const Offset(-120, -40));
      await tester.pump();
      // Squeeze the price scale (to see far prices, e.g. 2 $) and pull the times.
      await tester.dragFrom(Offset(size.width - 20, 40), const Offset(0, 200));
      await tester.pump();
      await tester.dragFrom(Offset(size.width / 2, size.height - 8), const Offset(-100, 0));
      await tester.pump();
      expect(tester.takeException(), isNull);
      // The small chart (no tools) inside a page that scrolls.
      await tester.pumpWidget(MaterialApp(
        home: Scaffold(
          body: ListView(children: [
            SizedBox(height: size.height, child: PriceChart(candles: fillGaps(raw, 60), interval: 60, title: 'LOCK')),
          ]),
        ),
      ));
      await tester.drag(find.byType(PriceChart), const Offset(150, 0));
      await tester.pump();
      expect(tester.takeException(), isNull);
    }
    await tester.pump(const Duration(seconds: 1)); // double-tap wait on the price scale
    await tester.binding.setSurfaceSize(null);
  });

  test('squeezing the prices and stretching them back keeps the price in view', () {
    // 0,15–0,25 $ with the price at 0,18: squeeze a lot (to see 2 $), then back.
    var (lo, hi) = scaleRange(0.15, 0.25, 0.18, 40);
    expect(lo, 0);
    expect(hi, greaterThan(2));
    expect(0.18, inInclusiveRange(lo, hi));
    for (var i = 0; i < 6; i++) {
      (lo, hi) = scaleRange(lo, hi, 0.18, 1 / 3);
      expect(0.18, inInclusiveRange(lo, hi));
    }
    expect(hi - lo, lessThan(0.2));
  });
}
