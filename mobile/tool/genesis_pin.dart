// Prints the two codes that fix the real network inside the final app:
//   dart run tool/genesis_pin.dart http://<real LockKeeper>:<rpc port>
// Use the LockKeeper address, not a genesis.json file: the app checks the
// genesis as LockKeepers send it, which is written a little differently.
// then build with --dart-define=CHAIN_ID=... --dart-define=GENESIS_HASH=...
// (scripts\app-windows.ps1 / app-android.ps1 -ChainId ... -GenesisHash ...).
import 'dart:convert';
import 'dart:io';

import 'package:encryptlock_wallet/core/network_pin.dart';

Future<void> main(List<String> args) async {
  if (args.length != 1) {
    stderr.writeln('usage: dart run tool/genesis_pin.dart <genesis.json | http://host:26657>');
    exit(2);
  }
  Object? genesis;
  if (args[0].startsWith('http')) {
    final client = HttpClient();
    final req = await client.getUrl(Uri.parse(args[0]).replace(path: '/genesis'));
    final res = await req.close();
    genesis = (jsonDecode(await res.transform(utf8.decoder).join()) as Map)['result']['genesis'];
    client.close();
  } else {
    genesis = jsonDecode(File(args[0]).readAsStringSync());
  }
  stdout.writeln('CHAIN_ID=${(genesis as Map)['chain_id']}');
  stdout.writeln('GENESIS_HASH=${await NetworkPin.hashGenesis(genesis)}');
}
