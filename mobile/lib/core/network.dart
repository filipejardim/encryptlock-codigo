import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'last_answers.dart';
import 'network_pin.dart';

/// A transfer waiting for the recipient to accept it (1 hour).
class PendingTransfer {
  const PendingTransfer({
    required this.id,
    required this.sender,
    required this.recipient,
    required this.amount,
    required this.expiresAt,
    required this.awaitingApproval,
    required this.releaseAt,
  });

  factory PendingTransfer.fromJson(Map<String, dynamic> j) => PendingTransfer(
        id: int.parse('${j['id'] ?? 0}'),
        sender: j['sender'] as String? ?? '',
        recipient: j['recipient'] as String? ?? '',
        amount: BigInt.parse('${j['amount'] ?? 0}'),
        expiresAt: DateTime.fromMillisecondsSinceEpoch(int.parse('${j['expires_at'] ?? 0}') * 1000),
        awaitingApproval: j['awaiting_approval'] == true,
        releaseAt: int.parse('${j['release_at'] ?? 0}'),
      );

  final int id;
  final String sender;
  final String recipient;
  final BigInt amount;

  /// Network time when it goes back to the sender if not accepted.
  final DateTime expiresAt;

  /// The sender's second device must approve it first.
  final bool awaitingApproval;

  /// From a Sovereign Vault: can only be accepted from this time (Unix s).
  final int releaseAt;
}

/// An account's protection settings on the network (white paper 7.2).
class Shield {
  const Shield({
    this.device = '',
    this.pendingDevice = '',
    this.deviceChangePending = false,
    this.deviceChangeAt = 0,
    this.dailyLimit = 0,
    this.approvalAbove = 0,
    this.pendingDailyLimit = 0,
    this.pendingApprovalAbove = 0,
    this.limitsChangeAt = 0,
    this.vaultDays = 0,
    this.pendingVaultDays = 0,
    this.vaultChangeAt = 0,
  });

  factory Shield.fromJson(Map<String, dynamic> j) {
    int i(String k) => int.tryParse('${j[k] ?? 0}') ?? 0;
    final limitAt = i('daily_limit_change_at');
    final aboveAt = i('approval_above_change_at');
    return Shield(
      device: j['device'] as String? ?? '',
      pendingDevice: j['pending_device'] as String? ?? '',
      deviceChangePending: j['device_change_pending'] == true,
      deviceChangeAt: i('device_change_at'),
      dailyLimit: i('daily_limit'),
      approvalAbove: i('approval_above'),
      pendingDailyLimit: limitAt == 0 ? 0 : i('pending_daily_limit'),
      pendingApprovalAbove: aboveAt == 0 ? 0 : i('pending_approval_above'),
      limitsChangeAt: limitAt > aboveAt ? limitAt : aboveAt,
      vaultDays: i('vault_delay') ~/ 86400,
      pendingVaultDays: i('vault_delay_change_at') == 0 ? 0 : i('pending_vault_delay') ~/ 86400,
      vaultChangeAt: i('vault_delay_change_at'),
    );
  }

  /// The second device (lock1...), or '' if none.
  final String device;

  /// A change of second device waiting its 7 days ('' = removal).
  final String pendingDevice;
  final bool deviceChangePending;
  final int deviceChangeAt;

  /// Lockunit per 24 h (0 = no limit) and above which the second device
  /// must approve (0 = only new contacts).
  final int dailyLimit;
  final int approvalAbove;

  /// Higher limits wait 48 h; when they apply (Unix s).
  final int pendingDailyLimit;
  final int pendingApprovalAbove;
  final int limitsChangeAt;

  /// Sovereign Vault: days every sending waits (0 = not a vault), and a
  /// pending change (lowering waits for the current delay).
  final int vaultDays;
  final int pendingVaultDays;
  final int vaultChangeAt;
}

/// An heir and the share of the coins (percent).
class Heir {
  const Heir(this.address, this.percent);

  final String address;
  final int percent;
}

