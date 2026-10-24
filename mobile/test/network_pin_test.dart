import 'package:encryptlock_wallet/core/network_pin.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('the genesis code does not depend on key order or spacing', () async {
    final a = await NetworkPin.hashGenesis({
      'chain_id': 'encryptlock-1',
      'app_state': {'bank': {'supply': [], 'params': {'x': '1'}}, 'auth': {}},
      'initial_height': '1',
    });
    final b = await NetworkPin.hashGenesis({
      'initial_height': '1',
      'app_state': {'auth': {}, 'bank': {'params': {'x': '1'}, 'supply': []}},
      'chain_id': 'encryptlock-1',
    });
    expect(a, b);
    expect(a, hasLength(64));
  });

  test('any change to the genesis changes the code', () async {
    final a = await NetworkPin.hashGenesis({'chain_id': 'encryptlock-1', 'founder': '400000'});
    final b = await NetworkPin.hashGenesis({'chain_id': 'encryptlock-1', 'founder': '400001'});
    expect(a, isNot(b));
  });

  test('test versions follow any network', () async {
    // Built without CHAIN_ID / GENESIS_HASH (as in tests): nothing is refused.
    NetworkPin.checkChainId('encryptlock-test-anything');
    await NetworkPin.checkGenesis({'chain_id': 'other'});
  });
}
