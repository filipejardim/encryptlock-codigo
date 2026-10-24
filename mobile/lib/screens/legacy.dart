import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

import '../app_state.dart';
import '../core/network.dart';
import '../l10n/gen/app_localizations.dart';
import '../theme.dart';
import '../widgets/brand.dart';
import 'home.dart' show formatLock;
import 'shield.dart' show AddressField, runAction, shortAddress;

String _date(BuildContext context, int unixSeconds) => DateFormat.yMMMd(Localizations.localeOf(context).toString())
    .add_Hm()
    .format(DateTime.fromMillisecondsSinceEpoch(unixSeconds * 1000));

TextStyle get _label => const TextStyle(color: Brand.pearl);

/// Loads something from the network and shows it, with pull to refresh.
class _Loaded<T> extends StatefulWidget {
  const _Loaded({super.key, required this.load, required this.builder});

  final Future<T> Function(AppState state) load;
  final Widget Function(BuildContext context, T value, VoidCallback reload) builder;

  @override
  State<_Loaded<T>> createState() => _LoadedState<T>();
}

class _LoadedState<T> extends State<_Loaded<T>> {
  Future<T>? _future;

  void _reload() => setState(() => _future = widget.load(AppScope.of(context)));

  @override
  Widget build(BuildContext context) {
    _future ??= widget.load(AppScope.of(context));
    return FutureBuilder<T>(
      future: _future,
      builder: (context, snap) {
        if (snap.hasError) {
          return Center(child: Text(AppLocalizations.of(context).networkError, textAlign: TextAlign.center));
        }
        if (!snap.hasData) return const Center(child: CircularProgressIndicator(color: Brand.neonGreen));
        return snap.requireData == null ? const SizedBox.shrink() : widget.builder(context, snap.requireData, _reload);
      },
    );
  }
}

/// Address list with a remove button per row (heirs carry a percent).
class _AddressRow extends StatelessWidget {
  const _AddressRow({required this.address, this.trailing, required this.onRemove});

  final String address;
  final String? trailing;
  final VoidCallback onRemove;

  @override
  Widget build(BuildContext context) => Row(
        children: [
          Expanded(child: Text(shortAddress(address), style: const TextStyle(fontWeight: FontWeight.w600))),
          if (trailing != null) Text(trailing!, style: const TextStyle(color: Brand.neonGreen)),
          IconButton(icon: const Icon(Icons.close, color: Brand.pearl), onPressed: onRemove),
        ],
      );
}

// ---------------------------------------------------------------- Inheritance

class InheritanceScreen extends StatelessWidget {
  const InheritanceScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    return Scaffold(
      appBar: BrandBar(title: t.inheritance),
      body: ChipBackground(
        child: _Loaded<(RecoveryStatus, bool)>(
          // Also: is this wallet the development fund? (Only its owner sees the note.)
          load: (s) async => (await s.client.recovery(s.address!), await s.client.devFund() == s.address),
          builder: (context, r, reload) => _InheritanceForm(plan: r.$1, devFund: r.$2, reload: reload),
        ),
      ),
    );
  }
}

class _InheritanceForm extends StatefulWidget {
  const _InheritanceForm({required this.plan, required this.reload, this.devFund = false});

  final RecoveryStatus plan;

  /// This wallet is the development fund: its future fees go to the first heir.
  final bool devFund;
  final VoidCallback reload;

  @override
  State<_InheritanceForm> createState() => _InheritanceFormState();
}

class _InheritanceFormState extends State<_InheritanceForm> {
  late final List<Heir> _heirs = [...widget.plan.heirs];
  late final _days = TextEditingController(
    text: '${widget.plan.inactivityDays == 0 ? 365 : widget.plan.inactivityDays}',
  );
  final _address = TextEditingController();
  final _percent = TextEditingController();
  String? _error;
  bool _busy = false;

  @override
  void dispose() {
    _days.dispose();
    _address.dispose();
    _percent.dispose();
    super.dispose();
  }

  Future<void> _add() async {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final address = _address.text.trim();
    final percent = int.tryParse(_percent.text.trim()) ?? 0;
    if (!await state.core.isAddress(address) || address == state.address) {
      setState(() => _error = t.invalidAddress);
      return;
    }
    if (percent < 1 || percent > 100) {
      setState(() => _error = t.percentsMust100);
      return;
    }
    setState(() {
      _heirs.add(Heir(address, percent));
      _address.clear();
      _percent.clear();
      _error = null;
    });
  }

