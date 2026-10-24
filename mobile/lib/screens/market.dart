import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:intl/intl.dart';

import '../app_state.dart';
import '../core/market.dart';
import '../core/network.dart' show ChainError;
import '../core/polygon.dart' show BuyProblem, BuyProblemKind;
import '../core/waiting_orders.dart';
import '../l10n/gen/app_localizations.dart';
import '../theme.dart';
import '../widgets/brand.dart';
import '../widgets/candles.dart';
import 'chart.dart' show MarketChart;
import 'home.dart' show KeepersLine, formatLock;
import 'security.dart' show chainErrorText, confirmPassword;

/// Dollars with 2 decimals, more when a small price needs them (0,0004 $).
String formatUsd(double v, String locale) => '${NumberFormat('#,##0.00####', locale).format(v)} \$';

/// Price shown in dollars and, when the chosen currency is not the dollar,
/// in that currency too: "0,04 $ · ≈ 0,037 €".
String priceText(AppState state, double usd, String locale) {
  final main = formatUsd(usd, locale);
  if (state.currency.code == 'USD') return main;
  final other = state.fiat(usd);
  return other == null
      ? main
      : '$main · ≈ ${NumberFormat('#,##0.00####', locale).format(other)} ${state.currency.symbol}';
}

/// Small strip on the wallet: the price and a Trade button. Opens the market.
class MarketStrip extends StatelessWidget {
  const MarketStrip({super.key});

  static final _rising = risingCandles();

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final locale = Localizations.localeOf(context).toString();
    final last = state.price?.last;
    final open = [
      for (final o in state.offers)
        if (o.tradeId == 0) o,
    ];
    final sells = [
      for (final o in open)
        if (o.sell) o.price,
    ]..sort();
    final buys = [
      for (final o in open)
        if (!o.sell) o.price,
    ]..sort();
    // Line 2: the offers waiting for someone (best for a buyer: the cheapest
    // sale; best for a seller: the highest purchase) and how many there are.
    final hint = [
      if (sells.isNotEmpty) t.bestSell(formatUsd(sells.first, locale)),
      if (buys.isNotEmpty) t.bestBuy(formatUsd(buys.last, locale)),
      if (open.isNotEmpty) t.offersCount('${open.length}'),
    ].join(' · ');
    final phone = MediaQuery.sizeOf(context).width < 600;
    void openMarket() => Navigator.of(context).push(MaterialPageRoute(builder: (_) => const MarketScreen()));
    return NeonCard(
      padding: EdgeInsets.zero,
      child: InkWell(
        onTap: openMarket,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(12, 10, 10, 10),
          child: Row(
            children: [
              // A small night-time trading chart: candles going up, side
              // by side. Only decoration, not real prices.
              Container(
                // Smaller on a phone, where the strip is narrow.
                width: phone ? 42 : 64,
                height: phone ? 34 : 44,
                padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 3),
                decoration: BoxDecoration(
                  color: const Color(0xFF060B0D),
                  border: Border.all(color: Brand.cardBorder),
                  borderRadius: BorderRadius.circular(4),
                ),
                child: CustomPaint(painter: CandlesPainter(_rising, body: 0.72)),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    // Line 1: the last trade actually made (the real price).
                    Text(
                      last == null
                          ? t.noPriceYet
                          : t.lastTrade(
                              formatLock(last.lock, locale),
                              formatUsd(usdcToDouble(last.usdc), locale),
                              priceText(state, last.price, locale),
                            ),
                      maxLines: 2,
                      style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 14),
                    ),
                    Text(
                      hint.isEmpty ? t.noOffers : hint,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(color: Brand.pearl, fontSize: 12),
                    ),
                  ],
                ),
              ),
              const SizedBox(width: 8),
              FilledButton.icon(
                onPressed: openMarket,
                style: FilledButton.styleFrom(
                  backgroundColor: Brand.neonGreen,
                  foregroundColor: Brand.bottomBar,
                  padding: const EdgeInsets.symmetric(horizontal: 14),
                  visualDensity: VisualDensity.compact,
                ),
                icon: const Icon(Icons.swap_horiz, size: 18),
                label: Text(t.trade, style: const TextStyle(fontWeight: FontWeight.w800)),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// How to buy EncryptLock, step by step: USDC on the Polygon network and a
/// little POL, both in this wallet. On the market and on the USDC/POL coins.
class HowToBuyCard extends StatelessWidget {
  const HowToBuyCard({super.key});

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    return NeonCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            '🛒 ${t.buyHowTitle}',
            style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w700, color: Brand.neonGreen),
          ),
          for (final (i, step) in [t.buyHowStep1, t.buyHowStep2, t.buyHowStep3].indexed)
            Padding(
              padding: const EdgeInsets.only(top: 10),
              child: Text('${i + 1}. $step', style: const TextStyle(height: 1.45)),
            ),
        ],
      ),
    );
  }
}

