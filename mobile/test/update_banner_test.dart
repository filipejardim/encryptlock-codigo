import 'dart:io';
import 'dart:ui' as ui;

import 'package:encryptlock_wallet/app_state.dart';
import 'package:encryptlock_wallet/core/core.dart';
import 'package:encryptlock_wallet/l10n/gen/app_localizations.dart';
import 'package:encryptlock_wallet/screens/home.dart' show UpdateBanner;
import 'package:encryptlock_wallet/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// With SHOTS=(folder) the banner is also saved as pictures (Windows fonts).
final _shots = Platform.environment['SHOTS'];

void main() {
  testWidgets('new version: the banner with Update, and the download progress, at every window size', (tester) async {
    SharedPreferences.setMockInitialValues({});
    if (_shots != null) {
      await tester.runAsync(() async {
        for (final (family, file) in [
          ('Roboto', r'C:\Windows\Fonts\segoeui.ttf'),
          ('Roboto', r'C:\Windows\Fonts\segoeuib.ttf'),
          ('MaterialIcons', r'C:\src\flutter\bin\cache\artifacts\material_fonts\MaterialIcons-Regular.otf'),
        ]) {
          if (File(file).existsSync()) {
            await (FontLoader(family)..addFont(Future.value(ByteData.sublistView(File(file).readAsBytesSync())))).load();
          }
        }
      });
    }
    final state = AppState(core: PreviewCore())..newVersion = '1.0.1';
    const sizes = {'normal': Size(1264, 780), 'maximizado': Size(1920, 1000), 'estreito': Size(760, 700), 'telemovel': Size(400, 820)};
    for (final progress in [null, 0.42]) {
      state.updateProgress = progress;
      for (final e in sizes.entries) {
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
              home: const Scaffold(body: Padding(padding: EdgeInsets.all(20), child: UpdateBanner())),
            ),
          ),
        ));
        await tester.pump();
        expect(tester.takeException(), isNull, reason: e.key);
        expect(find.text('🆕 Nova versão 1.0.1 disponível'), findsOneWidget);
        expect(find.text('Atualizar'), progress == null ? findsOneWidget : findsNothing);
        if (progress != null) expect(find.text('A descarregar a versão nova… 42%'), findsOneWidget);
        if (_shots != null) {
          await tester.runAsync(() async {
            final image = await (key.currentContext!.findRenderObject()! as RenderRepaintBoundary).toImage();
            final png = await image.toByteData(format: ui.ImageByteFormat.png);
            File('$_shots/atualizar-${progress == null ? 'botao' : 'progresso'}-${e.key}.png').writeAsBytesSync(png!.buffer.asUint8List());
          });
        }
      }
    }
    addTearDown(tester.view.reset);
  });
}