  Future<void> _save() async {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final days = int.tryParse(_days.text.trim()) ?? 0;
    if (_heirs.isNotEmpty && _heirs.fold(0, (sum, h) => sum + h.percent) != 100) {
      setState(() => _error = t.percentsMust100);
      return;
    }
    if (days < 365) {
      setState(() => _error = t.inactivityDaysField);
      return;
    }
    final p = widget.plan;
    setState(() {
      _busy = true;
      _error = null;
    });
    await runAction(
      context,
      () => state.setPlan(RecoveryStatus(
        heirs: _heirs,
        inactivityDays: days,
        guardians: p.guardians,
        guardianThreshold: p.guardianThreshold,
        heirThreshold: p.heirThreshold,
      )),
      t.planSaved,
    );
    if (!mounted) return;
    setState(() => _busy = false);
    widget.reload();
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final p = widget.plan;
    return ListView(
      padding: const EdgeInsets.all(20),
      children: [
        NeonCard(child: Text(t.inheritanceExplain, style: const TextStyle(height: 1.4))),
        const SizedBox(height: 16),
        if (widget.devFund) ...[
          NeonCard(
            glowColor: Brand.neonBlue,
            child: Text('🏛️ ${t.devFundHeirNote}', style: const TextStyle(height: 1.4, fontWeight: FontWeight.w600)),
          ),
          const SizedBox(height: 16),
        ],
        NeonCard(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              if (p.lastActive > 0) Text(t.lastAlive(_date(context, p.lastActive)), style: _label),
              const SizedBox(height: 10),
              NeonButton(
                label: t.imHere,
                icon: Icons.favorite,
                onPressed: _busy
                    ? null
                    : () async {
                        await runAction(context, state.alive, t.aliveOk);
                        widget.reload();
                      },
              ),
            ],
          ),
        ),
        const SizedBox(height: 16),
        if (p.planChangeAt > 0) ...[
          Text(t.planChangeWait(_date(context, p.planChangeAt)), style: _label),
          const SizedBox(height: 12),
        ],
        for (var i = 0; i < _heirs.length; i++)
          _AddressRow(
            address: _heirs[i].address,
            trailing: '${_heirs[i].percent}%',
            onRemove: () => setState(() => _heirs.removeAt(i)),
          ),
        const SizedBox(height: 8),
        AddressField(controller: _address, label: t.heirAddress),
        const SizedBox(height: 12),
        Row(
          children: [
            SizedBox(
              width: 110,
              child: TextField(
                controller: _percent,
                keyboardType: TextInputType.number,
                decoration: const InputDecoration(suffixText: '%'),
              ),
            ),
            const SizedBox(width: 12),
            Expanded(child: NeonButton(label: t.addHeir, icon: Icons.person_add, secondary: true, onPressed: _add)),
          ],
        ),
        const SizedBox(height: 18),
        Text(t.inactivityDaysField, style: _label),
        const SizedBox(height: 8),
        TextField(controller: _days, keyboardType: TextInputType.number),
        ErrorText(_error),
        const SizedBox(height: 20),
        _busy
            ? const Center(child: CircularProgressIndicator(color: Brand.neonGreen))
            : NeonButton(label: t.save, icon: Icons.save, onPressed: _save),
      ],
    );
  }
}

// ------------------------------------------------------------ Trusted people

class GuardiansScreen extends StatelessWidget {
  const GuardiansScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    return Scaffold(
      appBar: BrandBar(title: t.guardians),
      body: ChipBackground(
        child: _Loaded<RecoveryStatus>(
          load: (s) => s.client.recovery(s.address!),
          builder: (context, plan, reload) => _GuardiansForm(plan: plan, reload: reload),
        ),
      ),
    );
  }
}

class _GuardiansForm extends StatefulWidget {
  const _GuardiansForm({required this.plan, required this.reload});

  final RecoveryStatus plan;
  final VoidCallback reload;

  @override
  State<_GuardiansForm> createState() => _GuardiansFormState();
}

class _GuardiansFormState extends State<_GuardiansForm> {
  late final List<String> _people = [...widget.plan.guardians];
  late final _needed = TextEditingController(
    text: '${widget.plan.guardianThreshold < 2 ? 2 : widget.plan.guardianThreshold}',
  );
  final _address = TextEditingController();
  String? _error;
  bool _busy = false;

  @override
  void dispose() {
    _needed.dispose();
    _address.dispose();
    super.dispose();
  }

  Future<void> _add() async {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final address = _address.text.trim();
    if (!await state.core.isAddress(address) || address == state.address || _people.contains(address)) {
      setState(() => _error = t.invalidAddress);
      return;
    }
    setState(() {
      _people.add(address);
      _address.clear();
      _error = null;
    });
  }

