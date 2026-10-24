import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'package:flutter/material.dart';
import 'package:qr_flutter/qr_flutter.dart';

import '../app_state.dart';
import '../l10n/gen/app_localizations.dart';
import '../theme.dart';
import '../widgets/brand.dart';
import 'password.dart';
import 'send.dart' show ScanScreen;

/// What a "take the wallet to the phone" QR code holds: the 24 words sealed
/// (Argon2id + AES-256-GCM) with a one-time code shown next to it.
const _walletPrefix = 'encryptlock-wallet:';

/// Code for moving a wallet, which Send must not take as an address.
bool isWalletMoveCode(String code) => code.trim().startsWith(_walletPrefix);

/// The one-time code: 8 characters without the ones easy to confuse (about
/// 40 bits; with Argon2id, too slow to guess from a photo of the QR code).
String _newCode() {
  const chars = 'ABCDEFGHJKMNPQRSTUVWXYZ23456789';
  final rnd = Random.secure();
  return List.generate(8, (_) => chars[rnd.nextInt(chars.length)]).join();
}

/// Computer: shows the wallet as a QR code plus a one-time code, for 3
/// minutes. The phone scans it inside the app and types the code.
class ShowWalletQrScreen extends StatefulWidget {
  const ShowWalletQrScreen({super.key});

  @override
  State<ShowWalletQrScreen> createState() => _ShowWalletQrScreenState();
}

