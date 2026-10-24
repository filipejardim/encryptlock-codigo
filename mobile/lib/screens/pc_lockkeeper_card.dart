import 'package:flutter/material.dart';

import '../app_state.dart';
import '../core/network.dart' show ChainError, LockKeeperRotation;
import '../core/pc_lockkeeper.dart';
import '../l10n/gen/app_localizations.dart';
import '../theme.dart';
import '../widgets/brand.dart';
import 'home.dart' show formatLock;
import 'security.dart';

/// Explains the security chip check before Windows asks "Allow?". True to go on.
Future<bool> confirmChipCheck(BuildContext context) async {
  final t = AppLocalizations.of(context);
  final go = await showDialog<bool>(
    context: context,
    builder: (dialogContext) => AlertDialog(
      backgroundColor: Brand.bottomBar,
      title: const Text('🔐'),
      content: Text(t.chipAllowHint, style: const TextStyle(height: 1.4)),
      actions: [
        TextButton(onPressed: () => Navigator.pop(dialogContext, false), child: Text(t.cancel)),
        TextButton(onPressed: () => Navigator.pop(dialogContext, true), child: const Text('OK')),
      ],
    ),
  );
  return go == true;
}

/// Computer only: "Make this PC a LockKeeper", and afterwards its state with
/// an on/off switch.
class PcLockKeeperCard extends StatelessWidget {
  const PcLockKeeperCard({super.key});

  Future<void> _become(BuildContext context) async {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final locale = Localizations.localeOf(context).toString();
    final need = state.pcMinGuarantee;
    final balance = state.status?.balance ?? BigInt.zero;
    final messenger = ScaffoldMessenger.of(context);
    // The guarantee plus a little for the fee.
    if (balance < need + BigInt.from(1000000)) {
      messenger.showSnackBar(SnackBar(content: Text(t.pcLkNeedMore(formatLock(need + BigInt.from(1000000), locale)))));
      return;
    }
    if (!await confirmPassword(context) || !context.mounted) return;
    try {
      // Once: the security chip (Windows asks "Allow?").
      if (!PcLockKeeper.isSetUp && !PcLockKeeper.hasChipTicket) {
        if (!await confirmChipCheck(context)) return;
        final maker = await state.checkPcChip();
        messenger.showSnackBar(SnackBar(content: Text(t.chipOk(maker))));
      }
      await state.becomePcLockKeeper();
    } on ChipProblem catch (e) {
      messenger.showSnackBar(SnackBar(
        duration: const Duration(seconds: 8),
        content: Text(e.kind == ChipProblemKind.cancelled
            ? t.chipCancelled
            : e.detail.isEmpty
                ? t.chipNotAccepted
                : '${t.chipNotAccepted}\n(${e.detail})'),
      ));
    } catch (e) {
      final msg = e is ChainError ? e.log : '$e';
      messenger.showSnackBar(SnackBar(content: Text(t.pcLkError(msg))));
    }
  }

  @override
  Widget build(BuildContext context) {
    if (!PcLockKeeper.isSupported) return const SizedBox.shrink();
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final locale = Localizations.localeOf(context).toString();
    final title = TextStyle(
      fontSize: 17,
      fontWeight: FontWeight.w700,
      color: Brand.neonGreen,
      shadows: [Shadow(color: Brand.neonGreen.withValues(alpha: 0.6), blurRadius: 10)],
    );

    if (!PcLockKeeper.isSetUp) {
      return Padding(
        padding: const EdgeInsets.only(top: 20),
        child: NeonCard(
          padding: const EdgeInsets.all(18),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('🖥️ ${t.pcLkTitle}', style: title),
              const SizedBox(height: 10),
              Text(t.pcLkBody, style: const TextStyle(color: Brand.text, height: 1.45)),
              const SizedBox(height: 10),
              Text(
                t.pcLkGuarantee(formatLock(state.pcMinGuarantee, locale)),
                style: const TextStyle(color: Brand.pearl, fontWeight: FontWeight.w600),
              ),
              const SizedBox(height: 14),
              if (state.pcLockKeeperBusy)
                Row(
                  children: [
                    const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2)),
                    const SizedBox(width: 12),
                    Expanded(child: Text(t.pcLkWorking)),
                  ],
                )
              else
                NeonButton(label: t.pcLkButton, icon: Icons.memory, onPressed: () => _become(context)),
            ],
          ),
        ),
      );
    }

    final r = state.rotation;
    return Padding(
      padding: const EdgeInsets.only(top: 20),
      child: NeonCard(
        padding: const EdgeInsets.fromLTRB(18, 10, 10, 14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: Text('🖥️ ${state.pcLockKeeperOn ? t.pcLkOn : t.pcLkOff}', style: title),
              value: state.pcLockKeeperOn,
              activeThumbColor: Brand.neonGreen,
              onChanged: (v) async {
                final messenger = ScaffoldMessenger.of(context);
                try {
                  await state.setPcLockKeeperOn(v);
                } catch (e) {
                  // Never silent: the switch would just stay off.
                  messenger.showSnackBar(SnackBar(content: Text(t.pcLkError('$e'))));
                }
              },
            ),
            // The owner's choice, explained: start with Windows or not.
            if (state.pcLockKeeperOn)
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                dense: true,
                title: Text('🔁 ${t.pcLkAutostart}', style: const TextStyle(color: Brand.text, fontWeight: FontWeight.w600)),
                subtitle: Text(t.pcLkAutostartHint, style: const TextStyle(color: Brand.pearl, height: 1.35)),
                value: state.pcAutostart,
                activeThumbColor: Brand.neonGreen,
                onChanged: (v) => state.setPcAutostart(v),
              ),
            if (state.pcLockKeeperOn && r != null) ...[
              Text(
                r.signing ? '✍️ ${t.pcLkSigning}' : '🔎 ${t.pcLkWaiting}',
                style: const TextStyle(color: Brand.text),
              ),
              const SizedBox(height: 4),
            ],
            // Something to know: behind the network, chip not confirmed, or
            // no "I am on" reaching the network.
            if (state.pcLockKeeperOn && (state.pcBehind ?? 0) > 30)
              _Warning('⏳ ${t.pcLkBehind(state.pcBehind!)}', Brand.neonBlue)
            // Only when the open wallet is the LockKeeper's owner (another wallet
            // has nothing to prove).
            else if (state.pcLockKeeperOn && r != null && !r.chip && (state.status?.isLockKeeper ?? false))
              _Warning('⚠️ ${t.pcLkNoChip}', Brand.danger)
            else if (state.pcNotSeen)
              _Warning('⚠️ ${t.pcLkNotSeen}', Brand.danger),
            if (r != null) HoursLog(r),
          ],
        ),
      ),
    );
  }
}

/// A short warning line on the card.
class _Warning extends StatelessWidget {
  const _Warning(this.text, this.color);

  final String text;
  final Color color;

  @override
  Widget build(BuildContext context) => Padding(
        padding: const EdgeInsets.only(bottom: 6),
        child: Text(text, style: TextStyle(color: color, height: 1.35, fontWeight: FontWeight.w600)),
      );
}

/// The hours log of a LockKeeper: today, yesterday, 7 days, 30 days.
class HoursLog extends StatelessWidget {
  const HoursLog(this.r, {super.key});

  final LockKeeperRotation r;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    String h(int seconds) => (seconds / 3600).toStringAsFixed(1);
    return Text(
      '⏱️ ${t.pcLkHoursLog(h(r.today), h(r.yesterday), h(r.week), h(r.month))}',
      style: const TextStyle(color: Brand.pearl, height: 1.4),
    );
  }
}