/// The market: price, chart, offers to sell and to buy, and placing offers.
class MarketScreen extends StatefulWidget {
  const MarketScreen({super.key});

  @override
  State<MarketScreen> createState() => _MarketScreenState();
}

class _MarketScreenState extends State<MarketScreen> {
  int? _busy;

  /// My buy offers someone sold to, waiting for my payment.
  List<MarketTrade> _toPay = const [];

  Future<void> _loadToPay() async {
    try {
      final list = await AppScope.of(context).tradesToPay();
      if (mounted) setState(() => _toPay = list);
    } catch (_) {}
  }

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) {
        AppScope.of(context).refreshMarket();
        _loadToPay();
      }
    });
  }

  /// No LockKeeper of the blockchain answers: the Buy / Sell waits on this
  /// device and is done by itself as soon as one does.
  Future<void> _wait(WaitingOrder order) async {
    final t = AppLocalizations.of(context);
    final messenger = ScaffoldMessenger.of(context);
    await AppScope.of(context).addWaiting(order);
    messenger.hideCurrentSnackBar();
    messenger.showSnackBar(SnackBar(content: Text('⏳ ${t.orderWaiting}'), duration: const Duration(seconds: 10)));
  }

  static String _newId() => '${DateTime.now().microsecondsSinceEpoch}';

  Future<void> _run(Future<void> Function() action, String done, [int? id, WaitingOrder? waiting]) async {
    final t = AppLocalizations.of(context);
    final messenger = ScaffoldMessenger.of(context);
    setState(() => _busy = id ?? -1);
    try {
      await action();
      messenger.showSnackBar(SnackBar(content: Text(done)));
    } on NotReachable {
      if (waiting != null && mounted) {
        await _wait(waiting);
      } else {
        messenger.showSnackBar(SnackBar(content: Text(t.networkError)));
      }
    } on BuyProblem catch (e) {
      messenger.showSnackBar(
        SnackBar(
          content: Text(switch (e.kind) {
            BuyProblemKind.noUsdc => t.buyNoUsdc,
            BuyProblemKind.noPol || BuyProblemKind.noPolAmount => t.buyNoPol,
            BuyProblemKind.notReady => t.buyNotReady,
            BuyProblemKind.wrongNetwork => t.buyWrongNetwork,
          }),
        ),
      );
    } on ChainError catch (e) {
      messenger.showSnackBar(SnackBar(content: Text(chainErrorText(t, e.log))));
    } catch (e) {
      debugPrint('market action failed: $e');
      messenger.showSnackBar(SnackBar(content: Text(t.networkError)));
    } finally {
      if (mounted) setState(() => _busy = null);
    }
  }

  /// "Buy": confirm, then reserve the offer and pay it on Polygon from this
  /// wallet; the LOCK arrive when the blockchain's LockKeepers see the payment.
  Future<void> _buy(MarketOffer o) async {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final locale = Localizations.localeOf(context).toString();
    final messenger = ScaffoldMessenger.of(context);
    final sure = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        backgroundColor: Brand.bottomBar,
        title: Text(t.buyLock),
        content: Text(
          t.buyConfirm(formatLock(o.lock, locale), formatUsd(usdcToDouble(o.usdc), locale)),
          style: const TextStyle(height: 1.4),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(dialogContext, false), child: Text(t.cancel)),
          TextButton(onPressed: () => Navigator.pop(dialogContext, true), child: Text(t.confirm)),
        ],
      ),
    );
    if (sure != true || !mounted) return;
    if (!await confirmPassword(context) || !mounted) return;
    setState(() => _busy = o.id);
    messenger.showSnackBar(SnackBar(content: Text(t.buyPaying), duration: const Duration(seconds: 20)));
    try {
      await state.buy(o);
      messenger.hideCurrentSnackBar();
      messenger.showSnackBar(SnackBar(content: Text(t.buyDone)));
    } on NotReachable {
      if (mounted) {
        await _wait(WaitingOrder(id: _newId(), newOffer: false, sell: false, lock: o.lock, usdc: o.usdc, offerId: o.id));
      }
    } on BuyProblem catch (e) {
      messenger.hideCurrentSnackBar();
      messenger.showSnackBar(
        SnackBar(
          content: Text(switch (e.kind) {
            BuyProblemKind.noUsdc => t.buyNoUsdc,
            BuyProblemKind.noPol || BuyProblemKind.noPolAmount => t.buyNoPol,
            BuyProblemKind.notReady => t.buyNotReady,
            BuyProblemKind.wrongNetwork => t.buyWrongNetwork,
          }),
        ),
      );
    } on ChainError catch (e) {
      messenger.hideCurrentSnackBar();
      messenger.showSnackBar(SnackBar(content: Text(chainErrorText(t, e.log))));
    } catch (e) {
      debugPrint('buy failed: $e');
      messenger.hideCurrentSnackBar();
      messenger.showSnackBar(SnackBar(content: Text(t.networkError)));
    } finally {
      if (mounted) setState(() => _busy = null);
    }
  }

  /// "Sell" to a buy offer: the LOCK wait in the market until the buyer pays.
  Future<void> _sell(MarketOffer o) async {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final locale = Localizations.localeOf(context).toString();
    final sure = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        backgroundColor: Brand.bottomBar,
        title: Text(t.sellLock),
        content: Text(
          t.sellConfirm(formatLock(o.lock, locale), formatUsd(usdcToDouble(o.usdc), locale)),
          style: const TextStyle(height: 1.4),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(dialogContext, false), child: Text(t.cancel)),
          TextButton(onPressed: () => Navigator.pop(dialogContext, true), child: Text(t.confirm)),
        ],
      ),
    );
    if (sure != true || !mounted) return;
    if (!await confirmPassword(context) || !mounted) return;
    await _run(
      () => state.sellTo(o),
      t.sellDone,
      o.id,
      WaitingOrder(id: _newId(), newOffer: false, sell: true, lock: o.lock, usdc: o.usdc, offerId: o.id),
    );
  }

  /// Pays on Polygon a buy offer of mine that someone sold to.
  Future<void> _pay(MarketTrade trade) async {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final messenger = ScaffoldMessenger.of(context);
    if (!await confirmPassword(context) || !mounted) return;
    setState(() => _busy = -trade.id);
    messenger.showSnackBar(SnackBar(content: Text(t.buyPaying), duration: const Duration(seconds: 20)));
    try {
      await state.payTrade(trade);
      messenger.hideCurrentSnackBar();
      messenger.showSnackBar(SnackBar(content: Text(t.buyDone)));
    } on BuyProblem catch (e) {
      messenger.hideCurrentSnackBar();
      messenger.showSnackBar(
        SnackBar(
          content: Text(switch (e.kind) {
            BuyProblemKind.noUsdc => t.buyNoUsdc,
            BuyProblemKind.noPol || BuyProblemKind.noPolAmount => t.buyNoPol,
            BuyProblemKind.notReady => t.buyNotReady,
            BuyProblemKind.wrongNetwork => t.buyWrongNetwork,
          }),
        ),
      );
    } on ChainError catch (e) {
      messenger.hideCurrentSnackBar();
      messenger.showSnackBar(SnackBar(content: Text(chainErrorText(t, e.log))));
    } catch (e) {
      debugPrint('pay failed: $e');
      messenger.hideCurrentSnackBar();
      messenger.showSnackBar(SnackBar(content: Text(t.networkError)));
    } finally {
      if (mounted) setState(() => _busy = null);
      await _loadToPay();
    }
  }

  Future<void> _newOffer(bool sell) async {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final offer = await showDialog<_Draft>(
      context: context,
      builder: (_) => _OfferDialog(sell: sell),
    );
    if (offer == null || !mounted) return;
    final locale = Localizations.localeOf(context).toString();
    final lockText = formatLock(offer.lock, locale);
    final usdText = NumberFormat('#,##0.00', locale).format(usdcToDouble(offer.usdc));
    final sure = await showDialog<bool>(
      context: context,
      builder: (c) => AlertDialog(
        backgroundColor: Brand.bottomBar,
        title: Text(sell ? t.sellLock : t.buyLock),
        content: Text(sell ? t.confirmSell(lockText, usdText) : t.confirmBuy(usdText, lockText)),
        actions: [
          TextButton(onPressed: () => Navigator.pop(c, false), child: Text(t.cancel)),
          TextButton(onPressed: () => Navigator.pop(c, true), child: Text(t.confirm)),
        ],
      ),
    );
    if (sure != true || !mounted) return;
    await _run(
      () => state.placeOffer(sell: sell, lock: offer.lock, usdc: offer.usdc, expiresAt: offer.expiresAt),
      t.offerPlaced,
      null,
      WaitingOrder(id: _newId(), newOffer: true, sell: sell, lock: offer.lock, usdc: offer.usdc, expiresAt: offer.expiresAt),
    );
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final locale = Localizations.localeOf(context).toString();
    final price = state.price;
    final last = price?.last;
    final sells = [
      // A sell offer waiting for its second device shows only to its maker.
      for (final o in state.offers)
        if (o.sell && (!o.awaitingApproval || o.maker == state.address)) o,
    ]..sort((a, b) => a.price.compareTo(b.price));
    final buys = [
      for (final o in state.offers)
        if (!o.sell) o,
    ]..sort((a, b) => b.price.compareTo(a.price));

    Widget offerRow(MarketOffer o) {
      final mine = o.maker == state.address;
      final Widget action;
      if (o.tradeId != 0) {
        action = Text(
          t.inTrade,
          style: const TextStyle(color: Brand.neonBlue, fontWeight: FontWeight.w700),
        );
      } else if (mine) {
        action = TextButton(
          onPressed: _busy != null ? null : () => _run(() => state.cancelOffer(o.id), t.offerCanceled, o.id),
          child: Text(t.cancel, style: const TextStyle(color: Brand.danger)),
        );
      } else {
        action = _busy == o.id
            ? const SizedBox(width: 22, height: 22, child: CircularProgressIndicator(strokeWidth: 2))
            : OutlinedButton(
                onPressed: _busy != null
                    ? null
                    : o.sell
                    ? () => _buy(o)
                    : () => _sell(o),
                child: Text(o.sell ? t.buyLock : t.sellLock),
              );
      }
      return Padding(
        padding: const EdgeInsets.symmetric(vertical: 6),
        child: Row(
          children: [
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    '${formatLock(o.lock, locale)} LOCK  ·  ${formatUsd(usdcToDouble(o.usdc), locale)}',
                    style: const TextStyle(fontWeight: FontWeight.w700),
                  ),
                  Text(
                    [
                      t.priceEach(formatUsd(o.price, locale)),
                      if (mine) t.yourOffer,
                      if (mine && o.awaitingApproval) '⏳ ${t.offerAwaitingDevice}',
                    ].join(' · '),
                    style: TextStyle(color: mine ? Brand.neonGreen : Brand.pearl, fontSize: 12),
                  ),
                ],
              ),
            ),
            const SizedBox(width: 8),
            // Shrinks a little in a narrow column instead of running out of it.
            Flexible(child: FittedBox(fit: BoxFit.scaleDown, alignment: Alignment.centerRight, child: action)),
          ],
        ),
      );
    }

    Widget section(String title, List<MarketOffer> list) => NeonCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(title, style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w700)),
          const SizedBox(height: 6),
          if (list.isEmpty)
            Text(t.noOffers, style: const TextStyle(color: Brand.pearl))
          else
            for (final o in list) offerRow(o),
        ],
      ),
    );

    // Computer window (wide and tall enough): nothing hidden. The top half is
    // the price and the chart from one side to the other; below, the buttons
    // and the bottom half for the sell and buy offers side by side (only that
    // part scrolls when there are many). Phone or small window: one column.
    final screen = MediaQuery.sizeOf(context);
    final desk = screen.width >= 700 && screen.height >= 520;

    final header = [
      ShaderMask(
        shaderCallback: (r) => Brand.blueGradient.createShader(r),
        child: Text(
          last == null ? t.noPriceYet : t.perLock(formatUsd(last.price, locale)),
          textAlign: TextAlign.center,
          style: TextStyle(fontSize: desk ? 22 : 26, fontWeight: FontWeight.w800, color: Colors.white),
        ),
      ),
      Text(
        [
          if (last != null && state.currency.code != 'USD' && state.fiat(last.price) != null)
            '≈ ${NumberFormat('#,##0.00####', locale).format(state.fiat(last.price))} ${state.currency.symbol}',
          if (price?.dayMedian != null) t.median24h(formatUsd(price!.dayMedian!, locale)),
        ].join('   ·   '),
        textAlign: TextAlign.center,
        style: const TextStyle(color: Brand.pearl),
      ),
    ];

    final warning = Text(
      '🛡️ ${t.marketWarning}',
      textAlign: TextAlign.center,
      style: const TextStyle(color: Brand.neonGreen, fontWeight: FontWeight.w600),
    );

    final buttons = Row(
      children: [
        Expanded(
          child: NeonButton(
            label: t.sellLock,
            icon: Icons.north_east,
            onPressed: _busy != null ? null : () => _newOffer(true),
          ),
        ),
        const SizedBox(width: 12),
        Expanded(
          child: NeonButton(
            label: t.buyLock,
            icon: Icons.south_west,
            secondary: true,
            onPressed: _busy != null ? null : () => _newOffer(false),
          ),
        ),
      ],
    );

    // Someone sold to my buy offer: pay now on Polygon. What is missing to
    // pay shows right away, before pressing the button.
    final wallet = state.polygon;
    final owed = _toPay.fold(BigInt.zero, (sum, tr) => sum + tr.usdc);
    final missing = wallet == null
        ? null
        : wallet.usdc < owed
        ? t.buyNoUsdc
        : wallet.pol == BigInt.zero
        ? t.buyNoPol
        : null;
    final toPay = NeonCard(
      glowColor: Brand.neonGreen,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text('💳 ${t.toPayTitle}', style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w700)),
          if (missing != null)
            Padding(
              padding: const EdgeInsets.only(top: 6),
              child: Text('⚠️ $missing', style: const TextStyle(color: Brand.danger, height: 1.4)),
            ),
          for (final tr in _toPay)
            Padding(
              padding: const EdgeInsets.only(top: 10),
              child: Row(
                children: [
                  Expanded(
                    child: Text(
                      '${formatLock(tr.lock, locale)} LOCK  ·  ${formatUsd(usdcToDouble(tr.usdc), locale)}',
                      style: const TextStyle(fontWeight: FontWeight.w700),
                    ),
                  ),
                  _busy == -tr.id
                      ? const SizedBox(width: 22, height: 22, child: CircularProgressIndicator(strokeWidth: 2))
                      : FilledButton(onPressed: _busy != null ? null : () => _pay(tr), child: Text(t.payNow)),
                ],
              ),
            ),
        ],
      ),
    );
    // No USDC yet: how to buy EncryptLock, step by step.
    final noUsdc = (state.polygon?.usdc ?? BigInt.zero) == BigInt.zero;

    // Buy / Sell asked while no LockKeeper of the blockchain answered: done by
    // themselves as soon as one does; can be canceled until then.
    final waiting = NeonCard(
      glowColor: Brand.neonBlue,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text('⏳ ${t.waitingTitle}', style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w700)),
          for (final w in state.waitingOrders)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Row(
                children: [
                  Expanded(
                    child: Text(
                      (w.sell ? t.waitingSell : t.waitingBuy)(formatLock(w.lock, locale), formatUsd(usdcToDouble(w.usdc), locale)),
                      style: const TextStyle(fontWeight: FontWeight.w700),
                    ),
                  ),
                  TextButton(
                    onPressed: () => state.cancelWaiting(w.id),
                    child: Text(t.cancel, style: const TextStyle(color: Brand.danger)),
                  ),
                ],
              ),
            ),
        ],
      ),
    );
    // LockKeepers of the blockchain on now, or that none answers (Buy / Sell wait).
    final offline = [const SizedBox(height: 6), KeepersLine(state: state)];

    Widget body;
    if (desk) {
      body = LayoutBuilder(
        builder: (context, box) => Padding(
          padding: const EdgeInsets.fromLTRB(16, 12, 16, 0),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              SizedBox(
                height: box.maxHeight * 0.5,
                child: NeonCard(
                  padding: const EdgeInsets.fromLTRB(16, 12, 16, 12),
                  child: Column(
                    children: [
                      ...header,
                      if (last != null) ...[
                        const SizedBox(height: 8),
                        const Expanded(child: MarketChart(fill: true)),
                      ],
                    ],
                  ),
                ),
              ),
              ...offline,
              const SizedBox(height: 8),
              warning,
              const SizedBox(height: 8),
              buttons,
              const SizedBox(height: 12),
              Expanded(
                child: RefreshIndicator(
                  onRefresh: state.refreshMarket,
                  child: ListView(
                    padding: const EdgeInsets.only(bottom: 16),
                    children: [
                      if (state.waitingOrders.isNotEmpty) ...[waiting, const SizedBox(height: 12)],
                      if (_toPay.isNotEmpty) ...[toPay, const SizedBox(height: 12)],
                      Row(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Expanded(child: section(t.sellOffers, sells)),
                          const SizedBox(width: 16),
                          Expanded(child: section(t.buyOffers, buys)),
                        ],
                      ),
                      if (noUsdc) ...[const SizedBox(height: 12), const HowToBuyCard()],
                    ],
                  ),
                ),
              ),
            ],
          ),
        ),
      );
    } else {
      body = Align(
        alignment: Alignment.topCenter,
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 820),
          child: RefreshIndicator(
            onRefresh: state.refreshMarket,
            child: ListView(
              padding: const EdgeInsets.all(20),
              children: [
                NeonCard(
                  child: Column(
                    children: [
                      ...header,
                      if (last != null) ...[const SizedBox(height: 12), const MarketChart()],
                    ],
                  ),
                ),
                ...offline,
                const SizedBox(height: 12),
                warning,
                const SizedBox(height: 14),
                buttons,
                if (state.waitingOrders.isNotEmpty) ...[const SizedBox(height: 18), waiting],
                if (noUsdc) ...[const SizedBox(height: 18), const HowToBuyCard()],
                if (_toPay.isNotEmpty) ...[const SizedBox(height: 18), toPay],
                const SizedBox(height: 18),
                section(t.sellOffers, sells),
                const SizedBox(height: 16),
                section(t.buyOffers, buys),
              ],
            ),
          ),
        ),
      );
    }

    return Scaffold(
      appBar: BrandBar(title: '${t.market} · TRADE', lit: true, background: const RunningCandles()),
      body: ChipBackground(child: body),
    );
  }
}