class _ShowWalletQrScreenState extends State<ShowWalletQrScreen> {
  final _code = _newCode();
  String? _data;
  Timer? _close;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) async {
      final state = AppScope.of(context);
      final record = await state.core.sealSecret(state.words!, _code);
      if (mounted) setState(() => _data = '$_walletPrefix$record');
    });
    // It never stays on the screen: closes by itself after 3 minutes.
    _close = Timer(const Duration(minutes: 3), () {
      if (mounted) Navigator.pop(context);
    });
  }

  @override
  void dispose() {
    _close?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final data = _data;
    final code = '${_code.substring(0, 4)} ${_code.substring(4)}';
    return Scaffold(
      appBar: BrandBar(title: t.walletToPhone),
      body: ChipBackground(
        child: Align(
          alignment: Alignment.topCenter,
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 640),
            child: ListView(
              padding: const EdgeInsets.all(24),
              children: [
                Text(t.walletToPhoneSteps, style: const TextStyle(height: 1.5)),
                const SizedBox(height: 18),
                if (data == null)
                  const Center(child: CircularProgressIndicator())
                else
                  Center(
                    child: Container(
                      padding: const EdgeInsets.all(14),
                      decoration: BoxDecoration(color: Colors.white, borderRadius: BorderRadius.circular(24)),
                      child: QrImageView(data: data, size: 260, backgroundColor: Colors.white),
                    ),
                  ),
                const SizedBox(height: 18),
                Center(
                  child: SelectableText(
                    code,
                    style: const TextStyle(fontSize: 34, fontWeight: FontWeight.w800, letterSpacing: 4, color: Brand.neonGreen),
                  ),
                ),
                const SizedBox(height: 12),
                Text(t.walletToPhoneWarning, textAlign: TextAlign.center, style: const TextStyle(color: Brand.danger)),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// Phone (first screen): scan the other device's QR code, type its code, and
/// the same wallet opens here (then a password for this device).
Future<void> receiveWalletByQr(BuildContext context) async {
  // A computer usually has no camera: it shows a code for the phone instead.
  if (Platform.isWindows) {
    await Navigator.push(context, MaterialPageRoute(builder: (_) => const ReceiveFromPhoneScreen()));
    return;
  }
  final t = AppLocalizations.of(context);
  final state = AppScope.of(context);
  final messenger = ScaffoldMessenger.of(context);
  final scanned = await Navigator.push<String>(context, MaterialPageRoute(builder: (_) => const ScanScreen()));
  if (scanned == null || !context.mounted) return;
  if (!isWalletMoveCode(scanned)) {
    messenger.showSnackBar(SnackBar(content: Text(t.walletCodeNotWallet)));
    return;
  }
  final controller = TextEditingController();
  final code = await showDialog<String>(
    context: context,
    builder: (dialogContext) => AlertDialog(
      backgroundColor: Brand.bottomBar,
      title: Text(t.walletCodeAsk),
      content: TextField(
        controller: controller,
        autofocus: true,
        textCapitalization: TextCapitalization.characters,
        decoration: const InputDecoration(hintText: 'ABCD 2345'),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.pop(dialogContext), child: Text(t.cancel)),
        TextButton(onPressed: () => Navigator.pop(dialogContext, controller.text), child: Text(t.confirm)),
      ],
    ),
  );
  if (code == null || !context.mounted) return;
  final clean = code.toUpperCase().replaceAll(RegExp(r'[^A-Z0-9]'), '');
  final words = await state.core.openSecret(scanned.trim().substring(_walletPrefix.length), clean);
  if (!context.mounted) return;
  if (words == null || !await state.core.validateMnemonic(words)) {
    messenger.showSnackBar(SnackBar(content: Text(t.walletCodeWrong)));
    return;
  }
  if (!context.mounted) return;
  await Navigator.push(context, MaterialPageRoute(builder: (_) => PasswordScreen(words: words)));
}

/// What the computer's "receive the wallet" QR code holds: the address of a
/// one-time receiver on the home Wi-Fi.
const _pcReceivePrefix = 'encryptlock-pc-receive:';

/// Code for receiving a wallet on a computer, which Send must not take as an
/// address either.
bool isPcReceiveCode(String code) => code.trim().startsWith(_pcReceivePrefix);

/// Computer (first screen, "I have a wallet on another device"): shows a QR
/// code; the phone sends the wallet over the home Wi-Fi, sealed with a code
/// the phone shows. Nothing goes through the internet or a server. The
/// receiver accepts one wallet and closes after 5 minutes.
class ReceiveFromPhoneScreen extends StatefulWidget {
  const ReceiveFromPhoneScreen({super.key});

  @override
  State<ReceiveFromPhoneScreen> createState() => _ReceiveFromPhoneScreenState();
}

class _ReceiveFromPhoneScreenState extends State<ReceiveFromPhoneScreen> {
  HttpServer? _server;
  String? _qr;
  String? _record;
  Timer? _close;

  @override
  void initState() {
    super.initState();
    _start();
    _close = Timer(const Duration(minutes: 5), () {
      if (mounted) Navigator.pop(context);
    });
  }

  @override
  void dispose() {
    _close?.cancel();
    _server?.close(force: true);
    super.dispose();
  }

  Future<String?> _wifiAddress() async {
    for (final nic in await NetworkInterface.list(type: InternetAddressType.IPv4)) {
      for (final a in nic.addresses) {
        final ip = a.address;
        if (ip.startsWith('192.168.') || ip.startsWith('10.') || RegExp(r'^172\.(1[6-9]|2\d|3[01])\.').hasMatch(ip)) return ip;
      }
    }
    return null;
  }

  Future<void> _start() async {
    final ip = await _wifiAddress();
    if (ip == null) return;
    final token = _newCode() + _newCode();
    final server = await HttpServer.bind(InternetAddress.anyIPv4, 0);
    _server = server;
    server.listen((req) async {
      if (req.method == 'POST' && req.uri.path == '/$token' && _record == null) {
        final body = await utf8.decoder.bind(req).join();
        req.response.statusCode = 200;
        await req.response.close();
        await server.close(force: true);
        if (mounted) setState(() => _record = body);
        await _askCode(body);
      } else {
        req.response.statusCode = 404;
        await req.response.close();
      }
    });
    if (mounted) setState(() => _qr = '${_pcReceivePrefix}http://$ip:${server.port}/$token');
  }

  Future<void> _askCode(String record) async {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final controller = TextEditingController();
    final code = await showDialog<String>(
      context: context,
      barrierDismissible: false,
      builder: (dialogContext) => AlertDialog(
        backgroundColor: Brand.bottomBar,
        title: Text(t.walletCodeAsk),
        content: TextField(
          controller: controller,
          autofocus: true,
          textCapitalization: TextCapitalization.characters,
          decoration: const InputDecoration(hintText: 'ABCD 2345'),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(dialogContext), child: Text(t.cancel)),
          TextButton(onPressed: () => Navigator.pop(dialogContext, controller.text), child: Text(t.confirm)),
        ],
      ),
    );
    if (code == null || !mounted) return;
    final clean = code.toUpperCase().replaceAll(RegExp(r'[^A-Z0-9]'), '');
    final words = await state.core.openSecret(record, clean);
    if (!mounted) return;
    final valid = words != null && await state.core.validateMnemonic(words);
    if (!mounted) return;
    if (!valid) {
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(t.walletCodeWrong)));
      await _askCode(record);
      return;
    }
    if (!mounted) return;
    await Navigator.pushReplacement(context, MaterialPageRoute(builder: (_) => PasswordScreen(words: words)));
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final qr = _qr;
    return Scaffold(
      appBar: BrandBar(title: t.haveOtherDevice),
      body: ChipBackground(
        child: Align(
          alignment: Alignment.topCenter,
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 640),
            child: ListView(
              padding: const EdgeInsets.all(24),
              children: [
                Text(t.walletToPcSteps, style: const TextStyle(height: 1.5)),
                const SizedBox(height: 18),
                if (qr == null)
                  const Center(child: CircularProgressIndicator())
                else
                  Center(
                    child: Container(
                      padding: const EdgeInsets.all(14),
                      decoration: BoxDecoration(color: Colors.white, borderRadius: BorderRadius.circular(24)),
                      child: QrImageView(data: qr, size: 260, backgroundColor: Colors.white),
                    ),
                  ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// Phone (Settings): scan the computer's QR code and send the wallet to it
/// over the home Wi-Fi, sealed with a one-time code shown here.
Future<void> sendWalletToComputer(BuildContext context) async {
  final t = AppLocalizations.of(context);
  final state = AppScope.of(context);
  final messenger = ScaffoldMessenger.of(context);
  final scanned = await Navigator.push<String>(context, MaterialPageRoute(builder: (_) => const ScanScreen()));
  if (scanned == null || !context.mounted) return;
  if (!isPcReceiveCode(scanned)) {
    messenger.showSnackBar(SnackBar(content: Text(t.walletToPcNotPc)));
    return;
  }
  final url = Uri.tryParse(scanned.trim().substring(_pcReceivePrefix.length));
  // Only an address on the home network, never the internet.
  if (url == null || url.scheme != 'http' || !RegExp(r'^(192\.168\.|10\.|172\.(1[6-9]|2\d|3[01])\.)').hasMatch(url.host)) {
    messenger.showSnackBar(SnackBar(content: Text(t.walletToPcNotPc)));
    return;
  }
  final code = _newCode();
  final record = await state.core.sealSecret(state.words!, code);
  final client = HttpClient()..connectionTimeout = const Duration(seconds: 8);
  try {
    final req = await client.postUrl(url);
    req.write(record);
    final res = await req.close().timeout(const Duration(seconds: 10));
    if (res.statusCode != 200) throw const HttpException('refused');
  } catch (_) {
    messenger.showSnackBar(SnackBar(content: Text(t.walletToPcFailed)));
    return;
  } finally {
    client.close();
  }
  if (!context.mounted) return;
  await showDialog<void>(
    context: context,
    builder: (dialogContext) => AlertDialog(
      backgroundColor: Brand.bottomBar,
      title: Text(t.walletToPc),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(t.walletToPcCode),
          const SizedBox(height: 14),
          SelectableText(
            '${code.substring(0, 4)} ${code.substring(4)}',
            style: const TextStyle(fontSize: 30, fontWeight: FontWeight.w800, letterSpacing: 4, color: Brand.neonGreen),
          ),
        ],
      ),
      actions: [TextButton(onPressed: () => Navigator.pop(dialogContext), child: Text(t.confirm))],
    ),
  );
}