  Future<void> _save() async {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final needed = int.tryParse(_needed.text.trim()) ?? 0;
    if (_people.isNotEmpty && (needed < 2 || needed > _people.length)) {
      setState(() => _error = t.guardiansNeeded);
      return;
    }
    final p = widget.plan;
    setState(() {
      _busy = true;
      _error = null;
    });
    await runAction(
      context,
      () => state.setPlan(RecoveryStatus(
        heirs: p.heirs,
        inactivityDays: p.inactivityDays == 0 ? 365 : p.inactivityDays,
        guardians: _people,
        guardianThreshold: _people.isEmpty ? 0 : needed,
        heirThreshold: p.heirThreshold,
      )),
      t.planSaved,
    );
    if (!mounted) return;
    setState(() => _busy = false);
    widget.reload();
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    return ListView(
      padding: const EdgeInsets.all(20),
      children: [
        NeonCard(child: Text(t.guardiansExplain, style: const TextStyle(height: 1.4))),
        const SizedBox(height: 16),
        if (widget.plan.planChangeAt > 0) ...[
          Text(t.planChangeWait(_date(context, widget.plan.planChangeAt)), style: _label),
          const SizedBox(height: 12),
        ],
        for (var i = 0; i < _people.length; i++)
          _AddressRow(address: _people[i], onRemove: () => setState(() => _people.removeAt(i))),
        const SizedBox(height: 8),
        AddressField(controller: _address, label: t.guardianAddress),
        const SizedBox(height: 12),
        NeonButton(label: t.addGuardian, icon: Icons.person_add, secondary: true, onPressed: _add),
        const SizedBox(height: 18),
        Text(t.guardiansNeeded, style: _label),
        const SizedBox(height: 8),
        TextField(controller: _needed, keyboardType: TextInputType.number),
        ErrorText(_error),
        const SizedBox(height: 20),
        _busy
            ? const Center(child: CircularProgressIndicator(color: Brand.neonGreen))
            : NeonButton(label: t.save, icon: Icons.save, onPressed: _save),
      ],
    );
  }
}

// ----------------------------------------------------------- Sovereign Vault

class VaultScreen extends StatefulWidget {
  const VaultScreen({super.key});

  @override
  State<VaultScreen> createState() => _VaultScreenState();
}

class _VaultScreenState extends State<VaultScreen> {
  final _days = TextEditingController();
  final _amount = TextEditingController();
  final _lockDays = TextEditingController();
  String? _error;
  bool _busy = false;
  int _reloads = 0;

  @override
  void dispose() {
    _days.dispose();
    _amount.dispose();
    _lockDays.dispose();
    super.dispose();
  }

