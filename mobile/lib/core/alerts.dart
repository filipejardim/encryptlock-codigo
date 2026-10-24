import 'dart:async';
import 'dart:io';
import 'dart:ui';

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';
import 'package:intl/intl.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:workmanager/workmanager.dart';

import '../l10n/gen/app_localizations.dart';
import 'market.dart';
import 'network.dart';

/// Security alerts without any server: about every 15 minutes, even with the
/// app closed, Android runs [checkAlerts], which asks the LockKeeper about
/// this wallet's PUBLIC address (never a secret) and shows a notification:
/// - coins sent from this account ("if it wasn't you, press It wasn't me"),
/// - coins waiting to be accepted (1 hour),
/// - a transfer of a protected account waiting for this second device,
/// - an inheritance or recovery request on this account,
/// - someone sold to this wallet's buy offer: pay now.
class Alerts {
  static const _task = 'encryptlock-alerts';
  static const _addressKey = 'encryptlock.watch.address';
  static const _seenKey = 'encryptlock.alerts.seen';
  static const onKey = 'encryptlock.alerts.on';

  static final _notifications = FlutterLocalNotificationsPlugin();

  /// Starts the background check (once per install; Android keeps it).
  static Future<void> start() async {
    await Workmanager().initialize(alertsDispatcher);
    await Workmanager().registerPeriodicTask(
      _task,
      _task,
      frequency: const Duration(minutes: 15),
      constraints: Constraints(networkType: NetworkType.connected),
      existingWorkPolicy: ExistingPeriodicWorkPolicy.keep,
    );
    // Test builds also check once right away (no 15-minute wait).
    if (kDebugMode) await Workmanager().registerOneOffTask('$_task-now', '$_task-now');
  }

  static Timer? _desktop;

  /// Computer: no background scheduler, so the check runs every minute while
  /// the app is open (also when it is minimised or locked).
  static void startDesktop() {
    _desktop?.cancel();
    _desktop = Timer.periodic(const Duration(minutes: 1), (_) async {
      try {
        await checkAlerts();
      } catch (e) {
        debugPrint('alerts check failed: $e');
      }
    });
  }

  /// Remembers which public address to watch (set when the wallet opens).
  static Future<void> watch(String? address) async {
    final prefs = await SharedPreferences.getInstance();
    if (address == null) {
      await prefs.remove(_addressKey);
      await prefs.remove(_seenKey);
    } else {
      await prefs.setString(_addressKey, address);
    }
  }

  /// Android 13+ asks the owner once whether the app may notify.
  static Future<void> askPermission() async {
    await _init();
    await _notifications
        .resolvePlatformSpecificImplementation<AndroidFlutterLocalNotificationsPlugin>()
        ?.requestNotificationsPermission();
  }

  static Future<void> _init() => _notifications.initialize(
        settings: const InitializationSettings(
          android: AndroidInitializationSettings('ic_stat_lock'),
          windows: WindowsInitializationSettings(
            appName: 'EncryptLock',
            appUserModelId: 'EncryptLock.Wallet',
            guid: '6f1c2b7e-5a3d-4e8b-9c41-2d7a9e3f0b15',
          ),
        ),
      );

  static AppLocalizations _texts(SharedPreferences prefs) {
    final tag = prefs.getString('encryptlock.language');
    final parts = (tag ?? PlatformDispatcher.instance.locale.toLanguageTag().replaceAll('-', '_')).split('_');
    final locale = tag == 'zh_Hant'
        ? const Locale.fromSubtags(languageCode: 'zh', scriptCode: 'Hant')
        : Locale(parts[0], parts.length > 1 && parts[1].length == 2 ? parts[1] : null);
    try {
      return lookupAppLocalizations(locale);
    } catch (_) {
      try {
        return lookupAppLocalizations(Locale(parts[0]));
      } catch (_) {
        return lookupAppLocalizations(const Locale('en'));
      }
    }
  }

