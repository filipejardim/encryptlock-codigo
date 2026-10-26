import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'core/alerts.dart';
import 'core/attempt_guard.dart';
import 'core/biometric.dart';
import 'core/core.dart';
import 'core/network.dart';
import 'core/network_pin.dart';
import 'core/pc_lockkeeper.dart';
import 'core/last_answers.dart';
import 'core/market.dart';
import 'core/polygon.dart';
import 'core/updater.dart';
import 'core/version.dart';
import 'core/waiting_orders.dart';
import 'core/wallet_store.dart';

/// The languages of the app, with their names written in each language.
class AppLanguage {
  const AppLanguage(this.tag, this.name, this.locale);

  final String tag;
  final String name;
  final Locale locale;

  static const all = [
    AppLanguage('en', 'English', Locale('en')),
    AppLanguage('pt', 'Português (Portugal)', Locale('pt')),
    AppLanguage('pt_BR', 'Português (Brasil)', Locale('pt', 'BR')),
    AppLanguage('es', 'Español', Locale('es')),
    AppLanguage('fr', 'Français', Locale('fr')),
    AppLanguage('de', 'Deutsch', Locale('de')),
    AppLanguage('it', 'Italiano', Locale('it')),
    AppLanguage('nl', 'Nederlands', Locale('nl')),
    AppLanguage('pl', 'Polski', Locale('pl')),
    AppLanguage('tr', 'Türkçe', Locale('tr')),
    AppLanguage('ru', 'Русский', Locale('ru')),
    AppLanguage('uk', 'Українська', Locale('uk')),
    AppLanguage('ar', 'العربية', Locale('ar')),
    AppLanguage('hi', 'हिन्दी', Locale('hi')),
    AppLanguage('zh', '简体中文', Locale('zh')),
    AppLanguage('zh_Hant', '繁體中文', Locale.fromSubtags(languageCode: 'zh', scriptCode: 'Hant')),
    AppLanguage('ja', '日本語', Locale('ja')),
    AppLanguage('ko', '한국어', Locale('ko')),
    AppLanguage('id', 'Bahasa Indonesia', Locale('id')),
    AppLanguage('vi', 'Tiếng Việt', Locale('vi')),
    AppLanguage('th', 'ไทย', Locale('th')),
  ];

  /// The language the app is showing for this locale (English if none).
  static AppLanguage forLocale(Locale l) =>
      byTag(l.scriptCode == 'Hant' ? 'zh_Hant' : null) ??
      byTag(l.countryCode == null ? null : '${l.languageCode}_${l.countryCode}') ??
      byTag(l.languageCode) ??
      all.first;

  static AppLanguage? byTag(String? tag) {
    for (final l in all) {
      if (l.tag == tag) return l;
    }
    return null;
  }
}

/// Currency shown next to the LOCK balance (no market price yet: "≈ — €").
class FiatCurrency {
  const FiatCurrency(this.code, this.symbol);

  final String code;
  final String symbol;

  static const all = [FiatCurrency('EUR', '€'), FiatCurrency('USD', r'$')];

  static FiatCurrency byCode(String? code) => all.firstWhere((c) => c.code == code, orElse: () => all.first);
}

/// What the whole app shares: chosen language, currency and the open wallet.
class AppState extends ChangeNotifier {
  AppState({WalletCore? core, WalletStore? store, AttemptGuard? guard, AttemptGuard? sendCodeGuard})
      : core = core ?? WalletCore.create(),
        guard = guard ?? AttemptGuard(),
        sendCodeGuard = sendCodeGuard ?? AttemptGuard(prefix: 'encryptlock.sendcode.attempts') {
    this.store = store ?? WalletStore(core: this.core);
  }

  final WalletCore core;
  late final WalletStore store;
  final AttemptGuard guard;

  /// Separate pauses for wrong send codes.
  final AttemptGuard sendCodeGuard;

  /// Fingerprint/face unlock is on (the words are sealed with that key).
  bool biometricOn = false;

  /// A send code is asked before every sending.
  bool sendCodeOn = false;

  /// Address from an encryptlock: link (QR read by the phone's camera),
  /// waiting to fill the Send screen once the wallet is open.
  String? pendingSendTo;

  void openLink(String? link) {
    final m = RegExp(r'lock1[0-9a-z]+').firstMatch((link ?? '').toLowerCase());
    if (m == null) return;
    pendingSendTo = m.group(0);
    notifyListeners();
  }

  /// Background security alerts (on unless turned off).
  bool alertsOn = true;

  Future<void> setAlerts(bool on) async {
    alertsOn = on;
    notifyListeners();
    final prefs = await SharedPreferences.getInstance();
    await prefs.setBool(Alerts.onKey, on);
    if (on && Platform.isAndroid) await Alerts.askPermission();
  }

  static const _sendCodeCheck = 'encryptlock-send-code';

  static const _languageKey = 'encryptlock.language';
  static const _currencyKey = 'encryptlock.currency';
  static const _autoLockKey = 'encryptlock.autolock';
  static const _lockKeeperKey = 'encryptlock.lockkeeper';

  /// The address found on the website (so an address typed in Settings is
  /// told apart and never replaced).
  static const _discoveredKey = 'encryptlock.lockkeeper.discovered';

  /// Last numbers the network gave, per wallet: shown while it cannot be
  /// reached (founder's computer off), with the time of that update.
  static const _lastKey = 'encryptlock.last';

  /// The LockKeeper the app talks to. 10.0.2.2 is the PC seen from the
  /// Android emulator; a real phone uses the PC's address on the Wi-Fi.
  /// Test builds can point to the founder's PC: --dart-define=LOCKKEEPER=…
  static const _builtInLockKeeper = String.fromEnvironment('LOCKKEEPER');

  /// Emulator: 10.0.2.2 is the PC. Computer app: the LockKeeper on this PC.
  static String get defaultLockKeeper => _builtInLockKeeper.isNotEmpty
      ? _builtInLockKeeper
      : (!kIsWeb && Platform.isAndroid ? 'http://10.0.2.2:1317' : 'http://127.0.0.1:1317');

  /// Test build for looking around (--dart-define=DEMO=true): opens straight
  /// into a test wallet, no password, no 24 words to write down. Only for the
  /// test network; never for real money.
  static const demo = bool.fromEnvironment('DEMO');

  /// Password of the test wallet (test builds only; shown on the unlock screen).
  static const demoPassword = 'encryptlock';

  /// Test build: true while the saved wallet is the test wallet.
  static const demoActiveKey = 'encryptlock.demo.active';

  /// Test build and the saved wallet is the test wallet (shows its password hint).
  bool demoWalletActive = false;

  /// Public test words (the standard BIP-39 example). Test network only.
  static const demoWords = 'legal winner thank year wave sausage worth useful legal winner thank year '
      'wave sausage worth useful legal winner thank year wave sausage worth title';
  String lockKeeperUrl = AppState.defaultLockKeeper;

  /// Latest information from the network; null until the first answer.
  AccountStatus? status;

