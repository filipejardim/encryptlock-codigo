import 'package:flutter/material.dart';

import '../app_state.dart';
import '../core/network.dart';
import '../l10n/gen/app_localizations.dart';
import '../screens/home.dart' show formatLock;
import '../screens/security.dart' show chainErrorText;
import '../theme.dart';
import 'brand.dart';

/// Transfers waiting: received ones to accept (1 hour), sent ones to cancel.
class PendingList extends StatefulWidget {
  const PendingList({super.key, required this.received, required this.sent});

  final List<PendingTransfer> received;
  final List<PendingTransfer> sent;

  @override
  State<PendingList> createState() => _PendingListState();
}

class _PendingListState extends State<PendingList> {
  /// The transfer being accepted or canceled right now.
  int? _busyId;

  static String _short(String a) => a.length > 16 ? '${a.substring(0, 10)}…${a.substring(a.length - 6)}' : a;

  static String _left(DateTime until) {
    final d = until.difference(DateTime.now());
    if (d.isNegative) return '0:00';
    final m = d.inMinutes, s = d.inSeconds % 60;
    return '$m:${s.toString().padLeft(2, '0')}';
  }

  Future<void> _act(PendingTransfer p, bool accept) async {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final messenger = ScaffoldMessenger.of(context);
    setState(() => _busyId = p.id);
    try {
      if (accept) {
        await state.accept(p.id);
      } else {
        await state.cancel(p.id);
      }
      messenger.showSnackBar(SnackBar(content: Text(accept ? t.acceptedOk : t.canceledOk)));
    } on ChainError catch (e) {
      messenger.showSnackBar(SnackBar(content: Text(chainErrorText(t, e.log))));
    } catch (e) {
      debugPrint('transaction failed: $e');
      messenger.showSnackBar(SnackBar(content: Text(t.networkError)));
    } finally {
      if (mounted) setState(() => _busyId = null);
    }
  }

  Widget _row(PendingTransfer p, bool incoming) {
    final t = AppLocalizations.of(context);
    final locale = Localizations.localeOf(context).toString();
    final color = incoming ? Brand.neonGreen : Brand.neonBlue;
    final busy = _busyId == p.id;
    final canAct = !p.awaitingApproval || !incoming;
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 8),
      child: Row(
        children: [
          Icon(incoming ? Icons.south_west : Icons.north_east, color: color),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  '${incoming ? '+' : '−'}${formatLock(p.amount, locale)} LOCK',
                  style: TextStyle(color: color, fontSize: 17, fontWeight: FontWeight.w700),
                ),
                Text(
                  incoming ? t.fromAddr(_short(p.sender)) : t.toAddr(_short(p.recipient)),
                  style: const TextStyle(color: Brand.pearl, fontSize: 13),
                ),
                Text(
                  p.awaitingApproval ? t.awaitingApproval : t.timeLeft(_left(p.expiresAt)),
                  style: const TextStyle(color: Brand.pearl, fontSize: 13),
                ),
              ],
            ),
          ),
          if (busy)
            const SizedBox(width: 28, height: 28, child: CircularProgressIndicator(color: Brand.neonGreen))
          else if (canAct)
            FilledButton(
              style: FilledButton.styleFrom(
                backgroundColor: incoming ? const Color(0xFF237A43) : Brand.card,
                side: BorderSide(color: color),
              ),
              onPressed: _busyId == null ? () => _act(p, incoming) : null,
              child: Text(incoming ? t.accept : t.cancel, style: const TextStyle(color: Brand.text)),
            ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    if (widget.received.isEmpty && widget.sent.isEmpty) return const SizedBox.shrink();
    Widget title(String s) => Padding(
          padding: const EdgeInsets.only(bottom: 4),
          child: Text(s, style: const TextStyle(color: Brand.text, fontSize: 17, fontWeight: FontWeight.w700)),
        );
    return Padding(
      padding: const EdgeInsets.only(top: 20),
      child: NeonCard(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            if (widget.received.isNotEmpty) ...[
              title(t.incoming),
              for (final p in widget.received) _row(p, true),
            ],
            if (widget.received.isNotEmpty && widget.sent.isNotEmpty)
              const Divider(color: Brand.cardBorder, height: 24),
            if (widget.sent.isNotEmpty) ...[
              title(t.outgoing),
              for (final p in widget.sent) _row(p, false),
            ],
          ],
        ),
      ),
    );
  }
}