/// Inheritance and recovery plan and what is running on an account.
class RecoveryStatus {
  const RecoveryStatus({
    this.heirs = const [],
    this.inactivityDays = 0,
    this.guardians = const [],
    this.guardianThreshold = 0,
    this.heirThreshold = 0,
    this.planChangeAt = 0,
    this.lastActive = 0,
    this.claimKind = '',
    this.claimExecutesAt = 0,
    this.confirmations = 0,
  });

  factory RecoveryStatus.fromJson(Map<String, dynamic> j) {
    int i(Object? v) => int.tryParse('${v ?? 0}') ?? 0;
    final plan = j['plan'] as Map<String, dynamic>? ?? const {};
    final claim = j['claim'] as Map<String, dynamic>? ?? const {};
    return RecoveryStatus(
      heirs: [
        for (final h in (plan['heirs'] as List<dynamic>? ?? const []))
          Heir((h as Map<String, dynamic>)['address'] as String? ?? '', i(h['percent'])),
      ],
      inactivityDays: i(plan['inactivity_days']),
      guardians: [for (final g in (plan['guardians'] as List<dynamic>? ?? const [])) g as String],
      guardianThreshold: i(plan['guardian_threshold']),
      heirThreshold: i(plan['heir_threshold']),
      planChangeAt: j['plan_change_pending'] == true ? i(j['plan_change_at']) : 0,
      lastActive: i(j['last_active']),
      claimKind: claim['kind'] as String? ?? '',
      claimExecutesAt: i(claim['executes_at']),
      confirmations: (j['heir_confirmations'] as List<dynamic>? ?? const []).length,
    );
  }

  final List<Heir> heirs;
  final int inactivityDays;
  final List<String> guardians;
  final int guardianThreshold;
  final int heirThreshold;

  /// A plan change waiting its 7 days (Unix s); 0 = none.
  final int planChangeAt;

  /// Last sign of life (Unix s).
  final int lastActive;

  /// "inheritance" or "recovery" when one is running, else ''.
  final String claimKind;
  final int claimExecutesAt;
  final int confirmations;
}

/// A LockKeeper as the public sees it.
class KeeperInfo {
  const KeeperInfo(this.name, this.tokens, this.jailed);

  final String name;
  final BigInt tokens;
  final bool jailed;
}

/// Each LockKeeper's share of the new LOCK (0 to 1), as the network pays it
/// for an hour in which they all stay as they are now: by guarantee, nothing
/// for one that is off (jailed), and from 20 on no one above 5% (the same
/// rule as x/powercap). The guarantee share alone would show the founder's
/// computer at ~76% with 20 on, when it earns 5% (founder's question, 21 Oct
/// 2026).
List<double> earningShares(List<KeeperInfo> keepers) {
  const minKeepers = 20, divisor = 20;
  final powers = [for (final k in keepers) k.jailed ? BigInt.zero : k.tokens];
  final on = [for (final p in powers) if (p > BigInt.zero) p];
  var capped = powers;
  if (on.length >= minKeepers) {
    final sorted = [...on]..sort((a, b) => b.compareTo(a));
    // suffix[k] = what the ones left uncapped hold when the k largest are capped.
    final suffix = List.filled(sorted.length + 1, BigInt.zero);
    for (var i = sorted.length - 1; i >= 0; i--) {
      suffix[i] = suffix[i + 1] + sorted[i];
    }
    for (var k = 0; k < divisor; k++) {
      final limit = suffix[k] ~/ BigInt.from(divisor - k);
      if (sorted[k] <= limit) {
        if (k > 0) capped = [for (final p in powers) p > limit ? limit : p];
        break;
      }
    }
  }
  final total = capped.fold(BigInt.zero, (s, p) => s + p);
  return [for (final p in capped) total == BigInt.zero ? 0.0 : p / total];
}

/// Public facts about the network (what the explorer shows).
class NetworkInfo {
  const NetworkInfo({required this.keepers, required this.supply});

  final List<KeeperInfo> keepers;

  /// LOCK in existence, in lockunit.
  final BigInt supply;
}

/// Coins locked until a date (they come back by themselves).
class CoinLock {
  const CoinLock(this.id, this.amount, this.until);

