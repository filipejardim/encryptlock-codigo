import 'dart:io';
import 'dart:ui' as ui;

import 'package:encryptlock_wallet/core/network.dart' show LockKeeperRotation;
import 'package:encryptlock_wallet/l10n/gen/app_localizations.dart';
import 'package:encryptlock_wallet/screens/pc_lockkeeper_card.dart';
import 'package:encryptlock_wallet/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

/// With SHOTS=(folder) the lights are also saved as pictures (Windows fonts).
final _shots = Platform.environment['SHOTS'];

LockKeeperRotation _r({required bool online, required bool signing}) =>
    LockKeeperRotation(online: online, signing: signing, today: 3600, yesterday: 0, week: 3600, month: 3600);

void main() {
  testWidgets('the LockKeeper lights: never "signing" while catching up, every window size', (tester) async {
    if (_shots != null) {
      await tester.runAsync(() async {
        for (final f in [r'C:\Windows\Fonts\segoeui.ttf', r'C:\Windows\Fonts\segoeuib.ttf']) {
          await (FontLoader('Roboto')..addFont(Future.value(ByteData.sublistView(File(f).readAsBytesSync())))).load();
        }
      });
    }
    final cases = {
      'a-assinar': PcLockKeeperLights(rotation: _r(online: true, signing: true)),
      'a-espera': PcLockKeeperLights(rotation: _r(online: true, signing: false)),
      'desligado': PcLockKeeperLights(rotation: _r(online: false, signing: false)),
      'atrasado': PcLockKeeperLights(rotation: _r(online: true, signing: true), behind: 1200),
    };
    for (final width in [1264.0, 760.0, 400.0]) {
      final size = Size(width, 700);
      await tester.binding.setSurfaceSize(size);
      tester.view.physicalSize = size;
      tester.view.devicePixelRatio = 1;
      final key = GlobalKey();
      await tester.pumpWidget(RepaintBoundary(
        key: key,
        child: MaterialApp(
          theme: buildTheme(),
          locale: const Locale('pt'),
          supportedLocales: AppLocalizations.supportedLocales,
          localizationsDelegates: AppLocalizations.localizationsDelegates,
          home: Scaffold(
            body: ListView(padding: const EdgeInsets.all(16), children: [
              for (final e in cases.entries) ...[Text(e.key, style: const TextStyle(color: Brand.pearl)), e.value],
            ]),
          ),
        ),
      ));
      await tester.pump();
      expect(tester.takeException(), isNull);
      // Signing shows only when up to date: twice (a-assinar), never for "atrasado".
      expect(find.text('A assinar blocos agora'), findsOneWidget);
      expect(find.textContaining('Espera 5 minutos'), findsOneWidget);
      // Off: only the red light, no "waiting for its turn".
      expect(find.textContaining('à espera da vez'), findsOneWidget);
      if (_shots != null) {
        await tester.runAsync(() async {
          final image = await (key.currentContext!.findRenderObject()! as RenderRepaintBoundary).toImage();
          final png = await image.toByteData(format: ui.ImageByteFormat.png);
          File('$_shots/luzes-${width.toInt()}.png').writeAsBytesSync(png!.buffer.asUint8List());
        });
      }
    }
    await tester.pumpWidget(const SizedBox());
    addTearDown(tester.view.reset);
    addTearDown(() => tester.binding.setSurfaceSize(null));
  });
}