  Future<void> _run(Future<void> Function() action, String done) async {
    setState(() {
      _busy = true;
      _error = null;
    });
    await runAction(context, action, done);
    if (!mounted) return;
    setState(() {
      _busy = false;
      _reloads++;
    });
    AppScope.of(context).refresh();
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final locale = Localizations.localeOf(context).toString();
    final shield = state.status?.shield ?? const Shield();
    return Scaffold(
      appBar: BrandBar(title: t.vault),
      body: ChipBackground(
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            NeonCard(child: Text(t.vaultExplain, style: const TextStyle(height: 1.4))),
            const SizedBox(height: 12),
            Text(
              shield.vaultDays == 0 ? t.vaultOff : t.vaultOn(shield.vaultDays),
              style: const TextStyle(color: Brand.neonGreen, fontWeight: FontWeight.w600),
            ),
            if (shield.vaultChangeAt > 0)
              Text(t.planChangeWait(_date(context, shield.vaultChangeAt)), style: _label),
            const SizedBox(height: 12),
            Text(t.vaultDaysField, style: _label),
            const SizedBox(height: 8),
            TextField(controller: _days, keyboardType: TextInputType.number),
            const SizedBox(height: 12),
            NeonButton(
              label: t.save,
              icon: Icons.account_balance,
              onPressed: _busy
                  ? null
                  : () {
                      final d = int.tryParse(_days.text.trim());
                      if (d == null || (d != 0 && (d < 7 || d > 365))) {
                        setState(() => _error = t.vaultDaysField);
                        return;
                      }
                      _run(() => state.setVault(d), t.vaultSaved);
                    },
            ),
            const SizedBox(height: 28),
            Text(t.locks, style: const TextStyle(fontSize: 18, fontWeight: FontWeight.w700)),
            const SizedBox(height: 6),
            Text(t.lockExplain, style: const TextStyle(color: Brand.pearl, height: 1.4)),
            const SizedBox(height: 10),
            _Loaded<List<CoinLock>>(
              key: ValueKey(_reloads),
              load: (s) => s.client.locks(s.address!),
              builder: (context, locks, _) => Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  for (final l in locks)
                    Padding(
                      padding: const EdgeInsets.symmetric(vertical: 4),
                      child: Text(
                        '🔒 ${t.lockedUntil(formatLock(l.amount, locale), _date(context, l.until))}',
                        style: const TextStyle(fontWeight: FontWeight.w600),
                      ),
                    ),
                ],
              ),
            ),
            const SizedBox(height: 10),
            TextField(
              controller: _amount,
              keyboardType: const TextInputType.numberWithOptions(decimal: true),
              decoration: InputDecoration(labelText: t.amount, suffixText: 'LOCK'),
            ),
            const SizedBox(height: 12),
            Text(t.lockDaysField, style: _label),
            const SizedBox(height: 8),
            TextField(controller: _lockDays, keyboardType: TextInputType.number),
            ErrorText(_error),
            const SizedBox(height: 12),
            _busy
                ? const Center(child: CircularProgressIndicator(color: Brand.neonGreen))
                : NeonButton(
                    label: t.lockButton,
                    icon: Icons.lock_clock,
                    onPressed: () async {
                      final units = await state.core.parseLock(_amount.text);
                      final days = int.tryParse(_lockDays.text.trim()) ?? 0;
                      if (units == null || days < 1 || days > 3650) {
                        setState(() => _error = t.invalidAmount);
                        return;
                      }
                      await _run(
                        () => state.lockCoins(units, DateTime.now().add(Duration(days: days))),
                        t.lockedOk,
                      );
                    },
                  ),
          ],
        ),
      ),
    );
  }
}

// ---------------------------------------------- Heir / trusted person of others

class HelpOthersScreen extends StatefulWidget {
  const HelpOthersScreen({super.key});

  @override
  State<HelpOthersScreen> createState() => _HelpOthersScreenState();
}

class _HelpOthersScreenState extends State<HelpOthersScreen> {
  final _account = TextEditingController();
  final _newAccount = TextEditingController();
  bool _busy = false;

  @override
  void dispose() {
    _account.dispose();
    _newAccount.dispose();
    super.dispose();
  }

  Future<void> _run(Future<void> Function() action) async {
    final t = AppLocalizations.of(context);
    setState(() => _busy = true);
    await runAction(context, action, t.done);
    if (mounted) setState(() => _busy = false);
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    return Scaffold(
      appBar: BrandBar(title: t.helpOthers),
      body: ChipBackground(
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            NeonCard(child: Text(t.helpOthersExplain, style: const TextStyle(height: 1.4))),
            const SizedBox(height: 18),
            AddressField(controller: _account, label: t.accountField),
            const SizedBox(height: 14),
            NeonButton(
              label: t.claimButton,
              icon: Icons.family_restroom,
              onPressed: _busy ? null : () => _run(() => state.claimInheritance(_account.text)),
            ),
            const SizedBox(height: 26),
            AddressField(controller: _newAccount, label: t.newAccountField),
            const SizedBox(height: 14),
            NeonButton(
              label: t.approveRecoveryButton,
              icon: Icons.handshake,
              secondary: true,
              onPressed: _busy ? null : () => _run(() => state.approveRecovery(_account.text, _newAccount.text)),
            ),
          ],
        ),
      ),
    );
  }
}

/// Wallet screen warning when someone asked for this account's coins.
class ClaimBanner extends StatefulWidget {
  const ClaimBanner({super.key});

  @override
  State<ClaimBanner> createState() => _ClaimBannerState();
}

class _ClaimBannerState extends State<ClaimBanner> {
  bool _busy = false;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    if ((state.status?.claimKind ?? '').isEmpty) return const SizedBox.shrink();
    return Padding(
      padding: const EdgeInsets.only(top: 14),
      child: NeonCard(
        glowColor: Brand.danger,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text('⚠️ ${t.claimBanner}', style: const TextStyle(fontWeight: FontWeight.w700, height: 1.4)),
            const SizedBox(height: 12),
            NeonButton(
              label: t.cancelClaim,
              icon: Icons.block,
              onPressed: _busy
                  ? null
                  : () async {
                      setState(() => _busy = true);
                      await runAction(context, state.cancelClaims, t.done);
                      if (mounted) setState(() => _busy = false);
                    },
            ),
          ],
        ),
      ),
    );
  }
}
