import 'dart:io';

import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

import '../app_state.dart';
import '../core/market.dart' show MarketOffer, usdcToDouble;
import '../core/network.dart';
import '../l10n/gen/app_localizations.dart';
import '../theme.dart';
import '../widgets/brand.dart';
import 'home.dart' show formatLock;
import 'market.dart' show formatUsd;
import 'security.dart' show chainErrorText;
import 'send.dart' show ScanScreen;

String shortAddress(String a) => a.length > 16 ? '${a.substring(0, 10)}…${a.substring(a.length - 6)}' : a;

String _date(BuildContext context, int unixSeconds) => DateFormat.yMMMd(
  Localizations.localeOf(context).toString(),
).add_Hm().format(DateTime.fromMillisecondsSinceEpoch(unixSeconds * 1000));

/// Runs a network action and shows the result (or the refusal) at the bottom.
Future<bool> runAction(BuildContext context, Future<void> Function() action, String done) async {
  final t = AppLocalizations.of(context);
  final messenger = ScaffoldMessenger.of(context);
  try {
    await action();
    messenger.showSnackBar(SnackBar(content: Text(done)));
    return true;
  } on ChainError catch (e) {
    messenger.showSnackBar(SnackBar(content: Text(chainErrorText(t, e.log))));
  } catch (e) {
    debugPrint('transaction failed: $e');
    messenger.showSnackBar(SnackBar(content: Text(t.networkError)));
  }
  return false;
}

/// Address field with a QR button (to read the other phone's Receive code).
class AddressField extends StatelessWidget {
  const AddressField({super.key, required this.controller, required this.label});

  final TextEditingController controller;
  final String label;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    return TextField(
      controller: controller,
      autocorrect: false,
      enableSuggestions: false,
      minLines: 1,
      maxLines: 2,
      decoration: InputDecoration(
        labelText: label,
        suffixIcon: !Platform.isAndroid
            ? null
            : IconButton(
                tooltip: t.scanQr,
                icon: const Icon(Icons.qr_code_scanner, color: Brand.neonBlue),
                onPressed: () async {
                  final code = await Navigator.push<String>(
                    context,
                    MaterialPageRoute(builder: (_) => const ScanScreen()),
                  );
                  if (code == null) return;
                  controller.text = RegExp(r'lock1[0-9a-z]+').firstMatch(code.toLowerCase())?.group(0) ?? code;
                },
              ),
      ),
    );
  }
}

/// Second device: another EncryptLock wallet (ideally another phone) that can
/// only approve and freeze, never send.
class DeviceScreen extends StatefulWidget {
  const DeviceScreen({super.key});

  @override
  State<DeviceScreen> createState() => _DeviceScreenState();
}

class _DeviceScreenState extends State<DeviceScreen> {
  final _device = TextEditingController();
  String? _error;
  bool _busy = false;

  @override
  void dispose() {
    _device.dispose();
    super.dispose();
  }

  Future<void> _set(String device) async {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    if (device.isNotEmpty && (!await state.core.isAddress(device) || device == state.address)) {
      setState(() => _error = t.invalidAddress);
      return;
    }
    if (!mounted) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    final ok = await runAction(context, () => state.setDevice(device), t.deviceSaved);
    if (!mounted) return;
    setState(() => _busy = false);
    if (ok) Navigator.pop(context);
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final shield = AppScope.of(context).status?.shield ?? const Shield();
    return Scaffold(
      appBar: BrandBar(title: t.shieldDevice),
      body: ChipBackground(
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            NeonCard(
              glowColor: Brand.neonBlue,
              child: Text(t.deviceExplain, style: const TextStyle(height: 1.4)),
            ),
            const SizedBox(height: 20),
            NeonCard(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(t.shieldDevice, style: const TextStyle(color: Brand.pearl)),
                  const SizedBox(height: 6),
                  SelectableText(
                    shield.device.isEmpty ? t.deviceNone : shield.device,
                    style: const TextStyle(color: Brand.neonGreen, fontWeight: FontWeight.w600),
                  ),
                  if (shield.deviceChangePending) ...[
                    const SizedBox(height: 10),
                    Text(
                      t.deviceChangeWait(
                        shield.pendingDevice.isEmpty ? t.deviceNone : shortAddress(shield.pendingDevice),
                        _date(context, shield.deviceChangeAt),
                      ),
                      style: const TextStyle(color: Brand.pearl),
                    ),
                  ],
                ],
              ),
            ),
            const SizedBox(height: 20),
            AddressField(controller: _device, label: t.deviceAddress),
            ErrorText(_error),
            const SizedBox(height: 20),
            if (_busy)
              const Center(child: CircularProgressIndicator(color: Brand.neonGreen))
            else ...[
              NeonButton(label: t.deviceLink, icon: Icons.phonelink_lock, onPressed: () => _set(_device.text.trim())),
              if (shield.device.isNotEmpty) ...[
                const SizedBox(height: 14),
                NeonButton(label: t.deviceRemove, icon: Icons.link_off, secondary: true, onPressed: () => _set('')),
              ],
            ],
          ],
        ),
      ),
    );
  }
}

