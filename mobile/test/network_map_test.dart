import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;

import 'package:encryptlock_wallet/app_state.dart';
import 'package:encryptlock_wallet/core/core.dart';
import 'package:encryptlock_wallet/l10n/gen/app_localizations.dart';
import 'package:encryptlock_wallet/theme.dart';
import 'package:encryptlock_wallet/widgets/network_map.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// With SHOTS=(folder) the map is also saved as pictures (Windows fonts).
final _shots = Platform.environment['SHOTS'];

void main() {
  for (final n in [10, 300]) {
    testWidgets('the network map draws $n LockKeepers at every window size', (tester) async {
      SharedPreferences.setMockInitialValues({});
      final validators = [
        for (var i = 0; i < n; i++)
          {'operator_address': 'lockvaloper1k$i', 'description': {'moniker': 'pc'}, 'tokens': '${300000000 + i}', 'jailed': false},
      ];
      final ops = [for (final v in validators) v['operator_address']];
      HttpOverrides.global = null;
      final server = (await tester.runAsync(() => HttpServer.bind(InternetAddress.loopbackIPv4, 0)))!;
      server.listen((req) {
        final path = req.uri.path;
        final body = path.contains('validators')
            ? {'validators': validators}
            : path.contains('overview')
                ? {
                    'online_keepers': [for (var i = 0; i < n; i++) if (i % 5 != 1) ops[i]],
                    'signing_keepers': [for (var i = 0; i < n; i++) if (i % 5 > 2) ops[i]],
                  }
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
      // The card's width in the app: computer left column (normal, maximised,
      // narrow window) and phone.
      const sizes = {'normal': 640.0, 'maximizada': 1000.0, 'estreita': 720.0, 'telemovel': 360.0};
      for (final e in sizes.entries) {
        final size = Size(e.value + 32, 520);
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
              home: const Scaffold(body: Padding(padding: EdgeInsets.all(16), child: NetworkMapCard())),
            ),
          ),
        ));
        for (var i = 0; i < 30 && find.text('$n LockKeepers').evaluate().isEmpty; i++) {
          await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 100)));
          await tester.pump(const Duration(milliseconds: 100));
        }
        await tester.pump(const Duration(seconds: 1));
        expect(tester.takeException(), isNull, reason: e.key);
        expect(find.text('$n LockKeepers'), findsOneWidget, reason: e.key);
        expect(find.text('MAPA DA REDE'), findsOneWidget);
        // Dragging turns it; the wheel and the buttons zoom; nothing breaks.
        await tester.drag(find.byType(NetworkMap), const Offset(80, 30));
        await tester.tap(find.byIcon(Icons.add));
        await tester.pump(const Duration(milliseconds: 200));
        await tester.tap(find.byIcon(Icons.remove));
        await tester.pump(const Duration(milliseconds: 200));
        expect(tester.takeException(), isNull, reason: e.key);
        if (_shots != null) {
          await tester.runAsync(() async {
            final image = await (key.currentContext!.findRenderObject()! as RenderRepaintBoundary).toImage();
            final png = await image.toByteData(format: ui.ImageByteFormat.png);
            File('$_shots/mapa-$n-${e.key}.png').writeAsBytesSync(png!.buffer.asUint8List());
          });
        }
      }
      await tester.pumpWidget(const SizedBox());
      addTearDown(tester.view.reset);
      addTearDown(() => tester.binding.setSurfaceSize(null));
    });
  }
}
