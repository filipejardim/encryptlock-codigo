import 'dart:io';
import 'dart:ui' as ui;

import 'package:encryptlock_wallet/core/market.dart';
import 'package:encryptlock_wallet/widgets/price_chart.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

/// With SHOTS=(folder) the charts are also saved as pictures.
final _shots = Platform.environment['SHOTS'];

List<Candle> _candles() {
  final now = DateTime.now().millisecondsSinceEpoch ~/ 1000;
  final t0 = now - now % 60 - 90 * 60;
  var p = 2.0;
  return [
    for (var i = 0; i < 90; i++)
      () {
        final o = p;
        p = (p * (i % 7 < 4 ? 1.02 : 0.975)).clamp(0.5, 50);
        return Candle(t0 + i * 60, o, (o > p ? o : p) * 1.01, (o < p ? o : p) * 0.99, p, 5);
      }(),
  ];
}

double _low(WidgetTester tester) => (tester.state(find.byType(PriceChart)) as dynamic).lowestShown as double;

Future<void> _shot(WidgetTester tester, GlobalKey key, String name) async {
  if (_shots == null) return;
  await tester.runAsync(() async {
    final image = await (key.currentContext!.findRenderObject()! as RenderRepaintBoundary).toImage();
    final png = await image.toByteData(format: ui.ImageByteFormat.png);
    File('$_shots/$name.png').writeAsBytesSync(png!.buffer.asUint8List());
  });
}

