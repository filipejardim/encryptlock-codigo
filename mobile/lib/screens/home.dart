import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:intl/intl.dart';
import 'package:url_launcher/url_launcher.dart';

import '../app_state.dart';
import '../core/biometric.dart';
import '../core/network_pin.dart';
import '../core/network.dart' show AccountStatus, ChainError;
import '../core/pc_lockkeeper.dart';
import '../core/polygon.dart';
import '../core/startup.dart';
import '../l10n/gen/app_localizations.dart';
import '../theme.dart';
import '../widgets/brand.dart';
import '../widgets/coin_carousel.dart';
import '../widgets/history_card.dart';
import '../widgets/pending_list.dart';
import 'receive.dart';
import 'security.dart';
import 'emission.dart';
import 'move_wallet.dart' show ShowWalletQrScreen, sendWalletToComputer;
import 'guide.dart';
import 'legacy.dart';
import 'market.dart';
import 'network_panel.dart';
import 'shield.dart';
import 'send.dart';
import 'password.dart';
import 'pc_lockkeeper_card.dart';
import 'welcome.dart';

/// Main screen with the Padlock-style bottom bar.
class HomeScreen extends StatefulWidget {
  const HomeScreen({super.key});

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  int _tab = 0;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    // A link (QR read by the camera) waits: go to Send.
    if (AppScope.of(context).pendingSendTo != null && _tab != 1) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) setState(() => _tab = 1);
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final pages = [
      _WalletTab(onNavigate: (i) => setState(() => _tab = i)),
      const SendTab(),
      const ReceiveTab(),
      const _SettingsTab(),
    ];
    final wide = isWide(context);
    final menu = _MoreMenu(onSettings: () => setState(() => _tab = 3));
    final items = [
      (Icons.account_balance_wallet_outlined, t.navWallet),
      (Icons.north_east, t.navSend),
      (Icons.south_west, t.navReceive),
      (Icons.settings_outlined, t.navSettings),
    ];
    if (wide) {
      // Computer: bar across the whole window, menu on the left, the page
      // uses all the width.
      return Scaffold(
        appBar: BrandBar(title: 'ENCRYPTLOCK', actions: [menu]),
        body: ChipBackground(
          child: Row(
            children: [
              _SideMenu(items: items, selected: _tab, onSelect: (i) => setState(() => _tab = i)),
              Expanded(child: _tab == 0 ? pages[0] : _PageWidth(child: pages[_tab])),
            ],
          ),
        ),
      );
    }
    return Scaffold(
      appBar: BrandBar(title: 'ENCRYPTLOCK', actions: [menu]),
      body: ChipBackground(child: pages[_tab]),
      bottomNavigationBar: NavigationBarTheme(
        data: NavigationBarThemeData(
          backgroundColor: Brand.bottomBar,
          indicatorColor: Brand.card,
          labelTextStyle: WidgetStateProperty.resolveWith(
            (s) => TextStyle(color: s.contains(WidgetState.selected) ? Brand.neonGreen : Brand.pearl),
          ),
          iconTheme: WidgetStateProperty.resolveWith(
            (s) => IconThemeData(color: s.contains(WidgetState.selected) ? Brand.neonGreen : Brand.pearl),
          ),
        ),
        child: NavigationBar(
          selectedIndex: _tab,
          onDestinationSelected: (i) => setState(() => _tab = i),
          destinations: [
            for (final (i, (icon, label)) in items.indexed)
              NavigationDestination(icon: _withDot(Icon(icon), i == 3 && AppScope.of(context).newVersion != null), label: label),
          ],
        ),
      ),
    );
  }
}

/// A red dot on a menu icon (a new version of the app is waiting).
Widget _withDot(Widget icon, bool dot) => Badge(isLabelVisible: dot, backgroundColor: Brand.danger, smallSize: 9, child: icon);

/// "New version available" with the Update button (wallet and Settings).
class UpdateBanner extends StatelessWidget {
  const UpdateBanner({super.key});

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final progress = state.updateProgress;
    return NeonCard(
      glowColor: Brand.neonBlue,
      padding: const EdgeInsets.all(14),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            '🆕 ${t.updateAvailable(state.newVersion!)}',
            textAlign: TextAlign.center,
            style: const TextStyle(color: Brand.text, fontWeight: FontWeight.w700, fontSize: 16),
          ),
          const SizedBox(height: 10),
          if (progress != null) ...[
            LinearProgressIndicator(value: progress, color: Brand.neonGreen, backgroundColor: Brand.card),
            const SizedBox(height: 8),
            Text(t.updateDownloading((progress * 100).round()), textAlign: TextAlign.center, style: const TextStyle(color: Brand.pearl)),
          ] else
            Center(
              child: FilledButton.icon(
                icon: const Icon(Icons.system_update_alt),
                label: Text(t.updateNow),
                onPressed: () async {
                  final open = await state.runUpdate();
                  if (open != null) await launchUrl(open, mode: LaunchMode.externalApplication);
                },
              ),
            ),
          if (state.updateFailed) ...[
            const SizedBox(height: 8),
            Text(t.updateFailed, textAlign: TextAlign.center, style: const TextStyle(color: Brand.danger)),
          ],
        ],
      ),
    );
  }
}

/// Computer-sized window (or tablet): wide layout.
bool isWide(BuildContext context) => MediaQuery.sizeOf(context).width >= 900;

/// Pages that are forms (send, receive, settings) get a generous width on a
/// computer, centred, instead of stretching across a huge screen.
class _PageWidth extends StatelessWidget {
  const _PageWidth({required this.child});

  final Widget child;

  @override
  Widget build(BuildContext context) => Align(
    alignment: Alignment.topCenter,
    child: ConstrainedBox(constraints: const BoxConstraints(maxWidth: 980), child: child),
  );
}

