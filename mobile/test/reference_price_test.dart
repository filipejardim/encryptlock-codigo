import 'package:encryptlock_wallet/core/market.dart';
import 'package:encryptlock_wallet/core/pc_lockkeeper.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  Map<String, dynamic> answer({String? median, Map<String, dynamic>? last}) => {
    'last': last,
    'day_lock': last == null ? '0' : last['lock'],
    'day_usdc': last == null ? '0' : last['usdc'],
    'recent': const [],
    'day_median': ?median,
  };

  test('balances are valued at the 24 h median, else the last trade', () {
    // 4 LOCK for 2 USDC = 0,50 $; median of the day 0,25 $.
    final last = {'trade_id': '1', 'lock': '400000000', 'usdc': '2000000', 'time': '1'};
    final withMedian = parsePrice(answer(median: '250000000', last: last));
    expect(withMedian.reference, 0.25);
    expect(withMedian.dayMedian, 0.25);
    // No trade in 24 hours: the last trade, as the network does.
    final noMedian = parsePrice(answer(median: '0', last: last));
    expect(noMedian.reference, 0.5);
    expect(noMedian.dayMedian, isNull);
    // An older LockKeeper without the median: the last trade too.
    expect(parsePrice(answer(last: last)).reference, 0.5);
    // No trade ever: no price.
    expect(parsePrice(answer()).reference, isNull);
  });

  test('the LockKeeper guarantee asks what the network asks: 1 dollar at the reference price, rounded up', () {
    final last = {'trade_id': '1', 'lock': '400000000', 'usdc': '2000000', 'time': '1'};
    // Median 0,25 $: 1 dollar = 4 LOCK.
    expect(PcLockKeeper.minGuarantee(parsePrice(answer(median: '250000000', last: last))), BigInt.from(400000000));
    // Last trade 0,50 $: 2 LOCK.
    expect(PcLockKeeper.minGuarantee(parsePrice(answer(median: '0', last: last))), BigInt.from(200000000));
    // 3 $ per LOCK: 0,333… LOCK, rounded up.
    expect(PcLockKeeper.minGuarantee(parsePrice(answer(median: '3000000000', last: last))), BigInt.from(33333334));
    // Very expensive: never less than 0,01 LOCK.
    expect(PcLockKeeper.minGuarantee(parsePrice(answer(median: '900000000000', last: last))), BigInt.from(1000000));
    // No price yet: the app asks for 1 LOCK.
    expect(PcLockKeeper.minGuarantee(null), BigInt.from(100000000));
  });

  test('a purchase is paid only if the trade has the LOCK, USDC and seller of the offer', () {
    final offer = MarketOffer.fromJson({'id': '7', 'side': 'SIDE_SELL', 'lock': '100000000', 'usdc': '10000000', 'polygon': '0xAbC0000000000000000000000000000000000001'});
    MarketTrade trade({String usdc = '10000000', String lock = '100000000', String seller = '0xabc0000000000000000000000000000000000001', String offer = '7'}) =>
        MarketTrade.fromJson({'id': '1', 'offer_id': offer, 'seller_polygon': seller, 'lock': lock, 'usdc': usdc, 'hash': '', 'pay_by': '1'});
    expect(sameTerms(offer, trade()), isTrue);
    expect(sameTerms(offer, trade(usdc: '20000000')), isFalse); // more dollars
    expect(sameTerms(offer, trade(lock: '50000000')), isFalse); // fewer LOCK
    expect(sameTerms(offer, trade(seller: '0x9990000000000000000000000000000000000009')), isFalse); // someone else
    expect(sameTerms(offer, trade(offer: '8')), isFalse);
  });
}
