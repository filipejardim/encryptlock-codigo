import 'package:flutter/material.dart';

/// EncryptLock colours, measured on the logo (design/logo-encryptlock-banner.png):
/// a dark circuit-board chip, mint-green neon lines and a silver padlock.
class Brand {
  /// Chip background (outside the panel) and the panel itself.
  static const background = Color(0xFF0E1619);
  static const panel = Color(0xFF10171F);
  static const barStart = Color(0xFF15252A);
  static const barEnd = Color(0xFF0B1214);
  static const card = Color(0xFF122025);
  static const cardBorder = Color(0xFF2E5A4C);
  static const bubble = Color(0xFF16282A);
  static const bottomBar = Color(0xFF0B1316);

  /// Mint neon of the frame line and the padlock edges.
  static const neonGreen = Color(0xFF94F7D1);

  /// The letters of ENCRYPTLOCK.
  static const mintText = Color(0xFFABF0C0);

  /// Second accent: the silver of the padlock (was blue before the new logo).
  static const neonBlue = Color(0xFFB9D3C6);

  /// Circuit traces on the background.
  static const trace = Color(0xFF1C3134);
  static const traceLight = Color(0xFF58A891);

  static const text = Colors.white;

  /// Pearl white: labels and secondary text (founder: no grey text).
  static const pearl = Color(0xFFF4F1E8);
  static const danger = Color(0xFFFF5C6C);

  /// Top bar: dark chip colours, left to right.
  static const barGradient = LinearGradient(colors: [barStart, barEnd]);

  /// Buttons: dark green-teal going darker, top to bottom.
  static const buttonGradient = LinearGradient(
    begin: Alignment.topCenter,
    end: Alignment.bottomCenter,
    colors: [Color(0xFF1F4A3E), Color(0xFF0F2420)],
  );

  /// Silver going to mint (the padlock): big numbers like the balance.
  static const blueGradient = LinearGradient(colors: [Color(0xFFE6F1EB), Color(0xFF8FD9B4)]);

  /// Soft neon glow around panels and the logo.
  static List<BoxShadow> glow([Color color = neonGreen, double strength = 1]) => [
        BoxShadow(color: color.withValues(alpha: 0.28 * strength), blurRadius: 16, spreadRadius: 0),
        BoxShadow(color: color.withValues(alpha: 0.12 * strength), blurRadius: 36, spreadRadius: 2),
      ];
}

ThemeData buildTheme() {
  final base = ThemeData.dark(useMaterial3: true);
  OutlineInputBorder border(Color color, [double width = 1]) => OutlineInputBorder(
        borderRadius: BorderRadius.circular(8),
        borderSide: BorderSide(color: color, width: width),
      );
  return base.copyWith(
    scaffoldBackgroundColor: Brand.background,
    colorScheme: base.colorScheme.copyWith(
      primary: Brand.neonGreen,
      secondary: Brand.neonBlue,
      surface: Brand.card,
      error: Brand.danger,
    ),
    textTheme: base.textTheme.apply(bodyColor: Brand.text, displayColor: Brand.text),
    appBarTheme: const AppBarTheme(
      backgroundColor: Colors.transparent,
      elevation: 0,
      centerTitle: true,
      foregroundColor: Brand.text,
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: Brand.card.withValues(alpha: 0.85),
      labelStyle: const TextStyle(color: Brand.pearl),
      hintStyle: const TextStyle(color: Brand.pearl),
      enabledBorder: border(Brand.cardBorder),
      disabledBorder: border(Brand.cardBorder.withValues(alpha: 0.5)),
      focusedBorder: border(Brand.neonGreen, 1.5),
      errorBorder: border(Brand.danger),
      focusedErrorBorder: border(Brand.danger, 1.5),
    ),
    snackBarTheme: const SnackBarThemeData(
      backgroundColor: Brand.card,
      contentTextStyle: TextStyle(color: Brand.text),
    ),
  );
}
