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
    // The guarantee plus a little for the fee; a wallet that is already a
    // LockKeeper (its guarantee already in) only moves to this computer.
    if (!(state.status?.isLockKeeper ?? false) && balance < need + BigInt.from(1000000)) {
      state.setPcNote(t.pcLkNeedMore(formatLock(need + BigInt.from(1000000), locale)), bad: true);
      return;
    }
    state.setPcNote(null);
    if (!await confirmPassword(context) || !context.mounted) return;
    try {
      // Once: the security chip (Windows asks "Allow?").
      if (!PcLockKeeper.isSetUp && !PcLockKeeper.hasChipTicket) {
        if (!await confirmChipCheck(context)) return;
        final maker = await state.checkPcChip();
        state.setPcNote('✅ ${t.chipOk(maker)}');
      }
      await state.becomePcLockKeeper();
    } on ChipProblem catch (e) {
      state.setPcNote(chipProblemText(t, e), bad: true);
    } catch (e) {
      final msg = e is ChainError ? e.log : '$e';
      state.setPcNote(t.pcLkError(msg), bad: true);
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
              PcNote(state: state),
            ],
          ),
        ),
      );
    }

    final r = state.rotation;
    final behind = state.pcLockKeeperOn && (state.pcBehind ?? 0) > 30;
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
            // At a glance (founder, 25 Oct 2026): a green or red light for
            // "connected", and for signing while it is up to date (behind,
            // "signing" would only confuse: 24 Oct a friend saw both).
            if (state.pcLockKeeperOn && (r != null || behind))
              PcLockKeeperLights(rotation: r, behind: behind ? state.pcBehind : null),
            // Something else to know: chip not confirmed, or no "I am on"
            // reaching the network. Only when the open wallet is the
            // LockKeeper's owner (another wallet has nothing to prove).
            if (behind)
              const SizedBox.shrink()
            else if (state.pcLockKeeperOn && r != null && !r.chip && (state.status?.isLockKeeper ?? false)) ...[
              _Warning('⚠️ ${t.pcLkNoChip}', Brand.danger),
              // The network's checker gives this computer's chip again (a
              // chip counts only while a LockKeeper works with it); the "I
              // am on" program sends the new ticket by itself.
              NeonButton(label: t.pcLkChipAgain, icon: Icons.memory, secondary: true, onPressed: () => _chipAgain(context)),
              const SizedBox(height: 6),
            ]
            else if (state.pcNotSeen)
              _Warning('⚠️ ${t.pcLkNotSeen}', Brand.danger),
            if (r != null) HoursLog(r),
            PcNote(state: state),
          ],
        ),
      ),
    );
  }
}

/// The LockKeeper's state at a glance: connected, and signing (or waiting
/// for its turn); while catching up (behind: blocks to go), what to do
/// instead of "signing", which would only confuse.
class PcLockKeeperLights extends StatelessWidget {
  const PcLockKeeperLights({super.key, this.rotation, this.behind});

  final LockKeeperRotation? rotation;
  final int? behind;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final r = rotation;
    final late = behind != null;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (r != null) ...[
          _Light(on: r.online && !late, text: r.online && !late ? t.pcLkConnected : t.pcLkNotConnected),
          if (r.online && !late) _Light(on: r.signing, text: r.signing ? t.pcLkSigning : t.pcLkWaiting, waiting: !r.signing),
          const SizedBox(height: 6),
        ],
        // Last step when nothing helps: write to us (the address needs no
        // translation).
        if (late) _Warning('⏳ ${t.pcLkBehind(behind!)}\n${t.pcLkBehindHelp}\n✉️ encryptlockoficial@proton.me', Brand.neonBlue),
      ],
    );
  }
}

/// A status line with a light: green when all is well, red when not, amber
/// while waiting for its turn to sign (normal, it still earns).
class _Light extends StatelessWidget {
  const _Light({required this.on, required this.text, this.waiting = false});

  final bool on;
  final bool waiting;
  final String text;

  @override
  Widget build(BuildContext context) {
    final color = on ? Brand.neonGreen : (waiting ? const Color(0xFFF2D27A) : Brand.danger);
    return Padding(
      padding: const EdgeInsets.only(bottom: 6),
      child: Row(
        children: [
          Container(
            width: 12,
            height: 12,
            decoration: BoxDecoration(
              color: color,
              shape: BoxShape.circle,
              boxShadow: [BoxShadow(color: color.withValues(alpha: 0.7), blurRadius: 8)],
            ),
          ),
          const SizedBox(width: 10),
          Expanded(child: Text(text, style: const TextStyle(color: Brand.text, fontWeight: FontWeight.w600))),
        ],
      ),
    );
  }
}

/// A short warning line on the card.
/// What the chip check said, in words.
String chipProblemText(AppLocalizations t, ChipProblem e) => e.kind == ChipProblemKind.cancelled
    ? t.chipCancelled
    : e.detail.isEmpty
        ? t.chipNotAccepted
        : '${t.chipNotAccepted}\n(${e.detail})';

/// Checks this computer's chip again, for a LockKeeper whose chip the network
/// has not confirmed.
Future<void> _chipAgain(BuildContext context) async {
  final t = AppLocalizations.of(context);
  final state = AppScope.of(context);
  if (!await confirmChipCheck(context)) return;
  state.setPcNote(null);
  try {
    final maker = await state.checkPcChip();
    state.setPcNote('✅ ${t.chipOk(maker)}');
  } on ChipProblem catch (e) {
    state.setPcNote(chipProblemText(t, e), bad: true);
  } catch (e) {
    state.setPcNote(t.pcLkError('$e'), bad: true);
  }
}

/// The last result of the LockKeeper button or the chip check: it stays in
/// the card until the owner closes it.
class PcNote extends StatelessWidget {
  const PcNote({super.key, required this.state});

  final AppState state;

  @override
  Widget build(BuildContext context) {
    final note = state.pcNote;
    if (note == null) return const SizedBox.shrink();
    final color = state.pcNoteBad ? Brand.danger : Brand.neonGreen;
    return Container(
      margin: const EdgeInsets.only(top: 12),
      padding: const EdgeInsets.fromLTRB(12, 10, 4, 10),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.10),
        border: Border.all(color: color.withValues(alpha: 0.7)),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Expanded(child: SelectableText(note, style: TextStyle(color: color, height: 1.4, fontWeight: FontWeight.w600))),
          IconButton(
            icon: const Icon(Icons.close, size: 18),
            color: Brand.pearl,
            tooltip: MaterialLocalizations.of(context).closeButtonTooltip,
            onPressed: () => state.setPcNote(null),
          ),
        ],
      ),
    );
  }
}

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