class _Draft {
  const _Draft(this.lock, this.usdc, this.expiresAt);

  final BigInt lock;
  final BigInt usdc;
  final int expiresAt;
}

/// "1.000,50" or "1000.5" → units with [decimals] (null if not a number).
BigInt? parseAmount(String text, int decimals) {
  final s = text.trim().replaceAll(' ', '').replaceAll(',', '.');
  if (!RegExp(r'^\d+(\.\d+)?$').hasMatch(s)) return null;
  final parts = s.split('.');
  final frac = parts.length > 1 ? parts[1] : '';
  if (frac.length > decimals) return null;
  return BigInt.parse(parts[0] + frac.padRight(decimals, '0'));
}

class _OfferDialog extends StatefulWidget {
  const _OfferDialog({required this.sell});

  final bool sell;

  @override
  State<_OfferDialog> createState() => _OfferDialogState();
}

class _OfferDialogState extends State<_OfferDialog> {
  final _lock = TextEditingController();
  final _usd = TextEditingController();

  /// Days the offer lasts; 0 = until canceled.
  int _days = 7;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final locale = Localizations.localeOf(context).toString();
    final lock = parseAmount(_lock.text, 8);
    final usdc = parseAmount(_usd.text, 6);
    final ok = lock != null && usdc != null && lock > BigInt.zero && usdc > BigInt.zero;
    final number = [FilteringTextInputFormatter.allow(RegExp(r'[0-9.,]'))];
    return AlertDialog(
      backgroundColor: Brand.bottomBar,
      title: Text(widget.sell ? t.sellLock : t.buyLock),
      content: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            TextField(
              controller: _lock,
              keyboardType: const TextInputType.numberWithOptions(decimal: true),
              inputFormatters: number,
              decoration: InputDecoration(labelText: t.amountLock, suffixText: 'LOCK'),
              onChanged: (_) => setState(() {}),
            ),
            const SizedBox(height: 10),
            TextField(
              controller: _usd,
              keyboardType: const TextInputType.numberWithOptions(decimal: true),
              inputFormatters: number,
              decoration: InputDecoration(labelText: t.totalUsd, suffixText: 'USDC'),
              onChanged: (_) => setState(() {}),
            ),
            if (ok) ...[
              const SizedBox(height: 10),
              Text(
                t.priceEach(formatUsd(usdcToDouble(usdc) / lockToDouble(lock), locale)),
                style: const TextStyle(color: Brand.neonGreen, fontWeight: FontWeight.w700),
              ),
              if (widget.sell)
                Text(
                  t.swapFeeLine(formatLock(lock * BigInt.from(25) ~/ BigInt.from(10000), locale)),
                  style: const TextStyle(color: Brand.pearl, fontSize: 13),
                ),
            ],
            const SizedBox(height: 14),
            Text(t.offerLasts, style: const TextStyle(color: Brand.pearl)),
            const SizedBox(height: 6),
            Wrap(
              spacing: 8,
              children: [
                for (final (days, label) in [(1, t.oneDay), (7, t.sevenDays), (0, t.untilCancel)])
                  ChoiceChip(
                    label: Text(label),
                    selected: _days == days,
                    onSelected: (_) => setState(() => _days = days),
                  ),
              ],
            ),
          ],
        ),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.pop(context), child: Text(t.cancel)),
        TextButton(
          onPressed: !ok
              ? null
              : () => Navigator.pop(
                  context,
                  _Draft(
                    lock,
                    usdc,
                    _days == 0 ? 0 : DateTime.now().add(Duration(days: _days)).millisecondsSinceEpoch ~/ 1000,
                  ),
                ),
          child: Text(t.confirm),
        ),
      ],
    );
  }
}

