import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../l10n/gen/app_localizations.dart';
import '../theme.dart';

/// Every screen sits on a dark circuit board, like the logo's background:
/// tidy bundles of parallel traces that run in from the sides, bend at 45°
/// and end in pads, plus outlined chip blocks. Left and right mirror each
/// other and nothing crosses. Drawn by the app, so it is sharp everywhere.
class ChipBackground extends StatelessWidget {
  const ChipBackground({super.key, required this.child});

  final Widget child;

  @override
  Widget build(BuildContext context) {
    return Stack(
      fit: StackFit.expand,
      children: [
        const RepaintBoundary(child: CustomPaint(painter: _ChipPainter())),
        SafeArea(child: child),
      ],
    );
  }
}

class _ChipPainter extends CustomPainter {
  const _ChipPainter();

  /// Offsets a polyline sideways by d (miter joins), so a bundle of
  /// traces keeps the same spacing through the 45° bends.
  static List<Offset> _offset(List<Offset> p, double d) {
    Offset normal(Offset a, Offset b) {
      final v = b - a;
      return Offset(-v.dy, v.dx) / v.distance;
    }

    final out = <Offset>[];
    for (var i = 0; i < p.length; i++) {
      if (i == 0) {
        out.add(p[0] + normal(p[0], p[1]) * d);
      } else if (i == p.length - 1) {
        out.add(p[i] + normal(p[i - 1], p[i]) * d);
      } else {
        final n1 = normal(p[i - 1], p[i]), n2 = normal(p[i], p[i + 1]);
        final m = n1 + n2;
        final k = 1 + n1.dx * n2.dx + n1.dy * n2.dy;
        out.add(p[i] + m * (d / k));
      }
    }
    return out;
  }

  @override
  void paint(Canvas canvas, Size size) {
    final w = size.width, h = size.height;
    final rect = Offset.zero & size;
    canvas.drawRect(
      rect,
      Paint()
        ..shader = const LinearGradient(
          begin: Alignment.topCenter,
          end: Alignment.bottomCenter,
          colors: [Color(0xFF122024), Brand.background, Color(0xFF0A1113)],
          stops: [0, 0.45, 1],
        ).createShader(rect),
    );

    final dim = Paint()
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1.1
      ..color = Brand.trace;
    final lit = Paint()
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1.3
      ..color = Brand.traceLight.withValues(alpha: 0.55);
    final glow = Paint()
      ..style = PaintingStyle.stroke
      ..strokeWidth = 5
      ..color = Brand.neonGreen.withValues(alpha: 0.10)
      ..maskFilter = const MaskFilter.blur(BlurStyle.normal, 4);

    // Everything is drawn for the left half and mirrored to the right.
    void both(void Function(bool right) draw) {
      draw(false);
      canvas.save();
      canvas.translate(w, 0);
      canvas.scale(-1, 1);
      draw(true);
      canvas.restore();
    }

    const gap = 7.0;
    final band = h / 10;
    // One bundle per band: [lines, run, bend (in bands), end x, bright].
    final bundles = <(int, double, double, double, bool)>[
      (4, 0.07, 0.30, 0.24, false),
      (3, 0.11, -0.28, 0.21, false),
      (5, 0.05, 0.32, 0.26, true),
      (3, 0.13, -0.30, 0.22, false),
      (4, 0.08, 0.28, 0.25, false),
      (2, 0.12, -0.25, 0.20, false),
      (4, 0.06, 0.30, 0.25, true),
      (3, 0.10, -0.28, 0.22, false),
      (5, 0.07, 0.26, 0.26, false),
    ];

    both((right) {
      for (var b = 0; b < bundles.length; b++) {
        final (n, run, bend, endX, bright) = bundles[b];
        // Mirror image is not identical: the right side uses the next band's
        // pattern, so the two sides look balanced but not copied.
        final (nn, rr, bb, ee, br) = right ? bundles[(b + 4) % bundles.length] : (n, run, bend, endX, bright);
        final y0 = band * (b + 0.75) - (bb < 0 ? 0 : band * bb * 0.5);
        final x1 = w * rr;
        final dy = band * bb;
        final centre = [Offset(-8, y0), Offset(x1, y0), Offset(x1 + dy.abs(), y0 + dy), Offset(w * ee, y0 + dy)];
        for (var i = 0; i < nn; i++) {
          final line = _offset(centre, (i - (nn - 1) / 2) * gap);
          final path = Path()..moveTo(line[0].dx, line[0].dy);
          for (final p in line.skip(1)) {
            path.lineTo(p.dx, p.dy);
          }
          if (br) canvas.drawPath(path, glow);
          canvas.drawPath(path, br ? lit : dim);
          // Pads, staggered like on a real board.
          final end = line.last + Offset(i.isEven ? 0 : 9, 0);
          if (i.isOdd) canvas.drawLine(line.last, end, br ? lit : dim);
          canvas.drawCircle(end + const Offset(2.6, 0), 2.6, br ? lit : dim);
        }
      }

      // Outlined chip blocks with cut corners, between the bundles.
      for (final (top, height, left, width) in [
        (2.15, 0.45, 0.03, 0.15),
        (5.10, 0.50, 0.03, 0.13),
        (8.05, 0.45, 0.03, 0.16),
      ]) {
        final r = Rect.fromLTWH(w * left, band * top, w * width, band * height);
        canvas.drawPath(chipPath(r, 8), dim);
        canvas.drawPath(chipPath(r.deflate(4), 5), dim..color = Brand.trace.withValues(alpha: 0.6));
        dim.color = Brand.trace;
      }
    });

    // Mint light from the top, as in the logo.
    canvas.drawRect(
      rect,
      Paint()
        ..shader = RadialGradient(
          center: const Alignment(0, -1.15),
          radius: 0.9,
          colors: [Brand.traceLight.withValues(alpha: 0.28), Brand.traceLight.withValues(alpha: 0)],
        ).createShader(rect),
    );
  }

