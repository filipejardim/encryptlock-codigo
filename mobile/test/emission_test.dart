import 'dart:io';
import 'dart:ui' as ui;

import 'package:encryptlock_wallet/app_state.dart';
import 'package:encryptlock_wallet/core/core.dart';
import 'package:encryptlock_wallet/l10n/gen/app_localizations.dart';
import 'package:encryptlock_wallet/screens/emission.dart';
import 'package:encryptlock_wallet/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// The "Coin release" page uses the same rule as the network: 1.000 at the
/// start (1.000 + 400.000 for the creator), 38.799.000 over 30 years, halving
/// every 5 years, and the everlasting reserve of 800.000, half of what is left
/// every 30 years.
void main() {
  test('the 30 years and the reserve add up to 40.000.000 with the start', () {
    final total = EmissionSchedule.genesisLock + EmissionSchedule.years(1, 30) + EmissionSchedule.reserveLock;
    expect(total, closeTo(40000000, 1));
  });

  test('each 5 years release half of the 5 before', () {
    for (var p = 1; p < 6; p++) {
      final before = EmissionSchedule.years((p - 1) * 5 + 1, p * 5);
      final after = EmissionSchedule.years(p * 5 + 1, p * 5 + 5);
      expect(after / before, closeTo(0.5, 1e-9));
    }
  });

  test('first month as in the white paper (about 452.718 LOCK)', () {
    expect(EmissionSchedule.month(1), closeTo(452718, 1));
  });

  test('the reserve: 400.000 in years 31â€“60, then half each time, every period down to the last LOCK', () {
    expect(EmissionSchedule.reservePeriod(0), 400000);
    expect(EmissionSchedule.reservePeriod(1), 200000);
    var units = 0;
    for (var k = 0; k < EmissionSchedule.reserveRows; k++) {
      units += EmissionSchedule.reserveUnitsIn(k);
    }
    expect(units, EmissionSchedule.reserveUnits); // exactly 800.000 LOCK
    expect(EmissionSchedule.reserveUnitsIn(EmissionSchedule.reserveRows - 1), 1); // the last: 0,00000001 LOCK
    expect(EmissionSchedule.lastLockYears, 1440);
    // Year 31 comes from the reserve.
    expect(EmissionSchedule.month(361), closeTo(400000 / 360, 1e-6));
  });

  testWidgets('the page with the reserve fits every window size', (tester) async {
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
    const sizes = {'telemovel': Size(400, 2000), 'normal': Size(1264, 1500)};
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
            home: const EmissionScreen(),
          ),
        ),
      ));
      await tester.pump(const Duration(milliseconds: 300));
      expect(tester.takeException(), isNull, reason: e.key);
      expect(find.textContaining('3466'), findsWidgets);
      if (shots != null) {
        await tester.runAsync(() async {
          final image = await (key.currentContext!.findRenderObject()! as RenderRepaintBoundary).toImage();
          final png = await image.toByteData(format: ui.ImageByteFormat.png);
          File('$shots/libertacao-${e.key}.png').writeAsBytesSync(png!.buffer.asUint8List());
        });
      }
    }
    addTearDown(tester.view.reset);
    addTearDown(() => tester.binding.setSurfaceSize(null));
  });
}

