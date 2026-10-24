import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/foundation.dart' show debugPrint, kIsWeb;
import 'package:flutter/services.dart' show MethodChannel;

import 'market.dart';
import 'network.dart';
import 'network_pin.dart';

/// "Make this PC a LockKeeper" (computer app only).
///
/// The app ships lockkeeper.exe next to itself. Making the PC a LockKeeper:
///  1. prepares a LockKeeper in %LOCALAPPDATA%\EncryptLock\lockkeeper that
///     joins the network of the LockKeeper the app talks to (its genesis and
///     its address come from there);
///  2. creates a helper account whose 24 words stay on this computer, so
///     "I am on" never needs the owner's password;
///  3. starts two background programs: the LockKeeper itself and
///     `lockkeeper pc-heartbeat`, which says "I am on" every 5 minutes.
///     They keep running with the app window closed; "Switch off" stops them.
/// The guarantee transaction is signed by the wallet (see AppState).
class PcLockKeeper {
  PcLockKeeper._();

  // The browser preview has no files or Platform: never a LockKeeper there.
  static bool get isSupported => !kIsWeb && Platform.isWindows && File(exe).existsSync();

  static String get exe => '${File(Platform.resolvedExecutable).parent.path}\\lockkeeper.exe';

  static String get home => '${Platform.environment['LOCALAPPDATA'] ?? Directory.systemTemp.path}\\EncryptLock\\lockkeeper';

  static String get _helperFile => '$home\\helper-24-words.txt';
  static String get _pidFile => '$home\\running.json';
  static String get _readyFile => '$home\\lockkeeper-ready';
  static String get _ticketFile => '$home\\chip-ticket.json';

  /// The network's chip checker confirmed this computer's chip (the helper
  /// sends the ticket to the network).
  static bool get hasChipTicket => !kIsWeb && File(_ticketFile).existsSync();

  // Ports of this computer's LockKeeper: different from the usual ones, so it
  // never collides with another LockKeeper on the same computer.
  /// How far back a new LockKeeper's trusted block is (state sync): before
  /// the network's last two copies of the state (every 120 blocks) and after
  /// the oldest whole block kept (480; light.go, statesync_fix.go).
  static const trustBack = 300;
  static const p2pPort = 27056;
  static const rpcPort = 27057;
  static const apiPort = 1717;
  static const grpcPort = 9490;
  // This computer's end of the direct connection to the network through
  // Cloudflare, when the LockKeeper joined from another home.
  static const doorPort = 27058;

  static String get _cloudflared => '${File(exe).parent.path}\\cloudflared.exe';
  static String get _doorFile => '$home\\door-p2p.txt';

  /// The LockKeeper on this PC was set up (the guarantee was sent).
  static bool get isSetUp => !kIsWeb && File(_readyFile).existsSync();

  /// Shortcut in the Windows Startup folder: when Windows starts it runs
  /// "lockkeeper pc-autostart", which starts the LockKeeper in the background
  /// without opening the app.
  static String get _startupLink =>
      '${Platform.environment['APPDATA']}\\Microsoft\\Windows\\Start Menu\\Programs\\Startup\\EncryptLock LockKeeper.lnk';

  /// The LockKeeper switches on by itself when Windows starts.
  static bool get autostart => _taskOn ?? File(_startupLink).existsSync();

  // From the Microsoft Store package (MSIX) a Startup-folder shortcut cannot
  // start the package's lockkeeper.exe: the package's own startup task does
  // (manifest TaskId "LockKeeperStart", windows/runner/startup_task.cpp).
  // null: not the package (the shortcut is used).
  static const _startup = MethodChannel('encryptlock/startup');
  static bool? _taskOn;

  /// Reads the package's startup task once, when the app opens.
  static Future<void> loadAutostart() async {
    try {
      final s = await _startup.invokeMethod<String>('state');
      _taskOn = s == null || s == 'unpackaged' || s == 'error' ? null : s.startsWith('enabled');
    } catch (_) {
      _taskOn = null; // no channel (tests, other systems)
    }
  }

  static Future<void> setAutostart(bool on) async {
    if (_taskOn != null) {
      final s = await _startup.invokeMethod<String>(on ? 'enable' : 'disable');
      _taskOn = s != null && s.startsWith('enabled');
      return;
    }
    final link = File(_startupLink);
    if (!on) {
      if (link.existsSync()) link.deleteSync();
      return;
    }
    String q(String s) => "'${s.replaceAll("'", "''")}'";
    final script = [
      '\$s = (New-Object -ComObject WScript.Shell).CreateShortcut(${q(link.path)})',
      '\$s.TargetPath = ${q(exe)}',
      '\$s.Arguments = ${q('pc-autostart --home "$home"')}',
      '\$s.WorkingDirectory = ${q(File(exe).parent.path)}',
      r'$s.WindowStyle = 7',
      r'$s.Save()',
    ].join('; ');
    await Process.run('powershell', ['-NoProfile', '-NonInteractive', '-Command', script]);
  }