void main() {
  testWidgets('full screen: pulling the chart up goes below zero (the price never stuck at the bottom)', (tester) async {
    const size = Size(1100, 600);
    await tester.binding.setSurfaceSize(size);
    if (_shots != null) {
      await tester.runAsync(() async {
        final font = File(r'C:\Windows\Fonts\segoeui.ttf');
        if (font.existsSync()) {
          await (FontLoader("Roboto")..addFont(Future.value(ByteData.sublistView(font.readAsBytesSync())))).load();
        }
      });
    }
    final key = GlobalKey();
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: RepaintBoundary(
          key: key,
          child: PriceChart(candles: fillGaps(_candles(), 60), interval: 60, drawings: <ChartDrawing>[], title: '1 LOCK = 2 \$'),
        ),
      ),
    ));
    // Squeeze the price scale a lot: zero comes to the bottom edge.
    await tester.dragFrom(Offset(size.width - 20, 60), const Offset(0, 300));
    await tester.pump();
    expect(_low(tester), 0);
    // Pull the chart up: the prices below zero come into view.
    await tester.dragFrom(Offset(size.width / 2, size.height / 2), const Offset(0, -220));
    await tester.pump();
    expect(_low(tester), lessThan(0));
    await _shot(tester, key, 'grafico-puxado-para-cima');
    // Double tap on the price scale: automatic again.
    await tester.tapAt(Offset(size.width - 20, 100));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.tapAt(Offset(size.width - 20, 100));
    await tester.pump(const Duration(milliseconds: 400));
    expect(_low(tester), greaterThan(0));
    expect(tester.takeException(), isNull);
    await tester.binding.setSurfaceSize(null);
  });

  testWidgets('with little history the candles still close up (thin, many on the screen)', (tester) async {
    const size = Size(1100, 600);
    await tester.binding.setSurfaceSize(size);
    final key = GlobalKey();
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: RepaintBoundary(
          key: key,
          child: PriceChart(candles: _candles().sublist(60), interval: 60, drawings: <ChartDrawing>[], title: '1 LOCK = 2 \$'),
        ),
      ),
    ));
    // Pull the times to the left a lot: many more candles fit than there are.
    for (var i = 0; i < 4; i++) {
      await tester.dragFrom(Offset(size.width / 2, size.height - 8), const Offset(-400, 0));
      await tester.pump();
    }
    expect((tester.state(find.byType(PriceChart)) as dynamic).candlesShown as double, 500);
    await _shot(tester, key, 'grafico-fechado');
    expect(tester.takeException(), isNull);
    await tester.binding.setSurfaceSize(null);
  });

  testWidgets('small chart: the mouse moves it every way, a finger only sideways (the page still scrolls)', (tester) async {
    const size = Size(900, 360);
    await tester.binding.setSurfaceSize(size);
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: ListView(children: [
          SizedBox(height: size.height, child: PriceChart(candles: fillGaps(_candles(), 60), interval: 60, title: 'LOCK')),
        ]),
      ),
    ));
    final state = tester.state(find.byType(PriceChart)) as dynamic;
    final before = _low(tester);
    // Middle button pressed and pulled up.
    final g = await tester.startGesture(const Offset(400, 180), kind: PointerDeviceKind.mouse, buttons: kMiddleMouseButton);
    for (var i = 0; i < 10; i++) {
      await g.moveBy(const Offset(0, -15));
      await tester.pump();
    }
    await g.up();
    await tester.pump();
    final afterMiddle = _low(tester);
    expect(afterMiddle, lessThan(before));
    // Left button pulled down and to the right (no key held).
    await tester.dragFrom(const Offset(400, 180), const Offset(200, 120), kind: PointerDeviceKind.mouse);
    await tester.pump();
    expect(_low(tester), greaterThan(afterMiddle));
    expect(state.candlesBack as double, greaterThan(0));
    // Left button pulled to the left, past the newest candle: empty space on the right.
    await tester.dragFrom(const Offset(700, 180), const Offset(-600, 0), kind: PointerDeviceKind.mouse);
    await tester.pump();
    expect(state.candlesBack as double, lessThan(0));
    // A finger pulling up and down does not move the chart's prices.
    final low = _low(tester);
    await tester.dragFrom(const Offset(400, 180), const Offset(0, -60));
    await tester.pump();
    expect(_low(tester), low);
    expect(tester.takeException(), isNull);
    await tester.binding.setSurfaceSize(null);
  });

  testWidgets('full screen: drag freely back into the past and forward into the empty space', (tester) async {
    const size = Size(1100, 600);
    await tester.binding.setSurfaceSize(size);
    final key = GlobalKey();
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: RepaintBoundary(
          key: key,
          child: PriceChart(candles: fillGaps(_candles(), 60), interval: 60, drawings: <ChartDrawing>[], title: '1 LOCK = 2 \$'),
        ),
      ),
    ));
    final state = tester.state(find.byType(PriceChart)) as dynamic;
    // To the left: the newest candles go towards the left edge, the right is empty.
    await tester.dragFrom(Offset(size.width - 200, 300), const Offset(-700, 0));
    await tester.pump();
    final forward = state.candlesBack as double;
    expect(forward, lessThan(-30));
    await _shot(tester, key, 'grafico-para-a-esquerda');
    // To the right, far: back to the first candles (and stops there).
    for (var i = 0; i < 4; i++) {
      await tester.dragFrom(const Offset(100, 300), const Offset(800, 0));
      await tester.pump();
    }
    expect(state.candlesBack as double, greaterThan(80));
    await _shot(tester, key, 'grafico-para-a-direita');
    // Up and down by grabbing the middle of the chart, with no key held.
    final low = _low(tester);
    await tester.dragFrom(Offset(size.width / 2, size.height / 2), const Offset(0, 120));
    await tester.pump();
    expect(_low(tester), greaterThan(low));
    // A sideways pull with a little shake keeps the automatic price scale.
    await tester.tapAt(Offset(size.width - 20, 100));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.tapAt(Offset(size.width - 20, 100));
    await tester.pump(const Duration(milliseconds: 400));
    final auto = _low(tester);
    await tester.dragFrom(Offset(size.width / 2, size.height / 2), const Offset(-150, 3));
    await tester.pump();
    expect(_low(tester), isNot(lessThan(0)));
    expect(auto, greaterThan(0));
    expect(tester.takeException(), isNull);
    await tester.binding.setSurfaceSize(null);
  });

  test('older and newer candles join in time order, the newest answer winning', () {
    const a = Candle(60, 1, 1, 1, 1, 1), b = Candle(120, 2, 2, 2, 2, 1), b2 = Candle(120, 3, 3, 3, 3, 2), c = Candle(180, 4, 4, 4, 4, 1);
    final m = mergeCandles([b, c], [a, b2]);
    expect(m.map((x) => x.time), [60, 120, 180]);
    expect(m[1].close, 3);
  });

  testWidgets('pulled back to the first candles, older history is asked for and the view stays put', (tester) async {
    const size = Size(1100, 600);
    await tester.binding.setSurfaceSize(size);
    final all = _candles();
    var shown = all.sublist(60); // the newest 30 first
    var asked = 0;
    late StateSetter set;
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: StatefulBuilder(builder: (context, setter) {
          set = setter;
          return PriceChart(
            candles: shown,
            interval: 60,
            drawings: <ChartDrawing>[],
            onNearStart: () => asked++,
          );
        }),
      ),
    ));
    final state = tester.state(find.byType(PriceChart)) as dynamic;
    await tester.dragFrom(const Offset(100, 300), const Offset(900, 0));
    await tester.pump();
    expect(asked, greaterThan(0));
    final back = state.candlesBack as double;
    // The older 60 arrive: the candles on the screen do not move.
    set(() => shown = mergeCandles(all.sublist(0, 60), shown));
    await tester.pump();
    expect(state.candlesBack as double, back);
    // And now it goes further back, to the very first candle.
    for (var i = 0; i < 3; i++) {
      await tester.dragFrom(const Offset(100, 300), const Offset(900, 0));
      await tester.pump();
    }
    expect(state.candlesBack as double, greaterThan(80));
    // A new minute: still looking at the same old candles.
    final before = state.candlesBack as double;
    final last = all.last;
    set(() => shown = [...shown, Candle(last.time + 60, last.close, last.close, last.close, last.close, 1)]);
    await tester.pump();
    expect(state.candlesBack as double, before + 1);
    expect(tester.takeException(), isNull);
    await tester.binding.setSurfaceSize(null);
  });
}
