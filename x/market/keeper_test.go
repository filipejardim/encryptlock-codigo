package market

import (
	"context"
	sdkmath "cosmossdk.io/math"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authcodec "github.com/cosmos/cosmos-sdk/x/auth/codec"

	"encryptlock/lockkeeper/protocol"
	"encryptlock/lockkeeper/x/market/types"
)

const (
	denom    = "lockunit"
	lockUnit = 100_000_000 // 1 LOCK
	usdcUnit = 1_000_000   // 1 USDC
)

type fakeBank struct{ balances map[string]uint64 }

func (b *fakeBank) move(from, to string, amt sdk.Coins) error {
	n := amt.AmountOf(denom).Uint64()
	if b.balances[from] < n {
		return sdkerrors.ErrInsufficientFunds
	}
	b.balances[from] -= n
	b.balances[to] += n
	return nil
}

func (b *fakeBank) SendCoinsFromAccountToModule(_ context.Context, from sdk.AccAddress, module string, amt sdk.Coins) error {
	return b.move(string(from), "module:"+module, amt)
}

func (b *fakeBank) SendCoinsFromModuleToAccount(_ context.Context, module string, to sdk.AccAddress, amt sdk.Coins) error {
	return b.move("module:"+module, string(to), amt)
}

func (b *fakeBank) SendCoinsFromModuleToModule(_ context.Context, from, to string, amt sdk.Coins) error {
	return b.move("module:"+from, "module:"+to, amt)
}

func (b *fakeBank) SpendableCoins(_ context.Context, a sdk.AccAddress) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewIntFromUint64(b.balances[string(a)])))
}

type fakeRegistry map[string]bool

func (r fakeRegistry) IsRegistered(_ context.Context, a sdk.AccAddress) (bool, error) {
	return r[string(a)], nil
}

type fakeFees struct{ genesis int64 }

func (f fakeFees) GenesisTime(context.Context) (int64, error) { return f.genesis, nil }
func (f fakeFees) FinalMilestoneReached(context.Context) bool { return false }
func (fakeFees) DevFundAddress(context.Context) (sdk.AccAddress, error) {
	return devFund, nil
}

var (
	seller  = sdk.AccAddress("seller______________")
	buyer   = sdk.AccAddress("buyer_______________")
	devFund = sdk.AccAddress("dev_fund____________")
	start   = time.Date(2026, time.October, 5, 12, 0, 0, 0, time.UTC)

	sellerPolygon = "0x1111111111111111111111111111111111111111"
	buyerPolygon  = "0x2222222222222222222222222222222222222222"
)

func setup(t *testing.T) (Keeper, *fakeBank, sdk.Context) {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.ModuleName)
	ctx := testutil.DefaultContext(key, storetypes.NewTransientStoreKey("transient_test")).WithBlockTime(start)
	bank := &fakeBank{balances: map[string]uint64{
		string(seller): 30_000 * lockUnit,
		string(buyer):  10 * lockUnit,
	}}
	reg := fakeRegistry{string(seller): true, string(buyer): true}
	k := NewKeeper(runtime.NewKVStoreService(key), codec.NewProtoCodec(codectypes.NewInterfaceRegistry()), bank, reg,
		fakeFees{genesis: start.Unix()}, authcodec.NewBech32Codec("lock"), denom, "fee_collector")
	NewAppModule(k).InitGenesis(ctx, nil, NewAppModule(k).DefaultGenesis(nil))
	pc, _ := json.Marshal(PolygonConfig{ChainID: 80002, Vault: "0x1111111111111111111111111111111111111111", USDC: "0x2222222222222222222222222222222222222222"})
	if err := k.Polygon.Set(ctx, pc); err != nil {
		t.Fatal(err)
	}
	return k, bank, ctx
}

func secret(b byte) ([]byte, []byte) {
	s := make([]byte, 32)
	s[0] = b
	h := sha256.Sum256(s)
	return s, h[:]
}

func TestSwapFeeIsQuarterPercent(t *testing.T) {
	if got := SwapFee(25_000 * lockUnit); got != 62_50000000 { // 62,5 LOCK
		t.Fatalf("fee of 25.000 LOCK = %d, want 62,5 LOCK", got)
	}
	if got := SwapFee(3); got != 0 {
		t.Fatalf("fee of 3 lockunit = %d, want 0 (rounded down)", got)
	}
}