/// Left menu on a computer.
class _SideMenu extends StatelessWidget {
  const _SideMenu({required this.items, required this.selected, required this.onSelect});

  final List<(IconData, String)> items;
  final int selected;
  final ValueChanged<int> onSelect;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: 270,
      decoration: const BoxDecoration(
        color: Color(0xE60B1316),
        border: Border(right: BorderSide(color: Color(0x6694F7D1))),
      ),
      padding: const EdgeInsets.symmetric(vertical: 18, horizontal: 14),
      // Scrolls only when the window is too low for the whole menu. The menu
      // on top, the help links at the bottom (spaced apart, never measured in
      // advance: they can never be cut off).
      child: LayoutBuilder(
        builder: (context, box) => SingleChildScrollView(
          child: ConstrainedBox(
            constraints: BoxConstraints(minHeight: box.maxHeight),
            child: _column(context),
          ),
        ),
      ),
    );
  }

  Widget _column(BuildContext context) {
    return Column(
      mainAxisAlignment: MainAxisAlignment.spaceBetween,
      children: [
        Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            // The full logo, big and visible, above the menu.
            const LogoBanner(width: 242),
            const SizedBox(height: 22),
            for (var i = 0; i < items.length; i++)
              Padding(
                padding: const EdgeInsets.only(bottom: 8),
                child: Material(
                  color: i == selected ? Brand.card : Colors.transparent,
                  shape: BeveledRectangleBorder(
                    borderRadius: const BorderRadius.all(Radius.circular(10)),
                    side: BorderSide(color: i == selected ? Brand.neonGreen : Colors.transparent),
                  ),
                  child: InkWell(
                    customBorder: const BeveledRectangleBorder(borderRadius: BorderRadius.all(Radius.circular(10))),
                    onTap: () => onSelect(i),
                    child: Padding(
                      padding: const EdgeInsets.symmetric(vertical: 14, horizontal: 14),
                      child: Row(
                        children: [
                          _withDot(
                            Icon(items[i].$1, color: i == selected ? Brand.neonGreen : Brand.pearl),
                            i == 3 && AppScope.of(context).newVersion != null,
                          ),
                          const SizedBox(width: 14),
                          Text(
                            items[i].$2,
                            style: TextStyle(
                              fontSize: 16,
                              fontWeight: FontWeight.w600,
                              color: i == selected ? Brand.neonGreen : Brand.pearl,
                            ),
                          ),
                        ],
                      ),
                    ),
                  ),
                ),
              ),
          ],
        ),
        GuideBuilder(
          // Transparent Material: the taps' ink shows over the menu's background.
          builder: (context, g) => Material(
            type: MaterialType.transparency,
            child: Column(
              children: [
                for (final (emoji, title, screen)
                    in [
                          ('📖', g.howtoTitle, const HowToScreen()),
                          ('✨', g.whyTitle, const WhyScreen()),
                          ('ℹ️', g.aboutTitle, const AboutScreen()),
                        ]
                        as List<(String, String, Widget)>)
                  ListTile(
                    dense: true,
                    leading: Text(emoji, style: const TextStyle(fontSize: 18)),
                    title: Text(title, style: const TextStyle(color: Brand.pearl, fontSize: 14)),
                    onTap: () => openGuide(context, screen),
                  ),
                // Padlock, the app for encrypted communication.
                ListTile(
                  dense: true,
                  leading: ClipOval(child: Image.asset('assets/padlock.png', width: 20, height: 20)),
                  title: Text(
                    AppLocalizations.of(context).padlockShort,
                    style: const TextStyle(color: Brand.pearl, fontSize: 14),
                  ),
                  onTap: () => openPadlock(context),
                ),
              ],
            ),
          ),
        ),
      ],
    );
  }
}

/// ⋮ at the top right: settings, language, lock.
class _MoreMenu extends StatelessWidget {
  const _MoreMenu({required this.onSettings});

  final VoidCallback onSettings;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    return PopupMenuButton<int>(
      icon: const Icon(Icons.more_vert, color: Brand.neonGreen),
      color: Brand.bottomBar,
      onSelected: (v) async {
        switch (v) {
          case 0:
            onSettings();
          case 1:
            showLanguagePicker(context);
          case 2:
            state.lock();
            final hasWallet = await state.store.hasWallet();
            if (!context.mounted) return;
            Navigator.of(context).pushAndRemoveUntil(
              MaterialPageRoute(builder: (_) => hasWallet ? const UnlockScreen() : const WelcomeScreen()),
              (_) => false,
            );
        }
      },
      itemBuilder: (_) => [
        PopupMenuItem(
          value: 0,
          child: ListTile(leading: const Icon(Icons.settings_outlined), title: Text(t.navSettings)),
        ),
        PopupMenuItem(
          value: 1,
          child: ListTile(
            leading: const Text('🌍', style: TextStyle(fontSize: 20)),
            title: Text(t.language),
          ),
        ),
        PopupMenuItem(
          value: 2,
          child: ListTile(leading: const Icon(Icons.lock), title: Text(t.lockWallet)),
        ),
      ],
    );
  }
}

/// The balance in the chosen currency: LOCK at the market price, USDC as
/// dollars, POL at the exchanges' price; "—" when there is no price yet.
String fiatText(AppState state, Coin coin, AccountStatus? status, String locale) {
  double? v;
  if (coin.isLock && status != null) {
    v = state.fiatValue(status.balance);
  } else if (coin.ticker == 'USDC' && state.polygon != null) {
    v = state.fiat(state.polygon!.usdc.toDouble() / 1e6);
  } else if (coin.ticker == 'POL' && state.polygon != null && state.usdPerPolPrice != null) {
    v = state.fiat(state.polygon!.pol.toDouble() / 1e18 * state.usdPerPolPrice!);
  }
  return v == null ? '—' : NumberFormat('#,##0.00', locale).format(v);
}