  @override
  bool shouldRepaint(covariant CustomPainter oldDelegate) => false;
}

/// The panel shape of the logo: a rectangle with cut corners.
Path chipPath(Rect r, double cut) => Path()
  ..moveTo(r.left + cut, r.top)
  ..lineTo(r.right - cut, r.top)
  ..lineTo(r.right, r.top + cut)
  ..lineTo(r.right, r.bottom - cut)
  ..lineTo(r.right - cut, r.bottom)
  ..lineTo(r.left + cut, r.bottom)
  ..lineTo(r.left, r.bottom - cut)
  ..lineTo(r.left, r.top + cut)
  ..close();

/// Draws the logo's frame: dark panel, metal edge, and an inner neon line
/// that is brightest on top.
class _FramePainter extends CustomPainter {
  const _FramePainter({required this.accent, required this.cut, required this.fill, this.glow = 1});

  final Color accent;
  final double cut;
  final Gradient fill;
  final double glow;

  @override
  void paint(Canvas canvas, Size size) {
    final r = Offset.zero & size;
    final outer = chipPath(r.deflate(0.5), cut);
    if (glow > 0) {
      canvas.drawPath(
        outer,
        Paint()
          ..style = PaintingStyle.stroke
          ..strokeWidth = 6
          ..color = accent.withValues(alpha: 0.22 * glow)
          ..maskFilter = const MaskFilter.blur(BlurStyle.normal, 9),
      );
    }
    canvas.drawPath(outer, Paint()..shader = fill.createShader(r));
    canvas.drawPath(
      outer,
      Paint()
        ..style = PaintingStyle.stroke
        ..strokeWidth = 1.3
        ..color = const Color(0xFF34514A),
    );
    final inner = r.deflate(4);
    canvas.drawPath(
      chipPath(inner, math.max(0, cut - 3)),
      Paint()
        ..style = PaintingStyle.stroke
        ..strokeWidth = 1.2
        ..shader = LinearGradient(
          begin: Alignment.topCenter,
          end: Alignment.bottomCenter,
          colors: [accent, accent.withValues(alpha: 0.35), accent.withValues(alpha: 0.55)],
          stops: const [0, 0.6, 1],
        ).createShader(inner),
    );
    // Bright top line, like the lit edge of the logo's frame.
    canvas.drawLine(
      Offset(inner.left + cut, inner.top),
      Offset(inner.right - cut, inner.top),
      Paint()
        ..strokeWidth = 2
        ..color = accent.withValues(alpha: 0.9)
        ..maskFilter = const MaskFilter.blur(BlurStyle.normal, 1.5),
    );
  }

