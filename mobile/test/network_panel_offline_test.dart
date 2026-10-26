import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;

import 'package:encryptlock_wallet/app_state.dart';
import 'package:encryptlock_wallet/core/core.dart';
import 'package:encryptlock_wallet/l10n/gen/app_localizations.dart';
import 'package:encryptlock_wallet/screens/network_panel.dart';
import 'package:encryptlock_wallet/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// With SHOTS=(folder) the panel is also saved as pictures (Windows fonts).
final _shots = Platform.environment['SHOTS'];

void main() {
  testWidgets('every LockKeeper is listed; the ones switched off are orange and earn 0%', (tester) async {
    SharedPreferences.setMockInitialValues({});
    // The network on 26 Oct 2026: 10 with a guarantee, 8 switched on.
    final guarantees = ['20000000000', '1000000000', '840000000', '840000000', '500000000', '333000000', '333000000', '333000000', '278000000', '278000000'];
    final validators = [
      for (final (i, g) in guarantees.indexed) {'operator_address': 'lockvaloper1k$i', 'description': {'moniker': 'pc'}, 'tokens': g, 'jailed': false},
    ];
    const off = {'lockvaloper1k1', 'lockvaloper1k3'};
    HttpOverrides.global = null;
    final server = (await tester.runAsync(() => HttpServer.bind(InternetAddress.loopbackIPv4, 0)))!;
    server.listen((req) {
      final path = req.uri.path;
      final body = path.contains('validators')
          ? {'validators': validators}
          : path.contains('overview')
              ? {'online': '8', 'signing': '8', 'seats': '100', 'online_keepers': [for (final v in validators) if (!off.contains(v['operator_address'])) v['operator_address']]}
              : {'amount': {'amount': '51944400000000'}};
      req.response
        ..headers.contentType = ContentType.json
        ..write(jsonEncode(body))
        ..close();
    });
    addTearDown(() => server.close(force: true));
    if (_shots != null) {
      await tester.runAsync(() async {
        for (final f in [r'C:\Windows\Fonts\segoeui.ttf', r'C:\Windows\Fonts\segoeuib.ttf']) {
          await (FontLoader('Roboto')..addFont(Future.value(ByteData.sublistView(File(f).readAsBytesSync())))).load();
        }
      });
    }
    final state = AppState(core: PreviewCore())..lockKeeperUrl = 'http://127.0.0.1:${server.port}';
    const sizes = {'normal': 1264.0, 'telemovel': 400.0};
    for (final e in sizes.entries) {
      final size = Size(e.value, 1100);
      await tester.binding.setSurfaceSize(size);
      tester.view.physicalSize = size;
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
            home: const Scaffold(body: SingleChildScrollView(padding: EdgeInsets.all(16), child: NetworkPanel())),
          ),
        ),
      ));
      for (var i = 0; i < 20 && find.text('0.0%').evaluate().isEmpty; i++) {
        await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 100)));
        await tester.pump();
      }
      expect(tester.takeException(), isNull, reason: e.key);
      expect(find.text('10'), findsOneWidget, reason: '${e.key}: all 10 counted');
      expect(find.text('0.0%'), findsNWidgets(2), reason: '${e.key}: the 2 switched off earn nothing');
      final orange = find.byWidgetPredicate((w) => w is Container && (w.decoration as BoxDecoration?)?.color == Brand.orange);
      expect(orange, findsNWidgets(2), reason: '${e.key}: the 2 switched off are orange');
      if (_shots != null) {
        await tester.runAsync(() async {
          final image = await (key.currentContext!.findRenderObject()! as RenderRepaintBoundary).toImage();
          final png = await image.toByteData(format: ui.ImageByteFormat.png);
          File('$_shots/painel-desligados-${e.key}.png').writeAsBytesSync(png!.buffer.asUint8List());
        });
      }
    }
    await tester.pumpWidget(const SizedBox());
    addTearDown(tester.view.reset);
    addTearDown(() => tester.binding.setSurfaceSize(null));
  });
}
