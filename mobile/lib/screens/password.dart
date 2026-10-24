import 'dart:async';

import 'package:flutter/material.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../app_state.dart';
import '../core/biometric.dart';
import '../l10n/gen/app_localizations.dart';
import '../theme.dart';
import '../widgets/brand.dart';
import 'home.dart';

/// Chooses the password that encrypts the wallet on this phone.
class PasswordScreen extends StatefulWidget {
  const PasswordScreen({super.key, required this.words});

  final String words;

  @override
  State<PasswordScreen> createState() => _PasswordScreenState();
}

class _PasswordScreenState extends State<PasswordScreen> {
  final _password = TextEditingController();
  final _repeat = TextEditingController();
  String? _error;
  bool _busy = false;

  @override
  void dispose() {
    _password.dispose();
    _repeat.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    final t = AppLocalizations.of(context);
    if (_password.text.length < 8) {
      setState(() => _error = t.passwordTooShort);
      return;
    }
    if (_password.text != _repeat.text) {
      setState(() => _error = t.passwordMismatch);
      return;
    }
    setState(() {
      _error = null;
      _busy = true;
    });
    final state = AppScope.of(context);
    try {
      await state.store.save(widget.words, _password.text);
      await state.guard.reset();
      // A real wallet replaces the test wallet of a test build.
      final prefs = await SharedPreferences.getInstance();
      await prefs.setBool(AppState.demoActiveKey, false);
      state.demoWalletActive = false;
      // A new or recovered wallet starts without the old wallet's extras.
      await state.disableBiometric();
      await state.removeSendCode();
      await state.open(widget.words);
    } catch (e) {
      setState(() {
        _busy = false;
        _error = t.errorGeneric(e.toString());
      });
      return;
    }
    if (!mounted) return;
    Navigator.of(context).pushAndRemoveUntil(MaterialPageRoute(builder: (_) => const HomeScreen()), (_) => false);
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    return Scaffold(
      appBar: BrandBar(title: t.passwordTitle),
      body: ChipBackground(
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            NeonCard(
              glowColor: Brand.neonBlue,
              child: Row(
                children: [
                  const Icon(Icons.lock_outline, color: Brand.neonBlue, size: 30),
                  const SizedBox(width: 12),
                  Expanded(child: Text(t.passwordExplain, style: const TextStyle(height: 1.4))),
                ],
              ),
            ),
            const SizedBox(height: 24),
            PasswordField(controller: _password, label: t.password),
            const SizedBox(height: 16),
            PasswordField(controller: _repeat, label: t.passwordRepeat),
            ErrorText(_error),
            const SizedBox(height: 24),
            _busy
                ? const Center(child: CircularProgressIndicator(color: Brand.neonGreen))
                : NeonButton(label: t.continueButton, icon: Icons.lock, onPressed: _save),
          ],
        ),
      ),
    );
  }
}

/// "Decrypt your wallet": shown every time the app opens.
class UnlockScreen extends StatefulWidget {
  const UnlockScreen({super.key});

  @override
  State<UnlockScreen> createState() => _UnlockScreenState();
}

class _UnlockScreenState extends State<UnlockScreen> {
  final _password = TextEditingController();
  int? _attemptsLeft;
  bool _busy = false;

