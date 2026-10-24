import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'dart:io';

import 'package:flutter/foundation.dart';

import 'app_state.dart';
import 'core/alerts.dart';
import 'core/market.dart' show usdcToDouble;
import 'core/network.dart' show ChainError;
import 'core/polygon.dart' show BuyProblem, BuyProblemKind;
import 'core/waiting_orders.dart';
import 'l10n/gen/app_localizations.dart';
import 'screens/home.dart';
import 'screens/market.dart' show formatUsd;
import 'screens/security.dart' show chainErrorText;
import 'screens/password.dart';
import 'screens/restore.dart';
import 'screens/welcome.dart';
import 'theme.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  final state = AppState();
  await state.load();
  final hasWallet = await state.store.hasWallet();
  // Opened from an encryptlock: link (Android): remember the address.
  if (!kIsWeb && Platform.isAndroid) {
    const links = MethodChannel('encryptlock/link');
    state.openLink(await links.invokeMethod<String>('initial').catchError((_) => null));
    links.setMethodCallHandler((call) async {
      if (call.method == 'link') state.openLink(call.arguments as String?);
    });
  }
  if (AppState.demo) {
    // Test build: the test wallet is saved with a fixed, public password, so
    // locking (by hand or by itself) works exactly like the real app.
    // If another wallet was recovered in the test build, that one is used
    // (it asks for its own password), not the test wallet.
    final prefs = await SharedPreferences.getInstance();
    if (!hasWallet) {
      await state.store.save(AppState.demoWords, AppState.demoPassword);
      await prefs.setBool(AppState.demoActiveKey, true);
    }
    // (No mark yet = wallet saved by an older test build = the test wallet.)
    if (prefs.getBool(AppState.demoActiveKey) ?? true) await state.open(AppState.demoWords);
  }
  // Background alerts use Android's scheduler (phone only for now).
  if (!kIsWeb && Platform.isAndroid) {
    try {
      await Alerts.start();
    } catch (e) {
      debugPrint('alerts not started: $e');
    }
  }
  if (!kIsWeb && Platform.isWindows) Alerts.startDesktop();
  runApp(EncryptLockApp(state: state, hasWallet: hasWallet));
}

class EncryptLockApp extends StatefulWidget {
  const EncryptLockApp({super.key, required this.state, required this.hasWallet});

  final AppState state;
  final bool hasWallet;

  @override
  State<EncryptLockApp> createState() => _EncryptLockAppState();
}

class _EncryptLockAppState extends State<EncryptLockApp> {
  final _navigator = GlobalKey<NavigatorState>();
  final _messenger = GlobalKey<ScaffoldMessengerState>();
  late final StreamSubscription<WaitingOutcome> _waiting;
  late final AppLifecycleListener _lifecycle;
  late final Timer _idleCheck;
  DateTime _lastTouch = DateTime.now();
  bool _wasOpen = false;

  @override
  void initState() {
    super.initState();
    // Leaving the app locks the wallet: coming back asks for the password.
    _lifecycle = AppLifecycleListener(onPause: _lock);
    // Not touching the screen for the chosen minutes also locks it.
    _idleCheck = Timer.periodic(const Duration(seconds: 10), (_) => _checkIdle());
    widget.state.addListener(_onStateChanged);
    _waiting = widget.state.waitingOutcomes.listen(_onWaitingOutcome);
  }

  /// A Buy / Sell that waited for a LockKeeper was done (or could not be):
  /// said on whatever screen is open.
  void _onWaitingOutcome(WaitingOutcome o) {
    final context = _navigator.currentContext;
    if (context == null) return;
    final t = AppLocalizations.of(context);
    final locale = Localizations.localeOf(context).toString();
    final w = o.order;
    final what = (w.sell ? t.waitingSell : t.waitingBuy)(formatLock(w.lock, locale), formatUsd(usdcToDouble(w.usdc), locale));
    final p = o.problem;
    final reason = p is BuyProblem
        ? switch (p.kind) {
            BuyProblemKind.noUsdc => t.buyNoUsdc,
            BuyProblemKind.noPol || BuyProblemKind.noPolAmount => t.buyNoPol,
            BuyProblemKind.notReady => t.buyNotReady,
            BuyProblemKind.wrongNetwork => t.buyWrongNetwork,
          }
        : p is ChainError
        ? chainErrorText(t, p.log)
        : t.networkError;
    final text = switch (o.result) {
      WaitingResult.done => t.waitingDone(what),
      WaitingResult.offerGone => t.waitingGone(what),
      WaitingResult.failed => t.waitingFailed(what, reason),
    };
    _messenger.currentState?.showSnackBar(SnackBar(content: Text(text), duration: const Duration(seconds: 10)));
  }

  @override
  void dispose() {
    _waiting.cancel();
    widget.state.removeListener(_onStateChanged);
    _idleCheck.cancel();
    _lifecycle.dispose();
    super.dispose();
  }

  void _onStateChanged() {
    // The idle time counts from the moment the wallet opens.
    if (widget.state.isOpen && !_wasOpen) _lastTouch = DateTime.now();
    _wasOpen = widget.state.isOpen;
  }

  void _checkIdle() {
    final limit = Duration(minutes: widget.state.autoLockMinutes);
    if (DateTime.now().difference(_lastTouch) >= limit) _lock();
  }

  void _lock() {
    if (!widget.state.isOpen) return;
    widget.state.lock();
    _navigator.currentState?.pushAndRemoveUntil(MaterialPageRoute(builder: (_) => const UnlockScreen()), (_) => false);
  }

  @override
  Widget build(BuildContext context) {
    final state = widget.state;
    return AppScope(
      state: state,
      child: ListenableBuilder(
        listenable: state,
        builder: (context, _) => MaterialApp(
          title: 'EncryptLock',
          navigatorKey: _navigator,
          scaffoldMessengerKey: _messenger,
          debugShowCheckedModeBanner: false,
          theme: buildTheme(),
          locale: state.language?.locale,
          supportedLocales: AppLocalizations.supportedLocales,
          localizationsDelegates: AppLocalizations.localizationsDelegates,
          // Every time the app opens with a wallet: "Decrypt your wallet".
          home: state.isOpen
              ? const HomeScreen()
              : widget.hasWallet
                  ? const UnlockScreen()
                  : const WelcomeScreen(),
          routes: {'/restore': (_) => const RestoreScreen()},
          builder: (context, child) => Listener(
            behavior: HitTestBehavior.translucent,
            onPointerDown: (_) => _lastTouch = DateTime.now(),
            child: child,
          ),
        ),
      ),
    );
  }
}

