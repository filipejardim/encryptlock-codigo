import 'dart:io';

import 'package:flutter/material.dart';
import 'package:intl/intl.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

import '../app_state.dart';
import '../core/network.dart';
import '../core/polygon.dart';
import '../l10n/gen/app_localizations.dart';
import '../theme.dart';
import '../widgets/brand.dart';
import 'home.dart' show formatLock, formatUnits;
import 'link_computer.dart' show isComputerLinkCode;
import 'move_wallet.dart' show isPcReceiveCode, isWalletMoveCode;
import 'security.dart' show askSendCode, chainErrorText;

/// The coins the wallet can send: LOCK on the EncryptLock network, USDC and
/// POL on Polygon.
enum SendCoin { lock, usdc, pol }

final _polygonAddress = RegExp(r'0x[0-9a-fA-F]{40}');

/// Decimal text ("1,5" or "1.5") to the coin's smallest units; null when it
/// is not a positive amount with at most [decimals] decimals.
BigInt? parseUnits(String text, int decimals) {
  final m = RegExp(r'^(\d+)(?:[.,](\d+))?$').firstMatch(text.trim());
  if (m == null || (m.group(2)?.length ?? 0) > decimals) return null;
  final v = BigInt.parse(m.group(1)! + (m.group(2) ?? '').padRight(decimals, '0'));
  return v > BigInt.zero ? v : null;
}

/// Send: recipient (typed, pasted or scanned), amount, fee shown before
/// confirming. LOCK: the recipient then has 1 hour to accept. USDC and POL go
/// on Polygon at once and cannot be undone.
class SendTab extends StatefulWidget {
  const SendTab({super.key});

  @override
  State<SendTab> createState() => _SendTabState();
}

class _SendTabState extends State<SendTab> {
  final _to = TextEditingController();
  final _amount = TextEditingController();
  int? _units;
  int? _fee;
  String? _error;
  bool _busy = false;
  SendCoin _coin = SendCoin.lock;
  BigInt? _polygonFee;

