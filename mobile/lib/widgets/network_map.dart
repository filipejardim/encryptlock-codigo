import 'dart:async';
import 'dart:math' as math;
import 'dart:ui' show PointMode;

import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';

import '../app_state.dart';
import '../core/network.dart';
import '../l10n/gen/app_localizations.dart';
import '../theme.dart';
import 'brand.dart';

/// The network map (founder, 25-26 Oct 2026): a world that turns, every
/// LockKeeper a little computer chip on it, joined to its neighbours by thin
/// circuit traces with sparks of light running along them. Drag to turn it,
/// wheel / two fingers / + − to come closer, ⛶ for the whole screen. The more
/// LockKeepers, the smaller the chips. Green: signing; amber: on, waiting for
/// its turn; red: off. A tapped chip shows only "lock1" and one letter, its
/// state and its hours today (public data, nothing that names anyone).
class NetworkMapCard extends StatefulWidget {
  const NetworkMapCard({super.key});

  @override
  State<NetworkMapCard> createState() => _NetworkMapCardState();
}

class _NetworkMapCardState extends State<NetworkMapCard> {
  NetworkInfo? _info;
  Timer? _timer;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _timer ??= Timer.periodic(const Duration(minutes: 1), (_) => _load());
    if (_info == null) _load();
  }

  Future<void> _load() async {
    final state = AppScope.of(context);
    try {
      final info = await LockKeeperClient(state.lockKeeperUrl).network();
      if (mounted) setState(() => _info = info);
    } catch (_) {
      final last = await LockKeeperClient.lastNetwork();
      if (mounted && _info == null && last != null) setState(() => _info = last);
    }
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final keepers = _info?.keepers ?? const <KeeperInfo>[];
    return Padding(
      padding: const EdgeInsets.only(top: 20),
      child: NeonCard(
        padding: EdgeInsets.zero,
        child: SizedBox(
          height: 420,
          child: ClipRRect(
            borderRadius: BorderRadius.circular(16),
            child: NetworkMap(keepers: keepers, onFullScreen: () => _fullScreen(context, keepers)),
          ),
        ),
      ),
    );
  }

  void _fullScreen(BuildContext context, List<KeeperInfo> keepers) {
    Navigator.of(context).push(MaterialPageRoute<void>(
      fullscreenDialog: true,
      builder: (_) => Scaffold(
        backgroundColor: const Color(0xFF070D10),
        body: SafeArea(child: NetworkMap(keepers: keepers, full: true, onFullScreen: () => Navigator.of(context).pop())),
      ),
    ));
  }
}

/// The map itself (also used on the whole screen).
class NetworkMap extends StatefulWidget {
  const NetworkMap({super.key, required this.keepers, this.full = false, this.onFullScreen});

  final List<KeeperInfo> keepers;
  final bool full;
  final VoidCallback? onFullScreen;

  @override
  State<NetworkMap> createState() => _NetworkMapState();
}

const _amber = Color(0xFFF2D27A);

Color _colorOf(KeeperInfo k) => k.signing ? Brand.neonGreen : (k.online ? _amber : Brand.danger);

/// A point on the unit sphere.
typedef _V = ({double x, double y, double z});

_V _norm(double x, double y, double z) {
  final l = math.sqrt(x * x + y * y + z * z);
  return (x: x / l, y: y / l, z: z / l);
}

_V _mix(_V p, _V q, double t) => _norm(p.x + (q.x - p.x) * t, p.y + (q.y - p.y) * t, p.z + (q.z - p.z) * t);

double _dist(_V p, _V q) => math.sqrt(math.pow(p.x - q.x, 2) + math.pow(p.y - q.y, 2) + math.pow(p.z - q.z, 2));

class _Link {
  _Link(this.a, this.b, this.points, this.phase, this.speed);
  final int a, b;
  final List<_V> points;
  final double phase, speed;
}

class _NetworkMapState extends State<NetworkMap> with SingleTickerProviderStateMixin {
  late final AnimationController _clock = AnimationController(vsync: this, duration: const Duration(days: 1))..forward();
  List<_V> _chips = const [];
  List<_Link> _links = const [];
  List<KeeperInfo> _order = const [];
  double _yaw = .6, _pitch = -.35, _zoom = 1, _zoomAtStart = 1;
  Duration _lastMove = Duration.zero;
  Offset? _tipAt;
  String? _tip;
  final List<(Offset, double, int)> _hits = [];

  @override
  void initState() {
    super.initState();
    _build();
    _clock.addListener(_tick);
  }