  /// True when the last attempt to reach the LockKeeper failed.
  bool networkError = false;

  /// With [networkError]: the device itself has no internet (otherwise the
  /// internet works and no LockKeeper of the blockchain answers).
  bool noInternet = false;

  /// When [status] last came from the network (null: never).
  DateTime? lastUpdate;
  DateTime? _discoverAt;
  String? _validatorAddress;
  String? _chainId;
  bool _refreshing = false;
  DateTime? _lastCollect;
  bool _collecting = false;

  Future<void> _collectEarnings() async {
    _collecting = true;
    _lastCollect = DateTime.now();
    try {
      await _submit((w, c, a, s) => core.sign('signCollectRewards', w, c, a, s));
    } catch (e) {
      debugPrint('collect failed: $e');
      _lastCollect = null; // try again at the next refresh
    } finally {
      _collecting = false;
    }
  }
  bool _registering = false;

  /// True while this wallet registers itself on the network (free, once).
  bool get registering => _registering;

  /// Minutes without touching the screen before the wallet locks itself.
  static const autoLockChoices = [1, 2, 5, 15];
  int autoLockMinutes = 2;

  FiatCurrency currency = FiatCurrency.all.first;

  /// Null = follow the phone's language.
  AppLanguage? language;

  /// The 24 words while the wallet is open (decrypted); null when locked.
  String? _words;
  String? address;

  /// The same wallet on Polygon (USDC and POL), from the same 24 words.
  String? polygonAddress;
  PolygonBalances? polygon;
  DateTime? _polygonAt;

  /// The market (LOCK ↔ USDC): offers and price, refreshed every 10 seconds.
  List<MarketOffer> offers = const [];
  MarketPrice? price;
  DateTime? _marketAt;

  /// Dollars per euro (European Central Bank, once a day).
  double? usdPerEur;
  DateTime? _eurAt;

  /// Dollars per POL (public exchange prices, every 10 minutes).
  double? usdPerPolPrice;
  DateTime? _polPriceAt;

  /// LOCK value in the chosen currency, from the market price (null = no
  /// price yet). The reference price (24-hour median weighed by volume):
  /// trades between two accounts of the same person at a made-up price
  /// cannot then make every balance look worth anything they like.
  double? fiatValue(BigInt lockUnits) {
    final p = price?.reference;
    if (p == null) return null;
    final usd = lockToDouble(lockUnits) * p;
    if (currency.code == 'USD') return usd;
    return usdPerEur == null ? null : usd / usdPerEur!;
  }

  /// Dollars shown in the chosen currency.
  double? fiat(double usd) => currency.code == 'USD' ? usd : (usdPerEur == null ? null : usd / usdPerEur!);

  bool get isOpen => _words != null;

  Future<void> load() async {
    final prefs = await SharedPreferences.getInstance();
    language = AppLanguage.byTag(prefs.getString(_languageKey));
    currency = FiatCurrency.byCode(prefs.getString(_currencyKey));
    final minutes = prefs.getInt(_autoLockKey);
    if (autoLockChoices.contains(minutes)) autoLockMinutes = minutes!;
    lockKeeperUrl = prefs.getString(_lockKeeperKey) ?? defaultLockKeeper;
    // The final app (fixed to the real network) always uses the real
    // Polygon, whatever a test version saved before; only test versions
    // can choose.
    PolygonNetwork.useReal(NetworkPin.chainId.isNotEmpty || (prefs.getBool(_realPolygonKey) ?? false));
    demoWalletActive = demo && (prefs.getBool(demoActiveKey) ?? true);
    guarded = prefs.getStringList(_guardedKey) ?? [];
    alertsOn = prefs.getBool(Alerts.onKey) ?? true;
    biometricOn = await store.biometricRecord() != null;
    sendCodeOn = await store.sendCodeRecord() != null;
    unawaited(_resumePcLockKeeper());
    unawaited(checkVersion());
    unawaited(_loadLastMarket());
  }

  /// The last price and offers the blockchain gave: on screen from the
  /// start, and while no LockKeeper answers.
  Future<void> _loadLastMarket() async {
    final p = await lastPrice();
    final o = await lastOffers();
    final k = await LockKeeperClient.lastKeepersOnline();
    if (price == null && p != null) price = p;
    if (offers.isEmpty && o != null) offers = o;
    keepersOnline ??= k;
    usdPerEur ??= (await SharedPreferences.getInstance()).getDouble(_eurKey);
    usdPerPolPrice ??= (await SharedPreferences.getInstance()).getDouble(_polKey);
    notifyListeners();
  }

  /// Last dollars per euro, kept: the balance keeps its value in euros.
  static const _eurKey = 'encryptlock.usd.per.eur';
  static const _polKey = 'encryptlock.usd.per.pol';

  /// LockKeepers of the blockchain switched on now (null: not known yet).
  int? keepersOnline;

  /// Turns fingerprint/face unlock on (asks for finger/face once).
  /// Throws BiometricException.
  Future<void> enableBiometric({required String title, required String cancel}) async {
    final w = _words;
    if (w == null) throw StateError('locked');
    await store.saveBiometricRecord(await Biometric.seal(w, title: title, cancel: cancel));
    biometricOn = true;
    notifyListeners();
  }

  Future<void> disableBiometric() async {
    await store.saveBiometricRecord(null);
    await Biometric.delete();
    biometricOn = false;
    notifyListeners();
  }

  /// Opens the wallet with finger/face. Throws BiometricException; when the
  /// phone's fingerprints changed, biometrics are turned off.
  Future<void> unlockWithBiometric({required String title, required String cancel}) async {
    final record = await store.biometricRecord();
    if (record == null) throw const BiometricException(BiometricFailure.unavailable);
    try {
      final w = await Biometric.open(record, title: title, cancel: cancel);
      await guard.reset();
      await open(w);
    } on BiometricException catch (e) {
      if (e.failure == BiometricFailure.invalidated) await disableBiometric();
      rethrow;
    }
  }

  Future<void> setSendCode(String code) async {
    await store.saveSendCodeRecord(await core.sealSecret(_sendCodeCheck, code));
    await sendCodeGuard.reset();
    sendCodeOn = true;
    notifyListeners();
  }

  Future<void> removeSendCode() async {
    await store.saveSendCodeRecord(null);
    sendCodeOn = false;
    notifyListeners();
  }

  /// Checks the send code; wrong codes count towards their own pauses.
  Future<bool> checkSendCode(String code) async {
    final record = await store.sendCodeRecord();
    if (record == null) return true;
    if (await core.openSecret(record, code) == _sendCodeCheck) {
      await sendCodeGuard.reset();
      return true;
    }
    await sendCodeGuard.recordFailure();
    return false;
  }

