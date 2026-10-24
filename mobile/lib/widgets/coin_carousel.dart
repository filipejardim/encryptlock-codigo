import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../theme.dart';
import 'coin_logos.dart';

export 'coin_logos.dart' show CoinMark;

/// A coin the wallet shows in the carousel.
class Coin {
  const Coin({
    required this.ticker,
    required this.name,
    required this.color,
    this.mark,
    this.image,
    this.network,
    this.ready = false,
    this.decimals = 8,
  });

  final String ticker;
  final String name;
  final Color color;

  /// Its logo, drawn on the coin (when there is no image).
  final CoinMark? mark;
  final String? image;

  /// The network it travels on, when not its own (e.g. Polygon).
  final String? network;

  /// False = shown as "coming soon" (no balance yet). Coins that are not
  /// ready must be removed before launch, so nobody thinks they work.
  final bool ready;

  /// Decimals of its smallest unit (LOCK 8, USDC 6, POL 18).
  final int decimals;

  bool get isLock => ticker == 'LOCK';

  /// Lives on Polygon, at the wallet's 0x… address (same 24 words).
  bool get onPolygon => ticker == 'USDC' || ticker == 'POL';

  /// Only what is needed to buy and sell LOCK (founder's decision of 7 Oct):
  /// LOCK, USDC on Polygon and POL for Polygon's network fee.
  static const all = [
    Coin(ticker: 'LOCK', name: 'EncryptLock', color: Brand.neonGreen, image: 'assets/coin-lock.png', ready: true),
    Coin(ticker: 'POL', name: 'Polygon', color: Color(0xFF8247E5), mark: CoinMark.polygon, ready: true, decimals: 18),
    Coin(
      ticker: 'USDC',
      name: 'USD Coin',
      color: Color(0xFF2775CA),
      mark: CoinMark.usdc,
      network: 'Polygon',
      ready: true,
      decimals: 6,
    ),
  ];
}

/// Coins on a ring seen slightly from above: drag sideways to turn it. The
/// coin in front is big and glowing (the active one); the others move back,
/// smaller and dimmer.
class CoinCarousel extends StatefulWidget {
  const CoinCarousel({super.key, required this.coins, required this.onChanged, this.initial = 0, this.size = 130});

  final List<Coin> coins;
  final ValueChanged<int> onChanged;

  /// The coin in front when the ring is built again (on a phone the ring
  /// leaves the list while scrolling down and comes back on the way up).
  final int initial;

  /// Diameter of the coin in front.
  final double size;

  @override
  State<CoinCarousel> createState() => _CoinCarouselState();
}

class _CoinCarouselState extends State<CoinCarousel> with SingleTickerProviderStateMixin {
  /// Which coin is in front, as a continuous number while turning.
  late double _position = widget.initial.toDouble();
  late final AnimationController _snap = AnimationController.unbounded(vsync: this);
  late int _active = widget.initial;

  int get _n => widget.coins.length;

  @override
  void initState() {
    super.initState();
    _snap.addListener(() => _setPosition(_snap.value));
  }

  @override
  void dispose() {
    _snap.dispose();
    super.dispose();
  }

  void _setPosition(double p) {
    setState(() => _position = p);
    final active = p.round() % _n;
    if (active != _active) {
      _active = active;
      widget.onChanged(active);
    }
  }

  void _animateTo(double target) {
    _snap.value = _position;
    _snap.animateTo(target, duration: const Duration(milliseconds: 450), curve: Curves.easeOutCubic);
  }

  @override
  Widget build(BuildContext context) => LayoutBuilder(builder: (context, box) => _ring(context, box.maxWidth));

  // The ring is as wide as the space it gets (its column on a computer).
  Widget _ring(BuildContext context, double width) {
    final radius = width * 0.30;
    final step = 2 * math.pi / _n;

    final items = <({double depth, Widget child})>[];
    for (var i = 0; i < _n; i++) {
      final angle = (i - _position) * step;
      final depth = (math.cos(angle) + 1) / 2; // 1 = in front, 0 = behind
      final scale = 0.4 + 0.6 * math.pow(depth, 1.4).toDouble();
      final x = math.sin(angle) * radius;
      final y = -(1 - depth) * widget.size * 0.42; // the back of the ring is higher
      final glow = math.pow(depth, 4).toDouble();
      items.add((
        depth: depth,
        child: Transform.translate(
          offset: Offset(x, y),
          child: Transform.scale(
            scale: scale,
            child: GestureDetector(
              onTap: () {
                var delta = (i - _position) % _n;
                if (delta > _n / 2) delta -= _n;
                _animateTo((_position + delta).roundToDouble());
              },
              child: _CoinBall(coin: widget.coins[i], size: widget.size, glow: glow, light: 0.35 + 0.65 * depth),
            ),
          ),
        ),
      ));
    }
    items.sort((a, b) => a.depth.compareTo(b.depth));

    return GestureDetector(
      behavior: HitTestBehavior.opaque,
      onHorizontalDragStart: (_) => _snap.stop(),
      onHorizontalDragUpdate: (d) => _setPosition(_position - d.delta.dx / (radius * 1.4)),
      onHorizontalDragEnd: (d) {
        final flick = -(d.primaryVelocity ?? 0) / 1500;
        _animateTo((_position + flick.clamp(-1.0, 1.0)).roundToDouble());
      },
      child: SizedBox(
        height: widget.size * 1.25,
        width: double.infinity,
        child: Stack(alignment: Alignment.center, children: [for (final item in items) item.child]),
      ),
    );
  }
}

class _CoinBall extends StatelessWidget {
  const _CoinBall({required this.coin, required this.size, required this.glow, required this.light});

  final Coin coin;
  final double size;

  /// 0..1: neon glow, strong only for the coin in front.
  final double glow;

  /// 0..1: how bright the coin is (the ones behind are dimmer, never black).
  final double light;

  @override
  Widget build(BuildContext context) {
    // Every coin has the same ring: a brighter line of its own colour.
    final ring = Border.all(color: Color.lerp(coin.color, Colors.white, 0.4)!, width: size * 0.03);
    final face = coin.image != null
        // EncryptLock: the padlock on the chip, centred, inside the ring.
        ? Container(
            width: size,
            height: size,
            decoration: BoxDecoration(shape: BoxShape.circle, border: ring, color: Brand.background),
            child: ClipOval(child: Image.asset(coin.image!, fit: BoxFit.cover)),
          )
        : Container(
            width: size,
            height: size,
            decoration: BoxDecoration(
              shape: BoxShape.circle,
              gradient: RadialGradient(
                center: const Alignment(-0.3, -0.4),
                colors: [
                  Color.lerp(coin.color, Colors.white, 0.25)!,
                  coin.color,
                  Color.lerp(coin.color, Colors.black, 0.45)!,
                ],
                stops: const [0, 0.6, 1],
              ),
              border: ring,
            ),
            child: CustomPaint(painter: CoinMarkPainter(coin.mark!)),
          );
    return Container(
      width: size,
      height: size,
      decoration: BoxDecoration(
        shape: BoxShape.circle,
        boxShadow: [
          BoxShadow(color: coin.color.withValues(alpha: 0.55 * glow), blurRadius: 26, spreadRadius: 2),
          BoxShadow(color: coin.color.withValues(alpha: 0.25 * glow), blurRadius: 60, spreadRadius: 8),
        ],
      ),
      child: ColorFiltered(
        colorFilter: ColorFilter.mode(Colors.black.withValues(alpha: (1 - light) * 0.75), BlendMode.srcATop),
        child: face,
      ),
    );
  }
}