  final int id;
  final BigInt amount;
  final int until;
}

/// A transfer that was accepted (finished), for the history.
class DoneTransfer {
  const DoneTransfer(this.id, this.incoming, this.other, this.amount, this.time, {this.estimated = false});

  final int id;
  final bool incoming;

  /// The address on the other side.
  final String other;
  final BigInt amount;
  final DateTime time;

  /// The block is no longer kept: [time] is the LockKeeper's estimate.
  final bool estimated;
}

/// The network refused a transaction; [log] is its explanation.
/// A LockKeeper in the rotation: on, signing, and its hours log (seconds on
/// today, yesterday, in the last 7 and 30 days).
class LockKeeperRotation {
  const LockKeeperRotation({
    required this.online,
    required this.signing,
    required this.today,
    required this.yesterday,
    required this.week,
    required this.month,
    this.chip = true,
  });

  final bool online;
  final bool signing;
  final int today;
  final int yesterday;
  final int week;
  final int month;

  /// The network accepts its "I am on": the computer's security chip is
  /// proven (or it started the network).
  final bool chip;
}

class ChainError implements Exception {
  const ChainError(this.log);

  final String log;

  @override
  String toString() => log;
}

/// What the app shows about one account, read from a LockKeeper.
class AccountStatus {
  AccountStatus({
    required this.height,
    required this.balance,
    required this.toCollect,
    required this.isLockKeeper,
    this.registered = true,
    this.frozen = false,
    this.unfreezeAt = 0,
    this.shield = const Shield(),
    this.claimKind = '',
    BigInt? locked,
    this.sent = const [],
    this.received = const [],
  }) : locked = locked ?? BigInt.zero;

  /// Registered accounts can receive (registration is free).
  final bool registered;

  /// Second device and limits.
  final Shield shield;

  /// "inheritance" or "recovery" when someone asked for this account's coins.
  final String claimKind;

  /// Coins locked until a date (not in the balance; they come back).
  final BigInt locked;

  /// "It wasn't me" was pressed: nothing can leave the account.
  final bool frozen;

  /// When the account alone asked to unfreeze (Unix s; it unfreezes 7 days
  /// later); 0 = not asked.
  final int unfreezeAt;
  final List<PendingTransfer> sent;
  final List<PendingTransfer> received;

  /// Latest block of the network.
  final int height;

  /// Free coins, in lockunit (1 LOCK = 100.000.000 lockunit).
  final BigInt balance;

  /// LockKeeper rewards + commission waiting to be collected, in lockunit.
  final BigInt toCollect;

  /// True when this account runs a LockKeeper.
  final bool isLockKeeper;
}

/// Reads public information from a LockKeeper (its REST API). Nothing secret
/// is ever sent: only the public address. No other server is contacted.
class LockKeeperClient {
  LockKeeperClient(this.baseUrl);

  final String baseUrl;

  static const timeout = Duration(seconds: 6);
  static const denom = 'lockunit';
  static final unitsPerLock = BigInt.from(100000000);

  Future<Map<String, dynamic>?> _get(String path) => _call('GET', path);

  /// Like _get, but any error (not a LockKeeper, …) just means "nothing".
  Future<Map<String, dynamic>?> _optional(String path) async {
    try {
      return await _get(path);
    } on HttpException {
      return null;
    }
  }

  Future<Map<String, dynamic>?> _call(String method, String path, [Map<String, dynamic>? body]) async {
    final client = HttpClient()..connectionTimeout = timeout;
    try {
      final base = baseUrl.endsWith('/') ? baseUrl.substring(0, baseUrl.length - 1) : baseUrl;
      final request = await client.openUrl(method, Uri.parse('$base$path')).timeout(timeout);
      if (body != null) {
        request.headers.contentType = ContentType.json;
        request.write(jsonEncode(body));
      }
      final response = await request.close().timeout(timeout);
      final text = await response.transform(utf8.decoder).join().timeout(timeout);
      if (response.statusCode == 404 || response.statusCode == 400) return null;
      if (response.statusCode != 200) throw HttpException('HTTP ${response.statusCode}');
      return jsonDecode(text) as Map<String, dynamic>;
    } finally {
      client.close(force: true);
    }
  }