  /// How many lockunit are 1 dollar at the reference price (as the network
  /// computes it, rounded up). Before the first trade there is no price and
  /// no network minimum: the app then asks for 1 LOCK.
  static BigInt minGuarantee(MarketPrice? price) {
    final p = price?.referenceUnits ?? BigInt.zero;
    if (p == BigInt.zero) return BigInt.from(100000000);
    // 1 USDC (10^6) · 10^11 / price, rounded up.
    final num = BigInt.from(1000000) * BigInt.from(100000000000);
    final need = (num + p - BigInt.one) ~/ p;
    // Never less than 0,01 LOCK (as the network requires).
    final floor = BigInt.from(1000000);
    return need < floor ? floor : need;
  }

  static Future<Map<String, dynamic>> _getJson(Uri uri) async {
    final client = HttpClient()..connectionTimeout = const Duration(seconds: 5);
    try {
      final req = await client.getUrl(uri);
      final res = await req.close().timeout(const Duration(seconds: 30));
      final body = await res.transform(utf8.decoder).join();
      if (res.statusCode != 200) throw HttpException('HTTP ${res.statusCode}');
      return jsonDecode(body) as Map<String, dynamic>;
    } finally {
      client.close();
    }
  }

  static Future<ProcessResult> _run(List<String> args) => Process.run(exe, args, runInShell: false);

  static void _setToml(String file, String section, String key, String value) {
    final lines = File(file).readAsLinesSync();
    var current = '';
    for (var i = 0; i < lines.length; i++) {
      final m = RegExp(r'^\[(.+)\]\s*$').firstMatch(lines[i]);
      if (m != null) {
        current = m.group(1)!;
      } else if (current == section && RegExp('^${RegExp.escape(key)}\\s*=').hasMatch(lines[i])) {
        lines[i] = '$key = $value';
      }
    }
    File(file).writeAsStringSync('${lines.join('\n')}\n');
  }

  /// Checks this computer's security chip (real computer, known maker, secret
  /// test) with the network's chip checker at [checker] (the network's REST
  /// address), for the helper [account]. Windows asks "Allow?" once: the chip
  /// only answers the secret test with administrator rights. Keeps the
  /// checker's ticket for the helper. Throws [ChipProblem]; returns the maker.
  static Future<String> checkChip({required String checker, required String account}) async {
    // This runs with administrator rights: only a plain web address and a
    // plain account go into the command (the address comes from the network
    // or the website, never trusted as text).
    if (!isSafeChecker(checker) || !RegExp(r'^lock1[02-9ac-hj-np-z]{38,90}$').hasMatch(account)) {
      throw StateError('invalid chip checker or account');
    }
    final out = File('${Directory.systemTemp.path}\\encryptlock-chip-${DateTime.now().millisecondsSinceEpoch}.json');
    final quoted = "'${exe.replaceAll("'", "''")}'";
    final r = await Process.run('powershell', [
      '-NoProfile',
      '-NonInteractive',
      '-Command',
      "Start-Process -FilePath $quoted -ArgumentList 'chip','--checker','$checker','--account','$account','--out','\"${out.path}\"' -Verb RunAs -Wait -WindowStyle Hidden",
    ]);
    if (!out.existsSync()) {
      debugPrint('chip check: ${r.stderr}');
      throw const ChipProblem(ChipProblemKind.cancelled);
    }
    final text = out.readAsStringSync();
    out.deleteSync();
    final res = jsonDecode(text) as Map<String, dynamic>;
    if (res['ok'] != true || res['ticket'] == null) {
      final error = '${res['error'] ?? ''}';
      debugPrint('chip check: $error');
      // The chip may be fine but the network's checker could not be reached.
      if (error.startsWith('chip checker:') || error.contains('connect')) throw StateError(error);
      throw ChipProblem(ChipProblemKind.notAccepted, error);
    }
    await Directory(home).create(recursive: true);
    File(_ticketFile).writeAsStringSync(text);
    return res['maker'] as String;
  }

  /// A chip checker address that is safe to put in a command: http(s), a host
  /// of letters, digits, dots and dashes, an optional port, nothing else.
  static bool isSafeChecker(String url) =>
      RegExp(r'^https?://[A-Za-z0-9.-]{1,253}(:[0-9]{1,5})?/?$').hasMatch(url);