/// An amount of a coin with [decimals] (USDC 6, POL 18), like [formatLock].
String formatUnits(BigInt units, int decimals, String locale) {
  final one = BigInt.from(10).pow(decimals);
  final whole = units ~/ one;
  final fraction = (units % one) / one;
  return NumberFormat('#,##0.00##', locale).format(whole.toDouble() + fraction);
}

/// LOCK amount (from lockunit) in the language's number style: 2 decimals,
/// up to 4 when a small amount needs them (1.000,00 · 0,001).
String formatLock(BigInt units, String locale) {
  final whole = units ~/ BigInt.from(100000000);
  final fraction = (units % BigInt.from(100000000)).toInt() / 100000000;
  return NumberFormat('#,##0.00##', locale).format(whole.toDouble() + fraction);
}

class _WalletTab extends StatefulWidget {
  const _WalletTab({required this.onNavigate});

  /// Switch to another page (send, receive) from the quick buttons.
  final ValueChanged<int> onNavigate;

  @override
  State<_WalletTab> createState() => _WalletTabState();
}

class _WalletTabState extends State<_WalletTab> {
  Timer? _poll;

  /// The coin in front of the carousel.
  int _coin = 0;

  @override
  void initState() {
    super.initState();
    // Live: ask the LockKeeper every 3 seconds (about 3 new blocks).
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) return;
      final state = AppScope.of(context);
      state.refresh();
      _poll = Timer.periodic(const Duration(seconds: 3), (_) => state.refresh());
    });
  }

  @override
  void dispose() {
    _poll?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final locale = Localizations.localeOf(context).toString();
    final coin = Coin.all[_coin];
    final status = coin.isLock ? state.status : null;
    final polygon = state.polygon;
    final String balance;
    if (coin.isLock) {
      balance = status == null ? '—' : formatLock(status.balance, locale);
    } else if (coin.onPolygon && polygon != null) {
      balance = formatUnits(coin.ticker == 'USDC' ? polygon.usdc : polygon.pol, coin.decimals, locale);
    } else {
      balance = '—';
    }
    final shownAddress = coin.onPolygon ? state.polygonAddress : state.address;
    // The blockchain panel (LockKeepers at work) only for LockKeepers;
    // everyone else sees how to become one.
    final showNetwork = (status?.isLockKeeper ?? false) || PcLockKeeper.isSetUp;
    final head = <Widget>[
      if (state.core.isPreview)
        Padding(
          padding: const EdgeInsets.only(bottom: 16),
          child: Text(
            t.previewMode,
            textAlign: TextAlign.center,
            style: const TextStyle(color: Brand.neonBlue),
          ),
        ),
      CoinCarousel(coins: Coin.all, initial: _coin, onChanged: (i) => setState(() => _coin = i)),
      const SizedBox(height: 4),
      Text(
        coin.name,
        textAlign: TextAlign.center,
        style: TextStyle(
          fontSize: 22,
          fontWeight: FontWeight.w800,
          color: coin.color,
          shadows: [Shadow(color: coin.color.withValues(alpha: 0.7), blurRadius: 14)],
        ),
      ),
      Text(
        [coin.ticker, ?coin.network, if (!coin.ready) t.comingSoon].join(' · '),
        textAlign: TextAlign.center,
        style: const TextStyle(color: Brand.pearl, fontSize: 15, letterSpacing: 2),
      ),
      if (state.newVersion != null) ...[const SizedBox(height: 10), const UpdateBanner()],
      if (coin.isLock) const ClaimBanner(),
      if (coin.isLock && (state.status?.frozen ?? false)) ...[
        const SizedBox(height: 14),
        NeonCard(
          glowColor: Brand.danger,
          padding: const EdgeInsets.all(14),
          child: Row(
            children: [
              const Text('❄️', style: TextStyle(fontSize: 24)),
              const SizedBox(width: 10),
              Expanded(
                child: Text(
                  t.frozenBanner,
                  style: const TextStyle(color: Brand.text, fontWeight: FontWeight.w700),
                ),
              ),
            ],
          ),
        ),
      ],
      const SizedBox(height: 18),
      NeonCard(
        glowColor: coin.color,
        child: Column(
          children: [
            Text(t.balance, style: const TextStyle(color: Brand.pearl, fontSize: 16)),
            const SizedBox(height: 8),
            ShaderMask(
              shaderCallback: (r) => Brand.blueGradient.createShader(r),
              child: FittedBox(
                fit: BoxFit.scaleDown,
                child: Text(
                  '$balance ${coin.ticker}',
                  style: const TextStyle(fontSize: 38, fontWeight: FontWeight.w800, color: Colors.white),
                ),
              ),
            ),
            const SizedBox(height: 4),
            // The value in euros/dollars comes from the market price (a dash
            // until the first trade).
            Text(
              '≈ ${fiatText(state, coin, status, locale)} ${state.currency.symbol}',
              style: const TextStyle(color: Brand.pearl, fontSize: 20, fontWeight: FontWeight.w600),
            ),
            if (status != null && status.locked > BigInt.zero) ...[
              const SizedBox(height: 6),
              Text(
                '🔒 ${t.lockedTotal(formatLock(status.locked, locale))}',
                style: const TextStyle(color: Brand.pearl),
              ),
            ],
            if (coin.isLock) ...[const SizedBox(height: 8), NetworkLine(state: state)],
            if (coin.onPolygon) ...[
              const SizedBox(height: 8),
              Text(
                PolygonNetwork.isTest ? t.polygonTestNet(PolygonNetwork.current.name) : 'Polygon',
                textAlign: TextAlign.center,
                style: const TextStyle(color: Brand.neonBlue, fontSize: 13),
              ),
            ],
          ],
        ),
      ),
      // Phone and computer: the market right under the balance, "Why" below.
      if (coin.isLock) ...[const SizedBox(height: 14), const MarketStrip()],
      // USDC and POL: what is needed here to buy LOCK.
      if (coin.onPolygon) ...[const SizedBox(height: 14), const HowToBuyCard()],
      const WhyCard(),
    ];
    final tail = <Widget>[
      // Transfers to accept come first: they go back after 1 hour.
      if (status != null) PendingList(received: status.received, sent: status.sent),
      // On a second device: what the protected accounts want to send.
      if (coin.isLock) const ApprovalsCard(),
      if (status != null && status.isLockKeeper) ...[
        const SizedBox(height: 20),
        _EarningsCard(units: status.toCollect, locale: locale),
      ],
      if (coin.ready) ...[
        const SizedBox(height: 20),
        // Compact: address on one line, small copy button beside it.
        NeonCard(
          padding: const EdgeInsets.fromLTRB(18, 12, 8, 12),
          child: Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      t.yourAddress,
                      style: TextStyle(
                        color: Brand.neonGreen,
                        fontSize: 14,
                        fontWeight: FontWeight.w700,
                        shadows: [Shadow(color: Brand.neonGreen.withValues(alpha: 0.6), blurRadius: 10)],
                      ),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      shownAddress ?? '',
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w600, letterSpacing: 0.2),
                    ),
                  ],
                ),
              ),
              IconButton(
                tooltip: t.copy,
                icon: const Icon(Icons.copy, color: Brand.neonGreen),
                onPressed: () {
                  Clipboard.setData(ClipboardData(text: shownAddress ?? ''));
                  ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(t.copied)));
                },
              ),
            ],
          ),
        ),
      ],
      if (coin.isLock && state.address != null) ...[const SizedBox(height: 20), const HistoryCard()],
      // Phone: earn LOCK with a computer (a phone cannot be a LockKeeper).
      if (!isWide(context) && coin.isLock) const EarnWithComputerCard(),
    ];
    if (!isWide(context)) {
      return ListView(
        padding: const EdgeInsets.all(20),
        children: [
          ...head,
          ...tail,
          if (showNetwork) ...[const SizedBox(height: 20), const NetworkPanel()],
        ],
      );
    }
    // Computer: coins and balance on the left; actions, transfers and the
    // network on the right.
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Expanded(
          flex: 11,
          child: ListView(padding: const EdgeInsets.fromLTRB(28, 24, 14, 28), children: head),
        ),
        Expanded(
          flex: 9,
          child: ListView(
            padding: const EdgeInsets.fromLTRB(14, 24, 28, 28),
            children: [
              if (coin.isLock)
                Row(
                  children: [
                    Expanded(
                      child: NeonButton(
                        label: t.navSend,
                        icon: Icons.north_east,
                        onPressed: () => widget.onNavigate(1),
                      ),
                    ),
                    const SizedBox(width: 14),
                    Expanded(
                      child: NeonButton(
                        label: t.navReceive,
                        icon: Icons.south_west,
                        secondary: true,
                        onPressed: () => widget.onNavigate(2),
                      ),
                    ),
                  ],
                ),
              ...tail,
              if (coin.isLock) const PcLockKeeperCard(),
              if (showNetwork) ...[const SizedBox(height: 20), const NetworkPanel()],
            ],
          ),
        ),
      ],
    );
  }
}