  Future<void> setLockKeeperUrl(String url) async {
    lockKeeperUrl = url.trim();
    status = null;
    price = null;
    offers = const [];
    await LastAnswers.clear();
    networkError = false;
    notifyListeners();
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_lockKeeperKey, lockKeeperUrl);
    await refresh();
  }

  Future<void> _saveLast(String addr, AccountStatus s) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString('$_lastKey.$addr', jsonEncode({
      'height': s.height,
      'balance': '${s.balance}',
      'toCollect': '${s.toCollect}',
      'locked': '${s.locked}',
      'isLockKeeper': s.isLockKeeper,
      'at': DateTime.now().millisecondsSinceEpoch,
    }));
  }

  Future<void> _loadLast(String addr) async {
    final raw = (await SharedPreferences.getInstance()).getString('$_lastKey.$addr');
    if (raw == null || address != addr) return;
    final j = jsonDecode(raw) as Map<String, dynamic>;
    status = AccountStatus(
      height: j['height'] as int,
      balance: BigInt.parse(j['balance'] as String),
      toCollect: BigInt.parse(j['toCollect'] as String),
      locked: BigInt.parse(j['locked'] as String),
      isLockKeeper: j['isLockKeeper'] as bool,
    );
    lastUpdate = DateTime.fromMillisecondsSinceEpoch(j['at'] as int);
  }

  /// Away from home the built-in address does not answer: the website says
  /// where the network can be reached now. Back home the built-in address
  /// answers again and is used. An address typed in Settings is never
  /// replaced.
  Future<void> _discover() async {
    if (_discoverAt != null && DateTime.now().difference(_discoverAt!) < const Duration(minutes: 1)) return;
    _discoverAt = DateTime.now();
    final prefs = await SharedPreferences.getInstance();
    final saved = prefs.getString(_lockKeeperKey);
    if (saved != null && saved != prefs.getString(_discoveredKey)) return;
    String? fromSite;
    try {
      fromSite = await networkAddressFromSite();
    } catch (e) {
      debugPrint('website address failed: $e');
    }
    for (final url in [fromSite, defaultLockKeeper]) {
      if (url == null || url == lockKeeperUrl) continue;
      try {
        await LockKeeperClient(url).chainId();
      } catch (_) {
        continue;
      }
      lockKeeperUrl = url;
      if (url == defaultLockKeeper) {
        await prefs.remove(_lockKeeperKey);
        await prefs.remove(_discoveredKey);
      } else {
        await prefs.setString(_lockKeeperKey, url);
        await prefs.setString(_discoveredKey, url);
      }
      unawaited(refresh());
      return;
    }
  }

  /// Asks the LockKeeper for the latest balance and rewards.
  Future<void> refresh() async {
    final addr = address;
    if (addr == null || _refreshing || core.isPreview) return;
    _refreshing = true;
    // Not from the LockKeepers: the euro rate and the Polygon balances (USDC
    // and POL can come in from outside even with no LockKeeper on).
    // Every 6 hours; without an answer (no internet yet), again a minute later.
    if (_eurAt == null || DateTime.now().difference(_eurAt!) >= Duration(hours: usdPerEur == null ? 0 : 6, minutes: 1)) {
      _eurAt = DateTime.now();
      unawaited(usdPerEuro().then((r) async {
        if (r != null) {
          usdPerEur = r;
          notifyListeners();
          await (await SharedPreferences.getInstance()).setDouble(_eurKey, r);
        }
      }));
    }
    // POL price: every 10 minutes; without an answer, again a minute later.
    if (_polPriceAt == null ||
        DateTime.now().difference(_polPriceAt!) >= Duration(minutes: usdPerPolPrice == null ? 1 : 10)) {
      _polPriceAt = DateTime.now();
      unawaited(usdPerPol().then((p) async {
        if (p != null) {
          usdPerPolPrice = p;
          notifyListeners();
          await (await SharedPreferences.getInstance()).setDouble(_polKey, p);
        }
      }));
    }
    // Polygon balances change rarely: every 30 seconds is enough.
    if (_polygonAt == null || DateTime.now().difference(_polygonAt!) >= const Duration(seconds: 30)) {
      _polygonAt = DateTime.now();
      unawaited(_refreshPolygon());
    }
    try {
      _validatorAddress ??= await core.validatorAddress(addr);
      final s = await LockKeeperClient(lockKeeperUrl).status(addr, _validatorAddress!);
      final waiting = <String, List<PendingTransfer>>{};
      final waitingOffers = <String, List<MarketOffer>>{};
      final allOffers = guarded.isEmpty ? const <MarketOffer>[] : await LockKeeperClient(lockKeeperUrl).offers();
      for (final account in guarded) {
        final p = await LockKeeperClient(lockKeeperUrl).pending(account);
        waiting[account] = [for (final t in p) if (t.awaitingApproval) t];
        waitingOffers[account] = [for (final o in allOffers) if (o.maker == account && o.awaitingApproval) o];
      }
      if (address != addr) return; // locked meanwhile
      approvals = waiting;
      offerApprovals = waitingOffers;
      status = s;
      networkError = false;
      noInternet = false;
      lastUpdate = DateTime.now();
      // Buy / Sell asked while no LockKeeper answered: done now.
      if (waitingOrders.isNotEmpty) unawaited(_runWaiting());
      unawaited(_saveLast(addr, s));
      // LockKeeper earnings go into the balance on opening and every hour.
      if (s.isLockKeeper && s.toCollect > BigInt.zero && !_collecting &&
          (_lastCollect == null || DateTime.now().difference(_lastCollect!) >= const Duration(hours: 1))) {
        unawaited(_collectEarnings());
      }
      if (!s.registered) unawaited(_register());
      if (PcLockKeeper.isSetUp || s.isLockKeeper) unawaited(_refreshRotation());
      if (_marketAt == null || DateTime.now().difference(_marketAt!) >= const Duration(seconds: 10)) {
        _marketAt = DateTime.now();
        unawaited(refreshMarket());
      }
    } catch (e) {
      debugPrint('refresh failed: $e');
      networkError = true;
      noInternet = !await hasInternet();
      if (status == null) await _loadLast(addr);
      unawaited(_discover());
    } finally {
      _refreshing = false;
    }
    notifyListeners();
  }

  // --- "Make this PC a LockKeeper" (computer app) ---

  static const _pcLockKeeperOnKey = 'encryptlock.pc.lockkeeper.on';

  /// The LockKeeper on this PC should be running (the person switched it on).
  bool pcLockKeeperOn = false;

  /// Preparing / switching on right now.
  bool pcLockKeeperBusy = false;

  /// The last result of the LockKeeper button or of the chip check, shown in
  /// the LockKeeper card until the owner closes it (a message at the bottom
  /// went away before people could read it: founder, 26 Oct 2026).
  String? pcNote;
  bool pcNoteBad = false;

  void setPcNote(String? text, {bool bad = false}) {
    pcNote = text;
    pcNoteBad = bad;
    notifyListeners();
  }

  /// What the network says about this wallet as a LockKeeper.
  LockKeeperRotation? rotation;

  /// How many blocks this PC's LockKeeper is behind the network (null while
  /// unknown or off).
  int? pcBehind;

  /// When this PC's LockKeeper was switched on in this session.
  DateTime? _pcOnSince;

  /// Switched on for a while and the network still has no "I am on" from it.
  bool get pcNotSeen =>
      pcLockKeeperOn &&
      (status?.isLockKeeper ?? false) &&
      rotation != null &&
      !rotation!.online &&
      _pcOnSince != null &&
      DateTime.now().difference(_pcOnSince!) > const Duration(minutes: 15);

  /// A newer version of the app, published on the website (null if none).
  String? newVersion;
  UpdateInfo? _update;

  /// "Update" in progress: 0..1 while downloading (null otherwise), and
  /// whether the last try failed.
  double? updateProgress;
  bool updateFailed = false;

  /// Reads the website's versao.json once (nothing happens offline).
  Future<void> checkVersion() async {
    final u = await latestUpdate();
    if (u != null && isNewer(u.version, appVersion)) {
      _update = u;
      newVersion = u.version;
      notifyListeners();
    }
  }

  /// The "Update" button. Installed from the website: downloads the new
  /// version, checks it and installs it (the computer's LockKeeper stops
  /// first and comes back by itself with the new app). From a store, or
  /// another copy: opens the store or the website (returns that address).
  Future<Uri?> runUpdate() async {
    final u = _update;
    if (u == null || updateProgress != null) return null;
    if (!await Updater.canUpdateItself()) {
      return Platform.isAndroid && !kIsWeb
          ? Uri.parse('https://play.google.com/store/apps/details?id=com.encryptlock.encryptlock_wallet')
          : Uri.parse('https://encryptlock.pages.dev/#download');
    }
    updateFailed = false;
    updateProgress = 0;
    notifyListeners();
    try {
      final file = await Updater.download(u, (p) {
        updateProgress = p;
        notifyListeners();
      });
      if (Platform.isWindows && PcLockKeeper.isSupported) await PcLockKeeper.stop();
      await Updater.install(file);
    } catch (e) {
      debugPrint('update: $e');
      updateFailed = true;
    } finally {
      updateProgress = null;
      notifyListeners();
    }
    return null;
  }

  /// LOCK the guarantee needs: 1 dollar at the last trade (1 LOCK before
  /// the first trade).
  BigInt get pcMinGuarantee => PcLockKeeper.minGuarantee(price);

  /// On opening the computer app: the LockKeeper switched on before starts
  /// again by itself (for instance after the computer restarted).
  Future<void> _resumePcLockKeeper() async {
    if (!PcLockKeeper.isSupported) return;
    await PcLockKeeper.loadAutostart();
    final prefs = await SharedPreferences.getInstance();
    final saved = prefs.getBool(_pcLockKeeperOnKey) ?? false;
    // Already running (started when Windows started, or by a script): the
    // switch shows it on.
    pcLockKeeperOn = PcLockKeeper.isSetUp && (saved || await PcLockKeeper.isRunning());
    if (pcLockKeeperOn && !saved) await prefs.setBool(_pcLockKeeperOnKey, true);
    if (pcLockKeeperOn) {
      _pcOnSince = DateTime.now();
      await PcLockKeeper.start();
    }
    notifyListeners();
  }

  Future<void> _refreshRotation() async {
    final addr = address;
    if (addr == null) return;
    try {
      rotation = await LockKeeperClient(lockKeeperUrl).rotation(addr);
      // This PC's LockKeeper compared with the network.
      final network = status?.height;
      final local = pcLockKeeperOn ? await PcLockKeeper.localHeight() : null;
      pcBehind = network != null && local != null ? network - local : null;
      notifyListeners();
    } catch (e) {
      debugPrint('rotation refresh failed: $e');
    }
  }

  /// Has the network's chip checker confirm this computer's security chip for
  /// its helper account (Windows asks "Allow?" once). Returns the maker.
  Future<String> checkPcChip() async {
    await Directory(PcLockKeeper.home).create(recursive: true);
    final helper = await core.address(await PcLockKeeper.helperWords(core.newMnemonic));
    return PcLockKeeper.checkChip(checker: lockKeeperUrl, account: helper);
  }

  /// Makes this PC a LockKeeper: prepares it, puts the guarantee (signed by
  /// the wallet) and switches it on. Throws on failure.
  Future<void> becomePcLockKeeper() async {
    final addr = address;
    if (_words == null || addr == null) throw StateError('locked');
    pcLockKeeperBusy = true;
    notifyListeners();
    try {
      final client = LockKeeperClient(lockKeeperUrl);
      _chainId ??= await client.chainId();
      final consKey = await PcLockKeeper.prepare(lockKeeperUrl, _chainId!);
      final helperWords = await PcLockKeeper.helperWords(core.newMnemonic);
      final helper = await core.address(helperWords);
      final units = pcMinGuarantee;
      if (!PcLockKeeper.isSetUp) {
        await _becomeOrMove(consKey: consKey, helper: helper, units: units);
        PcLockKeeper.markSetUp();
      }
      await setPcLockKeeperOn(true);
      // On by default; the owner can switch it off on the card.
      await setPcAutostart(true);
    } finally {
      pcLockKeeperBusy = false;
      notifyListeners();
    }
  }

  /// Computer, linked from a phone by QR code: prepares this computer's
  /// LockKeeper and its helper, switches them on (the helper publishes the
  /// block-signing key) and returns the helper address for the QR code. No
  /// wallet is needed on the computer.
  Future<String> preparePcForPhone() async {
    final client = LockKeeperClient(lockKeeperUrl);
    _chainId ??= await client.chainId();
    await PcLockKeeper.prepare(lockKeeperUrl, _chainId!);
    final helper = await core.address(await PcLockKeeper.helperWords(core.newMnemonic));
    await setPcLockKeeperOn(true);
    await setPcAutostart(true);
    return helper;
  }

  /// The LockKeeper account a computer's helper works for (null until a
  /// phone linked it).
  Future<String?> linkedKeeper(String helper) async => (await LockKeeperClient(lockKeeperUrl).helper(helper)).keeper;

  /// Phone: makes the computer whose QR code was scanned a blockchain
  /// LockKeeper of this wallet (the guarantee comes from this wallet; the
  /// earnings come here). Throws ChainError('no key') when the computer has
  /// not published its key yet.
  Future<void> linkComputer(String helper) async {
    final info = await LockKeeperClient(lockKeeperUrl).helper(helper);
    if (info.pubKey == null) throw const ChainError('no key');
    await _becomeOrMove(consKey: info.pubKey!, helper: helper, units: pcMinGuarantee);
  }

  /// Makes this wallet a LockKeeper on the computer with block-signing key
  /// [consKey] and helper [helper]; a wallet that already is one (its
  /// guarantee already in, e.g. from a computer whose chip did not work) is
  /// moved to this computer instead, keeping its guarantee (founder, 26 Oct
  /// 2026: the wallet can always go to a computer that works).
  Future<void> _becomeOrMove({required String consKey, required String helper, required BigInt units}) async {
    final addr = address;
    if (addr == null) throw StateError('locked');
    _validatorAddress ??= await core.validatorAddress(addr);
    final current = await LockKeeperClient(lockKeeperUrl).keeperKey(_validatorAddress!);
    if (current == null) {
      await _submit((w, c, a, s) => core.sign('signBecomeLockKeeper', w, c, a, s, {
        'consPubKey': consKey,
        'units': units.toInt(),
        'moniker': 'pc',
        'helper': helper,
      }));
    } else {
      await _submit((w, c, a, s) => core.sign('signMoveLockKeeper', w, c, a, s, {
        'consPubKey': current == consKey ? '' : consKey,
        'helper': helper,
      }));
    }
  }

  /// Switches this PC's LockKeeper on or off (off costs nothing).
  Future<void> setPcLockKeeperOn(bool on) async {
    if (on) {
      await PcLockKeeper.start();
    } else {
      await PcLockKeeper.stop();
      // Switched off: it must not come back by itself when Windows starts.
      await PcLockKeeper.setAutostart(false);
    }
    pcLockKeeperOn = on;
    _pcOnSince = on ? DateTime.now() : null;
    pcBehind = null;
    final prefs = await SharedPreferences.getInstance();
    await prefs.setBool(_pcLockKeeperOnKey, on);
    notifyListeners();
    unawaited(_refreshRotation());
  }

  /// This PC's LockKeeper switches on by itself when Windows starts.
  bool get pcAutostart => PcLockKeeper.autostart;

  Future<void> setPcAutostart(bool on) async {
    await PcLockKeeper.setAutostart(on);
    notifyListeners();
  }

  Future<void> refreshMarket() async {
    try {
      final c = LockKeeperClient(lockKeeperUrl);
      final results = await Future.wait([c.offers(), c.price()]);
      offers = results[0] as List<MarketOffer>;
      price = results[1] as MarketPrice;
      keepersOnline = await c.keepersOnline().catchError((_) => keepersOnline ?? 0);
      notifyListeners();
      unawaited(refundStuckPayments());
    } catch (e) {
      debugPrint('market refresh failed: $e');
      if (price == null || offers.isEmpty) await _loadLastMarket();
    }
  }

  static const _secretsKey = 'encryptlock.swap.secrets';

  /// Publishes an offer. Sell: the LOCK (and the 0,25% fee) stay in the
  /// market until it is taken or canceled. Buy: the app keeps the trade's
  /// secret to pay later, and publishes only when this wallet already has
  /// the USDC and the POL to pay (throws BuyProblem otherwise).
  Future<void> placeOffer({required bool sell, required BigInt lock, required BigInt usdc, required int expiresAt}) async {
    await _ensureReachable();
    var hash = '';
    if (!sell) {
      await _checkCanPay(usdc);
      final secret = await core.newSwapSecret();
      hash = await core.swapHash(secret);
      await _saveSecret(hash, secret);
    }
    await _sign('signPlaceOffer', {
      'side': sell ? 1 : 2,
      'lock': lock.toInt(),
      'usdc': usdc.toInt(),
      'polygon': polygonAddress!,
      'hash': hash,
      'expiresAt': expiresAt,
    });
    await refreshMarket();
  }

  /// Gas the three Polygon transactions of a purchase may use (approve,
  /// lock, claim; unused gas is not paid).
  static final _buyGas = BigInt.from(80000 + 160000 + 140000);

  /// "Buy": takes a sell offer and pays it on Polygon from this wallet:
  ///  1. a new secret, kept on the device before anything else;
  ///  2. the offer is reserved on EncryptLock with the secret's hash;
  ///  3. on Polygon: approve, lock the USDC in the vault for the seller,
  ///     and claim with the secret (the seller is paid);
  ///  4. the LockKeepers see the payment and hand over the LOCK.
  /// Throws BuyProblem when the wallet lacks USDC or POL, or the vault is
  /// not published on this Polygon network.
  Future<void> buy(MarketOffer offer) async {
    final me = polygonAddress;
    final addr = address;
    if (_words == null || me == null || addr == null) throw StateError('locked');
    await _ensureReachable();
    final fees = await _checkCanPay(offer.usdc);

    final secret = await core.newSwapSecret();
    final hash = await core.swapHash(secret);
    await _saveSecret(hash, secret);

    await _sign('signTakeOffer', {'id': offer.id, 'polygon': me, 'hash': hash});
    final trades = await LockKeeperClient(lockKeeperUrl).trades(addr);
    final trade = trades.where((t) => t.offerId == offer.id && t.hash == hash).firstOrNull;
    if (trade == null) throw const ChainError('trade not found');
    // Pay only what the person saw and accepted, to that seller: a
    // LockKeeper giving other terms (wrong or not honest) is never paid.
    if (!sameTerms(offer, trade)) throw const ChainError('the trade does not match the offer');
    await _payOnPolygon(trade, secret, fees);
  }

  /// "Sell" to a buy offer: this wallet's LOCK (and the 0,25% fee) wait in
  /// the market; the buyer has 12 hours to pay on Polygon. Without payment
  /// everything comes back.
  Future<void> sellTo(MarketOffer offer) async {
    await _ensureReachable();
    await _sign('signTakeOffer', {'id': offer.id, 'polygon': polygonAddress!, 'hash': ''});
    await refreshMarket();
  }

  /// My buy offers someone sold to: they wait for my payment on Polygon
  /// (only those whose secret this device keeps).
  Future<List<MarketTrade>> tradesToPay() async {
    final addr = address;
    if (addr == null) return const [];
    final prefs = await SharedPreferences.getInstance();
    final saved = Map<String, String>.from(jsonDecode(prefs.getString(_secretsKey) ?? '{}') as Map);
    final now = DateTime.now().millisecondsSinceEpoch ~/ 1000;
    final payments = await _payments();
    return [
      for (final t in await LockKeeperClient(lockKeeperUrl).trades(addr))
        if (t.buyer == addr && saved.containsKey(t.hash) && t.payBy > now + 120 && payments[t.hash]?['stage'] == null || payments[t.hash]?['stage'] == 'paying') t,
    ];
  }

  /// Pays one of [tradesToPay] on Polygon.
  Future<void> payTrade(MarketTrade trade) async {
    final prefs = await SharedPreferences.getInstance();
    final secret = (jsonDecode(prefs.getString(_secretsKey) ?? '{}') as Map)[trade.hash] as String?;
    if (secret == null) throw const ChainError('secret not found');
    await _payOnPolygon(trade, secret, await _checkCanPay(trade.usdc));
  }

  Future<void> _saveSecret(String hash, String secret) async {
    final prefs = await SharedPreferences.getInstance();
    final saved = Map<String, String>.from(jsonDecode(prefs.getString(_secretsKey) ?? '{}') as Map);
    saved[hash] = secret;
    await prefs.setString(_secretsKey, jsonEncode(saved));
  }

  /// Checks where the network watches payments (the app pays exactly there,
  /// and only on the Polygon network chosen in Settings), and that this
  /// wallet has the USDC and the POL for the fee.
  Future<({BigInt tip, BigInt feeCap, String vault, String usdc})> _checkCanPay(BigInt usdc) async {
    final me = polygonAddress!;
    final c = await LockKeeperClient(lockKeeperUrl).polygonConfig();
    if (c == null) throw const BuyProblem(BuyProblemKind.notReady);
    final network = PolygonNetwork.current;
    if (network.chainId != c.chainId || network.usdc.toLowerCase() != c.usdc.toLowerCase()) {
      throw const BuyProblem(BuyProblemKind.wrongNetwork);
    }
    final pc = PolygonClient();
    final balances = await pc.balances(me);
    if (balances.usdc < usdc) throw const BuyProblem(BuyProblemKind.noUsdc);
    final fees = await pc.fees();
    if (balances.pol < fees.feeCap * _buyGas) throw const BuyProblem(BuyProblemKind.noPol);
    return (tip: fees.tip, feeCap: fees.feeCap, vault: c.vault, usdc: c.usdc);
  }

  static const _paymentsKey = 'encryptlock.polygon.payments';

  /// Payments started on Polygon, by trade hash (kept until finished or the
  /// USDC are back): {stage: paying|handed|done, chainId, vault, usdc, seller,
  /// amount, timelock}. "handed": the secret was shown, the seller is paid;
  /// that payment is never sent again.
  Future<Map<String, dynamic>> _payments() async {
    final prefs = await SharedPreferences.getInstance();
    return Map<String, dynamic>.from(jsonDecode(prefs.getString(_paymentsKey) ?? '{}') as Map);
  }

  Future<void> _setPayment(String hash, Map<String, dynamic>? p) async {
    final all = await _payments();
    if (p == null) {
      all.remove(hash);
    } else {
      all[hash] = p;
    }
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_paymentsKey, jsonEncode(all));
  }

  /// The three Polygon transactions that pay a trade (approve, lock for the
  /// seller, claim with the secret); the LockKeepers then hand over the LOCK.
  ///
  /// Safe to run again after the app stopped half way: a lock already there
  /// makes the second lock fail harmlessly, and once the secret was shown it
  /// is never paid again. The secret is shown only while the trade is still
  /// reserved; otherwise the USDC come back by themselves after the deadline
  /// ([refundStuckPayments]).
  Future<void> _payOnPolygon(MarketTrade trade, String secret,
      ({BigInt tip, BigInt feeCap, String vault, String usdc}) pay) async {
    final words = _words;
    final me = polygonAddress;
    final addr = address;
    if (words == null || me == null || addr == null) throw StateError('locked');
    final record = (await _payments())[trade.hash] as Map<String, dynamic>?;
    if (record?['stage'] == 'handed' || record?['stage'] == 'done') return; // already paid: waiting for the LockKeepers
    final network = PolygonNetwork.current;
    await _setPayment(trade.hash, {
      'stage': 'paying',
      'chainId': network.chainId,
      'vault': pay.vault,
      'usdc': pay.usdc,
      'seller': trade.sellerPolygon,
      'amount': trade.usdc.toInt(),
      'timelock': trade.payBy,
    });
    final pc = PolygonClient();
    final first = await pc.nonce(me);
    final txs = jsonDecode(await core.sign('signPolygonPayment', words, '', '', 0, {
      'polygonChainId': network.chainId,
      'nonce': first,
      'tipWei': pay.tip.toString(),
      'feeCapWei': pay.feeCap.toString(),
      'usdc': pay.usdc,
      'vault': pay.vault,
      'seller': trade.sellerPolygon,
      'secret': secret,
      'amount': trade.usdc.toInt(),
      'timelock': trade.payBy,
    })) as List<dynamic>;
    // Approve and lock (a lock already made earlier fails harmlessly).
    for (final (i, tx) in txs.take(2).indexed) {
      final hash = await pc.sendInOrder('$tx', me, first + i);
      await (hash == null ? pc.included(me, first + i) : pc.confirmed(hash));
    }
    // Show the secret only while the trade is still reserved with time to
    // spare: otherwise the seller would be paid and the LOCK might not come.
    final now = DateTime.now().millisecondsSinceEpoch ~/ 1000;
    final open = (await LockKeeperClient(lockKeeperUrl).trades(addr)).any((t) => t.id == trade.id);
    if (!open || now > trade.payBy + 600) {
      throw const ChainError('the reservation ended before paying: the USDC come back by themselves after the deadline');
    }
    // Sent: from here on the secret may be out, so it is never paid again
    // (if the claim never lands, the USDC come back after the deadline).
    await _setPayment(trade.hash, {...?(await _payments())[trade.hash] as Map<String, dynamic>?, 'stage': 'handed'});
    final claimHash = await pc.sendInOrder('${txs[2]}', me, first + 2);
    if (claimHash == null) {
      // Already sent on an earlier try: stays "handed" (never paid again;
      // if it did not land, the USDC come back after the deadline).
      await pc.included(me, first + 2);
      unawaited(_refreshPolygon());
      await refreshMarket();
      return;
    }
    if (!await pc.confirmed(claimHash)) {
      // The claim failed (nothing was locked): nothing was paid.
      await _setPayment(trade.hash, {...?(await _payments())[trade.hash] as Map<String, dynamic>?, 'stage': 'paying'});
      throw const ChainError('polygon payment failed');
    }
    // Handed over and confirmed: never paid again, nothing to take back.
    await _setPayment(trade.hash, {...?(await _payments())[trade.hash] as Map<String, dynamic>?, 'stage': 'done'});
    unawaited(_refreshPolygon());
    await refreshMarket();
  }

  /// Takes back USDC locked for a trade but never handed over, once the
  /// vault's deadline has passed. Runs by itself with the market refresh.
  Future<void> refundStuckPayments() async {
    final words = _words;
    final me = polygonAddress;
    if (words == null || me == null) return;
    final now = DateTime.now().millisecondsSinceEpoch ~/ 1000;
    for (final e in (await _payments()).entries) {
      final p = e.value as Map<String, dynamic>;
      final timelock = p['timelock'] as int;
      // After the deadline a refund is always safe: it takes the USDC back
      // only if they were never handed to the seller (otherwise it fails
      // harmlessly).
      if (p['stage'] == 'done') {
        // Paid and confirmed: forgotten a day after the deadline.
        if (now > timelock + 86400) await _setPayment(e.key, null);
        continue;
      }
      if (now <= timelock + 120 || p['chainId'] != PolygonNetwork.current.chainId) continue;
      try {
        final pc = PolygonClient();
        final fees = await pc.fees();
        final tx = await core.sign('signPolygonRefund', words, '', '', 0, {
          'polygonChainId': p['chainId'] as int,
          'nonce': await pc.nonce(me),
          'tipWei': fees.tip.toString(),
          'feeCapWei': fees.feeCap.toString(),
          'usdc': p['usdc'] as String,
          'vault': p['vault'] as String,
          'seller': p['seller'] as String,
          'hash': e.key,
          'amount': p['amount'] as int,
          'timelock': timelock,
        });
        // Mined either way: back, or nothing was locked.
        await pc.confirmed(await pc.send(tx));
        await _setPayment(e.key, null);
        unawaited(_refreshPolygon());
      } catch (err) {
        debugPrint('refund later: $err'); // no POL or no network: next time
      }
    }
  }

  static const _realPolygonKey = 'encryptlock.polygon.real';

  /// USDC and POL on the real Polygon network (real money) or on the test
  /// network; Settings → Advanced.
  Future<void> setRealPolygon(bool real) async {
    PolygonNetwork.useReal(real);
    polygon = null;
    notifyListeners();
    final prefs = await SharedPreferences.getInstance();
    await prefs.setBool(_realPolygonKey, real);
    await _refreshPolygon();
  }

  static final _polGas = BigInt.from(21000);
  static final _usdcGas = BigInt.from(100000);

  /// The most the Polygon network can charge (in POL wei) to send POL or
  /// USDC now; usually much less is paid.
  Future<BigInt> polygonSendFee({required bool usdc}) async =>
      (await PolygonClient().fees()).feeCap * (usdc ? _usdcGas : _polGas);

  /// Sends POL or USDC on Polygon to a 0x address. Unlike LOCK this cannot
  /// be undone. [amount] in the coin's smallest units (USDC 6, POL 18).
  Future<void> sendPolygon({required bool usdc, required String to, required BigInt amount}) async {
    final words = _words;
    final me = polygonAddress;
    if (words == null || me == null) throw StateError('locked');
    final network = PolygonNetwork.current;
    final pc = PolygonClient();
    final balances = await pc.balances(me);
    final fees = await pc.fees();
    final fee = fees.feeCap * (usdc ? _usdcGas : _polGas);
    if (usdc && balances.usdc < amount) throw const BuyProblem(BuyProblemKind.noUsdc);
    if (balances.pol < fee + (usdc ? BigInt.zero : amount)) {
      throw BuyProblem(usdc ? BuyProblemKind.noPol : BuyProblemKind.noPolAmount);
    }
    final tx = await core.sign('signPolygonSend', words, '', '', 0, {
      'polygonChainId': network.chainId,
      'nonce': await pc.nonce(me),
      'tipWei': fees.tip.toString(),
      'feeCapWei': fees.feeCap.toString(),
      'token': usdc ? network.usdc : '',
      'to': to.trim(),
      'amount': amount.toString(),
    });
    if (!await pc.confirmed(await pc.send(tx))) throw const ChainError('polygon send failed');
    unawaited(_refreshPolygon());
  }

  Future<void> cancelOffer(int id) async {
    await _sign('signCancelOffer', {'id': id});
    await refreshMarket();
  }

  Future<void> _refreshPolygon() async {
    final addr = polygonAddress;
    if (addr == null) return;
    try {
      final b = await PolygonClient().balances(addr);
      if (polygonAddress != addr) return;
      polygon = b;
      notifyListeners();
    } catch (e) {
      debugPrint('polygon refresh failed: $e');
    }
  }

  Future<void> setAutoLock(int minutes) async {
    autoLockMinutes = minutes;
    notifyListeners();
    final prefs = await SharedPreferences.getInstance();
    await prefs.setInt(_autoLockKey, minutes);
  }

  Future<void> setCurrency(FiatCurrency c) async {
    currency = c;
    notifyListeners();
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_currencyKey, c.code);
  }

  Future<void> setLanguage(AppLanguage l) async {
    language = l;
    notifyListeners();
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_languageKey, l.tag);
  }

  /// Opens the wallet after creating, recovering or decrypting it.
  Future<void> open(String words) async {
    _words = words;
    address = await core.address(words);
    polygonAddress = await core.polygonAddress(words);
    await _loadWaiting();
    notifyListeners();
    if (!core.isPreview) {
      // Background alerts watch this public address (never a secret).
      await Alerts.watch(address);
      if (alertsOn && Platform.isAndroid) unawaited(Alerts.askPermission());
    }
  }

  /// A wallet must be registered before it can receive. It is free, so the
  /// app does it by itself the first time it reaches the network.
  Future<void> _register() async {
    final words = _words;
    if (words == null || _registering) return;
    _registering = true;
    notifyListeners();
    try {
      final client = LockKeeperClient(lockKeeperUrl);
      _chainId ??= await client.chainId();
      final acc = await client.account(address!) ?? ('0', 0);
      await client.broadcast(await core.signRegister(words, _chainId!, acc.$1, acc.$2));
    } catch (e) {
      // Tried again at the next refresh.
      debugPrint('register failed: $e');
    } finally {
      _registering = false;
    }
    await refresh();
  }

  /// Signs with both keys and sends one transaction; waits for its block.
  /// Throws ChainError when the network refuses it.
  Future<void> _submit(Future<String> Function(String words, String chainId, String account, int sequence) sign) async {
    final words = _words;
    final addr = address;
    if (words == null || addr == null) throw StateError('locked');
    final client = LockKeeperClient(lockKeeperUrl);
    _chainId ??= await client.chainId();
    final acc = await client.account(addr);
    if (acc == null) throw const ChainError('account not registered');
    await client.broadcast(await sign(words, _chainId!, acc.$1, acc.$2));
    await refresh();
  }

  Future<void> send(String to, int units) =>
      _submit((w, c, a, s) => core.signSend(w, c, a, s, to.trim(), units));

  Future<void> accept(int id) => _submit((w, c, a, s) => core.signAccept(w, c, a, s, id));

  Future<void> cancel(int id) => _submit((w, c, a, s) => core.signCancel(w, c, a, s, id));

  // --- Inheritance, trusted people, Sovereign Vault ---

  Future<void> _sign(String method, [Map<String, Object> extra = const {}]) =>
      _submit((w, c, a, s) => core.sign(method, w, c, a, s, extra));

  LockKeeperClient get client => LockKeeperClient(lockKeeperUrl);

  /// Saves the whole plan (heirs and trusted people go together).
  Future<void> setPlan(RecoveryStatus plan) => _sign('signSetPlan', {
        'plan': jsonEncode({
          'heirs': [for (final h in plan.heirs) {'address': h.address, 'percent': h.percent}],
          'inactivity_days': plan.inactivityDays,
          'guardians': plan.guardians,
          'guardian_threshold': plan.guardianThreshold,
          'heir_threshold': plan.heirThreshold,
        }),
      });

  /// "I'm here": sign of life; cancels an inheritance request.
  Future<void> alive() => _sign('signAlive');

  /// Stops any inheritance or recovery running on this account.
  Future<void> cancelClaims() => _sign('signCancelClaims');

  Future<void> claimInheritance(String account) => _sign('signClaimInheritance', {'account': account.trim()});

  Future<void> approveRecovery(String account, String newAddress) =>
      _sign('signApproveRecovery', {'account': account.trim(), 'newAddress': newAddress.trim()});

  Future<void> setVault(int days) => _sign('signSetVault', {'delayDays': days});

  Future<void> lockCoins(int units, DateTime until) =>
      _sign('signLock', {'units': units, 'until': until.millisecondsSinceEpoch ~/ 1000});

  /// "It wasn't me": freezes the account and cancels its pending transfers.
  Future<void> freeze() => _submit((w, c, a, s) => core.signFreeze(w, c, a, s));

  Future<void> unfreeze() => _submit((w, c, a, s) => core.signUnfreeze(w, c, a, s));

  Future<void> setDevice(String device) => _submit((w, c, a, s) => core.signSetDevice(w, c, a, s, device.trim()));

  Future<void> setLimits(int dailyLimit, int approvalAbove) =>
      _submit((w, c, a, s) => core.signSetLimits(w, c, a, s, dailyLimit, approvalAbove));

  // --- This phone as the second device of other accounts ---

  static const _guardedKey = 'encryptlock.guarded';

  /// Accounts this wallet protects as their second device.
  List<String> guarded = [];

  /// Their transfers waiting for this device's approval, by account.
  Map<String, List<PendingTransfer>> approvals = {};

  Future<void> setGuarded(List<String> accounts) async {
    guarded = accounts;
    notifyListeners();
    final prefs = await SharedPreferences.getInstance();
    await prefs.setStringList(_guardedKey, accounts);
  }

  Future<void> approve(int id) => _submit((w, c, a, s) => core.signApprove(w, c, a, s, id));

  /// Sell offers of the protected accounts waiting for this phone's approval.
  Map<String, List<MarketOffer>> offerApprovals = {};

  /// Lets a protected account's sell offer be taken (this phone is its
  /// second device).
  Future<void> approveOffer(int id) => _submit((w, c, a, s) => core.sign('signApproveOffer', w, c, a, s, {'id': id}));

  /// "It wasn't me" for an account this phone protects.
  Future<void> freezeGuarded(String account) => _submit((w, c, a, s) => core.signFreezeFor(w, c, a, s, account));

  /// The 24 words, only while the wallet is open (to show them on request).
  String? get words => _words;

  /// Re-encrypts the wallet on this phone with a new password.
  Future<void> changePassword(String newPassword) async {
    final w = _words;
    if (w == null) throw StateError('locked');
    await store.save(w, newPassword);
  }

  /// Removes the wallet from this phone; only the 24 words bring it back.
  Future<void> deleteWallet() async {
    await store.delete();
    await Alerts.watch(null);
    await Biometric.delete();
    await guard.reset();
    await sendCodeGuard.reset();
    biometricOn = false;
    sendCodeOn = false;
    lock();
  }

  /// Locks the wallet: the app forgets the words (Dart cannot overwrite
  /// memory; the Go core wipes its own copies); the password is needed again.
  void lock() {
    _words = null;
    address = null;
    polygonAddress = null;
    polygon = null;
    _polygonAt = null;
    status = null;
    _validatorAddress = null;
    _lastCollect = null;
    _chainId = null;
    waitingOrders = [];
    notifyListeners();
  }

  // --- Buy / Sell while no LockKeeper of the blockchain answers ---

  static const _waitingKey = 'encryptlock.waiting.orders';

  /// Buy / Sell asked while no LockKeeper answered, oldest first (this
  /// wallet's; done by themselves as soon as one answers).
  List<WaitingOrder> waitingOrders = [];

  final _waitingOutcomes = StreamController<WaitingOutcome>.broadcast();

  /// What happened to each waiting order once a LockKeeper answered.
  Stream<WaitingOutcome> get waitingOutcomes => _waitingOutcomes.stream;

  bool _runningWaiting = false;

  /// Nothing is sent when no LockKeeper answers: throws [NotReachable] (the
  /// order can then wait).
  Future<void> _ensureReachable() async {
    try {
      await LockKeeperClient(lockKeeperUrl).chainId();
    } catch (e) {
      if (e is SocketException || e is TimeoutException || e is HttpException || e is HandshakeException || e is TypeError) {
        throw const NotReachable();
      }
      rethrow;
    }
  }

  Future<void> _loadWaiting() async {
    final addr = address;
    if (addr == null) return;
    final raw = (await SharedPreferences.getInstance()).getString('$_waitingKey.$addr');
    waitingOrders = raw == null
        ? []
        : [for (final o in jsonDecode(raw) as List<dynamic>) WaitingOrder.fromJson(o as Map<String, dynamic>)];
  }

  Future<void> _saveWaiting(String addr) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString('$_waitingKey.$addr', jsonEncode([for (final o in waitingOrders) o.toJson()]));
  }

  /// Keeps a Buy / Sell to do as soon as a LockKeeper answers.
  Future<void> addWaiting(WaitingOrder order) async {
    final addr = address;
    if (addr == null) return;
    waitingOrders = [...waitingOrders, order];
    await _saveWaiting(addr);
    notifyListeners();
  }

  Future<void> cancelWaiting(String id) async {
    final addr = address;
    if (addr == null) return;
    waitingOrders = [for (final o in waitingOrders) if (o.id != id) o];
    await _saveWaiting(addr);
    notifyListeners();
  }

  /// Does the waiting orders, oldest first. An offer to take must still be
  /// there with the same LOCK and USDC (otherwise it is dropped, never taken
  /// at other terms). If no LockKeeper answers again, they keep waiting.
  Future<void> _runWaiting() async {
    final addr = address;
    if (_runningWaiting || addr == null || _words == null || waitingOrders.isEmpty) return;
    _runningWaiting = true;
    try {
      await refreshMarket();
      for (final o in List.of(waitingOrders)) {
        if (address != addr) return; // locked meanwhile
        WaitingOutcome outcome;
        try {
          if (o.newOffer) {
            await placeOffer(sell: o.sell, lock: o.lock, usdc: o.usdc, expiresAt: o.expiresAt);
            outcome = WaitingOutcome(o, WaitingResult.done);
          } else {
            // Selling takes a buy offer; buying takes a sell offer.
            final match = offers
                .where((x) => x.id == o.offerId && x.sell != o.sell && x.lock == o.lock && x.usdc == o.usdc && x.tradeId == 0)
                .firstOrNull;
            if (match == null) {
              outcome = WaitingOutcome(o, WaitingResult.offerGone);
            } else {
              await (o.sell ? sellTo(match) : buy(match));
              outcome = WaitingOutcome(o, WaitingResult.done);
            }
          }
        } on NotReachable {
          return; // still no LockKeeper: everything keeps waiting
        } catch (e) {
          // Never tried twice: a half-done purchase continues in "To pay".
          outcome = WaitingOutcome(o, WaitingResult.failed, e);
        }
        await cancelWaiting(o.id);
        _waitingOutcomes.add(outcome);
      }
    } finally {
      _runningWaiting = false;
    }
  }
}

/// Gives every screen access to the AppState.
class AppScope extends InheritedNotifier<AppState> {
  const AppScope({super.key, required AppState state, required super.child}) : super(notifier: state);

  static AppState of(BuildContext context) => context.dependOnInheritedWidgetOfExactType<AppScope>()!.notifier!;
}
