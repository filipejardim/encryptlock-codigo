import 'dart:math' as math;

import 'package:flutter/material.dart';

/// The marks of the other coins, drawn as their official logos look (white
/// on the coin's own colour): Bitcoin ₿ (tilted), Ethereum's diamond,
/// Polygon's linked shapes, Tether's T with a ring, USD Coin's $ in arcs.
enum CoinMark { bitcoin, ethereum, polygon, tether, usdc }

class CoinMarkPainter extends CustomPainter {
  const CoinMarkPainter(this.mark);

  final CoinMark mark;

  @override
  void paint(Canvas canvas, Size size) {
    final s = size.width;
    final white = Paint()..color = Colors.white;
    Offset p(double x, double y) => Offset(x * s, y * s);
    switch (mark) {
      case CoinMark.bitcoin:
        // ₿ tilted 14°, as in the Bitcoin logo.
        canvas.save();
        canvas.translate(s / 2, s / 2);
        canvas.rotate(14 * math.pi / 180);
        final tp = TextPainter(
          text: TextSpan(
            text: '₿',
            style: TextStyle(fontSize: s * 0.62, fontWeight: FontWeight.w900, color: Colors.white, height: 1),
          ),
          textDirection: TextDirection.ltr,
        )..layout();
        tp.paint(canvas, Offset(-tp.width / 2, -tp.height / 2));
        canvas.restore();
      case CoinMark.ethereum:
        // Two pyramids: the upper one in two shades, the lower one apart.
        Path poly(List<Offset> pts) => Path()..addPolygon(pts, true);
        final top = p(0.5, 0.14), mid = p(0.5, 0.63), l = p(0.29, 0.52), r = p(0.71, 0.52);
        canvas.drawPath(poly([top, l, mid]), Paint()..color = Colors.white);
        canvas.drawPath(poly([top, r, mid]), Paint()..color = Colors.white.withValues(alpha: 0.78));
        canvas.drawPath(poly([p(0.5, 0.42), l, mid]), Paint()..color = Colors.white.withValues(alpha: 0.88));
        canvas.drawPath(poly([p(0.5, 0.42), r, mid]), Paint()..color = Colors.white.withValues(alpha: 0.62));
        canvas.drawPath(poly([p(0.29, 0.565), p(0.5, 0.86), p(0.5, 0.685)]), Paint()..color = Colors.white);
        canvas.drawPath(
          poly([p(0.71, 0.565), p(0.5, 0.86), p(0.5, 0.685)]),
          Paint()..color = Colors.white.withValues(alpha: 0.78),
        );
      case CoinMark.polygon:
        // Two linked hexagon outlines, like Polygon's mark.
        final stroke = Paint()
          ..color = Colors.white
          ..style = PaintingStyle.stroke
          ..strokeWidth = s * 0.07
          ..strokeJoin = StrokeJoin.round;
        Path hex(double cx, double cy) => Path()
          ..addPolygon([
            for (var i = 0; i < 6; i++)
              p(cx + 0.17 * math.cos(math.pi / 3 * i), cy + 0.17 * math.sin(math.pi / 3 * i)),
          ], true);
        canvas.drawPath(hex(0.385, 0.56), stroke);
        canvas.drawPath(hex(0.615, 0.44), stroke);
      case CoinMark.tether:
        // T with a flat ring around its stem.
        canvas.drawRect(Rect.fromLTRB(0.25 * s, 0.24 * s, 0.75 * s, 0.37 * s), white);
        canvas.drawRect(Rect.fromLTRB(0.435 * s, 0.24 * s, 0.565 * s, 0.80 * s), white);
        canvas.drawOval(
          Rect.fromCenter(center: p(0.5, 0.47), width: 0.66 * s, height: 0.16 * s),
          Paint()
            ..color = Colors.white
            ..style = PaintingStyle.stroke
            ..strokeWidth = s * 0.05,
        );
      case CoinMark.usdc:
        // $ between two arcs (USD Coin's mark).
        final arc = Paint()
          ..color = Colors.white
          ..style = PaintingStyle.stroke
          ..strokeWidth = s * 0.06
          ..strokeCap = StrokeCap.round;
        final ring = Rect.fromCircle(center: p(0.5, 0.5), radius: s * 0.32);
        canvas.drawArc(ring, math.pi * 0.62, math.pi * 0.76, false, arc);
        canvas.drawArc(ring, -math.pi * 0.38, math.pi * 0.76, false, arc);
        final tp = TextPainter(
          text: TextSpan(
            text: r'$',
            style: TextStyle(fontSize: s * 0.42, fontWeight: FontWeight.w800, color: Colors.white, height: 1),
          ),
          textDirection: TextDirection.ltr,
        )..layout();
        tp.paint(canvas, Offset(s / 2 - tp.width / 2, s / 2 - tp.height / 2));
    }
  }

  @override
  bool shouldRepaint(covariant CoinMarkPainter old) => old.mark != mark;
}