  /// One check; returns how many notifications it showed.
  static Future<int> checkAlerts() async {
    final prefs = await SharedPreferences.getInstance();
    if (prefs.getBool(onKey) == false) return 0;
    final address = prefs.getString(_addressKey);
    if (address == null) return 0;
    final client = LockKeeperClient(prefs.getString('encryptlock.lockkeeper') ??
        const String.fromEnvironment('LOCKKEEPER').ifEmpty(
          Platform.isAndroid ? 'http://10.0.2.2:1317' : 'http://127.0.0.1:1317',
        ));
    final guarded = prefs.getStringList('encryptlock.guarded') ?? const [];
    final seen = (prefs.getStringList(_seenKey) ?? const []).toSet();
    final t = _texts(prefs);
    final locale = Intl.canonicalizedLocale(t.localeName);
    String lock(BigInt units) =>
        NumberFormat('#,##0.00##', locale).format(units.toDouble() / LockKeeperClient.unitsPerLock.toDouble());

    final alerts = <(String key, String title, String body)>[];
    final mine = await client.pendingBoth(address);
    for (final p in mine.received) {
      // Not yet acceptable: the sender's second device must approve first.
      if (p.awaitingApproval) continue;
      alerts.add(('in:${p.id}', t.alertIncomingTitle, t.alertIncomingBody(lock(p.amount))));
    }
    for (final p in mine.sent) {
      alerts.add(('out:${p.id}', t.alertSentTitle, t.alertSentBody(lock(p.amount))));
    }
    for (final account in guarded) {
      for (final p in await client.pending(account)) {
        if (p.awaitingApproval) {
          alerts.add(('ap:${p.id}', t.alertApprovalTitle, t.alertApprovalBody(lock(p.amount))));
        }
      }
    }
    final claim = await client.claim(address);
    if (claim != null) alerts.add(('claim:$claim', t.alertClaimTitle, t.alertClaimBody));
    // Someone sold to my buy offer: pay in Buy / Sell before the deadline.
    final now = DateTime.now().millisecondsSinceEpoch ~/ 1000;
    for (final tr in await client.trades(address)) {
      if (tr.buyer == address && tr.payBy > now) {
        final usd = NumberFormat('#,##0.00', locale).format(tr.usdc.toDouble() / 1e6);
        alerts.add(('pay:${tr.id}', '💳 ${t.toPayTitle}', '${lock(tr.lock)} LOCK  ·  $usd \$'));
      }
    }

    final fresh = [for (final a in alerts) if (!seen.contains(a.$1)) a];
    if (fresh.isEmpty) return 0;
    await _init();
    for (final a in fresh) {
      await _notifications.show(
        id: a.$1.hashCode & 0x7fffffff,
        title: a.$2,
        body: a.$3,
        notificationDetails: NotificationDetails(
          android: AndroidNotificationDetails(
            'encryptlock.security',
            t.alerts,
            icon: 'ic_stat_lock',
            color: const Color(0xFF94F7D1),
            importance: Importance.high,
            priority: Priority.high,
            styleInformation: BigTextStyleInformation(a.$3),
          ),
          windows: const WindowsNotificationDetails(),
        ),
      );
    }
    // Keep only what still exists, so the list never grows forever.
    await prefs.setStringList(_seenKey, [for (final a in alerts) a.$1]);
    return fresh.length;
  }
}

/// Entry point Android calls in the background.
@pragma('vm:entry-point')
void alertsDispatcher() {
  Workmanager().executeTask((task, input) async {
    WidgetsFlutterBinding.ensureInitialized();
    try {
      final shown = await Alerts.checkAlerts();
      debugPrint('alerts check: $shown shown');
    } catch (e, st) {
      // No network or LockKeeper off: try again next time.
      debugPrint('alerts check failed: $e $st');
    }
    return true;
  });
}

extension on String {
  String ifEmpty(String other) => isEmpty ? other : this;
}