  /// Sum of the lockunit coins in a list; decimals (rewards) are cut.
  static BigInt sumCoins(Object? coins) {
    var sum = BigInt.zero;
    for (final c in (coins as List<dynamic>? ?? const [])) {
      final coin = c as Map<String, dynamic>;
      if (coin['denom'] != denom) continue;
      sum += BigInt.parse((coin['amount'] as String).split('.').first);
    }
    return sum;
  }

  Future<AccountStatus> status(String address, String validatorAddress) async {
    final results = await Future.wait([
      _get('/cosmos/base/tendermint/v1beta1/blocks/latest'),
      _get('/cosmos/bank/v1beta1/balances/$address'),
      // Accounts that are not LockKeepers may get an error here: not fatal.
      _optional('/cosmos/distribution/v1beta1/delegators/$address/rewards'),
      _optional('/cosmos/distribution/v1beta1/validators/$validatorAddress/commission'),
    ]);
    final block = results[0];
    if (block == null) throw const HttpException('no block');
    final header = (block['block'] as Map<String, dynamic>)['header'] as Map<String, dynamic>;
    final commission = (results[3]?['commission'] as Map<String, dynamic>?)?['commission'];
    final rewards = results[2]?['total'];
    final more = await Future.wait([
      _get('/encryptlock/registry/v1/registered/$address'),
      _get('/encryptlock/transfers/v1/pending/$address'),
      _optional('/encryptlock/transfers/v1/shield/$address'),
      _optional('/encryptlock/recovery/v1/status/$address'),
      _optional('/encryptlock/vault/v1/locks/$address'),
    ]);
    var locked = BigInt.zero;
    for (final l in (more[4]?['locks'] as List<dynamic>? ?? const [])) {
      locked += BigInt.parse('${(l as Map<String, dynamic>)['amount'] ?? 0}');
    }
    final claim = ((more[3]?['status'] as Map<String, dynamic>?)?['claim'] as Map<String, dynamic>?)?['kind'];
    final shield = more[2]?['shield'] as Map<String, dynamic>?;
    List<PendingTransfer> list(Object? l) =>
        [for (final t in (l as List<dynamic>? ?? const [])) PendingTransfer.fromJson(t as Map<String, dynamic>)];
    return AccountStatus(
      height: int.parse(header['height'] as String),
      balance: sumCoins(results[1]?['balances']),
      toCollect: sumCoins(rewards) + sumCoins(commission),
      // A LockKeeper has a commission record (even if empty before its first earnings).
      isLockKeeper: commission != null || sumCoins(rewards) > BigInt.zero,
      registered: more[0]?['registered'] == true,
      frozen: shield?['frozen'] == true,
      shield: Shield.fromJson(shield ?? const {}),
      claimKind: claim as String? ?? '',
      locked: locked,
      unfreezeAt: int.tryParse('${shield?['unfreeze_owner_at'] ?? 0}') ?? 0,
      sent: list(more[1]?['sent']),
      received: list(more[1]?['received']),
    );
  }

  /// What the network knows about a computer's helper account: the
  /// block-signing key it announced (base64; null if none) and the LockKeeper
  /// it works for (null until a phone linked it).
  Future<({String? pubKey, String? keeper})> helper(String helper) async {
    final r = await _get('/encryptlock/rotation/v1/helper/$helper');
    final key = r?['pub_key'] as String?;
    final keeper = r?['keeper'] as String?;
    return (pubKey: key == null || key.isEmpty ? null : key, keeper: keeper == null || keeper.isEmpty ? null : keeper);
  }