class _SettingsTab extends StatelessWidget {
  const _SettingsTab();

  Future<void> _editLockKeeper(BuildContext context) async {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final controller = TextEditingController(text: state.lockKeeperUrl);
    final url = await showDialog<String>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        backgroundColor: Brand.bottomBar,
        title: Text(t.networkSetting),
        content: TextField(
          controller: controller,
          autocorrect: false,
          keyboardType: TextInputType.url,
          decoration: InputDecoration(helperText: t.networkHint, helperMaxLines: 3),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(dialogContext), child: Text(t.cancel)),
          TextButton(onPressed: () => Navigator.pop(dialogContext, controller.text), child: Text(t.save)),
        ],
      ),
    );
    // Not disposed here: the dialog is still fading out and uses it.
    if (url != null && url.trim().isNotEmpty) await state.setLockKeeperUrl(url);
  }

  void _pickAutoLock(BuildContext context) {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    showModalBottomSheet<void>(
      context: context,
      backgroundColor: Brand.bottomBar,
      showDragHandle: true,
      builder: (sheetContext) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            for (final m in AppState.autoLockChoices)
              ListTile(
                title: Text(t.autoLockMinutes(m)),
                trailing: state.autoLockMinutes == m ? const Icon(Icons.check, color: Brand.neonGreen) : null,
                onTap: () {
                  state.setAutoLock(m);
                  Navigator.pop(sheetContext);
                },
              ),
          ],
        ),
      ),
    );
  }

  void _pickCurrency(BuildContext context) {
    final state = AppScope.of(context);
    showModalBottomSheet<void>(
      context: context,
      backgroundColor: Brand.bottomBar,
      showDragHandle: true,
      builder: (sheetContext) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            for (final c in FiatCurrency.all)
              ListTile(
                title: Text('${c.code}  ${c.symbol}'),
                trailing: state.currency.code == c.code ? const Icon(Icons.check, color: Brand.neonGreen) : null,
                onTap: () {
                  state.setCurrency(c);
                  Navigator.pop(sheetContext);
                },
              ),
          ],
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    return ListView(
      padding: const EdgeInsets.all(20),
      children: [
        if (state.newVersion != null) ...[const UpdateBanner(), const SizedBox(height: 16)],
        NeonCard(
          padding: const EdgeInsets.symmetric(vertical: 6),
          child: Column(
            children: [
              _SettingsRow(
                emoji: '🌍',
                title: t.language,
                value: (state.language ?? AppLanguage.forLocale(Localizations.localeOf(context))).name,
                onTap: () => showLanguagePicker(context),
              ),
              const Divider(height: 1, color: Brand.cardBorder, indent: 20, endIndent: 20),
              _SettingsRow(
                emoji: '💱',
                title: t.currency,
                value: '${state.currency.code} ${state.currency.symbol}',
                onTap: () => _pickCurrency(context),
              ),
              const Divider(height: 1, color: Brand.cardBorder, indent: 20, endIndent: 20),
              _SettingsRow(
                emoji: '⏱️',
                title: t.autoLock,
                value: t.autoLockMinutes(state.autoLockMinutes),
                onTap: () => _pickAutoLock(context),
              ),
              if (Platform.isWindows) ...[
                const Divider(height: 1, color: Brand.cardBorder, indent: 20, endIndent: 20),
                const _StartupRow(),
              ],
            ],
          ),
        ),
        const SizedBox(height: 20),
        _SecurityCard(state: state),
        const SizedBox(height: 20),
        _ShieldCard(state: state),
        const SizedBox(height: 20),
        _LegacyCard(state: state),
        const SizedBox(height: 20),
        const _HelpCard(),
        const SizedBox(height: 20),
        const _AppsCard(),
        const SizedBox(height: 20),
        // Advanced: only in test versions (the LockKeeper's address, the
        // test or real Polygon). The final app finds the network by itself
        // and always uses the real Polygon: nothing to get wrong here.
        if (NetworkPin.chainId.isEmpty) ...[
        _SectionTitle(t.advanced),
        NeonCard(
          padding: const EdgeInsets.symmetric(vertical: 6),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(20, 10, 20, 4),
                child: Text(t.advancedExplain, style: const TextStyle(color: Brand.pearl, fontSize: 13, height: 1.4)),
              ),
              _SettingsRow(
                emoji: '🛰️',
                title: t.networkSetting,
                value: state.lockKeeperUrl,
                onTap: () => _editLockKeeper(context),
              ),
              SwitchListTile(
                contentPadding: const EdgeInsets.symmetric(horizontal: 20),
                secondary: const Text('💵', style: TextStyle(fontSize: 22)),
                title: Text(t.realPolygon, style: const TextStyle(fontWeight: FontWeight.w600)),
                subtitle: Text(t.realPolygonHint, style: const TextStyle(color: Brand.pearl, fontSize: 13)),
                value: !PolygonNetwork.isTest,
                activeThumbColor: Brand.neonGreen,
                onChanged: (on) async {
                  if (on) {
                    final ok = await showDialog<bool>(
                      context: context,
                      builder: (dialogContext) => AlertDialog(
                        backgroundColor: Brand.bottomBar,
                        title: Text(t.realPolygon),
                        content: Text(t.realPolygonConfirm, style: const TextStyle(height: 1.4)),
                        actions: [
                          TextButton(onPressed: () => Navigator.pop(dialogContext, false), child: Text(t.cancel)),
                          TextButton(onPressed: () => Navigator.pop(dialogContext, true), child: const Text('OK')),
                        ],
                      ),
                    );
                    if (ok != true) return;
                  }
                  await state.setRealPolygon(on);
                },
              ),
            ],
          ),
        ),
        const SizedBox(height: 20),
        ],
        NeonButton(
          label: t.lockWallet,
          icon: Icons.lock,
          onPressed: () async {
            state.lock();
            final hasWallet = await state.store.hasWallet();
            if (!context.mounted) return;
            Navigator.of(context).pushAndRemoveUntil(
              MaterialPageRoute(builder: (_) => hasWallet ? const UnlockScreen() : const WelcomeScreen()),
              (_) => false,
            );
          },
        ),
        const SizedBox(height: 28),
        // Danger zone, apart from the rest.
        NeonCard(
          glowColor: Brand.danger,
          padding: const EdgeInsets.symmetric(vertical: 6),
          child: _SettingsRow(emoji: '🗑️', title: t.deleteWallet, value: '', onTap: () => _deleteWallet(context)),
        ),
      ],
    );
  }

  Future<void> _deleteWallet(BuildContext context) async {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final sure = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        backgroundColor: Brand.bottomBar,
        title: Text(t.deleteWallet),
        content: Text(t.deleteExplain, style: const TextStyle(height: 1.4)),
        actions: [
          TextButton(onPressed: () => Navigator.pop(dialogContext, false), child: Text(t.cancel)),
          TextButton(
            onPressed: () => Navigator.pop(dialogContext, true),
            child: Text(t.confirm, style: const TextStyle(color: Brand.danger)),
          ),
        ],
      ),
    );
    if (sure != true || !context.mounted) return;
    if (!await confirmPassword(context) || !context.mounted) return;
    await state.deleteWallet();
    if (!context.mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(t.deleted)));
    Navigator.of(context).pushAndRemoveUntil(MaterialPageRoute(builder: (_) => const WelcomeScreen()), (_) => false);
  }
}

