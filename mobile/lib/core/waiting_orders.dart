/// A Buy / Sell asked while no LockKeeper of the blockchain could be reached.
/// It is kept on the device and done by itself as soon as one answers (with
/// the wallet open); it can be canceled until then.
class WaitingOrder {
  WaitingOrder({
    required this.id,
    required this.newOffer,
    required this.sell,
    required this.lock,
    required this.usdc,
    this.expiresAt = 0,
    this.offerId = 0,
  });

  WaitingOrder.fromJson(Map<String, dynamic> j)
    : id = '${j['id']}',
      newOffer = j['newOffer'] == true,
      sell = j['sell'] == true,
      lock = BigInt.parse('${j['lock']}'),
      usdc = BigInt.parse('${j['usdc']}'),
      expiresAt = j['expiresAt'] as int? ?? 0,
      offerId = j['offerId'] as int? ?? 0;

  final String id;

  /// True: publish a new offer. False: take the existing offer [offerId].
  final bool newOffer;

  /// What this wallet does: sell LOCK (true) or buy LOCK (false).
  final bool sell;

  /// LOCK in lockunit; USDC in units of 6 decimals.
  final BigInt lock;
  final BigInt usdc;

  /// New offer: when it ends (Unix s; 0 = until canceled).
  final int expiresAt;

  /// Taking an offer: its id. It is only taken if it is still there with the
  /// same LOCK and USDC.
  final int offerId;

  Map<String, dynamic> toJson() => {
    'id': id,
    'newOffer': newOffer,
    'sell': sell,
    'lock': '$lock',
    'usdc': '$usdc',
    'expiresAt': expiresAt,
    'offerId': offerId,
  };
}

/// What happened to a waiting order once a LockKeeper answered.
enum WaitingResult { done, offerGone, failed }

class WaitingOutcome {
  const WaitingOutcome(this.order, this.result, [this.problem]);

  final WaitingOrder order;
  final WaitingResult result;

  /// Why it failed (a BuyProblem, a ChainError or another error).
  final Object? problem;
}

/// No LockKeeper of the blockchain answers right now (nothing was sent).
class NotReachable implements Exception {
  const NotReachable();
}
