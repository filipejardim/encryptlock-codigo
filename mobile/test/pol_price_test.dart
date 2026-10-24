import 'package:encryptlock_wallet/app_state.dart';
import 'package:encryptlock_wallet/core/polygon.dart';
import 'package:encryptlock_wallet/screens/home.dart';
import 'package:encryptlock_wallet/widgets/coin_carousel.dart';
import 'package:flutter_test/flutter_test.dart';

/// POL shows its value in the chosen currency, like LOCK and USDC.
void main() {
  test('POL balance in euros and dollars at the exchanges price', () {
    final pol = Coin.all.firstWhere((c) => c.ticker == 'POL');
    final state = AppState()
      ..polygon = PolygonBalances(BigInt.from(10) * BigInt.from(10).pow(18), BigInt.zero)
      ..usdPerEur = 1.12;
    expect(fiatText(state, pol, null, 'pt_PT'), '—'); // no price yet
    state.usdPerPolPrice = 0.0945;
    state.currency = FiatCurrency.byCode('USD');
    expect(fiatText(state, pol, null, 'en'), '0.95'); // 10 POL × 0,0945 $
    state.currency = FiatCurrency.byCode('EUR');
    expect(fiatText(state, pol, null, 'en'), '0.84'); // ÷ 1,12
  });
}