  /// This LockKeeper's place in the rotation: on, signing, time on.
  Future<LockKeeperRotation> rotation(String address) async {
    final r = await _optional('/encryptlock/rotation/v1/status/$address');
    int n(String k) => int.tryParse('${r?[k] ?? 0}') ?? 0;
    return LockKeeperRotation(
      online: r?['online'] == true,
      signing: r?['signing'] == true,
      today: n('seconds_today'),
      yesterday: n('seconds_yesterday'),
      week: n('seconds_week'),
      month: n('seconds_month'),
      chip: r?['chip'] != false,
    );
  }

  Future<NetworkInfo> network() async {
    final res = await Future.wait([
      _get('/cosmos/staking/v1beta1/validators?status=BOND_STATUS_BONDED'),
      _get('/cosmos/bank/v1beta1/supply/by_denom?denom=lockunit'),
    ]);
    final both = {'validators': res[0], 'supply': res[1]};
    // Kept for when no LockKeeper can be reached.
    await LastAnswers.put('network', both);
    return parseNetwork(both);
  }

  /// The development fund's address (public; null if not answered).
  Future<String?> devFund() async => (await _optional('/encryptlock/transfers/v1/dev_fund'))?['address'] as String?;

  /// How many LockKeepers of the blockchain are switched on now (kept for
  /// when none can be reached).
  Future<int> keepersOnline() async {
    final r = await _get('/encryptlock/rotation/v1/overview');
    if (r == null) throw const HttpException('no overview');
    await LastAnswers.put('overview', r);
    return int.tryParse('${r['online'] ?? 0}') ?? 0;
  }

  static Future<int?> lastKeepersOnline() async {
    final r = await LastAnswers.get('overview');
    return r == null ? null : int.tryParse('${r['online'] ?? 0}');
  }

  /// The last LockKeepers and LOCK in existence the blockchain gave (null:
  /// none kept).
  static Future<NetworkInfo?> lastNetwork() async {
    final j = await LastAnswers.get('network');
    return j == null ? null : parseNetwork(j);
  }

  static NetworkInfo parseNetwork(Map<String, dynamic> both) {
    final res = [both['validators'] as Map<String, dynamic>?, both['supply'] as Map<String, dynamic>?];
    final keepers = [
      for (final v in (res[0]?['validators'] as List<dynamic>? ?? const []))
        KeeperInfo(
          ((v as Map<String, dynamic>)['description'] as Map<String, dynamic>?)?['moniker'] as String? ?? '—',
          BigInt.parse('${v['tokens'] ?? 0}'),
          v['jailed'] == true,
        ),
    ]..sort((a, b) => b.tokens.compareTo(a.tokens));
    final amount = (res[1]?['amount'] as Map<String, dynamic>?)?['amount'];
    return NetworkInfo(keepers: keepers, supply: BigInt.parse('${amount ?? 0}'));
  }

  /// Transfers an account sent and received that are still waiting.
  Future<({List<PendingTransfer> sent, List<PendingTransfer> received})> pendingBoth(String address) async {
    final res = await _get('/encryptlock/transfers/v1/pending/$address');
    List<PendingTransfer> list(Object? l) =>
        [for (final t in (l as List<dynamic>? ?? const [])) PendingTransfer.fromJson(t as Map<String, dynamic>)];
    return (sent: list(res?['sent']), received: list(res?['received']));
  }

  /// An inheritance or recovery request on this account, as "kind:start"
  /// (to tell requests apart); null when there is none.
  Future<String?> claim(String address) async {
    final res = await _optional('/encryptlock/recovery/v1/status/$address');
    final claim = (res?['status'] as Map<String, dynamic>?)?['claim'] as Map<String, dynamic>?;
    final kind = claim?['kind'] as String? ?? '';
    if (kind.isEmpty) return null;
    return '$kind:${claim?['started_at'] ?? 0}';
  }

  Future<RecoveryStatus> recovery(String address) async {
    final res = await _optional('/encryptlock/recovery/v1/status/$address');
    return RecoveryStatus.fromJson(res?['status'] as Map<String, dynamic>? ?? const {});
  }