  @override
  void didUpdateWidget(NetworkMap old) {
    super.didUpdateWidget(old);
    if (old.keepers != widget.keepers) _build();
  }

  Duration _last = Duration.zero;
  void _tick() {
    final now = _clock.lastElapsedDuration ?? Duration.zero;
    final dt = (now - _last).inMicroseconds / 1e6;
    _last = now;
    // Turns slowly by itself while nobody is holding it.
    if (now - _lastMove > const Duration(seconds: 2)) _yaw += .05 * dt.clamp(0, .05);
    setState(() {});
  }

  /// Places the chips evenly on the world (by account, so each keeps its
  /// place between refreshes) and joins each to its two nearest neighbours
  /// with a circuit trace.
  void _build() {
    final order = [...widget.keepers]..sort((a, b) => a.operator.compareTo(b.operator));
    final n = order.length;
    final r = math.Random(11);
    final chips = <_V>[];
    final gap = n == 0 ? 0.0 : math.sqrt(4 * math.pi / n) * .45;
    for (var i = 0; i < n; i++) {
      final y = 1 - (i + .5) * 2 / n, rad = math.sqrt(math.max(0, 1 - y * y)), th = i * 2.399963;
      chips.add(_norm(math.cos(th) * rad + (r.nextDouble() - .5) * gap, y + (r.nextDouble() - .5) * gap,
          math.sin(th) * rad + (r.nextDouble() - .5) * gap));
    }
    final links = <_Link>[];
    final near = n > 2000 ? 1 : 2;
    final cell = math.max(.08, 2.4 / math.sqrt(math.max(1, n)));
    final grid = <String, List<int>>{};
    String key(int a, int b, int c) => '$a,$b,$c';
    (int, int, int) cellOf(_V p) => ((p.x / cell).floor(), (p.y / cell).floor(), (p.z / cell).floor());
    for (var i = 0; i < n; i++) {
      final c = cellOf(chips[i]);
      grid.putIfAbsent(key(c.$1, c.$2, c.$3), () => []).add(i);
    }
    for (var i = 0; i < n; i++) {
      final c = cellOf(chips[i]), cand = <(int, double)>[];
      for (var dx = -1; dx <= 1; dx++) {
        for (var dy = -1; dy <= 1; dy++) {
          for (var dz = -1; dz <= 1; dz++) {
            for (final j in grid[key(c.$1 + dx, c.$2 + dy, c.$3 + dz)] ?? const <int>[]) {
              if (j > i) cand.add((j, _dist(chips[i], chips[j])));
            }
          }
        }
      }
      // Few LockKeepers far apart: the nearest ones, wherever they are.
      if (cand.isEmpty && n <= 50) {
        for (var j = i + 1; j < n; j++) {
          cand.add((j, _dist(chips[i], chips[j])));
        }
      }
      cand.sort((u, v) => u.$2.compareTo(v.$2));
      for (final (j, _) in cand.take(near)) {
        links.add(_Link(i, j, _trace(chips[i], chips[j], r.nextBool()), r.nextDouble(), .25 + r.nextDouble() * .5));
      }
    }
    _order = order;
    _chips = chips;
    _links = links;
  }

  /// A trace on the sphere like the ones on a computer board: along one
  /// direction, a cut corner, then the other.
  static List<_V> _trace(_V p, _V q, bool latFirst) {
    final corner = latFirst ? _norm(q.x, p.y, q.z) : _norm(p.x, q.y, p.z);
    final a = _mix(p, corner, .85), b = _mix(corner, q, .15), out = <_V>[];
    for (var t = 0.0; t <= 1.0001; t += .25) {
      out.add(_mix(p, a, t));
    }
    for (var t = .25; t <= 1.0001; t += .25) {
      out.add(_mix(a, b, t));
    }
    for (var t = .25; t <= 1.0001; t += .25) {
      out.add(_mix(b, q, t));
    }
    return out;
  }

  void _setZoom(double z) => setState(() {
        _zoom = z.clamp(.6, 12);
        _lastMove = _clock.lastElapsedDuration ?? Duration.zero;
      });

