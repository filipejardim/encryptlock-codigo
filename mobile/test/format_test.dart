import 'package:encryptlock_wallet/screens/home.dart' show formatLock;
import 'package:flutter_test/flutter_test.dart';
import 'package:intl/date_symbol_data_local.dart';

void main() {
  setUpAll(() => initializeDateFormatting());
  final lock = BigInt.from(100000000);

  test('two decimals in each language style', () {
    expect(formatLock(lock * BigInt.from(1000), 'pt'), '1.000,00');
    expect(formatLock(lock * BigInt.from(1000), 'en'), '1,000.00');
  });

  test('small amounts keep the decimals they need', () {
    expect(formatLock(BigInt.from(100000), 'pt'), '0,001');
    expect(formatLock(BigInt.from(1), 'pt'), '0,00');
  });
}
