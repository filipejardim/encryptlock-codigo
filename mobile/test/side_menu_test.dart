import 'dart:io';
import 'dart:ui' as ui;

import 'package:encryptlock_wallet/app_state.dart';
import 'package:encryptlock_wallet/core/core.dart';
import 'package:encryptlock_wallet/l10n/gen/app_localizations.dart';
import 'package:encryptlock_wallet/screens/home.dart';
import 'package:encryptlock_wallet/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// The computer's side menu: help links and Padlock fit every window size
/// (pictures with SHOTS=folder).
void main() {
  testWidgets('Padlock is in the side menu under the help links, at every computer window size', (tester) async {
    SharedPreferences.setMockInitialValues({});
    final shots = Platform.environment['SHOTS'];
    if (shots != null) {
      await tester.runAsync(() async {
        for (final f in [r'C:\Windows\Fonts\segoeui.ttf', r'C:\Windows\Fonts\segoeuib.ttf']) {
          if (File(f).existsSync()) {
            await (FontLoader('Roboto')..addFont(Future.value(ByteData.sublistView(File(f).readAsBytesSync())))).load();
          }
        }
        await (FontLoader('Montserrat')
              ..addFont(Future.value(ByteData.sublistView(File('assets/fonts/Montserrat-Bold.ttf').readAsBytesSync()))))
            .load();
      });
    }
    final state = AppState(core: PreviewCore());
    await tester.runAsync(() => state.open('preview'));
    const sizes = {'normal': Size(1264, 780), 'maximizado': Size(1920, 1000), 'baixa': Size(1100, 600), 'muito-baixa': Size(1000, 480)};
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
            home: const HomeScreen(),
          ),
        ),
      ));
      await tester.pump(const Duration(seconds: 1));
      if (shots != null) {
        await tester.runAsync(() async {
          final image = await (key.currentContext!.findRenderObject()! as RenderRepaintBoundary).toImage();
          final png = await image.toByteData(format: ui.ImageByteFormat.png);
          File('$shots/menu-${e.key}.png').writeAsBytesSync(png!.buffer.asUint8List());
        });
      }
      expect(tester.takeException(), isNull, reason: e.key);
      expect(find.text('Padlock · comunicação encriptada'), findsOneWidget, reason: e.key);
      // In a low window the menu scrolls to it.
      await tester.ensureVisible(find.text('Padlock · comunicação encriptada'));
      await tester.pump();
      await tester.tap(find.text('Padlock · comunicação encriptada'));
      await tester.pump();
      expect(find.textContaining('Em breve na Google Play'), findsOneWidget, reason: e.key);
      await tester.pump(const Duration(seconds: 5)); // the notice goes away
    }
    addTearDown(tester.view.reset);
    addTearDown(() => tester.binding.setSurfaceSize(null));
  });
}