/// Help: why EncryptLock, how to use it, who we are.
class _HelpCard extends StatelessWidget {
  const _HelpCard();

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    return GuideBuilder(
      builder: (context, g) => Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _SectionTitle(t.helpSection),
          NeonCard(
            padding: const EdgeInsets.symmetric(vertical: 6),
            child: Column(
              children: [
                _SettingsRow(
                  emoji: '📖',
                  title: g.howtoTitle,
                  value: '',
                  onTap: () => openGuide(context, const HowToScreen()),
                ),
                const Divider(height: 1, color: Brand.cardBorder, indent: 20, endIndent: 20),
                _SettingsRow(
                  emoji: '✨',
                  title: g.whyTitle,
                  value: '',
                  onTap: () => openGuide(context, const WhyScreen()),
                ),
                const Divider(height: 1, color: Brand.cardBorder, indent: 20, endIndent: 20),
                _SettingsRow(
                  emoji: '⛓️',
                  title: g.chainTitle,
                  value: '',
                  onTap: () => openGuide(context, const ChainScreen()),
                ),
                const Divider(height: 1, color: Brand.cardBorder, indent: 20, endIndent: 20),
                _SettingsRow(
                  emoji: '📊',
                  title: t.emTitle,
                  value: '',
                  onTap: () => openGuide(context, const EmissionScreen()),
                ),
                const Divider(height: 1, color: Brand.cardBorder, indent: 20, endIndent: 20),
                _SettingsRow(
                  emoji: 'ℹ️',
                  title: g.aboutTitle,
                  value: '',
                  onTap: () => openGuide(context, const AboutScreen()),
                ),
                const Divider(height: 1, color: Brand.cardBorder, indent: 20, endIndent: 20),
                _SettingsRow(emoji: '🔐', title: t.padlockShort, value: '', onTap: () => openPadlock(context)),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// Opens Padlock on Google Play, or says it is coming soon there.
void openPadlock(BuildContext context) {
  final url = _AppsCard.padlockUrl;
  if (url != null) {
    launchUrl(url, mode: LaunchMode.externalApplication);
    return;
  }
  ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(AppLocalizations.of(context).padlockSoon)));
}

/// Apps that work with EncryptLock (they can send and receive LOCK).
class _AppsCard extends StatelessWidget {
  const _AppsCard();

  /// Padlock on Google Play: null until Padlock is published there (the
  /// button then says "coming soon" and does nothing).
  static final Uri? padlockUrl = null;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        _SectionTitle(t.appsTitle),
        NeonCard(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Row(
                children: [
                  ClipOval(child: Image.asset('assets/padlock.png', width: 56, height: 56)),
                  const SizedBox(width: 14),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        const Text(
                          'Padlock',
                          style: TextStyle(fontSize: 18, fontWeight: FontWeight.w700, color: Brand.mintText),
                        ),
                        const SizedBox(height: 4),
                        Text(t.padlockDesc, style: const TextStyle(color: Brand.pearl, height: 1.35)),
                      ],
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 14),
              NeonButton(
                label: padlockUrl == null ? '${t.padlockOpen} · ${t.comingSoon}' : t.padlockOpen,
                icon: Icons.shop,
                secondary: true,
                onPressed: padlockUrl == null
                    ? null
                    : () => launchUrl(padlockUrl!, mode: LaunchMode.externalApplication),
              ),
            ],
          ),
        ),
      ],
    );
  }
}