// The founder's example: sell 25.000 LOCK for 1.000 USDC.
func TestCompleteTradeFromSellOffer(t *testing.T) {
	k, bank, ctx := setup(t)
	fee := SwapFee(25_000 * lockUnit)
	id, err := k.PlaceOffer(ctx, seller, types.Side_SIDE_SELL, 25_000*lockUnit, 1_000*usdcUnit, sellerPolygon, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := bank.balances[string(seller)]; got != 30_000*lockUnit-25_000*lockUnit-fee {
		t.Fatalf("seller after placing = %d: the LOCK and the fee must be held", got)
	}
	s, h := secret(1)
	tradeID, err := k.TakeOffer(ctx, buyer, id, buyerPolygon, h)
	if err != nil {
		t.Fatal(err)
	}
	// Reserved: nobody else can take it, the seller cannot cancel it.
	if _, err := k.TakeOffer(ctx, buyer, id, buyerPolygon, h); !errors.Is(err, types.ErrReserved) {
		t.Fatalf("second take: %v, want ErrReserved", err)
	}
	if err := k.CancelOffer(ctx, seller, id); !errors.Is(err, types.ErrReserved) {
		t.Fatalf("cancel while reserved: %v, want ErrReserved", err)
	}
	// A wrong secret pays nothing.
	wrong, _ := secret(2)
	if err := k.Settle(ctx, tradeID, wrong); !errors.Is(err, types.ErrWrongSecret) {
		t.Fatalf("wrong secret: %v", err)
	}
	// The USDC were paid on Polygon with the secret: the buyer gets the LOCK.
	ctx = ctx.WithBlockTime(start.Add(10 * time.Minute))
	if err := k.Settle(ctx, tradeID, s); err != nil {
		t.Fatal(err)
	}
	if got := bank.balances[string(buyer)]; got != 10*lockUnit+25_000*lockUnit {
		t.Fatalf("buyer = %d, want 25.010 LOCK", got)
	}
	// Fee split year 1: 75% development fund, 25% LockKeepers.
	if bank.balances[string(devFund)] != fee*75/100 || bank.balances["module:fee_collector"] != fee-fee*75/100 {
		t.Fatalf("fee split: dev %d, keepers %d (fee %d)", bank.balances[string(devFund)], bank.balances["module:fee_collector"], fee)
	}
	if bank.balances["module:market"] != 0 {
		t.Fatalf("market still holds %d", bank.balances["module:market"])
	}
	if _, err := k.Offers.Get(ctx, id); err == nil {
		t.Fatal("the offer must be gone")
	}
	if err := k.Settle(ctx, tradeID, s); !errors.Is(err, types.ErrNotFound) {
		t.Fatalf("settling twice: %v", err)
	}
	// The price: 1.000 USDC / 25.000 LOCK = 0,04 USDC per LOCK.
	last, dayLock, dayUsdc, recent, err := k.Price(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if last == nil || last.Lock != 25_000*lockUnit || last.Usdc != 1_000*usdcUnit || dayLock != last.Lock || dayUsdc != last.Usdc || len(recent) != 1 {
		t.Fatalf("price: last %+v, day %d/%d, recent %d", last, dayLock, dayUsdc, len(recent))
	}
}

func TestUnpaidTradeGivesEverythingBack(t *testing.T) {
	k, bank, ctx := setup(t)
	id, _ := k.PlaceOffer(ctx, seller, types.Side_SIDE_SELL, 1_000*lockUnit, 40*usdcUnit, sellerPolygon, nil, 0)
	_, h := secret(1)
	tradeID, err := k.TakeOffer(ctx, buyer, id, buyerPolygon, h)
	if err != nil {
		t.Fatal(err)
	}
	tr, _ := k.Trades.Get(ctx, tradeID)
	if tr.PayBy != start.Add(30*time.Minute).Unix() || tr.ReservedUntil != start.Add(60*time.Minute).Unix() {
		t.Fatalf("deadlines: pay by %d, reserved until %d", tr.PayBy, tr.ReservedUntil)
	}
	// Still reserved just before the end.
	ctx = ctx.WithBlockTime(start.Add(59 * time.Minute))
	if err := k.EndBlock(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Trades.Get(ctx, tradeID); err != nil {
		t.Fatal("the reservation ended too early")
	}
	// No payment in time: the reservation ends and the offer is open again.
	ctx = ctx.WithBlockTime(start.Add(60 * time.Minute))
	if err := k.EndBlock(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Trades.Get(ctx, tradeID); err == nil {
		t.Fatal("the unpaid trade must be gone")
	}
	o, _ := k.Offers.Get(ctx, id)
	if o.TradeId != 0 {
		t.Fatal("the offer must be open again")
	}
	// The seller cancels: all the LOCK and the fee come back.
	if err := k.CancelOffer(ctx, seller, id); err != nil {
		t.Fatal(err)
	}
	if bank.balances[string(seller)] != 30_000*lockUnit || bank.balances["module:market"] != 0 {
		t.Fatalf("seller %d, market %d", bank.balances[string(seller)], bank.balances["module:market"])
	}
}

func TestBuyOfferTakenBySeller(t *testing.T) {
	k, bank, ctx := setup(t)
	s, h := secret(7)
	// "I offer 1.000 USDC for 25.000 LOCK."
	id, err := k.PlaceOffer(ctx, buyer, types.Side_SIDE_BUY, 25_000*lockUnit, 1_000*usdcUnit, buyerPolygon, h, 0)
	if err != nil {
		t.Fatal(err)
	}
	fee := SwapFee(25_000 * lockUnit)
	tradeID, err := k.TakeOffer(ctx, seller, id, sellerPolygon, nil)
	if err != nil {
		t.Fatal(err)
	}
	if bank.balances["module:market"] != 25_000*lockUnit+fee {
		t.Fatal("the seller's LOCK and fee must be held")
	}
	tr, _ := k.Trades.Get(ctx, tradeID)
	// The buyer may be asleep: 12 hours to pay.
	if tr.PayBy != start.Add(12*time.Hour).Unix() || tr.SellerPolygon != sellerPolygon {
		t.Fatalf("trade: %+v", tr)
	}
	ctx = ctx.WithBlockTime(start.Add(8 * time.Hour))
	if err := k.Settle(ctx, tradeID, s); err != nil {
		t.Fatal(err)
	}
	if bank.balances[string(buyer)] != 10*lockUnit+25_000*lockUnit {
		t.Fatalf("buyer = %d", bank.balances[string(buyer)])
	}
}

func TestUnpaidBuyOfferGivesTheSellerBack(t *testing.T) {
	k, bank, ctx := setup(t)
	_, h := secret(7)
	id, _ := k.PlaceOffer(ctx, buyer, types.Side_SIDE_BUY, 100*lockUnit, 4*usdcUnit, buyerPolygon, h, 0)
	if _, err := k.TakeOffer(ctx, seller, id, sellerPolygon, nil); err != nil {
		t.Fatal(err)
	}
	ctx = ctx.WithBlockTime(start.Add(12*time.Hour + 30*time.Minute))
	if err := k.EndBlock(ctx); err != nil {
		t.Fatal(err)
	}
	if bank.balances[string(seller)] != 30_000*lockUnit || bank.balances["module:market"] != 0 {
		t.Fatalf("seller %d, market %d", bank.balances[string(seller)], bank.balances["module:market"])
	}
}

func TestOfferRules(t *testing.T) {
	k, bank, ctx := setup(t)
	_, h := secret(1)
	cases := []struct {
		name string
		err  error
		f    func() error
	}{
		{"zero LOCK", types.ErrInvalidOffer, func() error {
			_, err := k.PlaceOffer(ctx, seller, types.Side_SIDE_SELL, 0, usdcUnit, sellerPolygon, nil, 0)
			return err
		}},
		{"bad 0x address", types.ErrInvalidOffer, func() error {
			_, err := k.PlaceOffer(ctx, seller, types.Side_SIDE_SELL, lockUnit, usdcUnit, "0x123", nil, 0)
			return err
		}},
		{"buy offer without hash", types.ErrInvalidHash, func() error {
			_, err := k.PlaceOffer(ctx, buyer, types.Side_SIDE_BUY, lockUnit, usdcUnit, buyerPolygon, nil, 0)
			return err
		}},
		{"more than a year", types.ErrInvalidOffer, func() error {
			_, err := k.PlaceOffer(ctx, seller, types.Side_SIDE_SELL, lockUnit, usdcUnit, sellerPolygon, nil, start.AddDate(1, 1, 0).Unix())
			return err
		}},
		{"not enough LOCK", sdkerrors.ErrInsufficientFunds, func() error {
			_, err := k.PlaceOffer(ctx, buyer, types.Side_SIDE_SELL, 1_000*lockUnit, usdcUnit, buyerPolygon, nil, 0)
			return err
		}},
		{"own offer", types.ErrOwnOffer, func() error {
			id, _ := k.PlaceOffer(ctx, seller, types.Side_SIDE_SELL, lockUnit, usdcUnit, sellerPolygon, nil, 0)
			_, err := k.TakeOffer(ctx, seller, id, sellerPolygon, h)
			return err
		}},
		{"unregistered buyer", types.ErrNotRegistered, func() error {
			id, _ := k.PlaceOffer(ctx, seller, types.Side_SIDE_SELL, lockUnit, usdcUnit, sellerPolygon, nil, 0)
			_, err := k.TakeOffer(ctx, sdk.AccAddress("stranger____________"), id, buyerPolygon, h)
			return err
		}},
		{"someone else cancels", types.ErrNotMaker, func() error {
			id, _ := k.PlaceOffer(ctx, seller, types.Side_SIDE_SELL, lockUnit, usdcUnit, sellerPolygon, nil, 0)
			return k.CancelOffer(ctx, buyer, id)
		}},
	}
	for _, c := range cases {
		if err := c.f(); !errors.Is(err, c.err) {
			t.Errorf("%s: %v, want %v", c.name, err, c.err)
		}
	}
	_ = bank
}

func TestExpiredOfferGivesTheLOCKBack(t *testing.T) {
	k, bank, ctx := setup(t)
	day := start.Add(24 * time.Hour).Unix()
	id, err := k.PlaceOffer(ctx, seller, types.Side_SIDE_SELL, 500*lockUnit, 20*usdcUnit, sellerPolygon, nil, day)
	if err != nil {
		t.Fatal(err)
	}
	ctx = ctx.WithBlockTime(start.Add(24 * time.Hour))
	_, h := secret(1)
	if _, err := k.TakeOffer(ctx, buyer, id, buyerPolygon, h); !errors.Is(err, types.ErrExpired) {
		t.Fatalf("taking an expired offer: %v", err)
	}
	if err := k.EndBlock(ctx); err != nil {
		t.Fatal(err)
	}
	if bank.balances[string(seller)] != 30_000*lockUnit {
		t.Fatalf("seller = %d", bank.balances[string(seller)])
	}
}

func TestOffersBelowOneDollarAreRefused(t *testing.T) {
	k, _, ctx := setup(t)
	_, h := secret(3)
	if _, err := k.PlaceOffer(ctx, seller, types.Side_SIDE_SELL, 1_000*lockUnit, usdcUnit/2, sellerPolygon, nil, 0); !errors.Is(err, types.ErrSmallOffer) {
		t.Fatalf("0,50 USDC sell offer: %v, want ErrSmallOffer", err)
	}
	if _, err := k.PlaceOffer(ctx, buyer, types.Side_SIDE_BUY, 1_000*lockUnit, usdcUnit/2, buyerPolygon, h, 0); !errors.Is(err, types.ErrSmallOffer) {
		t.Fatalf("0,50 USDC buy offer: %v, want ErrSmallOffer", err)
	}
}

// Fair use: an account cannot fill the market with offers, nor hold every
// offer reserved without paying.
func TestNobodyCanBlockTheMarket(t *testing.T) {
	k, _, ctx := setup(t)
	for i := range protocol.SwapMaxOpenOffers {
		_, h := secret(byte(100 + i))
		if _, err := k.PlaceOffer(ctx, buyer, types.Side_SIDE_BUY, lockUnit, usdcUnit, buyerPolygon, h, 0); err != nil {
			t.Fatal(err)
		}
	}
	_, h := secret(99)
	if _, err := k.PlaceOffer(ctx, buyer, types.Side_SIDE_BUY, lockUnit, usdcUnit, buyerPolygon, h, 0); !errors.Is(err, types.ErrTooManyOffers) {
		t.Fatalf("offer over the limit: %v, want ErrTooManyOffers", err)
	}

	var offers []uint64
	for range 3 {
		id, err := k.PlaceOffer(ctx, seller, types.Side_SIDE_SELL, 10*lockUnit, usdcUnit, sellerPolygon, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		offers = append(offers, id)
	}
	// A griefer (the buyer here) reserves 2 and cannot reserve a third.
	for i, id := range offers[:2] {
		_, h := secret(byte(50 + i))
		if _, err := k.TakeOffer(ctx, buyer, id, buyerPolygon, h); err != nil {
			t.Fatal(err)
		}
	}
	_, h3 := secret(60)
	if _, err := k.TakeOffer(ctx, buyer, offers[2], buyerPolygon, h3); !errors.Is(err, types.ErrTooManyTrades) {
		t.Fatalf("third reservation: %v, want ErrTooManyTrades", err)
	}
	// Not paid: the reservations end and the griefer must wait.
	ctx = ctx.WithBlockTime(start.Add(protocol.SwapPayWindow + protocol.SwapReserveMargin + time.Minute))
	if err := k.EndBlock(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := k.TakeOffer(ctx, buyer, offers[0], buyerPolygon, h3); !errors.Is(err, types.ErrWaitAfterUnpaid) {
		t.Fatalf("taking again right after not paying: %v, want ErrWaitAfterUnpaid", err)
	}
	ctx = ctx.WithBlockTime(start.Add(protocol.SwapPayWindow + protocol.SwapReserveMargin + protocol.SwapUnpaidWait + 2*time.Minute))
	if _, err := k.TakeOffer(ctx, buyer, offers[0], buyerPolygon, h3); err != nil {
		t.Fatalf("taking after the wait: %v", err)
	}
}

func TestDayAverageOnlyCountsTheLast24Hours(t *testing.T) {
	k, _, ctx := setup(t)
	trade := func(at time.Time, lock, usdc uint64, n byte) {
		ctx = ctx.WithBlockTime(at)
		s, h := secret(n)
		id, err := k.PlaceOffer(ctx, seller, types.Side_SIDE_SELL, lock, usdc, sellerPolygon, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		tid, _ := k.TakeOffer(ctx, buyer, id, buyerPolygon, h)
		if err := k.Settle(ctx, tid, s); err != nil {
			t.Fatal(err)
		}
	}
	trade(start, 1_000*lockUnit, 10*usdcUnit, 1)                   // old: 0,01
	trade(start.Add(30*time.Hour), 1_000*lockUnit, 40*usdcUnit, 2) // 0,04
	trade(start.Add(31*time.Hour), 1_000*lockUnit, 60*usdcUnit, 3) // 0,06
	last, dayLock, dayUsdc, recent, _ := k.Price(ctx, 10)
	if last.Usdc != 60*usdcUnit || dayLock != 2_000*lockUnit || dayUsdc != 100*usdcUnit || len(recent) != 3 {
		t.Fatalf("last %+v, day %d LOCK / %d USDC, recent %d", last, dayLock, dayUsdc, len(recent))
	}
}

// Two accounts of the same person trading tiny amounts at a made-up price
// move the 24-hour average a lot, but not the median weighed by volume (the
// reference price for balances and the LockKeeper guarantee).
func TestMedianResistsTinyTradesAtAMadeUpPrice(t *testing.T) {
	k, _, ctx := setup(t)
	n := byte(0)
	trade := func(at time.Time, lock, usdc uint64) {
		ctx = ctx.WithBlockTime(at)
		n++
		s, h := secret(n)
		id, err := k.PlaceOffer(ctx, seller, types.Side_SIDE_SELL, lock, usdc, sellerPolygon, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		tid, _ := k.TakeOffer(ctx, buyer, id, buyerPolygon, h)
		if err := k.Settle(ctx, tid, s); err != nil {
			t.Fatal(err)
		}
	}
	if p, _ := k.ReferencePrice(ctx); p != 0 {
		t.Fatalf("before the first trade: %d, want 0 (no price)", p)
	}
	trade(start, 1_000*lockUnit, 10*usdcUnit) // 0,01, more than 24 h before the rest
	// More than a day later, no trade in the last 24 hours: the last trade.
	ctx = ctx.WithBlockTime(start.Add(30 * time.Hour))
	if p, _ := k.ReferencePrice(ctx); p != 10_000_000 {
		t.Fatalf("no trade in 24 h: %d, want the last trade's 0,01 $", p)
	}
	at := start.Add(30 * time.Hour)
	trade(at, 1_000*lockUnit, 40*usdcUnit) // 0,04
	trade(at, 1_000*lockUnit, 50*usdcUnit) // 0,05
	trade(at, 1_000*lockUnit, 60*usdcUnit) // 0,06
	if m, _ := k.DayMedian(ctx); m != 50_000_000 {
		t.Fatalf("median %d, want 0,05 $", m)
	}
	// 100 trades of 0,01 LOCK for 1 USDC each (100 $ per LOCK).
	for i := 0; i < 100; i++ {
		trade(at.Add(time.Duration(i)*time.Second), lockUnit/100, usdcUnit)
	}
	_, dayLock, dayUsdc, _, _ := k.Price(ctx, 0)
	if avg := float64(dayUsdc) / 1e6 / (float64(dayLock) / 1e8); avg < 0.08 {
		t.Fatalf("the average moved only to %.4f (the test expects it to be fooled)", avg)
	}
	if m, _ := k.ReferencePrice(ctx); m != 50_000_000 {
		t.Fatalf("median %d after the tiny trades, want still 0,05 $", m)
	}
	// Outside the 24 hours they no longer count at all.
	ctx = ctx.WithBlockTime(at.Add(25 * time.Hour))
	if m, _ := k.DayMedian(ctx); m != 0 {
		t.Fatalf("median %d a day later, want 0 (no trades)", m)
	}
}

// One Polygon payment settles one trade only: a secret cannot be in two
// trades at once, nor be used again once it paid.
func TestSecretCannotBeReused(t *testing.T) {
	k, _, ctx := setup(t)
	a, _ := k.PlaceOffer(ctx, seller, types.Side_SIDE_SELL, 100*lockUnit, 4*usdcUnit, sellerPolygon, nil, 0)
	b, _ := k.PlaceOffer(ctx, seller, types.Side_SIDE_SELL, 100*lockUnit, 4*usdcUnit, sellerPolygon, nil, 0)
	s, h := secret(7)
	tradeA, err := k.TakeOffer(ctx, buyer, a, buyerPolygon, h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.TakeOffer(ctx, buyer, b, buyerPolygon, h); !errors.Is(err, types.ErrHashUsed) {
		t.Fatalf("same secret in a second trade: %v, want ErrHashUsed", err)
	}
	if _, err := k.PlaceOffer(ctx, buyer, types.Side_SIDE_BUY, 100*lockUnit, 4*usdcUnit, buyerPolygon, h, 0); !errors.Is(err, types.ErrHashUsed) {
		t.Fatalf("buy offer with a secret in use: %v, want ErrHashUsed", err)
	}
	if err := k.Settle(ctx, tradeA, s); err != nil {
		t.Fatal(err)
	}
	if _, err := k.TakeOffer(ctx, buyer, b, buyerPolygon, h); !errors.Is(err, types.ErrHashUsed) {
		t.Fatalf("secret used again after paying: %v, want ErrHashUsed", err)
	}
	// An unpaid trade keeps its secret in use while a late payment can
	// still come (late.go), then frees it.
	_, h2 := secret(8)
	if _, err := k.TakeOffer(ctx, buyer, b, buyerPolygon, h2); err != nil {
		t.Fatal(err)
	}
	ctx = ctx.WithBlockTime(start.Add(2 * time.Hour))
	if err := k.EndBlock(ctx); err != nil {
		t.Fatal(err)
	}
	if has, _ := k.Hashes.Has(ctx, h2); !has {
		t.Fatal("the secret of an unpaid trade must stay in use during the late window")
	}
	ctx = ctx.WithBlockTime(start.Add(2*time.Hour + LateWindow))
	if err := k.EndBlock(ctx); err != nil {
		t.Fatal(err)
	}
	if has, _ := k.Hashes.Has(ctx, h2); has {
		t.Fatal("the secret must be free after the late window")
	}
}

func TestNoTradesWithoutPolygonWatching(t *testing.T) {
	k, _, ctx := setup(t)
	if err := k.Polygon.Remove(ctx); err != nil {
		t.Fatal(err)
	}
	id, err := k.PlaceOffer(ctx, seller, types.Side_SIDE_SELL, 100*lockUnit, 10_000_000, sellerPolygon, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, hash := secret(9)
	if _, err := k.TakeOffer(ctx, buyer, id, buyerPolygon, hash); !errors.Is(err, types.ErrNoPolygon) {
		t.Fatalf("take without Polygon watching: %v, want ErrNoPolygon", err)
	}
}

// The buyer paid but the network confirmed it only after the reservation
// ended: the LOCK still reach the buyer - from the open offer, or from the
// seller (who got the USDC) if the offer is gone.
func TestLatePaymentStillDelivers(t *testing.T) {
	k, bank, ctx := setup(t)
	id, _ := k.PlaceOffer(ctx, seller, types.Side_SIDE_SELL, 1_000*lockUnit, 40*usdcUnit, sellerPolygon, nil, 0)
	s, h := secret(1)
	tradeID, err := k.TakeOffer(ctx, buyer, id, buyerPolygon, h)
	if err != nil {
		t.Fatal(err)
	}
	ctx = ctx.WithBlockTime(start.Add(61 * time.Minute))
	if err := k.EndBlock(ctx); err != nil {
		t.Fatal(err)
	}
	before := bank.balances[string(buyer)]
	if err := k.Settle(ctx, tradeID, s); err != nil {
		t.Fatalf("late payment from the open offer: %v", err)
	}
	if got := bank.balances[string(buyer)] - before; got != 1_000*lockUnit {
		t.Fatalf("buyer got %d, want 1000 LOCK", got)
	}
	if _, err := k.Offers.Get(ctx, id); err == nil {
		t.Fatal("the offer must be gone after the late payment")
	}
	if err := k.Settle(ctx, tradeID, s); err == nil {
		t.Fatal("one payment settled twice")
	}

	// Second case: the seller canceled the offer meanwhile (got its LOCK
	// back): the LOCK come from the seller's balance.
	id2, _ := k.PlaceOffer(ctx, seller, types.Side_SIDE_SELL, 500*lockUnit, 20*usdcUnit, sellerPolygon, nil, 0)
	s2, h2 := secret(2)
	ctx = ctx.WithBlockTime(start.Add(8 * time.Hour)) // the buyer's wait after not paying is over
	trade2, err := k.TakeOffer(ctx, buyer, id2, buyerPolygon, h2)
	if err != nil {
		t.Fatal(err)
	}
	ctx = ctx.WithBlockTime(start.Add(10 * time.Hour))
	if err := k.EndBlock(ctx); err != nil {
		t.Fatal(err)
	}
	if err := k.CancelOffer(ctx, seller, id2); err != nil {
		t.Fatal(err)
	}
	sellerBefore, buyerBefore := bank.balances[string(seller)], bank.balances[string(buyer)]
	if err := k.Settle(ctx, trade2, s2); err != nil {
		t.Fatalf("late payment from the seller: %v", err)
	}
	if bank.balances[string(buyer)]-buyerBefore != 500*lockUnit {
		t.Fatal("buyer did not get the LOCK from the seller")
	}
	if sellerBefore-bank.balances[string(seller)] != 500*lockUnit+SwapFee(500*lockUnit) {
		t.Fatal("seller did not pay the LOCK and the fee")
	}
	// After the late window nothing settles any more.
	id3, _ := k.PlaceOffer(ctx, seller, types.Side_SIDE_SELL, 100*lockUnit, 4*usdcUnit, sellerPolygon, nil, 0)
	s3, h3 := secret(3)
	ctx = ctx.WithBlockTime(start.Add(17 * time.Hour))
	trade3, err := k.TakeOffer(ctx, buyer, id3, buyerPolygon, h3)
	if err != nil {
		t.Fatal(err)
	}
	ctx = ctx.WithBlockTime(start.Add(18*time.Hour + LateWindow))
	if err := k.EndBlock(ctx); err != nil {
		t.Fatal(err)
	}
	ctx = ctx.WithBlockTime(start.Add(18*time.Hour + 2*LateWindow))
	if err := k.EndBlock(ctx); err != nil {
		t.Fatal(err)
	}
	if err := k.Settle(ctx, trade3, s3); err == nil {
		t.Fatal("a payment after the late window settled")
	}
}