  Future<List<CoinLock>> locks(String address) async {
    final res = await _optional('/encryptlock/vault/v1/locks/$address');
    return [
      for (final l in (res?['locks'] as List<dynamic>? ?? const []))
        CoinLock(
          int.tryParse('${(l as Map<String, dynamic>)['id'] ?? 0}') ?? 0,
          BigInt.parse('${l['amount'] ?? 0}'),
          int.tryParse('${l['until'] ?? 0}') ?? 0,
        ),
    ];
  }

  /// The last accepted transfers of an account, sent and received, newest
  /// first (from the LockKeeper's transaction index).
  Future<List<DoneTransfer>> history(String address, {int limit = 10}) async {
    // LockKeepers from 22 Oct 2026 on read the index without opening blocks
    // (they keep only about 1 day of them): older transfers are kept too.
    final res = await _optional('/encryptlock/history/v1/$address?limit=$limit').catchError((_) => null);
    final list = res?['transfers'] as List<dynamic>?;
    if (list != null) {
      return [
        for (final t in list)
          DoneTransfer(
            int.tryParse('${(t as Map<String, dynamic>)['id']}') ?? 0,
            t['incoming'] == true,
            '${t['other'] ?? ''}',
            BigInt.tryParse('${t['amount']}') ?? BigInt.zero,
            DateTime.tryParse('${t['time']}')?.toLocal() ?? DateTime.now(),
            estimated: t['estimated'] == true,
          ),
      ];
    }
    return _historyFromBlocks(address, limit);
  }

  /// The search of older LockKeepers: it opens the block of each result, so
  /// it is empty as soon as one of them is no longer kept.
  Future<List<DoneTransfer>> _historyFromBlocks(String address, int limit) async {
    Future<List<DoneTransfer>> side(String role) async {
      final query = Uri.encodeQueryComponent("transfer_accepted.$role='$address'");
      final res = await _optional('/cosmos/tx/v1beta1/txs?query=$query&limit=$limit&order_by=ORDER_BY_DESC');
      final out = <DoneTransfer>[];
      for (final r in (res?['tx_responses'] as List<dynamic>? ?? const [])) {
        final tx = r as Map<String, dynamic>;
        final time = DateTime.tryParse('${tx['timestamp']}')?.toLocal() ?? DateTime.now();
        for (final e in (tx['events'] as List<dynamic>? ?? const [])) {
          final ev = e as Map<String, dynamic>;
          if (ev['type'] != 'transfer_accepted') continue;
          final a = {
            for (final x in (ev['attributes'] as List<dynamic>? ?? const []))
              '${(x as Map<String, dynamic>)['key']}': '${x['value']}',
          };
          if (a[role] != address) continue;
          final incoming = role == 'recipient';
          out.add(DoneTransfer(
            int.tryParse(a['id'] ?? '') ?? 0,
            incoming,
            (incoming ? a['sender'] : a['recipient']) ?? '',
            BigInt.tryParse(a['amount'] ?? '') ?? BigInt.zero,
            time,
          ));
        }
      }
      return out;
    }

    final both = [...await side('recipient'), ...await side('sender')]..sort((a, b) => b.time.compareTo(a.time));
    return both.take(limit).toList();
  }

  /// Transfers sent by an account that are still waiting.
  Future<List<PendingTransfer>> pending(String address) async {
    final res = await _optional('/encryptlock/transfers/v1/pending/$address');
    return [for (final t in (res?['sent'] as List<dynamic>? ?? const [])) PendingTransfer.fromJson(t as Map<String, dynamic>)];
  }

  /// The network's name (signatures are only valid on this network).
  Future<String> chainId() async {
    final info = await _get('/cosmos/base/tendermint/v1beta1/node_info');
    final network = ((info!['default_node_info'] as Map<String, dynamic>)['network']) as String;
    NetworkPin.checkChainId(network);
    return network;
  }

  /// Account number and next sequence; null if the network does not know
  /// the account yet (not registered).
  Future<(String, int)?> account(String address) async {
    final res = await _get('/cosmos/auth/v1beta1/accounts/$address');
    final acc = res?['account'] as Map<String, dynamic>?;
    if (acc == null) return null;
    // The account number can be above the largest Dart int: kept as text.
    return ('${acc['account_number'] ?? 0}', int.parse('${acc['sequence'] ?? 0}'));
  }

