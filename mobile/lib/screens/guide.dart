import 'package:flutter/material.dart';

import '../app_state.dart';
import '../core/guide.dart';
import '../l10n/gen/app_localizations.dart';
import '../theme.dart';
import '../widgets/brand.dart';

/// Builds with the guide texts of the language the app is showing.
class GuideBuilder extends StatelessWidget {
  const GuideBuilder({super.key, required this.builder});

  final Widget Function(BuildContext context, Guide guide) builder;

  @override
  Widget build(BuildContext context) {
    final tag = AppLanguage.forLocale(Localizations.localeOf(context)).tag;
    return FutureBuilder<Guide>(
      future: Guide.load(tag),
      builder: (context, snap) => snap.hasData ? builder(context, snap.data!) : const SizedBox.shrink(),
    );
  }
}

void openGuide(BuildContext context, Widget screen) =>
    Navigator.of(context).push(MaterialPageRoute(builder: (_) => screen));

/// A page of the guide: same look on phone and computer, readable width.
class _GuidePage extends StatelessWidget {
  const _GuidePage({required this.title, required this.children});

  final String Function(Guide g) title;
  final List<Widget> Function(Guide g) children;

  @override
  Widget build(BuildContext context) {
    return GuideBuilder(
      builder: (context, g) => Scaffold(
        appBar: BrandBar(title: title(g)),
        body: ChipBackground(
          child: Align(
            alignment: Alignment.topCenter,
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 820),
              child: ListView(padding: const EdgeInsets.all(20), children: children(g)),
            ),
          ),
        ),
      ),
    );
  }
}

class _ItemCard extends StatelessWidget {
  const _ItemCard(this.item);

  final GuideItem item;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 14),
      child: NeonCard(
        padding: const EdgeInsets.all(16),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            SizedBox(width: 34, child: Text(item.icon, style: const TextStyle(fontSize: 24))),
            const SizedBox(width: 10),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    item.title,
                    style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w700, color: Brand.neonGreen),
                  ),
                  const SizedBox(height: 6),
                  Text(item.body, style: const TextStyle(height: 1.45, color: Brand.text)),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

Widget _intro(String text) => Padding(
  padding: const EdgeInsets.only(bottom: 18),
  child: Text(
    text,
    textAlign: TextAlign.center,
    style: const TextStyle(fontSize: 17, height: 1.45, fontWeight: FontWeight.w600, color: Brand.pearl),
  ),
);

/// Why EncryptLock: the benefits of the new blockchain.
class WhyScreen extends StatelessWidget {
  const WhyScreen({super.key});

  @override
  Widget build(BuildContext context) =>
      _GuidePage(title: (g) => g.whyTitle, children: (g) => [_intro(g.whyIntro), for (final i in g.why) _ItemCard(i)]);
}

/// How to use EncryptLock: every option of the app.
class HowToScreen extends StatelessWidget {
  const HowToScreen({super.key});

  @override
  Widget build(BuildContext context) =>
      _GuidePage(title: (g) => g.howtoTitle, children: (g) => [for (final i in g.howto) _ItemCard(i)]);
}

/// How the EncryptLock blockchain works, in detail.
class ChainScreen extends StatelessWidget {
  const ChainScreen({super.key});

  @override
  Widget build(BuildContext context) =>
      _GuidePage(title: (g) => g.chainTitle, children: (g) => [_intro(g.chainIntro), for (final i in g.chain) _ItemCard(i)]);
}

/// Phone only: earn LOCK by running EncryptLock on a computer (no share
/// button: the person does it on the computer).
class EarnWithComputerCard extends StatelessWidget {
  const EarnWithComputerCard({super.key});

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    return GuideBuilder(
      builder: (context, g) => Padding(
        padding: const EdgeInsets.only(top: 20),
        child: NeonCard(
          padding: EdgeInsets.zero,
          child: InkWell(
            onTap: () => openGuide(context, const ChainScreen()),
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      const Text('🖥️', style: TextStyle(fontSize: 20)),
                      const SizedBox(width: 8),
                      Expanded(
                        child: Text(g.earnTitle, style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w700)),
                      ),
                    ],
                  ),
                  const SizedBox(height: 8),
                  Text(g.earnBody, style: const TextStyle(color: Brand.pearl, height: 1.4)),
                  const SizedBox(height: 10),
                  Align(
                    alignment: AlignmentDirectional.centerEnd,
                    child: Text(
                      '${t.readMore} ›',
                      style: const TextStyle(color: Brand.neonGreen, fontWeight: FontWeight.w700),
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// Who we are, and how to reach us.
class AboutScreen extends StatelessWidget {
  const AboutScreen({super.key});

  @override
  Widget build(BuildContext context) => _GuidePage(
    title: (g) => g.aboutTitle,
    children: (g) => [
      const Center(child: LogoBanner(width: 260)),
      const SizedBox(height: 20),
      NeonCard(child: Text(g.aboutBody, style: const TextStyle(fontSize: 16, height: 1.5))),
      const SizedBox(height: 16),
      _ItemCard(GuideItem('✉️', g.contactsTitle, g.contactsBody)),
    ],
  );
}

/// Under the balance: a short "why EncryptLock" that opens the full page.
class WhyCard extends StatelessWidget {
  const WhyCard({super.key});

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    return GuideBuilder(
      builder: (context, g) => Padding(
        padding: const EdgeInsets.only(top: 18),
        child: NeonCard(
          padding: EdgeInsets.zero,
          child: InkWell(
            onTap: () => openGuide(context, const WhyScreen()),
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      const Text('✨', style: TextStyle(fontSize: 20)),
                      const SizedBox(width: 8),
                      Expanded(
                        child: Text(g.whyTitle, style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w700)),
                      ),
                    ],
                  ),
                  const SizedBox(height: 8),
                  Text(g.whyIntro, style: const TextStyle(color: Brand.pearl, height: 1.4)),
                  const SizedBox(height: 10),
                  Wrap(
                    spacing: 8,
                    runSpacing: 6,
                    children: [
                      for (final i in g.why) Text('${i.icon} ${i.title}', style: const TextStyle(fontSize: 13)),
                    ],
                  ),
                  const SizedBox(height: 10),
                  Align(
                    alignment: AlignmentDirectional.centerEnd,
                    child: Text(
                      '${t.readMore} ›',
                      style: const TextStyle(color: Brand.neonGreen, fontWeight: FontWeight.w700),
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// "What are LockKeepers?" — opens in place, inside the network panel.
class LockKeepersExplain extends StatelessWidget {
  const LockKeepersExplain({super.key});

  @override
  Widget build(BuildContext context) {
    return GuideBuilder(
      builder: (context, g) => Theme(
        data: Theme.of(context).copyWith(dividerColor: Colors.transparent),
        child: ExpansionTile(
          tilePadding: EdgeInsets.zero,
          childrenPadding: const EdgeInsets.only(bottom: 8),
          iconColor: Brand.neonGreen,
          collapsedIconColor: Brand.neonGreen,
          title: Text(
            '❓ ${g.lkTitle}',
            style: const TextStyle(color: Brand.neonGreen, fontWeight: FontWeight.w600, fontSize: 15),
          ),
          children: [Text(g.lkBody, style: const TextStyle(color: Brand.text, height: 1.45))],
        ),
      ),
    );
  }
}
