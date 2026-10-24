import 'dart:async';

import 'package:flutter/material.dart';
import 'package:qr_flutter/qr_flutter.dart';

import '../app_state.dart';
import '../core/network.dart' show ChainError, LockKeeperClient, LockKeeperRotation;
import '../core/pc_lockkeeper.dart';
import '../l10n/gen/app_localizations.dart';
import '../theme.dart';
import '../widgets/brand.dart';
import 'home.dart' show formatLock;
import 'pc_lockkeeper_card.dart' show HoursLog, confirmChipCheck;
import 'security.dart';
import 'send.dart' show ScanScreen;

/// What the computer's QR code holds: its helper account.
const _qrPrefix = 'encryptlock-pc:';

/// Computer: "Make this computer a blockchain LockKeeper" with the phone.
/// Shows a QR code; the phone scans it and pays the guarantee. The 24 words
/// never come to the computer. Afterwards: the state and the on/off switch.
class PcLinkScreen extends StatefulWidget {
  const PcLinkScreen({super.key});

  @override
  State<PcLinkScreen> createState() => _PcLinkScreenState();
}

class _PcLinkScreenState extends State<PcLinkScreen> {
  String? _helper;
  String? _keeper;
  String? _error;
  LockKeeperRotation? _rotation;
  Timer? _poll;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _start());
  }

  @override
  void dispose() {
    _poll?.cancel();
    super.dispose();
  }

  Future<void> _start() async {
    final state = AppScope.of(context);
    final t = AppLocalizations.of(context);
    try {
      // Once: the security chip (Windows asks "Allow?").
      if (!PcLockKeeper.isSetUp && !PcLockKeeper.hasChipTicket) {
        if (!await confirmChipCheck(context)) {
          if (mounted) Navigator.of(context).pop();
          return;
        }
        await state.checkPcChip();
      }
      final helper = await state.preparePcForPhone();
      if (!mounted) return;
      setState(() => _helper = helper);
      _poll = Timer.periodic(const Duration(seconds: 3), (_) => _check());
      await _check();
    } on ChipProblem catch (e) {
      final msg = e.kind == ChipProblemKind.cancelled
          ? t.chipCancelled
          : e.detail.isEmpty
              ? t.chipNotAccepted
              : '${t.chipNotAccepted}\n(${e.detail})';
      if (mounted) setState(() => _error = msg);
    } catch (e) {
      if (mounted) setState(() => _error = e is ChainError ? e.log : '$e');
    }
  }

  Future<void> _check() async {
    final state = AppScope.of(context);
    final helper = _helper;
    if (helper == null) return;
    try {
      final keeper = await state.linkedKeeper(helper);
      if (keeper != null && !PcLockKeeper.isSetUp) PcLockKeeper.markSetUp();
      final rotation = keeper == null ? null : await LockKeeperClient(state.lockKeeperUrl).rotation(keeper);
      if (mounted) {
        setState(() {
          _keeper = keeper;
          _rotation = rotation;
        });
      }
    } catch (_) {
      // The network may still be starting: next round.
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final helper = _helper;
    final keeper = _keeper;
    final children = <Widget>[];
    if (_error != null) {
      children.add(NeonCard(glowColor: Brand.danger, child: Text(t.pcLkError(_error!))));
    } else if (helper == null) {
      children.add(Row(
        children: [
          const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2)),
          const SizedBox(width: 12),
          Expanded(child: Text(t.pcLkWorking)),
        ],
      ));
    } else if (keeper == null) {
      children.addAll([
        Text(t.pcLkBody, style: const TextStyle(color: Brand.text, height: 1.45)),
        const SizedBox(height: 18),
        Center(
          child: Container(
            padding: const EdgeInsets.all(14),
            decoration: BoxDecoration(
              color: Colors.white,
              borderRadius: BorderRadius.circular(24),
              boxShadow: Brand.glow(Brand.neonGreen, 0.8),
            ),
            child: QrImageView(data: '$_qrPrefix$helper', size: 240, backgroundColor: Colors.white),
          ),
        ),
        const SizedBox(height: 18),
        NeonCard(child: Text(t.linkPcSteps, style: const TextStyle(height: 1.5))),
        const SizedBox(height: 14),
        Row(
          children: [
            const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2)),
            const SizedBox(width: 12),
            Expanded(child: Text(t.linkPcWaiting, style: const TextStyle(color: Brand.pearl))),
          ],
        ),
      ]);
    } else {
      final r = _rotation;
      children.addAll([
        NeonCard(
          child: Text(
            '✅ ${t.linkPcDone('${keeper.substring(0, 12)}…${keeper.substring(keeper.length - 6)}')}',
            style: const TextStyle(fontWeight: FontWeight.w700, height: 1.4),
          ),
        ),
        const SizedBox(height: 14),
        NeonCard(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                title: Text('🖥️ ${state.pcLockKeeperOn ? t.pcLkOn : t.pcLkOff}'),
                value: state.pcLockKeeperOn,
                activeThumbColor: Brand.neonGreen,
                onChanged: (v) => state.setPcLockKeeperOn(v),
              ),
              if (state.pcLockKeeperOn && r != null)
                Text(r.signing ? '✍️ ${t.pcLkSigning}' : '🔎 ${t.pcLkWaiting}'),
              const SizedBox(height: 4),
              if (r != null) HoursLog(r),
            ],
          ),
        ),
      ]);
    }
    return Scaffold(
      appBar: BrandBar(title: t.pcLkTitle),
      body: ChipBackground(
        child: Align(
          alignment: Alignment.topCenter,
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 720),
            child: ListView(padding: const EdgeInsets.all(24), children: children),
          ),
        ),
      ),
    );
  }
}

