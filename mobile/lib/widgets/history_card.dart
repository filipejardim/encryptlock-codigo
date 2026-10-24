import 'dart:async';

import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

import '../app_state.dart';
import '../core/network.dart';
import '../l10n/gen/app_localizations.dart';
import '../screens/home.dart' show formatLock;
import '../theme.dart';
import 'brand.dart';

/// The last finished (accepted) transfers, sent and received.
class HistoryCard extends StatefulWidget {
  const HistoryCard({super.key});

  @override
  State<HistoryCard> createState() => _HistoryCardState();
}

class _HistoryCardState extends State<HistoryCard> {
  List<DoneTransfer>? _items;
  Timer? _poll;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _load();
      _poll = Timer.periodic(const Duration(seconds: 15), (_) => _load());
    });
  }

  @override
  void dispose() {
    _poll?.cancel();
    super.dispose();
  }

  Future<void> _load() async {
    if (!mounted) return;
    final state = AppScope.of(context);
    final address = state.address;
    if (address == null) return;
    try {
      final items = await state.client.history(address);
      if (mounted) setState(() => _items = items);
    } catch (_) {
      // Shown as empty until the LockKeeper answers.
    }
  }

  static String _short(String a) => a.length > 16 ? '${a.substring(0, 10)}…${a.substring(a.length - 6)}' : a;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final locale = Localizations.localeOf(context).toString();
    final items = _items;
    final when = DateFormat.yMMMd(locale).add_Hm();
    return NeonCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              const Text('🧾', style: TextStyle(fontSize: 20)),
              const SizedBox(width: 8),
              Flexible(child: Text(t.historyTitle, style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w700))),
            ],
          ),
          const SizedBox(height: 8),
          if (items == null)
            const Text('—', style: TextStyle(color: Brand.pearl))
          else if (items.isEmpty)
            Text(t.historyEmpty, style: const TextStyle(color: Brand.pearl))
          else
            for (final p in items)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 6),
                child: Row(
                  children: [
                    Icon(
                      p.incoming ? Icons.south_west : Icons.north_east,
                      color: p.incoming ? Brand.neonGreen : Brand.neonBlue,
                      size: 20,
                    ),
                    const SizedBox(width: 12),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            p.incoming ? t.fromAddr(_short(p.other)) : t.toAddr(_short(p.other)),
                            style: const TextStyle(fontSize: 14, fontWeight: FontWeight.w600),
                          ),
                          Text('${p.estimated ? '~ ' : ''}${when.format(p.time)}', style: const TextStyle(color: Brand.pearl, fontSize: 12)),
                        ],
                      ),
                    ),
                    Text(
                      '${p.incoming ? '+' : '−'}${formatLock(p.amount, locale)}',
                      style: TextStyle(
                        color: p.incoming ? Brand.neonGreen : Brand.neonBlue,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ],
                ),
              ),
        ],
      ),
    );
  }
}