  /// Prepares this computer's LockKeeper to join the network of [lockKeeperUrl]
  /// (the REST address the app uses; its RPC is on port 26657 of the same
  /// computer). Returns the base64 block-signing public key.
  static Future<String> prepare(String lockKeeperUrl, String chainId) async {
    final host = Uri.parse(lockKeeperUrl).host;
    var rpc = Uri(scheme: 'http', host: host, port: 26657);
    // From another home (the app talks to the network's public https door):
    // the RPC and the direct connection go through the door too.
    NetworkDoor? door;
    if (Uri.parse(lockKeeperUrl).scheme == 'https') {
      door = await networkDoorFromSite();
      if (door == null || door.rpc == null || door.p2pHost == null || !File(_cloudflared).existsSync()) {
        throw StateError('network door not available');
      }
      rpc = Uri.parse(door.rpc!);
    }
    final dir = Directory(home);
    if (!File('$home\\config\\priv_validator_key.json').existsSync()) {
      await dir.create(recursive: true);
      final r = await _run(['init', 'pc', '--chain-id', chainId, '--home', home]);
      if (r.exitCode != 0) throw StateError('init: ${r.stderr}');
    }
    // The network's genesis and the address to connect to.
    final genesis = (await _getJson(rpc.replace(path: '/genesis')))['result']['genesis'];
    NetworkPin.checkChainId('${(genesis as Map<String, dynamic>)['chain_id']}');
    await NetworkPin.checkGenesis(genesis);
    File('$home\\config\\genesis.json').writeAsStringSync(jsonEncode(genesis));
    final status = (await _getJson(rpc.replace(path: '/status')))['result'] as Map<String, dynamic>;
    final info = status['node_info'] as Map<String, dynamic>;
    final listen = Uri.parse((info['listen_addr'] as String).replaceFirst('tcp://', 'http://'));
    var peer = '${info['id']}@${host == 'localhost' ? '127.0.0.1' : host}:${listen.port}';
    final doorFile = File(_doorFile);
    if (door != null) {
      // The LockKeeper connects to this computer's end of the Cloudflare
      // connection, which start() opens.
      peer = '${info['id']}@127.0.0.1:$doorPort';
      doorFile.writeAsStringSync(door.p2pHost!);
    } else if (doorFile.existsSync()) {
      doorFile.deleteSync();
    }

    final cfg = '$home\\config\\config.toml';
    final app = '$home\\config\\app.toml';
    _setToml(cfg, 'rpc', 'laddr', '"tcp://127.0.0.1:$rpcPort"');
    _setToml(cfg, 'rpc', 'pprof_laddr', '""');
    _setToml(cfg, 'p2p', 'laddr', '"tcp://0.0.0.0:$p2pPort"');
    _setToml(cfg, 'p2p', 'persistent_peers', '"$peer"');
    _setToml(cfg, 'p2p', 'allow_duplicate_ip', 'true');
    _setToml(cfg, 'p2p', 'addr_book_strict', 'false');
    _setToml(app, 'api', 'enable', 'true');
    _setToml(app, 'api', 'address', '"tcp://127.0.0.1:$apiPort"');
    _setToml(app, 'grpc', 'address', '"localhost:$grpcPort"');

    // LockKeepers delete old blocks (only about 40 minutes are kept), so a new
    // one cannot replay the chain from the first block: it starts from the
    // copy of the network's state made every 10 minutes (state sync), checked
    // against this block.
    final sync = status['sync_info'] as Map<String, dynamic>;
    final earliest = int.parse('${sync['earliest_block_height']}');
    final latest = int.parse('${sync['latest_block_height']}');
    if (earliest > 1 && !Directory('$home\\data\\application.db').existsSync()) {
      // A copy is used only if it is newer than the trusted block: trust
      // a block older than the last two copies (120 blocks each), otherwise
      // the new LockKeeper waits for the next one.
      final trust = latest - trustBack > earliest ? latest - trustBack : earliest + 1;
      final block = await _getJson(rpc.replace(path: '/block', queryParameters: {'height': '$trust'}));
      final hash = block['result']['block_id']['hash'] as String;
      _setToml(cfg, 'statesync', 'enable', 'true');
      _setToml(cfg, 'statesync', 'rpc_servers', '"$rpc,$rpc"');
      _setToml(cfg, 'statesync', 'trust_height', '$trust');
      _setToml(cfg, 'statesync', 'trust_hash', '"$hash"');
      _setToml(cfg, 'statesync', 'trust_period', '"168h0m0s"');
    }

    final key = jsonDecode(File('$home\\config\\priv_validator_key.json').readAsStringSync()) as Map<String, dynamic>;
    return (key['pub_key'] as Map<String, dynamic>)['value'] as String;
  }

  /// The helper's 24 words (created the first time with [newWords]).
  static Future<String> helperWords(Future<String> Function() newWords) async {
    final f = File(_helperFile);
    if (f.existsSync()) return f.readAsStringSync().trim();
    final words = await newWords();
    await f.writeAsString(words);
    return words;
  }