  /// Time still to wait after too many wrong passwords.
  Duration _wait = Duration.zero;
  Timer? _ticker;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _refreshWait();
      // Fingerprint/face is offered at once when it is on.
      if (AppScope.of(context).biometricOn) _biometric();
    });
  }

  @override
  void dispose() {
    _ticker?.cancel();
    _password.dispose();
    super.dispose();
  }

  Future<void> _refreshWait() async {
    final wait = await AppScope.of(context).guard.remaining();
    if (!mounted) return;
    setState(() => _wait = wait);
    _ticker?.cancel();
    if (wait > Duration.zero) {
      _ticker = Timer(const Duration(seconds: 1), _refreshWait);
    }
  }

  static String _format(Duration d) {
    final s = d.inSeconds + (d.inMilliseconds % 1000 > 0 ? 1 : 0);
    final h = s ~/ 3600, m = (s % 3600) ~/ 60, sec = s % 60;
    String two(int v) => v.toString().padLeft(2, '0');
    return h > 0 ? '$h:${two(m)}:${two(sec)}' : '$m:${two(sec)}';
  }

  Future<void> _biometric() async {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    try {
      await state.unlockWithBiometric(title: t.biometricReason, cancel: t.usePassword);
    } on BiometricException catch (e) {
      if (!mounted) return;
      if (e.failure == BiometricFailure.invalidated) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(t.biometricChanged)));
      }
      setState(() {});
      return;
    }
    if (!mounted) return;
    Navigator.of(context).pushAndRemoveUntil(MaterialPageRoute(builder: (_) => const HomeScreen()), (_) => false);
  }

  Future<void> _unlock() async {
    final state = AppScope.of(context);
    if (await state.guard.remaining() > Duration.zero) {
      await _refreshWait();
      return;
    }
    setState(() => _busy = true);
    final words = await state.store.unlock(_password.text);
    if (words == null) {
      final pause = await state.guard.recordFailure();
      final left = await state.guard.attemptsLeft();
      if (!mounted) return;
      _password.clear();
      setState(() {
        _busy = false;
        _attemptsLeft = pause > Duration.zero ? null : left;
      });
      await _refreshWait();
      return;
    }
    await state.guard.reset();
    await state.open(words);
    if (!mounted) return;
    Navigator.of(context).pushAndRemoveUntil(MaterialPageRoute(builder: (_) => const HomeScreen()), (_) => false);
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final waiting = _wait > Duration.zero;
    return Scaffold(
      body: ChipBackground(
        child: Center(
          child: SingleChildScrollView(
            padding: const EdgeInsets.all(28),
            child: Column(
              children: [
                const LogoBanner(width: 340),
                const SizedBox(height: 28),
                Text(
                  t.decryptTitle,
                  style: const TextStyle(fontSize: 22, fontWeight: FontWeight.w700, color: Brand.neonBlue),
                ),
                const SizedBox(height: 20),
                PasswordField(
                  controller: _password,
                  label: t.password,
                  prefixIcon: Icons.key,
                  enabled: !waiting,
                  onChanged: (_) => setState(() => _attemptsLeft = null),
                  onSubmitted: (_) => _unlock(),
                ),
                if (AppScope.of(context).demoWalletActive)
                  Padding(
                    padding: const EdgeInsets.only(top: 10),
                    child: Text(
                      t.demoPassword(AppState.demoPassword),
                      textAlign: TextAlign.center,
                      style: const TextStyle(color: Brand.neonGreen, fontWeight: FontWeight.w600),
                    ),
                  ),
                // The rule, always visible: pauses stop anyone guessing.
                Padding(
                  padding: const EdgeInsets.only(top: 10),
                  child: Text(
                    t.attemptsRule,
                    textAlign: TextAlign.center,
                    style: const TextStyle(color: Brand.pearl, fontSize: 13, height: 1.35),
                  ),
                ),
                if (waiting)
                  ErrorText(t.tooManyAttempts(_format(_wait)))
                else if (_attemptsLeft != null)
                  ErrorText(t.wrongPasswordLeft(_attemptsLeft!)),
                const SizedBox(height: 22),
                _busy
                    ? const CircularProgressIndicator(color: Brand.neonGreen)
                    : NeonButton(label: t.decryptButton, icon: Icons.lock_open, onPressed: waiting ? null : _unlock),
                const SizedBox(height: 14),
                if (AppScope.of(context).biometricOn) ...[
                  NeonButton(
                    label: t.useBiometric,
                    icon: Icons.fingerprint,
                    secondary: true,
                    onPressed: _busy ? null : _biometric,
                  ),
                  const SizedBox(height: 6),
                ],
                TextButton(
                  onPressed: () => Navigator.of(context).pushNamed('/restore'),
                  child: Text(t.forgotPassword, textAlign: TextAlign.center, style: const TextStyle(color: Brand.neonBlue)),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