  /// Sends a signed transaction and waits until it is in a block.
  /// Throws [ChainError] when the network refuses it.
  Future<void> broadcast(String txBase64) async {
    final res = await _call('POST', '/cosmos/tx/v1beta1/txs', {'tx_bytes': txBase64, 'mode': 'BROADCAST_MODE_SYNC'});
    final tx = res?['tx_response'] as Map<String, dynamic>?;
    if (tx == null) throw const HttpException('no answer');
    if ((tx['code'] ?? 0) != 0) throw ChainError(tx['raw_log'] as String? ?? 'refused');
    final hash = tx['txhash'] as String;
    final deadline = DateTime.now().add(const Duration(seconds: 30));
    while (DateTime.now().isBefore(deadline)) {
      await Future<void>.delayed(const Duration(seconds: 1));
      final found = await _get('/cosmos/tx/v1beta1/txs/$hash').catchError((_) => null);
      final r = found?['tx_response'] as Map<String, dynamic>?;
      if (r == null) continue; // not in a block yet
      if ((r['code'] ?? 0) != 0) throw ChainError(r['raw_log'] as String? ?? 'refused');
      return;
    }
    throw const HttpException('timeout');
  }
}

/// The device has internet: the project's website answers (any answer).
/// Tells "no internet" apart from "no LockKeeper of the blockchain answers".
Future<bool> hasInternet() async {
  final client = HttpClient()..connectionTimeout = const Duration(seconds: 5);
  try {
    final req = await client.headUrl(Uri.parse('https://encryptlock.pages.dev/versao.json')).timeout(const Duration(seconds: 6));
    await req.close().timeout(const Duration(seconds: 6));
    return true;
  } catch (_) {
    return false;
  } finally {
    client.close(force: true);
  }
}

/// Where the network can be reached from anywhere right now, as the website
/// publishes it (rede.json: it changes when the founder's computer restarts);
/// null when the website does not say.
Future<String?> networkAddressFromSite() async => (await networkDoorFromSite())?.lockKeeper;

/// The network's door as the website publishes it: the public API, and for
/// computers that become LockKeepers from another home, the network's RPC and
/// the host of the LockKeepers' direct connection (raw TCP through
/// Cloudflare). Null when the website does not say.
class NetworkDoor {
  const NetworkDoor(this.lockKeeper, this.rpc, this.p2pHost, {this.genesis});

  final String lockKeeper;
  final String? rpc;
  final String? p2pHost;

  /// When the network started (its first block is deleted after a while).
  final DateTime? genesis;

  // Labels start and end with a letter or digit: never read as an option.
  static final _host = RegExp(r'^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$');

  static NetworkDoor? fromJson(Object? j) {
    if (j is! Map<String, dynamic>) return null;
    final url = j['lockkeeper'];
    if (url is! String || !url.startsWith('https://')) return null;
    final rpc = j['rpc'];
    final p2p = j['p2p'];
    // It goes into the LockKeeper's settings: https and a plain host only.
    final rpcHost = rpc is String && rpc.startsWith('https://') ? rpc.substring(8).replaceFirst(RegExp(r'/$'), '') : '';
    return NetworkDoor(
      url,
      _host.hasMatch(rpcHost) ? 'https://$rpcHost' : null,
      // It goes into a command: a plain host name only.
      p2p is String && _host.hasMatch(p2p) ? p2p : null,
      genesis: DateTime.tryParse('${j['genesis_time']}'),
    );
  }
}

Future<NetworkDoor?> networkDoorFromSite() async {
  final client = HttpClient()..connectionTimeout = const Duration(seconds: 6);
  try {
    final req = await client.getUrl(Uri.parse('https://encryptlock-rede.pages.dev/rede.json'));
    final res = await req.close().timeout(const Duration(seconds: 10));
    if (res.statusCode != 200) return null;
    return NetworkDoor.fromJson(jsonDecode(await res.transform(utf8.decoder).join()));
  } finally {
    client.close();
  }
}