  static void markSetUp() => File(_readyFile).writeAsStringSync(DateTime.now().toIso8601String());

  /// Starts the LockKeeper and the "I am on" program, unless already running.
  static Future<void> start() async {
    if (await isRunning()) return;
    await stop();
    final pids = <String, int>{};
    final doorFile = File(_doorFile);
    if (doorFile.existsSync()) {
      // Joined from another home: open this computer's end of the direct
      // connection first. The door's address changes when the founder's
      // computer restarts: take the newest one the website publishes.
      var host = doorFile.readAsStringSync().trim();
      try {
        final now = (await networkDoorFromSite())?.p2pHost;
        if (now != null && now != host) {
          host = now;
          doorFile.writeAsStringSync(host);
        }
      } catch (_) {}
      final bridge = await Process.start(_cloudflared, [
        'access', 'tcp', '--hostname', host, '--url', '127.0.0.1:$doorPort',
      ], mode: ProcessStartMode.detached);
      pids['door'] = bridge.pid;
    }
    final node = await Process.start(exe, ['start', '--home', home], mode: ProcessStartMode.detached);
    final beat = await Process.start(exe, [
      'pc-heartbeat', _helperFile, '--node', 'tcp://127.0.0.1:$rpcPort', '--home', home,
    ], mode: ProcessStartMode.detached);
    pids['node'] = node.pid;
    pids['heartbeat'] = beat.pid;
    File(_pidFile).writeAsStringSync(jsonEncode(pids));
    await _door(false);
  }

  static String _image(String key) => key == 'door' ? 'cloudflared.exe' : 'lockkeeper.exe';

  /// The founder's computer also opens the network's door to the outside
  /// (phones away from home, the website): porta-publica.txt holds the path
  /// of scripts\rede-publica.ps1. Nothing happens on anyone else's computer.
  static Future<void> _door(bool stop) async {
    final f = File('$home\\porta-publica.txt');
    if (!f.existsSync()) return;
    final script = f.readAsStringSync().trim();
    if (stop) {
      await Process.run('powershell', ['-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', script, '-Parar']);
    } else {
      // PowerShell needs a (hidden) console: a detached start would fail.
      final q = script.replaceAll("'", "''");
      await Process.run('powershell', [
        '-NoProfile',
        '-Command',
        "Start-Process powershell -WindowStyle Hidden -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-File','\"$q\"'",
      ]);
    }
  }

  /// Stops both programs. Nothing is lost: the guarantee stays, the
  /// LockKeeper only stops earning until it is on again.
  static Future<void> stop() async {
    final f = File(_pidFile);
    if (!f.existsSync()) return;
    final pids = jsonDecode(f.readAsStringSync()) as Map<String, dynamic>;
    // Only if that id is still our program: after Windows restarts an old
    // id can belong to any other program.
    for (final e in pids.entries) {
      await Process.run('taskkill', ['/F', '/FI', 'PID eq ${e.value}', '/FI', 'IMAGENAME eq ${_image(e.key)}']);
    }
    f.deleteSync();
    await _door(true);
  }

  /// Both programs are running.
  static Future<bool> isRunning() async {
    final f = File(_pidFile);
    if (!f.existsSync()) return false;
    final pids = jsonDecode(f.readAsStringSync()) as Map<String, dynamic>;
    for (final e in pids.entries) {
      final r = await Process.run('tasklist', ['/FI', 'PID eq ${e.value}', '/NH']);
      if (!'${r.stdout}'.toLowerCase().contains(_image(e.key))) return false;
    }
    return true;
  }

  /// The newest block this computer's LockKeeper has (null if it does not
  /// answer).
  static Future<int?> localHeight() async {
    try {
      final s = await _getJson(Uri(scheme: 'http', host: '127.0.0.1', port: rpcPort, path: '/status'));
      return int.tryParse('${s['result']['sync_info']['latest_block_height']}');
    } catch (_) {
      return null;
    }
  }

  /// This computer's LockKeeper has caught up with the network.
  static Future<bool> isSynced() async {
    try {
      final s = await _getJson(Uri(scheme: 'http', host: '127.0.0.1', port: rpcPort, path: '/status'));
      return (s['result']['sync_info']['catching_up'] as bool) == false;
    } catch (_) {
      return false;
    }
  }
}

enum ChipProblemKind { cancelled, notAccepted }

/// Why this computer cannot become a LockKeeper: Windows' "Allow?" was
/// refused, or the chip is missing or not accepted.
class ChipProblem implements Exception {
  const ChipProblem(this.kind, [this.detail = '']);

  final ChipProblemKind kind;

  /// What the chip test said (shown with the message, so the reason is
  /// known without any other test).
  final String detail;
}