/// Phone: scan a computer's QR code and make it a blockchain LockKeeper of
/// this wallet (the guarantee comes from here; the earnings come here).
Future<void> linkComputerFromPhone(BuildContext context) async {
  final t = AppLocalizations.of(context);
  final state = AppScope.of(context);
  final locale = Localizations.localeOf(context).toString();
  final messenger = ScaffoldMessenger.of(context);
  final code = await Navigator.push<String>(context, MaterialPageRoute(builder: (_) => const ScanScreen()));
  if (code == null || !context.mounted) return;
  final helper = code.trim().toLowerCase();
  if (!helper.startsWith(_qrPrefix) || !await state.core.isAddress(helper.substring(_qrPrefix.length))) {
    messenger.showSnackBar(SnackBar(content: Text(t.linkPhoneBadCode)));
    return;
  }
  if (!context.mounted) return;
  final need = state.pcMinGuarantee;
  final fee = BigInt.from(1000000);
  if ((state.status?.balance ?? BigInt.zero) < need + fee) {
    messenger.showSnackBar(SnackBar(content: Text(t.pcLkNeedMore(formatLock(need + fee, locale)))));
    return;
  }
  final sure = await showDialog<bool>(
    context: context,
    builder: (dialogContext) => AlertDialog(
      backgroundColor: Brand.bottomBar,
      title: Text(t.pcLkTitle),
      content: Text(t.linkPhoneConfirm(formatLock(need, locale)), style: const TextStyle(height: 1.4)),
      actions: [
        TextButton(onPressed: () => Navigator.pop(dialogContext, false), child: Text(t.cancel)),
        TextButton(onPressed: () => Navigator.pop(dialogContext, true), child: Text(t.confirm)),
      ],
    ),
  );
  if (sure != true || !context.mounted) return;
  if (!await confirmPassword(context)) return;
  try {
    await state.linkComputer(helper.substring(_qrPrefix.length));
    messenger.showSnackBar(SnackBar(content: Text(t.linkPhoneDone)));
  } on ChainError catch (e) {
    messenger.showSnackBar(SnackBar(content: Text(e.log == 'no key' ? t.linkPhoneNoKey : t.pcLkError(e.log))));
  } catch (e) {
    messenger.showSnackBar(SnackBar(content: Text(t.pcLkError('$e'))));
  }
}

/// Code for linking a computer, which Send must not take as an address.
bool isComputerLinkCode(String code) => code.trim().toLowerCase().startsWith(_qrPrefix);