  bool get _onPolygon => _coin != SendCoin.lock;
  int get _decimals => _coin == SendCoin.usdc ? 6 : 18;
  String get _symbol => _coinName(_coin);
  static String _coinName(SendCoin c) => switch (c) { SendCoin.lock => 'LOCK', SendCoin.usdc => 'USDC', SendCoin.pol => 'POL' };

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final state = AppScope.of(context);
    final to = state.pendingSendTo;
    if (to != null) {
      _to.text = to;
      if (_polygonAddress.hasMatch(to)) _coin = SendCoin.usdc;
      state.pendingSendTo = null;
    }
  }

  @override
  void dispose() {
    _to.dispose();
    _amount.dispose();
    super.dispose();
  }

  Future<void> _pickCoin(SendCoin coin) async {
    setState(() {
      _coin = coin;
      _units = null;
      _fee = null;
      _polygonFee = null;
      _error = null;
    });
    if (_amount.text.trim().isNotEmpty) await _amountChanged(_amount.text);
    if (coin == SendCoin.lock || !mounted) return;
    try {
      final fee = await AppScope.of(context).polygonSendFee(usdc: coin == SendCoin.usdc);
      if (mounted && _coin == coin) setState(() => _polygonFee = fee);
    } catch (e) {
      debugPrint('polygon fee failed: $e');
    }
  }

  Future<void> _amountChanged(String text) async {
    if (_onPolygon) {
      setState(() => _error = null);
      return;
    }
    final core = AppScope.of(context).core;
    final units = text.trim().isEmpty ? null : await core.parseLock(text);
    final fee = units == null ? null : await core.transferFee(units);
    if (!mounted) return;
    setState(() {
      _units = units;
      _fee = fee;
      _error = null;
    });
  }

  Future<void> _scan() async {
    final code = await Navigator.push<String>(context, MaterialPageRoute(builder: (_) => const ScanScreen()));
    if (code == null || !mounted) return;
    // A computer's link code holds an address too, but is not for sending.
    if (isComputerLinkCode(code) || isWalletMoveCode(code) || isPcReceiveCode(code)) {
      setState(() => _error = AppLocalizations.of(context).linkPhoneBadCode);
      return;
    }
    // A 0x code (also "ethereum:0x…@137") is a Polygon address.
    final polygon = _polygonAddress.firstMatch(code)?.group(0);
    if (polygon != null) {
      if (!_onPolygon) await _pickCoin(SendCoin.usdc);
      if (mounted) setState(() => _to.text = polygon);
      return;
    }
    // Accept "lock1..." or "encryptlock:lock1...?..." codes.
    final match = RegExp(r'lock1[0-9a-z]+').firstMatch(code.toLowerCase());
    if (match != null && _onPolygon) await _pickCoin(SendCoin.lock);
    if (!mounted) return;
    setState(() {
      _to.text = match?.group(0) ?? code;
      _error = null;
    });
  }

  Future<void> _sendPolygon() async {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final locale = Localizations.localeOf(context).toString();
    final to = _to.text.trim();
    if (!RegExp(r'^0x[0-9a-fA-F]{40}$').hasMatch(to) || to.toLowerCase() == state.polygonAddress?.toLowerCase()) {
      setState(() => _error = t.invalidPolygonAddress);
      return;
    }
    final amount = parseUnits(_amount.text, _decimals);
    if (amount == null) {
      setState(() => _error = t.invalidAmount);
      return;
    }
    final usdc = _coin == SendCoin.usdc;
    final fee = _polygonFee;
    final ok = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        backgroundColor: Brand.bottomBar,
        title: Text(t.confirmSendCoin(formatUnits(amount, _decimals, locale), _symbol)),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            SelectableText(to, style: const TextStyle(color: Brand.neonGreen, fontWeight: FontWeight.w600)),
            if (fee != null) ...[
              const SizedBox(height: 12),
              Text(t.polygonFeeLine(_formatFee(fee, locale))),
            ],
            const SizedBox(height: 12),
            Text(t.polygonSendNote, style: const TextStyle(color: Brand.pearl, height: 1.4)),
          ],
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(dialogContext, false), child: Text(t.cancel)),
          TextButton(onPressed: () => Navigator.pop(dialogContext, true), child: Text(t.sendButton)),
        ],
      ),
    );
    if (ok != true || !mounted) return;
    if (state.sendCodeOn && !await askSendCode(context)) return;
    if (!mounted) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await state.sendPolygon(usdc: usdc, to: to, amount: amount);
      if (!mounted) return;
      _to.clear();
      _amount.clear();
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(t.polygonSentOk)));
    } on BuyProblem catch (e) {
      setState(() => _error = switch (e.kind) {
            BuyProblemKind.noUsdc => t.notEnough,
            BuyProblemKind.noPolAmount => t.sendNoPolAmount,
            _ => t.buyNoPol,
          });
    } catch (e) {
      debugPrint('polygon send failed: $e');
      setState(() => _error = t.networkError);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  /// A small POL fee with enough decimals to show it (0,0021).
  static String _formatFee(BigInt wei, String locale) =>
      NumberFormat('#,##0.00####', locale).format(wei / BigInt.from(10).pow(18));

  Future<void> _send() async {
    if (_onPolygon) return _sendPolygon();
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final locale = Localizations.localeOf(context).toString();
    final to = _to.text.trim();
    if (!await state.core.isAddress(to) || to == state.address) {
      setState(() => _error = t.invalidAddress);
      return;
    }
    final units = _units;
    final fee = _fee;
    if (units == null || units <= 0 || fee == null) {
      setState(() => _error = t.invalidAmount);
      return;
    }
    final balance = state.status?.balance;
    if (balance != null && BigInt.from(units + fee) > balance) {
      setState(() => _error = t.notEnough);
      return;
    }
    if (!mounted) return;
    final ok = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        backgroundColor: Brand.bottomBar,
        title: Text(t.confirmSend(formatLock(BigInt.from(units), locale))),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            SelectableText(
              to,
              style: const TextStyle(color: Brand.neonGreen, fontWeight: FontWeight.w600),
            ),
            const SizedBox(height: 12),
            Text(t.feeLine(formatLock(BigInt.from(fee), locale), formatLock(BigInt.from(units + fee), locale))),
            const SizedBox(height: 12),
            Text(t.confirmSendNote, style: const TextStyle(color: Brand.pearl, height: 1.4)),
          ],
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(dialogContext, false), child: Text(t.cancel)),
          TextButton(onPressed: () => Navigator.pop(dialogContext, true), child: Text(t.sendButton)),
        ],
      ),
    );
    if (ok != true || !mounted) return;
    if (state.sendCodeOn && !await askSendCode(context)) return;
    if (!mounted) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await state.send(to, units);
      if (!mounted) return;
      _to.clear();
      _amount.clear();
      setState(() {
        _units = null;
        _fee = null;
      });
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(t.sentOk)));
    } on ChainError catch (e) {
      setState(() => _error = chainErrorText(t, e.log));
    } catch (e) {
      debugPrint('transaction failed: $e');
      setState(() => _error = t.networkError);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final locale = Localizations.localeOf(context).toString();
    final balance = state.status?.balance;
    final polygon = state.polygon;
    final balanceText = switch (_coin) {
      SendCoin.lock => balance == null ? null : '${formatLock(balance, locale)} LOCK',
      SendCoin.usdc => polygon == null ? null : '${formatUnits(polygon.usdc, 6, locale)} USDC',
      SendCoin.pol => polygon == null ? null : '${formatUnits(polygon.pol, 18, locale)} POL',
    };
    return ListView(
      padding: const EdgeInsets.all(20),
      children: [
        Text(
          t.navSend,
          textAlign: TextAlign.center,
          style: TextStyle(
            color: Brand.neonGreen,
            fontSize: 22,
            fontWeight: FontWeight.w700,
            shadows: [Shadow(color: Brand.neonGreen.withValues(alpha: 0.6), blurRadius: 12)],
          ),
        ),
        const SizedBox(height: 14),
        Wrap(
          alignment: WrapAlignment.center,
          spacing: 10,
          children: [
            for (final c in SendCoin.values)
              ChoiceChip(
                label: Text(_coinName(c)),
                selected: _coin == c,
                selectedColor: Brand.neonGreen.withValues(alpha: 0.25),
                onSelected: _busy ? null : (_) => _pickCoin(c),
              ),
          ],
        ),
        if (balanceText != null) ...[
          const SizedBox(height: 6),
          Text(
            '${t.balance}: $balanceText',
            textAlign: TextAlign.center,
            style: const TextStyle(color: Brand.pearl),
          ),
        ],
        const SizedBox(height: 22),
        TextField(
          controller: _to,
          autocorrect: false,
          enableSuggestions: false,
          minLines: 1,
          maxLines: 2,
          decoration: InputDecoration(
            labelText: _onPolygon ? t.sendToPolygon : t.sendTo,
            // No camera reader on the computer: paste the address instead.
            suffixIcon: !Platform.isAndroid
                ? null
                : IconButton(
                    tooltip: t.scanQr,
                    icon: const Icon(Icons.qr_code_scanner, color: Brand.neonBlue),
                    onPressed: _busy ? null : _scan,
                  ),
          ),
          onChanged: (_) => setState(() => _error = null),
        ),
        const SizedBox(height: 16),
        TextField(
          controller: _amount,
          keyboardType: const TextInputType.numberWithOptions(decimal: true),
          decoration: InputDecoration(labelText: t.amount, suffixText: _symbol),
          onChanged: _amountChanged,
        ),
        if (_onPolygon && _polygonFee != null) ...[
          const SizedBox(height: 10),
          Text(t.polygonFeeLine(_formatFee(_polygonFee!, locale)), style: const TextStyle(color: Brand.pearl)),
        ],
        if (!_onPolygon && _units != null && _fee != null) ...[
          const SizedBox(height: 10),
          Text(
            t.feeLine(formatLock(BigInt.from(_fee!), locale), formatLock(BigInt.from(_units! + _fee!), locale)),
            style: const TextStyle(color: Brand.pearl),
          ),
        ],
        ErrorText(_error),
        const SizedBox(height: 24),
        _busy
            ? const Center(child: CircularProgressIndicator(color: Brand.neonGreen))
            : NeonButton(label: t.sendButton, icon: Icons.north_east, onPressed: _send),
        const SizedBox(height: 16),
        Text(
          _onPolygon ? t.polygonSendNote : t.confirmSendNote,
          textAlign: TextAlign.center,
          style: const TextStyle(color: Brand.pearl, height: 1.4),
        ),
      ],
    );
  }
}

/// Camera view that returns the first QR code it reads.
class ScanScreen extends StatefulWidget {
  const ScanScreen({super.key});

  @override
  State<ScanScreen> createState() => _ScanScreenState();
}

class _ScanScreenState extends State<ScanScreen> {
  bool _done = false;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    return Scaffold(
      appBar: BrandBar(title: t.scanQr),
      body: Stack(
        fit: StackFit.expand,
        children: [
          MobileScanner(
            onDetect: (capture) {
              final value = capture.barcodes.isEmpty ? null : capture.barcodes.first.rawValue;
              if (value == null || _done) return;
              _done = true;
              Navigator.pop(context, value);
            },
            errorBuilder: (context, error) => Center(
              child: Padding(
                padding: const EdgeInsets.all(28),
                child: Text(t.cameraDenied, textAlign: TextAlign.center),
              ),
            ),
          ),
          Center(
            child: Container(
              width: 250,
              height: 250,
              decoration: BoxDecoration(
                border: Border.all(color: Brand.neonGreen, width: 3),
                borderRadius: BorderRadius.circular(24),
                boxShadow: Brand.glow(Brand.neonGreen, 0.8),
              ),
            ),
          ),
        ],
      ),
    );
  }
}
