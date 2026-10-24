import 'dart:io';
import 'dart:ui' as ui;

import 'package:encryptlock_wallet/app_state.dart';
import 'package:encryptlock_wallet/core/core.dart';
import 'package:encryptlock_wallet/l10n/gen/app_localizations.dart';
import 'package:encryptlock_wallet/screens/home.dart';
import 'package:encryptlock_wallet/theme.dart';
import 'package:encryptlock_wallet/widgets/coin_carousel.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// Phone: the coin ring leaves the list while scrolling down; on the way up
/// the coin in front is still the chosen one (it used to come back as
/// EncryptLock, with Polygon's name and balance under it).
void main() {
  testWidgets('the chosen coin stays in front after scrolling down and up on a phone', (tester) async {
    SharedPreferences.setMockInitialValues({});
    // The phone's real letters (the test letters are wider than any font).
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
    final shots = Platform.environment['SHOTS'];
    final key = GlobalKey();
    Future<void> shot(String name) async {
      if (shots == null) return;
      await tester.runAsync(() async {
        final image = await (key.currentContext!.findRenderObject()! as RenderRepaintBoundary).toImage();
        final png = await image.toByteData(format: ui.ImageByteFormat.png);
        File('$shots/telemovel-$name.png').writeAsBytesSync(png!.buffer.asUint8List());
      });
    }

    final state = AppState(core: PreviewCore());
    await tester.runAsync(() => state.open('preview'));
    await tester.binding.setSurfaceSize(const Size(400, 820));
    tester.view.physicalSize = const Size(400, 820);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    addTearDown(() => tester.binding.setSurfaceSize(null));
    await tester.pumpWidget(
      RepaintBoundary(
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
      ),
    );
    await tester.pump(const Duration(seconds: 1));

    // The coin drawn last in the ring is the one in front; only EncryptLock's
    // has a picture.
    bool lockInFront() {
      final stack = tester.widget<Stack>(
        find.descendant(of: find.byType(CoinCarousel), matching: find.byType(Stack)).first,
      );
      return find
          .descendant(of: find.byWidget(stack.children.last), matching: find.byType(Image))
          .evaluate()
          .isNotEmpty;
    }

    expect(lockInFront(), isTrue);
    await tester.drag(find.byType(CoinCarousel), const Offset(-120, 0));
    await tester.pump(const Duration(seconds: 1));
    final chosen = find.text('Polygon').evaluate().isNotEmpty ? 'Polygon' : 'USD Coin';
    expect(find.text(chosen), findsWidgets);
    expect(lockInFront(), isFalse);
    await shot('1-escolhida');

    final list = find.byType(ListView).first;
    for (var i = 0; i < 6; i++) {
      await tester.drag(list, const Offset(0, -600));
      await tester.pump(const Duration(milliseconds: 300));
    }
    expect(find.byType(CoinCarousel), findsNothing, reason: 'the ring left the list');
    for (var i = 0; i < 8; i++) {
      await tester.drag(list, const Offset(0, 600));
      await tester.pump(const Duration(milliseconds: 300));
    }
    await tester.pump(const Duration(seconds: 1));
    expect(find.text(chosen), findsWidgets);
    await shot('2-depois-do-scroll');
    expect(lockInFront(), isFalse, reason: '$chosen is still in front');
    await tester.pump(const Duration(seconds: 5));
  });
}