/// Computer only: open EncryptLock when Windows starts.
class _StartupRow extends StatefulWidget {
  const _StartupRow();

  @override
  State<_StartupRow> createState() => _StartupRowState();
}

class _StartupRowState extends State<_StartupRow> {
  bool? _on;

  @override
  void initState() {
    super.initState();
    WindowsStartup.isOn().then((on) {
      if (mounted) setState(() => _on = on);
    });
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    return SwitchListTile(
      contentPadding: const EdgeInsets.symmetric(horizontal: 20),
      secondary: const Text('🖥️', style: TextStyle(fontSize: 22)),
      title: Text(t.startWithWindows, style: const TextStyle(fontWeight: FontWeight.w600)),
      subtitle: Text(t.startWithWindowsHint, style: const TextStyle(color: Brand.pearl, fontSize: 13)),
      value: _on ?? false,
      activeThumbColor: Brand.neonGreen,
      onChanged: _on == null
          ? null
          : (v) async {
              await WindowsStartup.set(v);
              final now = await WindowsStartup.isOn();
              if (mounted) setState(() => _on = now);
            },
    );
  }
}

class _SectionTitle extends StatelessWidget {
  const _SectionTitle(this.text);

  final String text;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsetsDirectional.only(start: 6, bottom: 8),
    child: Text(
      text.toUpperCase(),
      style: const TextStyle(color: Brand.neonGreen, fontSize: 13, fontWeight: FontWeight.w700, letterSpacing: 1.5),
    ),
  );
}

/// Shield: second device, daily limit, and protecting other accounts.
/// Inheritance, trusted people, Sovereign Vault and helping others.
class _LegacyCard extends StatelessWidget {
  const _LegacyCard({required this.state});

  final AppState state;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final vault = state.status?.shield.vaultDays ?? 0;
    void open(Widget screen) => Navigator.push(context, MaterialPageRoute(builder: (_) => screen));
    const divider = Divider(height: 1, color: Brand.cardBorder, indent: 20, endIndent: 20);
    return NeonCard(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Column(
        children: [
          _SettingsRow(emoji: '👪', title: t.inheritance, value: '', onTap: () => open(const InheritanceScreen())),
          divider,
          _SettingsRow(emoji: '🤝', title: t.guardians, value: '', onTap: () => open(const GuardiansScreen())),
          divider,
          _SettingsRow(
            emoji: '🏦',
            title: t.vault,
            value: vault == 0 ? t.vaultOff : t.vaultOn(vault),
            onTap: () => open(const VaultScreen()),
          ),
          divider,
          _SettingsRow(emoji: '🧾', title: t.helpOthers, value: '', onTap: () => open(const HelpOthersScreen())),
        ],
      ),
    );
  }
}

class _ShieldCard extends StatelessWidget {
  const _ShieldCard({required this.state});

  final AppState state;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final locale = Localizations.localeOf(context).toString();
    final shield = state.status?.shield;
    Future<void> open(Widget screen) async {
      if (await confirmPassword(context) && context.mounted) {
        Navigator.push(context, MaterialPageRoute(builder: (_) => screen));
      }
    }

