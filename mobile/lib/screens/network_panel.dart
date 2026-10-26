import 'dart:async';

import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

import '../app_state.dart';
import '../core/network.dart';
import '../l10n/gen/app_localizations.dart';
import '../theme.dart';
import '../widgets/brand.dart';
import 'guide.dart';
import 'home.dart' show KeepersLine, formatLock;

/// The network at a glance, visible on the Wallet screen (not hidden in
/// Settings): latest block, LOCK in existence, and every LockKeeper with its
/// share of the vote. Public data only, like the explorer.
class NetworkPanel extends StatefulWidget {
  const NetworkPanel({super.key});

  @override
  State<NetworkPanel> createState() => _NetworkPanelState();
}

class _NetworkPanelState extends State<NetworkPanel> {
  NetworkInfo? _info;
  Timer? _timer;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
    _timer = Timer.periodic(const Duration(seconds: 6), (_) => _load());
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  Future<void> _load() async {
    if (!mounted) return;
    try {
      final info = await AppScope.of(context).client.network();
      if (mounted) setState(() => _info = info);
    } catch (_) {
      // No LockKeeper answers: the last numbers stay ("—" if none).
      final last = _info == null ? await LockKeeperClient.lastNetwork() : null;
      if (mounted && last != null) setState(() => _info = last);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final locale = Localizations.localeOf(context).toString();
    final info = _info;
    final total = info?.keepers.fold(BigInt.zero, (s, k) => s + k.tokens) ?? BigInt.zero;
    // The bars show each one's share of the new LOCK, not of the guarantee.
    final shares = info == null ? const <double>[] : earningShares(info.keepers);
    final number = NumberFormat.decimalPattern(locale);
    final supply = info == null ? '—' : number.format((info.supply ~/ BigInt.from(100000000)).toInt());
    return NeonCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              const Text('🛰️', style: TextStyle(fontSize: 20)),
              const SizedBox(width: 8),
              Flexible(child: Text(t.networkPanel, style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w700))),
            ],
          ),
          const LockKeepersExplain(),
          const SizedBox(height: 8),
          KeepersLine(state: state),
          const SizedBox(height: 12),
          Row(
            children: [
              Expanded(child: _Fact(label: t.latestBlock, value: state.status == null ? '—' : number.format(state.status!.height))),
              Expanded(child: _Fact(label: t.lockInExistence, value: supply)),
              Expanded(child: _Fact(label: 'LockKeepers', value: info == null ? '—' : '${info.keepers.length}')),
            ],
          ),
          if (info != null && info.keepers.isNotEmpty) ...[
            const Divider(color: Brand.cardBorder, height: 26),
            for (final (i, k) in info.keepers.indexed)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 5),
                child: Row(
                  children: [
                    Container(
                      width: 9,
                      height: 9,
                      decoration: BoxDecoration(
                        shape: BoxShape.circle,
                        color: k.jailed ? Brand.danger : (k.online ? Brand.neonGreen : Brand.orange),
                        boxShadow: Brand.glow(k.jailed ? Brand.danger : (k.online ? Brand.neonGreen : Brand.orange), k.jailed ? 0 : 0.6),
                      ),
                    ),
                    const SizedBox(width: 10),
                    Expanded(child: Text(k.name, style: const TextStyle(fontWeight: FontWeight.w600))),
                    SizedBox(
                      width: 90,
                      child: ClipRRect(
                        child: LinearProgressIndicator(
                          value: shares[i],
                          minHeight: 6,
                          backgroundColor: Brand.bubble,
                          color: Brand.neonGreen,
                        ),
                      ),
                    ),
                    const SizedBox(width: 10),
                    SizedBox(
                      width: 52,
                      child: Text(
                        '${(shares[i] * 100).toStringAsFixed(1)}%',
                        textAlign: TextAlign.end,
                        style: const TextStyle(color: Brand.pearl),
                      ),
                    ),
                  ],
                ),
              ),
            const SizedBox(height: 6),
            Text(t.earningSharesNote, style: const TextStyle(color: Brand.pearl, fontSize: 13)),
            const SizedBox(height: 4),
            Text(
              t.inGuaranteeTotal(formatLock(total, locale)),
              style: const TextStyle(color: Brand.pearl, fontSize: 13),
            ),
          ],
        ],
      ),
    );
  }
}

class _Fact extends StatelessWidget {
  const _Fact({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) => Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(label, style: const TextStyle(color: Brand.pearl, fontSize: 12)),
          const SizedBox(height: 2),
          Text(
            value,
            style: const TextStyle(fontSize: 18, fontWeight: FontWeight.w800, color: Brand.mintText),
          ),
        ],
      );
}