  @override
  bool shouldRepaint(covariant _FramePainter old) => old.accent != accent || old.glow != glow;
}

const _panelFill = LinearGradient(
  begin: Alignment.topCenter,
  end: Alignment.bottomCenter,
  colors: [Color(0xF2152429), Color(0xF20F181D)],
);

/// The logo: the square padlock tile inside the chip frame.
class NeonLogo extends StatelessWidget {
  const NeonLogo({super.key, this.size = 150});

  final double size;

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      width: size,
      height: size,
      child: CustomPaint(
        painter: const _FramePainter(accent: Brand.neonGreen, cut: 14, fill: _panelFill),
        child: Padding(
          padding: const EdgeInsets.all(5),
          child: ClipPath(
            clipper: _ChipClipper(11),
            child: Image.asset('assets/icon.png', fit: BoxFit.cover),
          ),
        ),
      ),
    );
  }
}

/// The full logo (padlock + ENCRYPTLOCK in its frame), for first screens.
class LogoBanner extends StatelessWidget {
  const LogoBanner({super.key, this.width = 320});

  final double width;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: width,
      decoration: BoxDecoration(boxShadow: Brand.glow(Brand.neonGreen, 0.6)),
      child: ClipPath(
        clipper: _ChipClipper(12),
        child: Image.asset('assets/logo-banner.png', fit: BoxFit.cover),
      ),
    );
  }
}

class _ChipClipper extends CustomClipper<Path> {
  const _ChipClipper(this.cut);

  final double cut;

  @override
  Path getClip(Size size) => chipPath(Offset.zero & size, cut);

  @override
  bool shouldReclip(covariant _ChipClipper old) => old.cut != cut;
}

/// "ENCRYPTLOCK" exactly as in the logo (the glowing letters themselves).
class BrandTitle extends StatelessWidget {
  const BrandTitle({super.key, this.size = 26});

  /// Height of the letters.
  final double size;

  @override
  Widget build(BuildContext context) => Image.asset('assets/wordmark.png', height: size * 1.25);
}

/// ENCRYPTLOCK with a spotlight above it: a soft mint glow from the top and
/// a brighter reflection on the upper part of the letters.
class LitTitle extends StatelessWidget {
  const LitTitle({super.key, this.size = 26});

  final double size;

  @override
  Widget build(BuildContext context) {
    return Stack(
      clipBehavior: Clip.none,
      alignment: Alignment.center,
      children: [
        _Spotlight(size),
        BrandTitle(size: size),
        // Reflection on the top half of the letters.
        ShaderMask(
          blendMode: BlendMode.srcATop,
          shaderCallback: (r) => LinearGradient(
            begin: Alignment.topCenter,
            end: Alignment.bottomCenter,
            colors: [Colors.white.withValues(alpha: 0.75), Colors.white.withValues(alpha: 0)],
            stops: const [0, 0.55],
          ).createShader(r),
          child: BrandTitle(size: size),
        ),
      ],
    );
  }
}

/// Any title lit like ENCRYPTLOCK: the logo's letters (Montserrat, wide)
/// drawn as a neon tube - a vivid green line with a light core around a
/// darker, see-through sage inside - plus the glow, the light cone from
/// above and the reflection on top. Colours measured on the logo.
class LitText extends StatelessWidget {
  const LitText(this.text, {super.key, this.size = 26});

  final String text;
  final double size;