/// Daily limit and the amount above which the second device must approve.
class LimitsScreen extends StatefulWidget {
  const LimitsScreen({super.key});

  @override
  State<LimitsScreen> createState() => _LimitsScreenState();
}

class _LimitsScreenState extends State<LimitsScreen> {
  final _daily = TextEditingController();
  final _above = TextEditingController();
  String? _error;
  bool _busy = false;

  @override
  void dispose() {
    _daily.dispose();
    _above.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    Future<int?> units(TextEditingController c) async => c.text.trim().isEmpty ? 0 : await state.core.parseLock(c.text);
    final daily = await units(_daily);
    final above = await units(_above);
    if (daily == null || above == null) {
      setState(() => _error = t.invalidAmount);
      return;
    }
    if (!mounted) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    final ok = await runAction(context, () => state.setLimits(daily, above), t.limitsSaved);
    if (!mounted) return;
    setState(() => _busy = false);
    if (ok) Navigator.pop(context);
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final locale = Localizations.localeOf(context).toString();
    final shield = AppScope.of(context).status?.shield ?? const Shield();
    String lock(int units) => formatLock(BigInt.from(units), locale);
    return Scaffold(
      appBar: BrandBar(title: t.limits),
      body: ChipBackground(
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            NeonCard(
              glowColor: Brand.neonBlue,
              child: Text(t.limitsExplain, style: const TextStyle(height: 1.4)),
            ),
            const SizedBox(height: 20),
            NeonCard(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    shield.dailyLimit == 0 ? t.limitsNone : t.limitsValue(lock(shield.dailyLimit)),
                    style: const TextStyle(color: Brand.neonGreen, fontWeight: FontWeight.w600),
                  ),
                  if (shield.approvalAbove > 0) ...[
                    const SizedBox(height: 6),
                    Text(t.approvalAboveValue(lock(shield.approvalAbove)), style: const TextStyle(color: Brand.pearl)),
                  ],
                  if (shield.limitsChangeAt > 0) ...[
                    const SizedBox(height: 6),
                    Text(
                      t.limitsChangeWait(_date(context, shield.limitsChangeAt)),
                      style: const TextStyle(color: Brand.pearl),
                    ),
                  ],
                ],
              ),
            ),
            const SizedBox(height: 20),
            Text(t.dailyLimitField, style: const TextStyle(color: Brand.pearl)),
            const SizedBox(height: 8),
            TextField(
              controller: _daily,
              keyboardType: const TextInputType.numberWithOptions(decimal: true),
              decoration: const InputDecoration(suffixText: 'LOCK'),
            ),
            const SizedBox(height: 16),
            Text(t.approvalAboveField, style: const TextStyle(color: Brand.pearl)),
            const SizedBox(height: 8),
            TextField(
              controller: _above,
              keyboardType: const TextInputType.numberWithOptions(decimal: true),
              decoration: const InputDecoration(suffixText: 'LOCK'),
            ),
            ErrorText(_error),
            const SizedBox(height: 24),
            _busy
                ? const Center(child: CircularProgressIndicator(color: Brand.neonGreen))
                : NeonButton(label: t.save, icon: Icons.speed, onPressed: _save),
          ],
        ),
      ),
    );
  }
}

/// This phone as the second device of other accounts.
class GuardScreen extends StatefulWidget {
  const GuardScreen({super.key});

  @override
  State<GuardScreen> createState() => _GuardScreenState();
}

class _GuardScreenState extends State<GuardScreen> {
  final _account = TextEditingController();
  String? _error;

  @override
  void dispose() {
    _account.dispose();
    super.dispose();
  }

