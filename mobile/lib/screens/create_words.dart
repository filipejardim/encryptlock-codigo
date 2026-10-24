import 'dart:math';

import 'package:flutter/material.dart';

import '../app_state.dart';
import '../l10n/gen/app_localizations.dart';
import '../theme.dart';
import '../widgets/brand.dart';
import 'password.dart';

/// Shows the 24 new recovery words, once, with the warning to write them on
/// paper.
class CreateWordsScreen extends StatefulWidget {
  const CreateWordsScreen({super.key});

  @override
  State<CreateWordsScreen> createState() => _CreateWordsScreenState();
}

class _CreateWordsScreenState extends State<CreateWordsScreen> {
  String? _words;
  String? _error;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (_words == null && _error == null) {
      AppScope.of(context).core.newMnemonic().then(
            (w) => setState(() => _words = w),
            onError: (Object e) => setState(() => _error = e.toString()),
          );
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final words = _words?.split(' ');
    return Scaffold(
      appBar: BrandBar(title: t.wordsTitle),
      body: ChipBackground(
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            NeonCard(
              glowColor: Brand.neonBlue,
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Icon(Icons.warning_amber_rounded, color: Brand.neonBlue, size: 30),
                  const SizedBox(width: 12),
                  Expanded(child: Text(t.wordsWarning, style: const TextStyle(height: 1.4))),
                ],
              ),
            ),
            const SizedBox(height: 20),
            if (_error != null) ErrorText(t.errorGeneric(_error!)),
            if (words == null && _error == null) const Center(child: CircularProgressIndicator(color: Brand.neonGreen)),
            if (words != null)
              NeonCard(
                padding: const EdgeInsets.all(14),
                child: GridView.count(
                  crossAxisCount: 2,
                  shrinkWrap: true,
                  physics: const NeverScrollableScrollPhysics(),
                  childAspectRatio: 3.6,
                  mainAxisSpacing: 8,
                  crossAxisSpacing: 8,
                  children: [
                    for (var i = 0; i < words.length; i++) WordTile(number: i + 1, word: words[i]),
                  ],
                ),
              ),
            const SizedBox(height: 24),
            NeonButton(
              label: t.wordsWritten,
              icon: Icons.check_circle_outline,
              onPressed: words == null
                  ? null
                  : () => Navigator.push(
                        context,
                        MaterialPageRoute(builder: (_) => ConfirmWordsScreen(words: _words!)),
                      ),
            ),
          ],
        ),
      ),
    );
  }
}

/// One numbered recovery word (also used by Settings → see the 24 words).
class WordTile extends StatelessWidget {
  const WordTile({super.key, required this.number, required this.word});

  final int number;
  final String word;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12),
      decoration: BoxDecoration(
        color: Brand.bubble,
        borderRadius: BorderRadius.circular(14),
        border: Border.all(color: Brand.cardBorder),
      ),
      child: Row(
        children: [
          SizedBox(
            width: 28,
            child: Text('$number', style: const TextStyle(color: Brand.neonGreen, fontWeight: FontWeight.w700)),
          ),
          Expanded(
            child: Text(word, style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600, letterSpacing: 0.5)),
          ),
        ],
      ),
    );
  }
}

/// Asks for 3 of the 24 words, to be sure they were written down.
class ConfirmWordsScreen extends StatefulWidget {
  const ConfirmWordsScreen({super.key, required this.words});

  final String words;

  @override
  State<ConfirmWordsScreen> createState() => _ConfirmWordsScreenState();
}

class _ConfirmWordsScreenState extends State<ConfirmWordsScreen> {
  late final List<int> _positions;
  late final List<TextEditingController> _controllers;
  bool _wrong = false;

  @override
  void initState() {
    super.initState();
    final rng = Random.secure();
    final picked = <int>{};
    while (picked.length < 3) {
      picked.add(rng.nextInt(24));
    }
    _positions = picked.toList()..sort();
    _controllers = List.generate(3, (_) => TextEditingController());
  }

  @override
  void dispose() {
    for (final c in _controllers) {
      c.dispose();
    }
    super.dispose();
  }

  void _check() {
    final words = widget.words.split(' ');
    for (var i = 0; i < 3; i++) {
      if (_controllers[i].text.trim().toLowerCase() != words[_positions[i]]) {
        setState(() => _wrong = true);
        return;
      }
    }
    Navigator.push(context, MaterialPageRoute(builder: (_) => PasswordScreen(words: widget.words)));
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    return Scaffold(
      appBar: BrandBar(title: t.confirmTitle),
      body: ChipBackground(
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            for (var i = 0; i < 3; i++) ...[
              TextField(
                controller: _controllers[i],
                autocorrect: false,
                enableSuggestions: false,
                decoration: InputDecoration(labelText: t.confirmWord(_positions[i] + 1)),
                onChanged: (_) => setState(() => _wrong = false),
              ),
              const SizedBox(height: 16),
            ],
            if (_wrong) ErrorText(t.confirmWrong),
            const SizedBox(height: 20),
            NeonButton(label: t.continueButton, icon: Icons.arrow_forward, onPressed: _check),
          ],
        ),
      ),
    );
  }
}
