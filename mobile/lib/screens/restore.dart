import 'package:flutter/material.dart';

import '../app_state.dart';
import '../l10n/gen/app_localizations.dart';
import '../widgets/brand.dart';
import 'password.dart';

/// "I already have my 24 words": recovers the wallet on this phone.
class RestoreScreen extends StatefulWidget {
  const RestoreScreen({super.key});

  @override
  State<RestoreScreen> createState() => _RestoreScreenState();
}

class _RestoreScreenState extends State<RestoreScreen> {
  final _controller = TextEditingController();
  bool _invalid = false;
  bool _busy = false;

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  Future<void> _continue() async {
    final words = _controller.text.trim().toLowerCase().split(RegExp(r'\s+')).join(' ');
    setState(() => _busy = true);
    final valid = await AppScope.of(context).core.validateMnemonic(words);
    if (!mounted) return;
    setState(() {
      _busy = false;
      _invalid = !valid;
    });
    if (valid) {
      Navigator.push(context, MaterialPageRoute(builder: (_) => PasswordScreen(words: words)));
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    return Scaffold(
      appBar: BrandBar(title: t.restoreTitle),
      body: ChipBackground(
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            const Center(child: NeonLogo(size: 110)),
            const SizedBox(height: 28),
            TextField(
              controller: _controller,
              minLines: 5,
              maxLines: 8,
              autocorrect: false,
              enableSuggestions: false,
              decoration: InputDecoration(hintText: t.restoreHint),
              onChanged: (_) => setState(() => _invalid = false),
            ),
            if (_invalid) ErrorText(t.restoreInvalid),
            const SizedBox(height: 24),
            NeonButton(label: t.continueButton, icon: Icons.arrow_forward, onPressed: _busy ? null : _continue),
          ],
        ),
      ),
    );
  }
}