    return NeonCard(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Column(
        children: [
          _SettingsRow(
            emoji: '📱',
            title: t.shieldDevice,
            value: (shield == null || shield.device.isEmpty) ? t.deviceNone : shortAddress(shield.device),
            onTap: () => open(const DeviceScreen()),
          ),
          const Divider(height: 1, color: Brand.cardBorder, indent: 20, endIndent: 20),
          _SettingsRow(
            emoji: '📊',
            title: t.limits,
            value: (shield == null || shield.dailyLimit == 0)
                ? t.limitsNone
                : t.limitsValue(formatLock(BigInt.from(shield.dailyLimit), locale)),
            onTap: () => open(const LimitsScreen()),
          ),
          const Divider(height: 1, color: Brand.cardBorder, indent: 20, endIndent: 20),
          _SettingsRow(
            emoji: '🔔',
            title: t.alerts,
            value: state.alertsOn ? t.on : t.off,
            onTap: () => state.setAlerts(!state.alertsOn),
          ),
          const Divider(height: 1, color: Brand.cardBorder, indent: 20, endIndent: 20),
          _SettingsRow(
            emoji: '🛡️',
            title: t.guardOther,
            value: state.guarded.isEmpty ? t.deviceNone : '${state.guarded.length}',
            onTap: () => Navigator.push(context, MaterialPageRoute(builder: (_) => const GuardScreen())),
          ),
        ],
      ),
    );
  }
}

/// Security: password, the 24 words and "It wasn't me".
class _SecurityCard extends StatelessWidget {
  const _SecurityCard({required this.state});

  final AppState state;

  Future<void> _toggleBiometric(BuildContext context) async {
    final t = AppLocalizations.of(context);
    final messenger = ScaffoldMessenger.of(context);
    if (state.biometricOn) {
      await state.disableBiometric();
      return;
    }
    if (!await Biometric.available()) {
      messenger.showSnackBar(SnackBar(content: Text(t.biometricUnavailable)));
      return;
    }
    if (!context.mounted || !await confirmPassword(context)) return;
    try {
      await state.enableBiometric(title: t.biometricReason, cancel: t.cancel);
      messenger.showSnackBar(SnackBar(content: Text(t.biometricEnabled)));
    } on BiometricException catch (e) {
      if (e.failure != BiometricFailure.canceled) {
        messenger.showSnackBar(SnackBar(content: Text(t.biometricUnavailable)));
      }
    }
  }

  Future<void> _notMe(BuildContext context) async {
    final t = AppLocalizations.of(context);
    final messenger = ScaffoldMessenger.of(context);
    final frozen = state.status?.frozen ?? false;
    final sure = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        backgroundColor: Brand.bottomBar,
        title: Text(frozen ? t.unfreeze : t.notMe),
        content: Text(frozen ? t.unfreezeExplain : t.notMeExplain, style: const TextStyle(height: 1.4)),
        actions: [
          TextButton(onPressed: () => Navigator.pop(dialogContext, false), child: Text(t.cancel)),
          TextButton(
            onPressed: () => Navigator.pop(dialogContext, true),
            child: Text(t.confirm, style: TextStyle(color: frozen ? Brand.neonGreen : Brand.danger)),
          ),
        ],
      ),
    );
    if (sure != true) return;
    try {
      if (frozen) {
        await state.unfreeze();
        messenger.showSnackBar(SnackBar(content: Text(t.unfreezeAsked)));
      } else {
        // No password here on purpose: in an emergency every second counts.
        await state.freeze();
        messenger.showSnackBar(SnackBar(content: Text(t.frozenOk)));
      }
    } on ChainError catch (e) {
      messenger.showSnackBar(SnackBar(content: Text(chainErrorText(t, e.log))));
    } catch (e) {
      debugPrint('transaction failed: $e');
      messenger.showSnackBar(SnackBar(content: Text(t.networkError)));
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final frozen = state.status?.frozen ?? false;
    return NeonCard(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Column(
        children: [
          _SettingsRow(
            emoji: '🔑',
            title: t.changePassword,
            value: '',
            onTap: () async {
              if (await confirmPassword(context) && context.mounted) {
                Navigator.push(context, MaterialPageRoute(builder: (_) => const ChangePasswordScreen()));
              }
            },
          ),
          // Fingerprint / face exists only on the phone.
          if (Platform.isAndroid) ...[
            const Divider(height: 1, color: Brand.cardBorder, indent: 20, endIndent: 20),
            _SettingsRow(
              emoji: '👆',
              title: t.biometric,
              value: state.biometricOn ? t.on : t.off,
              onTap: () => _toggleBiometric(context),
            ),
          ],
          const Divider(height: 1, color: Brand.cardBorder, indent: 20, endIndent: 20),
          _SettingsRow(
            emoji: '🔢',
            title: t.sendCode,
            value: state.sendCodeOn ? t.on : t.off,
            onTap: () async {
              if (!await confirmPassword(context) || !context.mounted) return;
              if (state.sendCodeOn) {
                await state.removeSendCode();
                if (context.mounted) {
                  ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(t.sendCodeRemoved)));
                }
              } else {
                await Navigator.push(context, MaterialPageRoute(builder: (_) => const SendCodeScreen()));
              }
            },
          ),
          const Divider(height: 1, color: Brand.cardBorder, indent: 20, endIndent: 20),
          _SettingsRow(
            emoji: '📜',
            title: t.showWords,
            value: '',
            onTap: () async {
              if (await confirmPassword(context) && context.mounted) {
                Navigator.push(context, MaterialPageRoute(builder: (_) => const ShowWordsScreen()));
              }
            },
          ),
          // Phone: the same wallet on the computer, over the home Wi-Fi.
          if (!isWide(context)) ...[
            const Divider(height: 1, color: Brand.cardBorder, indent: 20, endIndent: 20),
            _SettingsRow(
              emoji: '💻',
              title: t.walletToPc,
              value: '',
              onTap: () async {
                if (await confirmPassword(context) && context.mounted) await sendWalletToComputer(context);
              },
            ),
          ],
          // Computer: the same wallet on the phone, by QR code + one-time code.
          if (isWide(context)) ...[
            const Divider(height: 1, color: Brand.cardBorder, indent: 20, endIndent: 20),
            _SettingsRow(
              emoji: '📲',
              title: t.walletToPhone,
              value: '',
              onTap: () async {
                if (await confirmPassword(context) && context.mounted) {
                  Navigator.push(context, MaterialPageRoute(builder: (_) => const ShowWalletQrScreen()));
                }
              },
            ),
          ],
          const Divider(height: 1, color: Brand.cardBorder, indent: 20, endIndent: 20),
          _SettingsRow(
            emoji: frozen ? '❄️' : '🚨',
            title: frozen ? t.unfreeze : t.notMe,
            value: frozen ? t.frozenBanner : '',
            onTap: () => _notMe(context),
          ),
        ],
      ),
    );
  }
}