  @override
  Widget build(BuildContext context) {
    final base = TextStyle(
      fontFamily: 'Montserrat',
      fontWeight: FontWeight.w700,
      fontSize: size,
      letterSpacing: size * 0.03,
      height: 1.1,
    );
    Widget line(double width, Color color, {double blur = 0}) => Text(
      text,
      style: base.copyWith(
        foreground: Paint()
          ..style = PaintingStyle.stroke
          ..strokeWidth = width
          ..color = color
          ..maskFilter = blur == 0 ? null : MaskFilter.blur(BlurStyle.normal, blur),
      ),
    );
    final letters = Stack(
      children: [
        // Green glow around the tube.
        line(size * 0.1, const Color(0xFF35C445).withValues(alpha: 0.85), blur: size * 0.12),
        // Inside: sage green, a little see-through; the top half lights up below.
        Text(text, style: base.copyWith(color: const Color(0xFF86A688).withValues(alpha: 0.9))),
        // The tube: vivid green outside, light core.
        line(size * 0.045, const Color(0xFF4CD45C)),
        line(size * 0.016, const Color(0xFFD6F8D2)),
      ],
    );
    return Stack(
      clipBehavior: Clip.none,
      alignment: Alignment.center,
      children: [
        _Spotlight(size),
        letters,
        // Reflection on the top half of the letters.
        ShaderMask(
          blendMode: BlendMode.srcATop,
          shaderCallback: (r) => LinearGradient(
            begin: Alignment.topCenter,
            end: Alignment.bottomCenter,
            colors: [Colors.white.withValues(alpha: 0.45), Colors.white.withValues(alpha: 0)],
            stops: const [0, 0.55],
          ).createShader(r),
          child: letters,
        ),
      ],
    );
  }
}

/// The light cone coming down from above a lit title.
class _Spotlight extends StatelessWidget {
  const _Spotlight(this.size);

  final double size;

  @override
  Widget build(BuildContext context) {
    return Positioned(
      top: -size * 1.2,
      child: IgnorePointer(
        child: Container(
          width: size * 14,
          height: size * 3.0,
          decoration: BoxDecoration(
            gradient: RadialGradient(
              center: Alignment.topCenter,
              radius: 0.9,
              colors: [Brand.neonGreen.withValues(alpha: 0.6), Brand.neonGreen.withValues(alpha: 0)],
            ),
          ),
        ),
      ),
    );
  }
}

/// Dark chip panel with the logo's neon frame.
class NeonCard extends StatelessWidget {
  const NeonCard({
    super.key,
    required this.child,
    this.padding = const EdgeInsets.all(20),
    this.glowColor = Brand.neonGreen,
  });

  final Widget child;
  final EdgeInsets padding;
  final Color glowColor;

  @override
  Widget build(BuildContext context) {
    return CustomPaint(
      painter: _FramePainter(accent: glowColor, cut: 14, fill: _panelFill, glow: 0.8),
      child: Padding(padding: padding, child: child),
    );
  }
}

/// Main button: dark green with the neon frame; secondary buttons use silver.
class NeonButton extends StatelessWidget {
  const NeonButton({super.key, required this.label, required this.onPressed, this.icon, this.secondary = false});

  final String label;
  final VoidCallback? onPressed;
  final IconData? icon;
  final bool secondary;