  void _tap(Offset at) {
    (Offset, double, int)? best;
    var bestD = double.infinity;
    for (final h in _hits) {
      final d = (h.$1 - at).distance;
      if (d < math.max(12, h.$2) && d < bestD) {
        bestD = d;
        best = h;
      }
    }
    if (best == null) {
      setState(() => _tip = null);
      return;
    }
    final k = _order[best.$3];
    final t = AppLocalizations.of(context);
    final state = k.signing ? t.mapSigning : (k.online ? t.mapWaiting : t.mapOff);
    final account = accountOfOperator(k.operator);
    final short = account == null || account.length < 6 ? 'lock1' : account.substring(0, 6);
    setState(() {
      _tip = '$short…  ·  $state';
      _tipAt = best!.$1;
    });
    // The hours on today, asked only for the chip that was tapped.
    if (account != null) {
      final url = AppScope.of(context).lockKeeperUrl;
      LockKeeperClient(url).rotation(account).then((r) {
        if (mounted && _tipAt == best!.$1) setState(() => _tip = '$short…  ·  $state  ·  ${t.mapHoursToday((r.today / 3600).toStringAsFixed(1))}');
      }).catchError((_) {});
    }
  }

  @override
  void dispose() {
    _clock.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final now = (_clock.lastElapsedDuration ?? Duration.zero).inMicroseconds / 1e6;
    final title = TextStyle(
      color: Brand.neonGreen,
      fontSize: 15,
      fontWeight: FontWeight.w800,
      letterSpacing: 1,
      shadows: [Shadow(color: Brand.neonGreen.withValues(alpha: 0.6), blurRadius: 10)],
    );
    Widget tool(IconData icon, String tip, VoidCallback onTap) => Padding(
          padding: const EdgeInsets.only(bottom: 8),
          child: Tooltip(
            message: tip,
            child: InkWell(
              onTap: onTap,
              borderRadius: BorderRadius.circular(10),
              child: Container(
                width: 38,
                height: 38,
                decoration: BoxDecoration(
                  color: const Color(0xD90E1619),
                  border: Border.all(color: Brand.neonGreen.withValues(alpha: .6), width: 1.5),
                  borderRadius: BorderRadius.circular(10),
                  boxShadow: Brand.glow(Brand.neonGreen, .25),
                ),
                child: Icon(icon, color: Brand.neonGreen, size: 20),
              ),
            ),
          ),
        );
    return Container(
      decoration: const BoxDecoration(
        gradient: RadialGradient(center: Alignment(0, -.1), radius: .9, colors: [Color(0xFF10202A), Color(0xFF0B1418), Color(0xFF070D10)]),
      ),
      child: Stack(
        children: [
          Positioned.fill(
            child: Listener(
              onPointerSignal: (e) {
                if (e is PointerScrollEvent) _setZoom(_zoom * math.exp(-e.scrollDelta.dy * .0015));
              },
              child: GestureDetector(
                behavior: HitTestBehavior.opaque,
                onScaleStart: (_) => _zoomAtStart = _zoom,
                onScaleUpdate: (d) => setState(() {
                  if (d.pointerCount > 1) {
                    _zoom = (_zoomAtStart * d.scale).clamp(.6, 12);
                  } else {
                    _yaw += d.focalPointDelta.dx * .006 / _zoom;
                    _pitch = (_pitch + d.focalPointDelta.dy * .006 / _zoom).clamp(-1.3, 1.3);
                  }
                  _lastMove = _clock.lastElapsedDuration ?? Duration.zero;
                }),
                onTapUp: (d) => _tap(d.localPosition),
                child: CustomPaint(
                  painter: _MapPainter(
                    chips: _chips,
                    links: _links,
                    keepers: _order,
                    yaw: _yaw,
                    pitch: _pitch,
                    zoom: _zoom,
                    time: now,
                    hits: _hits,
                  ),
                ),
              ),
            ),
          ),
          Positioned(left: 18, top: 14, child: Text(t.mapTitle.toUpperCase(), style: title)),
          Positioned(
            right: 62,
            top: 18,
            child: Text(t.mapCount(_order.length), style: const TextStyle(color: Brand.neonBlue, fontWeight: FontWeight.w700)),
          ),
          Positioned(
            right: 12,
            top: 10,
            child: Column(
              children: [
                tool(widget.full ? Icons.close_fullscreen : Icons.open_in_full, t.mapFullScreen, () => widget.onFullScreen?.call()),
                tool(Icons.add, t.mapZoomIn, () => _setZoom(_zoom * 1.4)),
                tool(Icons.remove, t.mapZoomOut, () => _setZoom(_zoom / 1.4)),
              ],
            ),
          ),
          Positioned(
            left: 18,
            right: 18,
            bottom: 12,
            child: Wrap(
              spacing: 14,
              runSpacing: 4,
              children: [
                for (final (c, s) in [(Brand.neonGreen, t.mapSigning), (_amber, t.mapWaiting), (Brand.danger, t.mapOff)])
                  Row(mainAxisSize: MainAxisSize.min, children: [
                    Container(width: 9, height: 9, decoration: BoxDecoration(color: c, borderRadius: BorderRadius.circular(2))),
                    const SizedBox(width: 6),
                    Text(s, style: const TextStyle(color: Brand.neonBlue, fontSize: 13, fontWeight: FontWeight.w600)),
                  ]),
              ],
            ),
          ),
          if (_tip != null && _tipAt != null)
            Positioned(
              left: (_tipAt!.dx + 12).clamp(8, math.max(8, (context.size?.width ?? 300) - 260)),
              top: math.max(8, _tipAt!.dy - 44),
              child: IgnorePointer(
                child: Container(
                  padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                  decoration: BoxDecoration(
                    color: const Color(0xF20A1215),
                    border: Border.all(color: Brand.neonGreen.withValues(alpha: .7)),
                    borderRadius: BorderRadius.circular(10),
                  ),
                  child: Text(_tip!, style: const TextStyle(color: Brand.pearl, fontWeight: FontWeight.w700)),
                ),
              ),
            ),
        ],
      ),
    );
  }
}