/// One clean line in Settings: coloured icon, name, chosen value, arrow.
class _SettingsRow extends StatelessWidget {
  const _SettingsRow({required this.emoji, required this.title, required this.value, required this.onTap});

  final String emoji;
  final String title;
  final String value;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return ListTile(
      contentPadding: const EdgeInsets.symmetric(horizontal: 20, vertical: 4),
      leading: Text(emoji, style: const TextStyle(fontSize: 26)),
      title: Text(
        title,
        style: const TextStyle(color: Brand.text, fontSize: 17, fontWeight: FontWeight.w600),
      ),
      subtitle: value.isEmpty ? null : Text(value, style: const TextStyle(color: Brand.neonBlue)),
      trailing: const Icon(Icons.chevron_right, color: Brand.pearl),
      onTap: onTap,
    );
  }
}

/// The wallet's line under the balance: green while this device is online,
/// red without internet. Whether LockKeepers are on is not shown here (the
/// balance, its value and USDC / POL coming in do not depend on it): that is
/// in the Blockchain · LockKeepers panel and in the market ([KeepersLine]).
class NetworkLine extends StatelessWidget {
  const NetworkLine({super.key, required this.state});

  final AppState state;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final status = state.status;
    final locale = Localizations.localeOf(context).toString();
    final offline = state.networkError && state.noInternet;
    final known = status != null || state.networkError;
    final last = offline ? state.lastUpdate : null;
    final text = offline
        ? (last != null ? t.noInternetSince(_time(last, locale)) : t.noInternet)
        : !known
        ? t.connecting
        : status != null && !state.networkError
        ? t.connectedBlock(NumberFormat.decimalPattern(locale).format(status.height))
        : t.online;
    return _DotLine(text: text, color: offline ? Brand.danger : (known ? Brand.neonGreen : Brand.pearl));
  }
}

/// Market and Blockchain · LockKeepers: how many LockKeepers of the
/// blockchain are on, or that none answers (Buy / Sell then wait).
class KeepersLine extends StatelessWidget {
  const KeepersLine({super.key, required this.state});

  final AppState state;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final locale = Localizations.localeOf(context).toString();
    final last = state.lastUpdate;
    if (state.networkError && state.noInternet) {
      return _DotLine(text: last != null ? t.noInternetSince(_time(last, locale)) : t.noInternet, color: Brand.danger);
    }
    if (state.networkError) {
      return _DotLine(
        text: last != null ? t.waitingKeeperSince(_time(last, locale)) : t.waitingKeeper,
        color: Brand.neonBlue,
      );
    }
    final n = state.keepersOnline;
    if (n == null) return const SizedBox.shrink();
    return _DotLine(text: t.keepersOnline(n), color: n > 0 ? Brand.neonGreen : Brand.neonBlue);
  }
}

String _time(DateTime d, String locale) => DateUtils.isSameDay(d, DateTime.now())
    ? DateFormat.Hm(locale).format(d)
    : DateFormat.MMMd(locale).add_Hm().format(d);

class _DotLine extends StatelessWidget {
  const _DotLine({required this.text, required this.color});

  final String text;
  final Color color;

  @override
  Widget build(BuildContext context) => Row(
    mainAxisAlignment: MainAxisAlignment.center,
    children: [
      Container(
        width: 10,
        height: 10,
        decoration: BoxDecoration(shape: BoxShape.circle, color: color, boxShadow: Brand.glow(color, 0.8)),
      ),
      const SizedBox(width: 8),
      Flexible(
        child: Text(
          text,
          textAlign: TextAlign.center,
          style: const TextStyle(color: Brand.pearl),
        ),
      ),
    ],
  );
}

/// LockKeeper earnings waiting to be collected; the number rolls up live.
class _EarningsCard extends StatelessWidget {
  const _EarningsCard({required this.units, required this.locale});

  final BigInt units;
  final String locale;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final target = units.toDouble();
    return NeonCard(
      glowColor: Brand.neonGreen,
      child: Column(
        children: [
          Row(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              const Text('⛏️', style: TextStyle(fontSize: 22)),
              const SizedBox(width: 8),
              Flexible(
                child: Text(
                  t.lockKeeperEarnings,
                  textAlign: TextAlign.center,
                  style: const TextStyle(color: Brand.neonGreen, fontSize: 18, fontWeight: FontWeight.w700),
                ),
              ),
            ],
          ),
          const SizedBox(height: 10),
          TweenAnimationBuilder<double>(
            tween: Tween(end: target),
            duration: const Duration(milliseconds: 2800),
            builder: (context, value, _) => FittedBox(
              fit: BoxFit.scaleDown,
              child: Text(
                '+${formatLock(BigInt.from(value), locale)} LOCK',
                style: TextStyle(
                  fontSize: 30,
                  fontWeight: FontWeight.w800,
                  color: Brand.neonGreen,
                  shadows: [Shadow(color: Brand.neonGreen.withValues(alpha: 0.7), blurRadius: 16)],
                ),
              ),
            ),
          ),
          if (AppScope.of(context).rotation case final r?) ...[const SizedBox(height: 8), HoursLog(r)],
          const SizedBox(height: 6),
          Text(
            t.toCollect,
            textAlign: TextAlign.center,
            style: const TextStyle(color: Brand.pearl),
          ),
        ],
      ),
    );
  }
}
