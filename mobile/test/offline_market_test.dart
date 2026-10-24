import 'dart:io';
import 'dart:ui' as ui;

import 'package:encryptlock_wallet/app_state.dart';
import 'package:encryptlock_wallet/core/core.dart';
import 'package:encryptlock_wallet/core/market.dart';
import 'package:encryptlock_wallet/core/waiting_orders.dart';
import 'package:encryptlock_wallet/l10n/gen/app_localizations.dart';
import 'package:encryptlock_wallet/screens/home.dart' show KeepersLine, NetworkLine;
import 'package:encryptlock_wallet/screens/market.dart';
import 'package:encryptlock_wallet/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// With SHOTS=(folder) the screens are also saved as pictures (Windows fonts).
final _shots = Platform.environment['SHOTS'];

Future<void> _loadFonts() async {
  Future<void> load(String family, String file) async {
    final f = File(file);
    if (!f.existsSync()) return;
    final loader = FontLoader(family)..addFont(Future.value(ByteData.sublistView(f.readAsBytesSync())));
    await loader.load();
  }

  await load('Roboto', r'C:\Windows\Fonts\segoeui.ttf');
  await load('Roboto', r'C:\Windows\Fonts\segoeuib.ttf');
  await load('Montserrat', 'assets/fonts/Montserrat-Bold.ttf');
  await load('MaterialIcons', r'C:\src\flutter\bin\cache\artifacts\material_fonts\MaterialIcons-Regular.otf');
}

void main() {
  test('waiting orders keep everything they need', () {
    final o = WaitingOrder(
      id: '1',
      newOffer: false,
      sell: false,
      lock: BigInt.parse('1000000000'),
      usdc: BigInt.from(2000000),
      offerId: 7,
    );
    final back = WaitingOrder.fromJson(o.toJson());
    expect(back.newOffer, false);
    expect(back.sell, false);
    expect(back.lock, o.lock);
    expect(back.usdc, o.usdc);
    expect(back.offerId, 7);
  });

  testWidgets('wallet line: green online with no LockKeeper on, red only without internet', (tester) async {
    SharedPreferences.setMockInitialValues({});
    final state = AppState(core: PreviewCore())
      ..networkError = true
      ..noInternet = false
      ..lastUpdate = DateTime.now();
    Future<void> show() => tester.pumpWidget(MaterialApp(
          locale: const Locale('pt'),
          supportedLocales: AppLocalizations.supportedLocales,
          localizationsDelegates: AppLocalizations.localizationsDelegates,
          home: Scaffold(body: Column(children: [NetworkLine(state: state), KeepersLine(state: state)])),
        ));
    await show();
    expect(find.text('Online'), findsOneWidget);
    expect(find.textContaining('À espera'), findsOneWidget); // only the LockKeepers line
    state.noInternet = true;
    await show();
    expect(find.textContaining('Sem internet'), findsNWidgets(2));
    state
      ..networkError = false
      ..noInternet = false
      ..keepersOnline = 12;
    await show();
    expect(find.text('12 LockKeepers da blockchain ligados'), findsOneWidget);
  });

  testWidgets('market with no LockKeeper on: last numbers, Buy / Sell and waiting orders, every window size', (tester) async {
    SharedPreferences.setMockInitialValues({});
    if (_shots != null) await tester.runAsync(_loadFonts);
    final state = AppState(core: PreviewCore());
    await tester.runAsync(() => state.open('preview'));
    final now = DateTime.now().millisecondsSinceEpoch ~/ 1000;
    state
      ..networkError = true
      ..noInternet = false
      ..lastUpdate = DateTime(2026, 10, 12, 12, 20)
      ..price = parsePrice({
        'last': {'lock': '1000000000', 'usdc': '2000000', 'time': '$now'},
        'day_lock': '1000000000',
        'day_usdc': '2000000',
        'day_median': '190000000',
      })
      ..offers = parseOffers({
        'offers': [
          {'id': '2', 'maker': 'lock1other', 'side': 'SIDE_SELL', 'lock': '1000000000', 'usdc': '2000000'},
          {'id': '3', 'maker': 'lock1other', 'side': 'SIDE_BUY', 'lock': '500000000', 'usdc': '900000'},
        ],
      })
      ..waitingOrders = [
        WaitingOrder(id: 'a', newOffer: false, sell: false, lock: BigInt.parse('1000000000'), usdc: BigInt.from(2000000), offerId: 2),
        WaitingOrder(id: 'b', newOffer: true, sell: true, lock: BigInt.parse('2500000000'), usdc: BigInt.from(6000000)),
      ];

    const sizes = {'normal': Size(1264, 780), 'maximizado': Size(1920, 1000), 'estreito': Size(760, 700), 'telemovel': Size(400, 820)};
    for (final e in sizes.entries) {
      await tester.binding.setSurfaceSize(e.value);
      tester.view.physicalSize = e.value;
      tester.view.devicePixelRatio = 1;
      final key = GlobalKey();
      await tester.pumpWidget(RepaintBoundary(
        key: key,
        child: AppScope(
          state: state,
          child: MaterialApp(
            theme: buildTheme(),
            locale: const Locale('pt'),
            supportedLocales: AppLocalizations.supportedLocales,
            localizationsDelegates: AppLocalizations.localizationsDelegates,
            home: const MarketScreen(),
          ),
        ),
      ));
      await tester.pump(const Duration(milliseconds: 300));
      expect(tester.takeException(), isNull, reason: e.key);
      expect(find.textContaining('À espera que um LockKeeper da blockchain ligue'), findsOneWidget, reason: e.key);
      expect(find.text('⏳ Pedidos em espera'), findsOneWidget, reason: e.key);
      expect(find.textContaining('Mediana 24 h: 0,19'), findsOneWidget, reason: e.key);
      if (_shots != null) {
        await tester.runAsync(() async {
          final boundary = key.currentContext!.findRenderObject()! as RenderRepaintBoundary;
          final image = await boundary.toImage();
          final png = await image.toByteData(format: ui.ImageByteFormat.png);
          File('$_shots/mercado-${e.key}.png').writeAsBytesSync(png!.buffer.asUint8List());
        });
      }
    }
    addTearDown(tester.view.reset);
    addTearDown(() => tester.binding.setSurfaceSize(null));
  });
}