class _MapPainter extends CustomPainter {
  _MapPainter({
    required this.chips,
    required this.links,
    required this.keepers,
    required this.yaw,
    required this.pitch,
    required this.zoom,
    required this.time,
    required this.hits,
  });

  final List<_V> chips;
  final List<_Link> links;
  final List<KeeperInfo> keepers;
  final double yaw, pitch, zoom, time;
  final List<(Offset, double, int)> hits;

  _V _rot(_V p) {
    final cy = math.cos(yaw), sy = math.sin(yaw), cp = math.cos(pitch), sp = math.sin(pitch);
    final x1 = p.x * cy + p.z * sy, z1 = -p.x * sy + p.z * cy;
    return (x: x1, y: p.y * cp - z1 * sp, z: p.y * sp + z1 * cp);
  }

  @override
  void paint(Canvas canvas, Size size) {
    final r = math.min(size.width, size.height) * .38 * zoom;
    final c = Offset(size.width / 2, size.height / 2 + 10);
    Offset proj(_V q) => Offset(c.dx + q.x * r, c.dy - q.y * r);

    // The world: dark glass, a mint rim, a faint grid.
    canvas.drawCircle(
      c,
      r,
      Paint()
        ..shader = RadialGradient(
          center: const Alignment(-.3, -.35),
          colors: const [Color(0x8C285048), Color(0x990E1E22), Color(0x33060C0E)],
          stops: const [0, .7, 1],
        ).createShader(Rect.fromCircle(center: c, radius: r * 1.05)),
    );
    canvas.drawCircle(
      c,
      r,
      Paint()
        ..style = PaintingStyle.stroke
        ..strokeWidth = 1.2
        ..color = Brand.neonGreen.withValues(alpha: .55)
        ..maskFilter = const MaskFilter.blur(BlurStyle.solid, 6),
    );
    final grid = Paint()
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1
      ..color = Brand.neonGreen.withValues(alpha: .07);
    void line(Iterable<_V> pts) {
      final path = Path();
      var first = true;
      for (final p in pts) {
        final q = _rot(p);
        if (q.z < 0) {
          first = true;
          continue;
        }
        final o = proj(q);
        first ? path.moveTo(o.dx, o.dy) : path.lineTo(o.dx, o.dy);
        first = false;
      }
      canvas.drawPath(path, grid);
    }

    for (var lat = -60; lat <= 60; lat += 30) {
      final a = lat * math.pi / 180;
      line([for (var lon = 0; lon <= 360; lon += 6) (x: math.cos(a) * math.cos(lon * math.pi / 180), y: math.sin(a), z: math.cos(a) * math.sin(lon * math.pi / 180))]);
    }
    for (var lon = 0; lon < 180; lon += 30) {
      final b = lon * math.pi / 180;
      line([for (var lat = -90; lat <= 90; lat += 6) (x: math.cos(lat * math.pi / 180) * math.cos(b), y: math.sin(lat * math.pi / 180), z: math.cos(lat * math.pi / 180) * math.sin(b))]);
    }
    // The network's heart, glowing inside.
    final pulse = .5 + .5 * math.sin(time / .6);
    canvas.drawCircle(
      c,
      r * .45,
      Paint()
        ..shader = RadialGradient(colors: [Brand.neonGreen.withValues(alpha: .18 + .08 * pulse), Brand.neonGreen.withValues(alpha: 0)])
            .createShader(Rect.fromCircle(center: c, radius: r * .45)),
    );

    final n = chips.length;
    if (n == 0) return;
    final rc = [for (final p in chips) _rot(p)];
    final trace = Paint()
      ..style = PaintingStyle.stroke
      ..strokeCap = StrokeCap.round
      ..strokeJoin = StrokeJoin.round
      ..strokeWidth = math.max(.6, .9 * zoom);
    for (final l in links) {
      final a = rc[l.a], b = rc[l.b];
      if (a.z < .02 && b.z < .02) continue;
      final pts = [for (final p in l.points) _rot(p)];
      if (pts.any((q) => q.z < 0)) continue;
      final pp = [for (final q in pts) proj(q)];
      final on = keepers[l.a].online && keepers[l.b].online, depth = (a.z + b.z) / 2;
      trace.color = on ? Brand.neonGreen.withValues(alpha: .18 + .3 * depth) : Brand.neonBlue.withValues(alpha: .08 + .1 * depth);
      canvas.drawPoints(PointMode.polygon, pp, trace);
      if (on && n <= 3000) {
        // The spark running along the trace.
        final k = (time * l.speed + l.phase) % 1, f = k * (pp.length - 1), i = f.floor(), t = f - i;
        final o = Offset.lerp(pp[i], pp[math.min(i + 1, pp.length - 1)], t)!;
        canvas.drawCircle(
          o,
          6 * zoom,
          Paint()
            ..shader = RadialGradient(colors: [const Color(0xF2DCFFF0), Brand.neonGreen.withValues(alpha: 0)])
                .createShader(Rect.fromCircle(center: o, radius: 6 * zoom)),
        );
      }
    }

    // The chips, flattened towards the edge; the more there are, the smaller.
    hits.clear();
    final s = (15 * math.sqrt(40 / n)).clamp(1.6, 24) * zoom;
    final order = [for (var i = 0; i < n; i++) if (rc[i].z > 0) i]..sort((a, b) => rc[a].z.compareTo(rc[b].z));
    for (final i in order) {
      final q = rc[i], o = proj(q), col = _colorOf(keepers[i]), sz = s * (.55 + .45 * q.z);
      final alpha = .45 + .55 * q.z;
      hits.add((o, sz, i));
      if (sz < 3) {
        canvas.drawRect(Rect.fromCenter(center: o, width: sz, height: sz), Paint()..color = col.withValues(alpha: alpha));
        continue;
      }
      canvas.save();
      canvas.translate(o.dx, o.dy);
      final ang = math.atan2(-q.y, q.x);
      canvas.rotate(ang);
      canvas.scale(math.max(.25, q.z), 1);
      canvas.rotate(-ang);
      final pin = Paint()
        ..color = col.withValues(alpha: alpha)
        ..strokeWidth = math.max(.6, sz / 9);
      for (var k = -1; k <= 1; k++) {
        final d = k * sz * .28;
        canvas.drawLine(Offset(d, -sz * .5), Offset(d, -sz * .72), pin);
        canvas.drawLine(Offset(d, sz * .5), Offset(d, sz * .72), pin);
        canvas.drawLine(Offset(-sz * .5, d), Offset(-sz * .72, d), pin);
        canvas.drawLine(Offset(sz * .5, d), Offset(sz * .72, d), pin);
      }
      final body = RRect.fromRectAndRadius(Rect.fromCenter(center: Offset.zero, width: sz, height: sz), Radius.circular(sz * .15));
      canvas.drawRRect(
        body,
        Paint()
          ..color = col.withValues(alpha: alpha * .6)
          ..maskFilter = MaskFilter.blur(BlurStyle.outer, keepers[i].signing ? 6 : 3),
      );
      canvas.drawRRect(body, Paint()..color = const Color(0xFF0D1A1E).withValues(alpha: alpha));
      canvas.drawRRect(
        body,
        Paint()
          ..style = PaintingStyle.stroke
          ..strokeWidth = math.max(.6, sz / 9)
          ..color = col.withValues(alpha: alpha),
      );
      canvas.drawRect(Rect.fromCenter(center: Offset.zero, width: sz * .4, height: sz * .4), Paint()..color = col.withValues(alpha: alpha * .8));
      canvas.restore();
    }
  }

  @override
  bool shouldRepaint(_MapPainter old) => true;
}
