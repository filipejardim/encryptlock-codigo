import 'package:flutter/material.dart';

import '../app_state.dart';
import '../l10n/gen/app_localizations.dart';
import '../theme.dart';
import '../widgets/brand.dart';
import 'create_words.dart' show WordTile;

/// Asks for the wallet password before a sensitive action. Wrong answers
/// count towards the same pauses as the unlock screen.
Future<bool> confirmPassword(BuildContext context) async {
  final t = AppLocalizations.of(context);
  final state = AppScope.of(context);
  final messenger = ScaffoldMessenger.of(context);
  final wait = await state.guard.remaining();
  if (wait > Duration.zero) {
    messenger.showSnackBar(SnackBar(content: Text(t.tooManyAttempts('${wait.inMinutes + 1} min'))));
    return false;
  }
  if (!context.mounted) return false;
  final controller = TextEditingController();
  final password = await showDialog<String>(
    context: context,
    builder: (dialogContext) => AlertDialog(
      backgroundColor: Brand.bottomBar,
      title: Text(t.enterPassword),
      content: PasswordField(
        controller: controller,
        label: t.password,
        onSubmitted: (v) => Navigator.pop(dialogContext, v),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.pop(dialogContext), child: Text(t.cancel)),
        TextButton(onPressed: () => Navigator.pop(dialogContext, controller.text), child: Text(t.confirm)),
      ],
    ),
  );
  // Not disposed here: the dialog is still fading out and uses it.
  if (password == null) return false;
  if (await state.store.unlock(password) != null) {
    await state.guard.reset();
    return true;
  }
  final pause = await state.guard.recordFailure();
  final left = await state.guard.attemptsLeft();
  messenger.showSnackBar(SnackBar(
    content: Text(pause > Duration.zero ? t.tooManyAttempts('${pause.inMinutes} min') : t.wrongPasswordLeft(left)),
  ));
  return false;
}

/// New password for this phone (the 24 words do not change).
class ChangePasswordScreen extends StatefulWidget {
  const ChangePasswordScreen({super.key});

  @override
  State<ChangePasswordScreen> createState() => _ChangePasswordScreenState();
}

class _ChangePasswordScreenState extends State<ChangePasswordScreen> {
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
    setState(() => _busy = true);
    await AppScope.of(context).changePassword(_password.text);
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(t.passwordChanged)));
    Navigator.pop(context);
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    return Scaffold(
      appBar: BrandBar(title: t.changePassword),
      body: ChipBackground(
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            PasswordField(controller: _password, label: t.newPassword),
            const SizedBox(height: 16),
            PasswordField(controller: _repeat, label: t.passwordRepeat),
            ErrorText(_error),
            const SizedBox(height: 24),
            _busy
                ? const Center(child: CircularProgressIndicator(color: Brand.neonGreen))
                : NeonButton(label: t.save, icon: Icons.lock, onPressed: _save),
          ],
        ),
      ),
    );
  }
}

