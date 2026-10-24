import 'package:flutter/material.dart';

import '../app_state.dart';
import '../l10n/gen/app_localizations.dart';
import '../theme.dart';
import '../widgets/brand.dart';
import 'create_words.dart';
import 'move_wallet.dart' show receiveWalletByQr;
import 'restore.dart';

/// First screen: create a new wallet or recover one with the 24 words.
class WelcomeScreen extends StatelessWidget {
  const WelcomeScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    return Scaffold(
      body: ChipBackground(
        child: Stack(
          children: [
            Center(
              child: SingleChildScrollView(
                padding: const EdgeInsets.symmetric(horizontal: 28, vertical: 56),
                child: _WelcomeContent(t: t),
              ),
            ),
            const PositionedDirectional(top: 4, end: 8, child: LanguageButton()),
          ],
        ),
      ),
    );
  }
}

class _WelcomeContent extends StatelessWidget {
  const _WelcomeContent({required this.t});

  final AppLocalizations t;

  @override
  Widget build(BuildContext context) {
    return Column(
              children: [
                const LogoBanner(width: 360),
                const SizedBox(height: 22),
                Text(
                  t.tagline,
                  textAlign: TextAlign.center,
                  style: const TextStyle(color: Brand.neonBlue, fontSize: 16, letterSpacing: 0.5),
                ),
                const SizedBox(height: 48),
                NeonButton(
                  label: t.createWallet,
                  icon: Icons.add_moderator_outlined,
                  onPressed: () => Navigator.push(context, MaterialPageRoute(builder: (_) => const CreateWordsScreen())),
                ),
                const SizedBox(height: 18),
                NeonButton(
                  label: t.haveWords,
                  icon: Icons.key_outlined,
                  secondary: true,
                  onPressed: () => Navigator.push(context, MaterialPageRoute(builder: (_) => const RestoreScreen())),
                ),
                const SizedBox(height: 18),
                // The same wallet as on another device, by QR code (no 24
                // words to type, no second wallet).
                NeonButton(
                  label: t.haveOtherDevice,
                  icon: Icons.qr_code_scanner,
                  secondary: true,
                  onPressed: () => receiveWalletByQr(context),
                ),
              ],
            );
  }
}

/// Language picker: shows each language in its own name.
void showLanguagePicker(BuildContext context) {
  final state = AppScope.of(context);
  showModalBottomSheet<void>(
    context: context,
    backgroundColor: Brand.bottomBar,
    showDragHandle: true,
    builder: (sheetContext) => SafeArea(
      child: ListView(
        children: [
          for (final l in AppLanguage.all)
            ListTile(
              title: Text(l.name),
              trailing: state.language?.tag == l.tag ? const Icon(Icons.check, color: Brand.neonGreen) : null,
              onTap: () {
                state.setLanguage(l);
                Navigator.pop(sheetContext);
              },
            ),
        ],
      ),
    ),
  );
}

/// Language button on the first screen: coloured globe + language name.
class LanguageButton extends StatelessWidget {
  const LanguageButton({super.key});

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    return TextButton.icon(
      icon: const Text('🌍', style: TextStyle(fontSize: 22)),
      label: Text(state.language?.name ?? t.language, style: const TextStyle(color: Brand.neonBlue)),
      onPressed: () => showLanguagePicker(context),
    );
  }
}