  Future<void> _add() async {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final account = _account.text.trim();
    if (!await state.core.isAddress(account) || account == state.address) {
      setState(() => _error = t.invalidAddress);
      return;
    }
    if (!state.guarded.contains(account)) await state.setGuarded([...state.guarded, account]);
    _account.clear();
    setState(() => _error = null);
    state.refresh();
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    return Scaffold(
      appBar: BrandBar(title: t.guardOther),
      body: ChipBackground(
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            NeonCard(
              glowColor: Brand.neonBlue,
              child: Text(t.guardExplain, style: const TextStyle(height: 1.4)),
            ),
            const SizedBox(height: 20),
            NeonCard(
              padding: const EdgeInsets.symmetric(vertical: 6),
              child: state.guarded.isEmpty
                  ? Padding(padding: const EdgeInsets.all(16), child: Text(t.deviceNone))
                  : Column(
                      children: [
                        for (final a in state.guarded)
                          ListTile(
                            leading: const Text('🛡️', style: TextStyle(fontSize: 22)),
                            title: Text(shortAddress(a), style: const TextStyle(fontWeight: FontWeight.w600)),
                            trailing: IconButton(
                              icon: const Icon(Icons.close, color: Brand.pearl),
                              onPressed: () => state.setGuarded([
                                for (final g in state.guarded)
                                  if (g != a) g,
                              ]),
                            ),
                          ),
                      ],
                    ),
            ),
            const SizedBox(height: 20),
            AddressField(controller: _account, label: t.guardAccountField),
            ErrorText(_error),
            const SizedBox(height: 20),
            NeonButton(label: t.guardAdd, icon: Icons.add_moderator_outlined, onPressed: _add),
          ],
        ),
      ),
    );
  }
}

/// On the Wallet screen of a second device: what waits for its approval.
class ApprovalsCard extends StatefulWidget {
  const ApprovalsCard({super.key});

  @override
  State<ApprovalsCard> createState() => _ApprovalsCardState();
}

class _ApprovalsCardState extends State<ApprovalsCard> {
  bool _busy = false;

  Future<void> _do(Future<void> Function() action, String done) async {
    setState(() => _busy = true);
    await runAction(context, action, done);
    if (mounted) setState(() => _busy = false);
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final locale = Localizations.localeOf(context).toString();
    if (state.guarded.isEmpty) return const SizedBox.shrink();
    return Padding(
      padding: const EdgeInsets.only(top: 20),
      child: NeonCard(
        glowColor: Brand.neonBlue,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(t.approvalsTitle, style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w700)),
            for (final account in state.guarded) ...[
              const SizedBox(height: 12),
              Row(
                children: [
                  const Text('🛡️', style: TextStyle(fontSize: 20)),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(shortAddress(account), style: const TextStyle(color: Brand.pearl)),
                  ),
                  TextButton(
                    onPressed: _busy ? null : () => _do(() => state.freezeGuarded(account), t.frozenOk),
                    child: Text(t.notMe, style: const TextStyle(color: Brand.danger)),
                  ),
                ],
              ),
              for (final p in state.approvals[account] ?? const <PendingTransfer>[])
                Row(
                  children: [
                    const SizedBox(width: 28),
                    Expanded(
                      child: Text(
                        '−${formatLock(p.amount, locale)} LOCK  →  ${shortAddress(p.recipient)}',
                        style: const TextStyle(color: Brand.neonBlue, fontWeight: FontWeight.w600),
                      ),
                    ),
                    FilledButton(
                      style: FilledButton.styleFrom(backgroundColor: const Color(0xFF237A43)),
                      onPressed: _busy ? null : () => _do(() => state.approve(p.id), t.approvedOk),
                      child: Text(t.approve, style: const TextStyle(color: Brand.text)),
                    ),
                  ],
                ),
              // Sell offers on the market wait for this phone too.
              for (final o in state.offerApprovals[account] ?? const <MarketOffer>[])
                Row(
                  children: [
                    const SizedBox(width: 28),
                    Expanded(
                      child: Text(
                        '🏷️ ${t.approveSale(formatLock(o.lock, locale), formatUsd(usdcToDouble(o.usdc), locale))}',
                        style: const TextStyle(color: Brand.neonBlue, fontWeight: FontWeight.w600),
                      ),
                    ),
                    FilledButton(
                      style: FilledButton.styleFrom(backgroundColor: const Color(0xFF237A43)),
                      onPressed: _busy ? null : () => _do(() => state.approveOffer(o.id), t.approvedOk),
                      child: Text(t.approve, style: const TextStyle(color: Brand.text)),
                    ),
                  ],
                ),
            ],
          ],
        ),
      ),
    );
  }
}