/// The 24 recovery words, after the password, with a strong warning.
class ShowWordsScreen extends StatelessWidget {
  const ShowWordsScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final words = (AppScope.of(context).words ?? '').split(' ');
    return Scaffold(
      appBar: BrandBar(title: t.showWords),
      body: ChipBackground(
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            NeonCard(
              glowColor: Brand.danger,
              child: Row(
                children: [
                  const Icon(Icons.warning_amber_rounded, color: Brand.danger, size: 30),
                  const SizedBox(width: 12),
                  Expanded(child: Text(t.showWordsWarning, style: const TextStyle(height: 1.4))),
                ],
              ),
            ),
            const SizedBox(height: 20),
            NeonCard(
              padding: const EdgeInsets.all(14),
              child: GridView.count(
                crossAxisCount: 2,
                shrinkWrap: true,
                physics: const NeverScrollableScrollPhysics(),
                childAspectRatio: 3.6,
                mainAxisSpacing: 8,
                crossAxisSpacing: 8,
                children: [for (var i = 0; i < words.length; i++) WordTile(number: i + 1, word: words[i])],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// Asks the send code (separate from the password) before sending.
Future<bool> askSendCode(BuildContext context) async {
  final t = AppLocalizations.of(context);
  final state = AppScope.of(context);
  final messenger = ScaffoldMessenger.of(context);
  final wait = await state.sendCodeGuard.remaining();
  if (wait > Duration.zero) {
    messenger.showSnackBar(SnackBar(content: Text(t.tooManyAttempts('${wait.inMinutes + 1} min'))));
    return false;
  }
  if (!context.mounted) return false;
  final controller = TextEditingController();
  final code = await showDialog<String>(
    context: context,
    builder: (dialogContext) => AlertDialog(
      backgroundColor: Brand.bottomBar,
      title: Text(t.sendCode),
      content: TextField(
        controller: controller,
        autofocus: true,
        obscureText: true,
        keyboardType: TextInputType.number,
        maxLength: 8,
        decoration: InputDecoration(labelText: t.sendCodeEnter),
        onSubmitted: (v) => Navigator.pop(dialogContext, v),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.pop(dialogContext), child: Text(t.cancel)),
        TextButton(onPressed: () => Navigator.pop(dialogContext, controller.text), child: Text(t.confirm)),
      ],
    ),
  );
  // Not disposed here: the dialog is still fading out and uses it.
  if (code == null) return false;
  if (await state.checkSendCode(code)) return true;
  messenger.showSnackBar(SnackBar(content: Text(t.sendCodeWrong)));
  return false;
}

/// Chooses the send code: 4 to 8 digits, typed twice.
class SendCodeScreen extends StatefulWidget {
  const SendCodeScreen({super.key});

  @override
  State<SendCodeScreen> createState() => _SendCodeScreenState();
}

class _SendCodeScreenState extends State<SendCodeScreen> {
  final _code = TextEditingController();
  final _repeat = TextEditingController();
  String? _error;
  bool _busy = false;

  @override
  void dispose() {
    _code.dispose();
    _repeat.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    final t = AppLocalizations.of(context);
    if (!RegExp(r'^[0-9]{4,8}$').hasMatch(_code.text)) {
      setState(() => _error = t.sendCodeInvalid);
      return;
    }
    if (_code.text != _repeat.text) {
      setState(() => _error = t.sendCodeMismatch);
      return;
    }
    setState(() => _busy = true);
    await AppScope.of(context).setSendCode(_code.text);
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(t.sendCodeSet)));
    Navigator.pop(context);
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    InputDecoration deco(String label) => InputDecoration(labelText: label, counterText: '');
    return Scaffold(
      appBar: BrandBar(title: t.sendCode),
      body: ChipBackground(
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            NeonCard(
              glowColor: Brand.neonBlue,
              child: Text(t.sendCodeExplain, style: const TextStyle(height: 1.4)),
            ),
            const SizedBox(height: 20),
            TextField(
              controller: _code,
              obscureText: true,
              keyboardType: TextInputType.number,
              maxLength: 8,
              decoration: deco(t.sendCodeNew),
            ),
            const SizedBox(height: 16),
            TextField(
              controller: _repeat,
              obscureText: true,
              keyboardType: TextInputType.number,
              maxLength: 8,
              decoration: deco(t.sendCodeRepeat),
            ),
            ErrorText(_error),
            const SizedBox(height: 24),
            _busy
                ? const Center(child: CircularProgressIndicator(color: Brand.neonGreen))
                : NeonButton(label: t.save, icon: Icons.pin, onPressed: _save),
          ],
        ),
      ),
    );
  }
}

/// The network's refusal in the owner's language when we know the cause;
/// otherwise its own (English) explanation.
String chainErrorText(AppLocalizations t, String log) {
  if (log.contains('frozen')) return t.frozenBanner;
  if (log.contains('insufficient')) return t.notEnough;
  return t.txFailed(log);
}