  @override
  Widget build(BuildContext context) {
    final enabled = onPressed != null;
    final accent = secondary ? Brand.neonBlue : Brand.neonGreen;
    return Opacity(
      opacity: enabled ? 1 : 0.5,
      child: CustomPaint(
        painter: _FramePainter(
          accent: accent,
          cut: 10,
          fill: secondary ? const LinearGradient(colors: [Color(0xE0142226), Color(0xE00F1A1E)]) : Brand.buttonGradient,
          glow: enabled ? 1 : 0,
        ),
        child: Material(
          color: Colors.transparent,
          child: InkWell(
            customBorder: const BeveledRectangleBorder(borderRadius: BorderRadius.all(Radius.circular(10))),
            onTap: onPressed,
            child: Padding(
              padding: const EdgeInsets.symmetric(vertical: 18, horizontal: 16),
              child: Row(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  if (icon != null) ...[Icon(icon, color: accent), const SizedBox(width: 10)],
                  Flexible(
                    child: Text(
                      label,
                      textAlign: TextAlign.center,
                      style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w600, color: Brand.text),
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// Screen title bar: dark chip with the lit mint line underneath. The main
/// screen shows ENCRYPTLOCK exactly as in the logo.
class BrandBar extends StatelessWidget implements PreferredSizeWidget {
  const BrandBar({super.key, required this.title, this.actions, this.background, this.lit = false});

  final String title;
  final List<Widget>? actions;

  /// Drawn behind the title (for instance candles on the market).
  final Widget? background;

  /// The title lit like ENCRYPTLOCK (neon letters, light from above).
  final bool lit;

  @override
  Size get preferredSize => const Size.fromHeight(64);

  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: const BoxDecoration(
        gradient: Brand.barGradient,
        border: Border(bottom: BorderSide(color: Color(0x9994F7D1))),
        boxShadow: [BoxShadow(color: Color(0x3394F7D1), blurRadius: 12, offset: Offset(0, 2))],
      ),
      child: SafeArea(
        bottom: false,
        child: Stack(
          fit: StackFit.expand,
          children: [
            if (background != null) Positioned.fill(child: background!),
            AppBar(
              // Computer: ENCRYPTLOCK in the middle of a bar that spans the
              // window, lit from above (the logo itself is in the side menu).
              centerTitle: true,
              title: title == 'ENCRYPTLOCK' && MediaQuery.sizeOf(context).width >= 900
                  ? const LitTitle()
                  : title == 'ENCRYPTLOCK'
                  ? const BrandTitle(size: 28)
                  : lit
                  ? LitText(title.toUpperCase())
                  : Text(
                      title,
                      style: TextStyle(
                        color: Brand.mintText,
                        fontWeight: FontWeight.w700,
                        letterSpacing: 1.5,
                        shadows: [Shadow(color: Brand.neonGreen.withValues(alpha: 0.6), blurRadius: 10)],
                      ),
                    ),
              actions: actions,
            ),
          ],
        ),
      ),
    );
  }
}

/// A short error line in red.
class ErrorText extends StatelessWidget {
  const ErrorText(this.text, {super.key});

  final String? text;

  @override
  Widget build(BuildContext context) {
    if (text == null) return const SizedBox.shrink();
    return Padding(
      padding: const EdgeInsets.only(top: 12),
      child: Text(
        text!,
        style: const TextStyle(color: Brand.danger, fontWeight: FontWeight.w600),
      ),
    );
  }
}

/// Password field with an eye button to show or hide what is being typed.
class PasswordField extends StatefulWidget {
  const PasswordField({
    super.key,
    required this.controller,
    required this.label,
    this.prefixIcon,
    this.onChanged,
    this.onSubmitted,
    this.enabled = true,
  });

  final TextEditingController controller;
  final String label;
  final IconData? prefixIcon;
  final ValueChanged<String>? onChanged;
  final ValueChanged<String>? onSubmitted;
  final bool enabled;

  @override
  State<PasswordField> createState() => _PasswordFieldState();
}

class _PasswordFieldState extends State<PasswordField> {
  bool _visible = false;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    return TextField(
      controller: widget.controller,
      obscureText: !_visible,
      enabled: widget.enabled,
      autocorrect: false,
      enableSuggestions: false,
      keyboardType: TextInputType.visiblePassword,
      decoration: InputDecoration(
        labelText: widget.label,
        prefixIcon: widget.prefixIcon == null ? null : Icon(widget.prefixIcon),
        suffixIcon: IconButton(
          tooltip: _visible ? t.hidePassword : t.showPassword,
          icon: Icon(_visible ? Icons.visibility_off : Icons.visibility, color: Brand.neonBlue),
          onPressed: () => setState(() => _visible = !_visible),
        ),
      ),
      onChanged: widget.onChanged,
      onSubmitted: widget.onSubmitted,
    );
  }
}
