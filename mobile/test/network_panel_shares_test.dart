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

Map<String, dynamic> _validator(String lock, {bool jailed = false}) => {
      'description': {'moniker': 'pc'},
      'tokens': lock,
      'jailed': jailed,
    };

void main() {
  testWidgets('the bars show the share of the new LOCK: 5% each with 20 on, every window size', (tester) async {
    SharedPreferences.setMockInitialValues({});
    // A LockKeeper on this computer that answers like the network would
    // with the founder's 200 LOCK of guarantee and 19 computers with 3,33.
    HttpOverrides.global = null;
    final server = (await tester.runAsync(() => HttpServer.bind(InternetAddress.loopbackIPv4, 0)))!;
    server.listen((req) {
      final body = req.uri.path.contains('validators')
          ? {'validators': [_validator('20000000000'), for (var i = 0; i < 19; i++) _validator('333333334')]}
          : {'amount': {'amount': '43900000000000'}};
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
        await (FontLoader('Montserrat')
              ..addFont(Future.value(ByteData.sublistView(File('assets/fonts/Montserrat-Bold.ttf').readAsBytesSync()))))
            .load();
      });
    }
    final state = AppState(core: PreviewCore())..lockKeeperUrl = 'http://127.0.0.1:${server.port}';
    const sizes = {'normal': 1264.0, 'maximizada': 1920.0, 'estreita': 760.0, 'telemovel': 400.0};
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
      for (var i = 0; i < 20 && find.text('5.0%').evaluate().isEmpty; i++) {
        await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 100)));
        await tester.pump();
      }
      expect(tester.takeException(), isNull, reason: e.key);
      expect(find.text('5.0%'), findsNWidgets(20), reason: e.key);
      expect(find.textContaining('ninguém recebe mais de 5%'), findsOneWidget);
      if (_shots != null) {
        await tester.runAsync(() async {
          final image = await (key.currentContext!.findRenderObject()! as RenderRepaintBoundary).toImage();
          final png = await image.toByteData(format: ui.ImageByteFormat.png);
          File('$_shots/painel-${e.key}.png').writeAsBytesSync(png!.buffer.asUint8List());
        });
      }
    }
    await tester.pumpWidget(const SizedBox());
    addTearDown(tester.view.reset);
    addTearDown(() => tester.binding.setSurfaceSize(null));
  });
}
