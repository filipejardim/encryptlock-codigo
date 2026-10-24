// Package market implements the LOCK ↔ USDC market (white paper, section 9;
// EncryptLock_trocas_USDC.md): offers to sell or buy LOCK for USDC on Polygon,
// without anyone holding other people's money.
//
// The LOCK of a trade sit in this module. The buyer pays the USDC on Polygon
// through the swap vault (an HTLC), revealing a secret; only a confirmed
// payment (Settle, called when the LockKeepers have seen it on Polygon) gives
// the LOCK to the buyer. Without payment the reservation ends and the LOCK go
// back. The seller pays the 0,25% swap fee in LOCK.
package market

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	"cosmossdk.io/core/store"
	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"encryptlock/lockkeeper/protocol"
	"encryptlock/lockkeeper/x/market/types"
	"encryptlock/lockkeeper/x/transfers"
)

// BankKeeper is the part of the bank module this module uses.
type BankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
	SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins
}

// Registry tells whether an account is registered (only registered accounts
// can receive LOCK).
type Registry interface {
	IsRegistered(ctx context.Context, addr sdk.AccAddress) (bool, error)
}

// Fees gives what the fee split needs: the genesis time (which period) and
// the development fund's address.
type Fees interface {
	GenesisTime(ctx context.Context) (int64, error)
	DevFundAddress(ctx context.Context) (sdk.AccAddress, error)
	// FinalMilestoneReached: 10.000.000 active wallets (fee split 50/50).
	FinalMilestoneReached(ctx context.Context) bool
}

// Keeper stores offers, trades and the finished trades (the price).
type Keeper struct {
	bank         BankKeeper
	registry     Registry
	fees         Fees
	feeCollector string
	addressCodec address.Codec
	denom        string

	NextOffer collections.Sequence
	NextTrade collections.Sequence
	Offers    collections.Map[uint64, types.Offer]
	Trades    collections.Map[uint64, types.Trade]
	// OffersByExpiry: (expires_at, offer id) of offers that expire.
	OffersByExpiry collections.KeySet[collections.Pair[int64, uint64]]
	// TradesByEnd: (reserved_until, trade id).
	TradesByEnd collections.KeySet[collections.Pair[int64, uint64]]
	// Fills: finished trades that count for the price, in order.
	NextFill collections.Sequence
	Fills    collections.Map[uint64, types.Fill]
	// Hashes: the hash of each secret in a trade (→ trade id) or already
	// used to pay (→ 0, forever). One Polygon payment can then settle one
	// trade only.
	Hashes collections.Map[[]byte, uint64]
	// Polygon: where the network watches USDC payments (genesis; JSON of
	// PolygonConfig). Without it no offer can be taken: nobody could confirm
	// the payment and the buyer would pay for nothing.
	Polygon collections.Item[[]byte]
	// Fair use: open offers per maker, trades waiting for each buyer's
	// payment, and until when a buyer who did not pay must wait.
	OpenOffers  collections.Map[sdk.AccAddress, uint64]
	BuyerTrades collections.Map[sdk.AccAddress, uint64]
	UnpaidUntil collections.Map[sdk.AccAddress, int64]
	// ByMaker: each account's offers (to take them down on "Not me").
	ByMaker collections.KeySet[collections.Pair[sdk.AccAddress, uint64]]
	// Late: unpaid trades still waiting for a late payment (late.go).
	Late      collections.Map[uint64, types.Trade]
	LateByEnd collections.KeySet[collections.Pair[int64, uint64]]
	shield    *Shield
}

// Shield is the account protection of x/transfers (security review, 11
// Oct): LOCK sold on the market leave the account like a transfer, so the
// same protections apply.
type Shield interface {
	CheckSell(ctx context.Context, seller sdk.AccAddress, amount uint64) (needsApproval bool, err error)
	IsFrozen(ctx context.Context, addr sdk.AccAddress) (bool, error)
	IsDeviceOf(ctx context.Context, owner, signer sdk.AccAddress) (bool, error)
}

// SetShield connects the account protection (once, when the app starts).
func (k Keeper) SetShield(s Shield) { *k.shield = s }

// PolygonConfig is the network's Polygon side, the same for every LockKeeper.
type PolygonConfig struct {
	ChainID    uint64 `json:"chain_id"`    // 137 Polygon, 80002 Amoy (test)
	Vault      string `json:"vault"`       // the swap vault (HTLC) contract
	USDC       string `json:"usdc"`        // the USDC contract
	StartBlock uint64 `json:"start_block"` // the vault's first block
}

// Valid: a network, and two contract addresses.
func (c PolygonConfig) Valid() bool {
	return c.ChainID != 0 && ValidPolygonAddress(c.Vault) && ValidPolygonAddress(c.USDC)
}

// PolygonConfig returns the network's Polygon side (ok false if none).
func (k Keeper) PolygonConfig(ctx context.Context) (PolygonConfig, bool, error) {
	var c PolygonConfig
	bz, err := k.Polygon.Get(ctx)
	if errors.Is(err, collections.ErrNotFound) {
		return c, false, nil
	}
	if err != nil {
		return c, false, err
	}
	if err := json.Unmarshal(bz, &c); err != nil {
		return c, false, err
	}
	return c, c.Valid(), nil
}

// NewKeeper creates the market keeper. feeCollector is the module account the
// distribution module pays LockKeepers from.
func NewKeeper(storeService store.KVStoreService, cdc codec.BinaryCodec, bank BankKeeper, registry Registry, fees Fees, addressCodec address.Codec, denom, feeCollector string) Keeper {
	sb := collections.NewSchemaBuilder(storeService)
	k := Keeper{
		bank:           bank,
		registry:       registry,
		fees:           fees,
		feeCollector:   feeCollector,
		addressCodec:   addressCodec,
		denom:          denom,
		NextOffer:      collections.NewSequence(sb, collections.NewPrefix(0), "next_offer"),
		NextTrade:      collections.NewSequence(sb, collections.NewPrefix(1), "next_trade"),
		Offers:         collections.NewMap(sb, collections.NewPrefix(2), "offers", collections.Uint64Key, codec.CollValue[types.Offer](cdc)),
		Trades:         collections.NewMap(sb, collections.NewPrefix(3), "trades", collections.Uint64Key, codec.CollValue[types.Trade](cdc)),
		OffersByExpiry: collections.NewKeySet(sb, collections.NewPrefix(4), "offers_by_expiry", collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key)),
		TradesByEnd:    collections.NewKeySet(sb, collections.NewPrefix(5), "trades_by_end", collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key)),
		NextFill:       collections.NewSequence(sb, collections.NewPrefix(6), "next_fill"),
		Fills:          collections.NewMap(sb, collections.NewPrefix(7), "fills", collections.Uint64Key, codec.CollValue[types.Fill](cdc)),
		Hashes:         collections.NewMap(sb, collections.NewPrefix(8), "hashes", collections.BytesKey, collections.Uint64Value),
		Polygon:        collections.NewItem(sb, collections.NewPrefix(9), "polygon", collections.BytesValue),
		OpenOffers:     collections.NewMap(sb, collections.NewPrefix(10), "open_offers", sdk.AccAddressKey, collections.Uint64Value),
		BuyerTrades:    collections.NewMap(sb, collections.NewPrefix(11), "buyer_trades", sdk.AccAddressKey, collections.Uint64Value),
		UnpaidUntil:    collections.NewMap(sb, collections.NewPrefix(12), "unpaid_until", sdk.AccAddressKey, collections.Int64Value),
		ByMaker:        collections.NewKeySet(sb, collections.NewPrefix(13), "by_maker", collections.PairKeyCodec(sdk.AccAddressKey, collections.Uint64Key)),
		Late:           collections.NewMap(sb, collections.NewPrefix(14), "late", collections.Uint64Key, codec.CollValue[types.Trade](cdc)),
		LateByEnd:      collections.NewKeySet(sb, collections.NewPrefix(15), "late_by_end", collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key)),
		shield:         new(Shield),
	}
	if _, err := sb.Build(); err != nil {
		panic(err)
	}
	return k
}

// SwapFee is 0,25% of the LOCK traded (rounded down).
func SwapFee(lock uint64) uint64 {
	return lock/10_000*protocol.SwapFeeBasisPoints + lock%10_000*protocol.SwapFeeBasisPoints/10_000
}

// ValidPolygonAddress: 0x followed by 40 hexadecimal digits.
func ValidPolygonAddress(s string) bool {
	if len(s) != 42 || !strings.HasPrefix(s, "0x") {
		return false
	}
	_, err := hex.DecodeString(s[2:])
	return err == nil
}

func (k Keeper) coins(amount uint64) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin(k.denom, sdkmath.NewIntFromUint64(amount)))
}

func now(ctx context.Context) time.Time { return sdk.UnwrapSDKContext(ctx).BlockTime() }

func (k Keeper) emit(ctx context.Context, name string, attrs ...string) {
	ev := sdk.NewEvent(name)
	for i := 0; i+1 < len(attrs); i += 2 {
		ev = ev.AppendAttributes(sdk.NewAttribute(attrs[i], attrs[i+1]))
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(ev)
}

func u(v uint64) string { return strconv.FormatUint(v, 10) }

func (k Keeper) registered(ctx context.Context, addr sdk.AccAddress) error {
	ok, err := k.registry.IsRegistered(ctx, addr)
	if err != nil {
		return err
	}
	if !ok {
		return types.ErrNotRegistered
	}
	return nil
}

// PlaceOffer publishes an offer. A sell offer locks lock + fee in the module.
func (k Keeper) PlaceOffer(ctx context.Context, maker sdk.AccAddress, side types.Side, lock, usdc uint64, polygon string, hash []byte, expiresAt int64) (uint64, error) {
	t := now(ctx)
	if lock == 0 || usdc == 0 || !ValidPolygonAddress(polygon) ||
		(side != types.Side_SIDE_SELL && side != types.Side_SIDE_BUY) ||
		(expiresAt != 0 && (expiresAt <= t.Unix() || expiresAt > t.AddDate(0, 0, protocol.SwapMaxOfferDays).Unix())) {
		return 0, types.ErrInvalidOffer
	}
	if usdc < protocol.SwapMinPriceUSDC {
		return 0, types.ErrSmallOffer
	}
	awaiting := false
	if n, err := k.count(ctx, k.OpenOffers, maker); err != nil {
		return 0, err
	} else if n >= protocol.SwapMaxOpenOffers {
		return 0, types.ErrTooManyOffers
	}
	if side == types.Side_SIDE_BUY {
		if len(hash) != sha256.Size {
			return 0, types.ErrInvalidHash
		}
		if used, err := k.Hashes.Has(ctx, hash); err != nil || used {
			return 0, types.ErrHashUsed
		}
		if err := k.registered(ctx, maker); err != nil {
			return 0, err
		}
	} else {
		hash = nil
		var err error
		if awaiting, err = k.checkSell(ctx, maker, lock+SwapFee(lock)); err != nil {
			return 0, err
		}
		if err := k.bank.SendCoinsFromAccountToModule(ctx, maker, types.ModuleName, k.coins(lock+SwapFee(lock))); err != nil {
			return 0, err
		}
	}
	id, err := k.NextOffer.Next(ctx)
	if err != nil {
		return 0, err
	}
	makerStr, err := k.addressCodec.BytesToString(maker)
	if err != nil {
		return 0, err
	}
	o := types.Offer{Id: id, Maker: makerStr, Side: side, Lock: lock, Usdc: usdc, Polygon: polygon, Hash: hash, CreatedAt: t.Unix(), ExpiresAt: expiresAt, AwaitingApproval: awaiting}
	if err := k.Offers.Set(ctx, id, o); err != nil {
		return 0, err
	}
	if err := k.add(ctx, k.OpenOffers, maker, 1); err != nil {
		return 0, err
	}
	if err := k.ByMaker.Set(ctx, collections.Join(maker, id)); err != nil {
		return 0, err
	}
	if expiresAt != 0 {
		if err := k.OffersByExpiry.Set(ctx, collections.Join(expiresAt, id)); err != nil {
			return 0, err
		}
	}
	k.emit(ctx, "offer_placed", "id", u(id), "maker", makerStr, "side", side.String(), "lock", u(lock), "usdc", u(usdc))
	return id, nil
}

// removeOffer deletes an offer; a sell offer's LOCK (and fee) go back.
func (k Keeper) removeOffer(ctx context.Context, o types.Offer, event string) error {
	if o.Side == types.Side_SIDE_SELL {
		maker, err := k.addressCodec.StringToBytes(o.Maker)
		if err != nil {
			return err
		}
		if err := k.bank.SendCoinsFromModuleToAccount(ctx, types.ModuleName, maker, k.coins(o.Lock+SwapFee(o.Lock))); err != nil {
			return err
		}
	}
	if err := k.forgetOffer(ctx, o); err != nil {
		return err
	}
	k.emit(ctx, event, "id", u(o.Id), "maker", o.Maker)
	return nil
}

// CancelOffer takes the maker's open offer down.
func (k Keeper) CancelOffer(ctx context.Context, maker sdk.AccAddress, id uint64) error {
	o, err := k.Offers.Get(ctx, id)
	if err != nil {
		return types.ErrNotFound
	}
	makerStr, err := k.addressCodec.BytesToString(maker)
	if err != nil {
		return err
	}
	if o.Maker != makerStr {
		return types.ErrNotMaker
	}
	if o.TradeId != 0 {
		return types.ErrReserved
	}
	return k.removeOffer(ctx, o, "offer_canceled")
}

// TakeOffer accepts an offer and reserves it for the payment. Taking a sell
// offer, the taker is the buyer and gives the hash of a secret; taking a buy
// offer, the taker is the seller and locks lock + fee here.
func (k Keeper) TakeOffer(ctx context.Context, taker sdk.AccAddress, id uint64, polygon string, hash []byte) (uint64, error) {
	o, err := k.Offers.Get(ctx, id)
	if err != nil {
		return 0, types.ErrNotFound
	}
	t := now(ctx)
	if o.TradeId != 0 {
		return 0, types.ErrReserved
	}
	if o.AwaitingApproval {
		return 0, types.ErrAwaitingApproval
	}
	if o.ExpiresAt != 0 && o.ExpiresAt <= t.Unix() {
		return 0, types.ErrExpired
	}
	takerStr, err := k.addressCodec.BytesToString(taker)
	if err != nil {
		return 0, err
	}
	if takerStr == o.Maker {
		return 0, types.ErrOwnOffer
	}
	if _, ok, err := k.PolygonConfig(ctx); err != nil {
		return 0, err
	} else if !ok {
		return 0, types.ErrNoPolygon
	}
	if !ValidPolygonAddress(polygon) {
		return 0, types.ErrInvalidOffer
	}
	trade := types.Trade{OfferId: o.Id, Lock: o.Lock, Fee: SwapFee(o.Lock), Usdc: o.Usdc, CreatedAt: t.Unix()}
	var payWindow time.Duration
	if o.Side == types.Side_SIDE_SELL {
		if len(hash) != sha256.Size {
			return 0, types.ErrInvalidHash
		}
		if err := k.registered(ctx, taker); err != nil {
			return 0, err
		}
		// "Not me" on the seller: nothing more of it can be bought.
		if s := *k.shield; s != nil {
			maker, err := k.addressCodec.StringToBytes(o.Maker)
			if err != nil {
				return 0, err
			}
			if frozen, err := s.IsFrozen(ctx, maker); err != nil {
				return 0, err
			} else if frozen {
				return 0, types.ErrSellerFrozen
			}
		}
		trade.Seller, trade.SellerPolygon = o.Maker, o.Polygon
		trade.Buyer, trade.BuyerPolygon = takerStr, polygon
		trade.Hash = hash
		payWindow = protocol.SwapPayWindow
	} else {
		// The taker sells: the account protection applies; with a second
		// device it must place a sell offer, which the device approves.
		if needsApproval, err := k.checkSell(ctx, taker, o.Lock+trade.Fee); err != nil {
			return 0, err
		} else if needsApproval {
			return 0, types.ErrNeedsApproval
		}
		if err := k.bank.SendCoinsFromAccountToModule(ctx, taker, types.ModuleName, k.coins(o.Lock+trade.Fee)); err != nil {
			return 0, err
		}
		trade.Seller, trade.SellerPolygon = takerStr, polygon
		trade.Buyer, trade.BuyerPolygon = o.Maker, o.Polygon
		trade.Hash = o.Hash
		payWindow = protocol.SwapBuyerPayWindow
	}
	if used, err := k.Hashes.Has(ctx, trade.Hash); err != nil || used {
		return 0, types.ErrHashUsed
	}
	buyer, err := k.addressCodec.StringToBytes(trade.Buyer)
	if err != nil {
		return 0, err
	}
	if until, err := k.UnpaidUntil.Get(ctx, buyer); err == nil && t.Unix() < until {
		return 0, types.ErrWaitAfterUnpaid
	}
	if n, err := k.count(ctx, k.BuyerTrades, buyer); err != nil {
		return 0, err
	} else if n >= protocol.SwapMaxOpenTrades {
		return 0, types.ErrTooManyTrades
	}
	if err := k.add(ctx, k.BuyerTrades, buyer, 1); err != nil {
		return 0, err
	}
	trade.PayBy = t.Add(payWindow).Unix()
	trade.ReservedUntil = t.Add(payWindow + protocol.SwapReserveMargin).Unix()
	trade.Id, err = k.NextTrade.Next(ctx)
	if err != nil {
		return 0, err
	}
	if err := k.Trades.Set(ctx, trade.Id, trade); err != nil {
		return 0, err
	}
	if err := k.TradesByEnd.Set(ctx, collections.Join(trade.ReservedUntil, trade.Id)); err != nil {
		return 0, err
	}
	if err := k.Hashes.Set(ctx, trade.Hash, trade.Id); err != nil {
		return 0, err
	}
	o.TradeId = trade.Id
	if err := k.Offers.Set(ctx, o.Id, o); err != nil {
		return 0, err
	}
	k.emit(ctx, "offer_taken", "id", u(o.Id), "trade_id", u(trade.Id), "seller", trade.Seller, "buyer", trade.Buyer,
		"lock", u(trade.Lock), "usdc", u(trade.Usdc), "hash", hex.EncodeToString(trade.Hash),
		"seller_polygon", trade.SellerPolygon, "pay_by", strconv.FormatInt(trade.PayBy, 10))
	return trade.Id, nil
}

func (k Keeper) removeTrade(ctx context.Context, tr types.Trade) error {
	if err := k.Trades.Remove(ctx, tr.Id); err != nil {
		return err
	}
	if buyer, err := k.addressCodec.StringToBytes(tr.Buyer); err == nil {
		if err := k.add(ctx, k.BuyerTrades, buyer, -1); err != nil {
			return err
		}
	}
	return k.TradesByEnd.Remove(ctx, collections.Join(tr.ReservedUntil, tr.Id))
}

// Settle finishes a trade whose USDC payment was seen on Polygon: the secret
// that paid the seller is shown here. The buyer gets the LOCK, the fee is
// split, and the trade counts for the price.
func (k Keeper) Settle(ctx context.Context, tradeID uint64, secret []byte) error {
	t := now(ctx)
	tr, err := k.Trades.Get(ctx, tradeID)
	if err != nil {
		// Paid after its reservation ended (late.go).
		late, ok, lerr := k.IsLate(ctx, tradeID)
		if lerr != nil {
			return lerr
		}
		if !ok {
			return types.ErrNotFound
		}
		if sum := sha256.Sum256(secret); !bytes.Equal(sum[:], late.Hash) {
			return types.ErrWrongSecret
		}
		lock, err := k.settleLate(ctx, late)
		if err != nil {
			return err
		}
		if err := k.Hashes.Set(ctx, late.Hash, 0); err != nil {
			return err
		}
		if lock == late.Lock && late.Usdc >= protocol.SwapMinPriceUSDC {
			id, err := k.NextFill.Next(ctx)
			if err != nil {
				return err
			}
			if err := k.Fills.Set(ctx, id, types.Fill{TradeId: late.Id, Lock: late.Lock, Usdc: late.Usdc, Time: t.Unix()}); err != nil {
				return err
			}
		}
		k.emit(ctx, "trade_settled", "trade_id", u(late.Id), "offer_id", u(late.OfferId), "seller", late.Seller, "buyer", late.Buyer,
			"lock", u(lock), "usdc", u(late.Usdc), "secret", hex.EncodeToString(secret), "late", "true")
		return nil
	}
	sum := sha256.Sum256(secret)
	if !bytes.Equal(sum[:], tr.Hash) {
		return types.ErrWrongSecret
	}
	// Still reserved: the LOCK are here, whatever the time (a payment seen
	// at the last moment settles).
	buyer, err := k.addressCodec.StringToBytes(tr.Buyer)
	if err != nil {
		return err
	}
	if err := k.bank.SendCoinsFromModuleToAccount(ctx, types.ModuleName, buyer, k.coins(tr.Lock)); err != nil {
		return err
	}
	if err := k.Hashes.Set(ctx, tr.Hash, 0); err != nil {
		return err
	}
	devShare, keepersShare, err := k.payFee(ctx, tr.Fee)
	if err != nil {
		return err
	}
	if err := k.removeTrade(ctx, tr); err != nil {
		return err
	}
	if o, err := k.Offers.Get(ctx, tr.OfferId); err == nil {
		if err := k.forgetOffer(ctx, o); err != nil {
			return err
		}
	}
	if tr.Usdc >= protocol.SwapMinPriceUSDC {
		id, err := k.NextFill.Next(ctx)
		if err != nil {
			return err
		}
		if err := k.Fills.Set(ctx, id, types.Fill{TradeId: tr.Id, Lock: tr.Lock, Usdc: tr.Usdc, Time: t.Unix()}); err != nil {
			return err
		}
	}
	k.emit(ctx, "trade_settled", "trade_id", u(tr.Id), "offer_id", u(tr.OfferId), "seller", tr.Seller, "buyer", tr.Buyer,
		"lock", u(tr.Lock), "usdc", u(tr.Usdc), "secret", hex.EncodeToString(secret),
		"fee", u(tr.Fee), "fee_dev_fund", u(devShare), "fee_lockkeepers", u(keepersShare))
	return nil
}

// payFee splits the swap fee held in the module like every fee.
func (k Keeper) payFee(ctx context.Context, fee uint64) (devShare, keepersShare uint64, err error) {
	genesis, err := k.fees.GenesisTime(ctx)
	if err != nil {
		return 0, 0, err
	}
	devShare, keepersShare = transfers.SplitFee(fee, now(ctx).Unix()-genesis, k.fees.FinalMilestoneReached(ctx))
	if devShare > 0 {
		devFund, err := k.fees.DevFundAddress(ctx)
		if err != nil {
			return 0, 0, err
		}
		if err := k.bank.SendCoinsFromModuleToAccount(ctx, types.ModuleName, devFund, k.coins(devShare)); err != nil {
			return 0, 0, err
		}
	}
	if keepersShare > 0 {
		if err := k.bank.SendCoinsFromModuleToModule(ctx, types.ModuleName, k.feeCollector, k.coins(keepersShare)); err != nil {
			return 0, 0, err
		}
	}
	return devShare, keepersShare, nil
}

// EndBlock ends unpaid reservations and expired offers. Every block.
// maxPerBlock: unpaid trades and expired offers handled in one block at most;
// the rest in the next blocks.
const maxPerBlock = 1000

func (k Keeper) EndBlock(ctx context.Context) error {
	t := now(ctx).Unix()
	// Unpaid trades: the LOCK go back.
	keys, err := firstKeys(ctx, k.TradesByEnd, collections.NewPrefixUntilPairRange[int64, uint64](t), maxPerBlock)
	if err != nil {
		return err
	}
	for _, key := range keys {
		tr, err := k.Trades.Get(ctx, key.K2())
		if err != nil {
			return err
		}
		if err := k.removeTrade(ctx, tr); err != nil {
			return err
		}
		// Unpaid: the trade still waits for a late payment (its secret stays
		// in use meanwhile, late.go), and the buyer waits before taking
		// another offer.
		if err := k.toLate(ctx, tr); err != nil {
			return err
		}
		if buyer, err := k.addressCodec.StringToBytes(tr.Buyer); err == nil {
			if err := k.UnpaidUntil.Set(ctx, buyer, t+int64(protocol.SwapUnpaidWait.Seconds())); err != nil {
				return err
			}
		}
		o, err := k.Offers.Get(ctx, tr.OfferId)
		if err != nil {
			return err
		}
		if o.Side == types.Side_SIDE_BUY {
			// The seller who took the buy offer gets the LOCK back; the buy
			// offer, unpaid by its own maker, comes down (its secret is
			// still in use).
			seller, err := k.addressCodec.StringToBytes(tr.Seller)
			if err != nil {
				return err
			}
			if err := k.bank.SendCoinsFromModuleToAccount(ctx, types.ModuleName, seller, k.coins(tr.Lock+tr.Fee)); err != nil {
				return err
			}
			if err := k.forgetOffer(ctx, o); err != nil {
				return err
			}
		} else {
			// The sell offer is open again, unless its seller pressed "Not
			// me" meanwhile (then its LOCK go back).
			o.TradeId = 0
			frozen := false
			if s := *k.shield; s != nil {
				if maker, err := k.addressCodec.StringToBytes(o.Maker); err == nil {
					frozen, _ = s.IsFrozen(ctx, maker)
				}
			}
			if frozen {
				if err := k.removeOffer(ctx, o, "offer_canceled"); err != nil {
					return err
				}
			} else if err := k.Offers.Set(ctx, o.Id, o); err != nil {
				return err
			}
		}
		k.emit(ctx, "trade_expired", "trade_id", u(tr.Id), "offer_id", u(o.Id), "seller", tr.Seller, "buyer", tr.Buyer)
	}
	if err := k.purgeLate(ctx); err != nil {
		return err
	}
	// Expired offers that are not in a trade.
	okeys, err := firstKeys(ctx, k.OffersByExpiry, collections.NewPrefixUntilPairRange[int64, uint64](t), maxPerBlock)
	if err != nil {
		return err
	}
	for _, key := range okeys {
		o, err := k.Offers.Get(ctx, key.K2())
		if err != nil {
			return err
		}
		if o.TradeId != 0 {
			continue // its reservation decides first
		}
		if err := k.removeOffer(ctx, o, "offer_expired"); err != nil {
			return err
		}
	}
	return nil
}

// AllOffers lists every offer, oldest first.
func (k Keeper) AllOffers(ctx context.Context) ([]types.Offer, error) {
	var out []types.Offer
	err := k.Offers.Walk(ctx, nil, func(_ uint64, o types.Offer) (bool, error) {
		out = append(out, o)
		return false, nil
	})
	return out, err
}

// TradesOf lists the trades waiting for payment where the account is the
// seller or the buyer.
func (k Keeper) TradesOf(ctx context.Context, addr string) ([]types.Trade, error) {
	var out []types.Trade
	err := k.Trades.Walk(ctx, nil, func(_ uint64, tr types.Trade) (bool, error) {
		if tr.Seller == addr || tr.Buyer == addr {
			out = append(out, tr)
		}
		return false, nil
	})
	return out, err
}

// Price returns the last trade, the totals of the last 24 hours and up to
// `recent` trades, newest first.
func (k Keeper) Price(ctx context.Context, recent int) (last *types.Fill, dayLock, dayUsdc uint64, fills []types.Fill, err error) {
	since := now(ctx).Add(-protocol.SwapPriceWindow).Unix()
	iter, err := k.Fills.Iterate(ctx, new(collections.Range[uint64]).Descending())
	if err != nil {
		return nil, 0, 0, nil, err
	}
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		f, err := iter.Value()
		if err != nil {
			return nil, 0, 0, nil, err
		}
		if last == nil {
			copy := f
			last = &copy
		}
		if f.Time >= since {
			dayLock += f.Lock
			dayUsdc += f.Usdc
		}
		if len(fills) < recent {
			fills = append(fills, f)
		}
		if f.Time < since && len(fills) >= recent {
			break
		}
	}
	return last, dayLock, dayUsdc, fills, nil
}

// maxMedianFills: the median reads at most the newest this many trades of the
// last 24 hours (a busy day never makes it slow).
const maxMedianFills = 10_000

// DayMedian is the price of the last 24 hours that half of the LOCK traded
// went above and half below (median weighed by volume), in billionths of a
// dollar per LOCK; 0 when there were no trades. Unlike the average, many
// tiny trades at a made-up price between two accounts of the same person
// barely move it: to move it they must trade more LOCK than everybody else
// that day, paying the 0,25% fee on all of it.
func (k Keeper) DayMedian(ctx context.Context) (uint64, error) {
	since := now(ctx).Add(-protocol.SwapPriceWindow).Unix()
	iter, err := k.Fills.Iterate(ctx, new(collections.Range[uint64]).Descending())
	if err != nil {
		return 0, err
	}
	defer iter.Close()
	type point struct{ price, lock uint64 }
	var points []point
	total := sdkmath.ZeroInt()
	for ; iter.Valid() && len(points) < maxMedianFills; iter.Next() {
		f, err := iter.Value()
		if err != nil {
			return 0, err
		}
		if f.Time < since {
			break
		}
		points = append(points, point{FillPrice(f), f.Lock})
		total = total.Add(sdkmath.NewIntFromUint64(f.Lock))
	}
	if len(points) == 0 {
		return 0, nil
	}
	sort.SliceStable(points, func(i, j int) bool { return points[i].price < points[j].price })
	half := sdkmath.ZeroInt()
	for _, p := range points {
		half = half.Add(sdkmath.NewIntFromUint64(p.lock))
		if half.MulRaw(2).GTE(total) {
			return p.price, nil
		}
	}
	return points[len(points)-1].price, nil
}

// ReferencePrice is the price that values LOCK (billionths of a dollar per
// LOCK): the 24-hour median, or the last trade when there was none in the
// last 24 hours; 0 before the first trade.
func (k Keeper) ReferencePrice(ctx context.Context) (uint64, error) {
	median, err := k.DayMedian(ctx)
	if err != nil || median != 0 {
		return median, err
	}
	last, _, _, _, err := k.Price(ctx, 0)
	if err != nil || last == nil {
		return 0, err
	}
	return FillPrice(*last), nil
}

// forgetOffer deletes an offer and its indexes (its LOCK are handled by the
// caller).
func (k Keeper) forgetOffer(ctx context.Context, o types.Offer) error {
	if err := k.Offers.Remove(ctx, o.Id); err != nil {
		return err
	}
	if o.ExpiresAt != 0 {
		if err := k.OffersByExpiry.Remove(ctx, collections.Join(o.ExpiresAt, o.Id)); err != nil {
			return err
		}
	}
	if maker, err := k.addressCodec.StringToBytes(o.Maker); err == nil {
		if err := k.ByMaker.Remove(ctx, collections.Join(sdk.AccAddress(maker), o.Id)); err != nil {
			return err
		}
		return k.add(ctx, k.OpenOffers, maker, -1)
	}
	return nil
}

// checkSell applies the seller's account protection (no shield: none).
func (k Keeper) checkSell(ctx context.Context, seller sdk.AccAddress, amount uint64) (bool, error) {
	if s := *k.shield; s != nil {
		return s.CheckSell(ctx, seller, amount)
	}
	return false, nil
}

// ApproveOffer, by the seller's second device: the sell offer can be taken.
func (k Keeper) ApproveOffer(ctx context.Context, device sdk.AccAddress, id uint64) error {
	o, err := k.Offers.Get(ctx, id)
	if err != nil {
		return types.ErrNotFound
	}
	if !o.AwaitingApproval {
		return nil
	}
	maker, err := k.addressCodec.StringToBytes(o.Maker)
	if err != nil {
		return err
	}
	s := *k.shield
	if s == nil {
		return types.ErrNotDevice
	}
	if ok, err := s.IsDeviceOf(ctx, maker, device); err != nil {
		return err
	} else if !ok {
		return types.ErrNotDevice
	}
	if frozen, err := s.IsFrozen(ctx, maker); err != nil {
		return err
	} else if frozen {
		return types.ErrSellerFrozen
	}
	o.AwaitingApproval = false
	if err := k.Offers.Set(ctx, id, o); err != nil {
		return err
	}
	k.emit(ctx, "offer_approved", "id", u(id), "maker", o.Maker)
	return nil
}

// TakeDownOffers removes an account's open offers ("Not me": a sell offer's
// LOCK go back to the account). Offers already in a trade stay: the buyer
// may have paid.
func (k Keeper) TakeDownOffers(ctx context.Context, maker sdk.AccAddress) error {
	iter, err := k.ByMaker.Iterate(ctx, collections.NewPrefixedPairRange[sdk.AccAddress, uint64](maker))
	if err != nil {
		return err
	}
	var ids []uint64
	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			iter.Close()
			return err
		}
		ids = append(ids, key.K2())
	}
	iter.Close()
	for _, id := range ids {
		o, err := k.Offers.Get(ctx, id)
		if err != nil || o.TradeId != 0 {
			continue
		}
		if err := k.removeOffer(ctx, o, "offer_canceled"); err != nil {
			return err
		}
	}
	return nil
}

func (k Keeper) count(ctx context.Context, m collections.Map[sdk.AccAddress, uint64], addr sdk.AccAddress) (uint64, error) {
	n, err := m.Get(ctx, addr)
	if errors.Is(err, collections.ErrNotFound) {
		return 0, nil
	}
	return n, err
}

// add changes a per-account counter (removed at zero).
func (k Keeper) add(ctx context.Context, m collections.Map[sdk.AccAddress, uint64], addr sdk.AccAddress, d int) error {
	n, err := k.count(ctx, m, addr)
	if err != nil {
		return err
	}
	switch {
	case d < 0 && n <= uint64(-d):
		return m.Remove(ctx, addr)
	case d < 0:
		return m.Set(ctx, addr, n-uint64(-d))
	default:
		return m.Set(ctx, addr, n+uint64(d))
	}
}

// firstKeys reads at most limit keys of a range, so a flood of due items
// cannot slow a block down (the rest go in the next blocks).
func firstKeys[K any](ctx context.Context, set collections.KeySet[K], rng collections.Ranger[K], limit int) ([]K, error) {
	iter, err := set.Iterate(ctx, rng)
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	var keys []K
	for ; iter.Valid() && len(keys) < limit; iter.Next() {
		key, err := iter.Key()
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func pair(a int64, b uint64) collections.Pair[int64, uint64] { return collections.Join(a, b) }

// Candle limits: at most this many candles per answer, and this many trades
// read for one answer (a query never runs for long, however busy the market).
const (
	maxCandles     = 500
	maxCandleFills = 200_000
)

// FillPrice is a trade's price in billionths of a dollar per LOCK.
func FillPrice(f types.Fill) uint64 {
	if f.Lock == 0 {
		return 0
	}
	// usdc has 6 decimals, LOCK 8: dollars per LOCK = usdc·10² / lock.
	p := sdkmath.NewIntFromUint64(f.Usdc).MulRaw(100_000_000_000).Quo(sdkmath.NewIntFromUint64(f.Lock))
	if !p.IsUint64() {
		return ^uint64(0)
	}
	return p.Uint64()
}

// Candles groups the finished trades into candles of `interval` seconds (the
// newest `limit` ones that have trades, starting before `before` when it is
// not 0), oldest first. Trades are kept forever, so the whole history can be
// read back page by page.
func (k Keeper) Candles(ctx context.Context, interval int64, limit int, before int64) ([]types.Candle, error) {
	if interval < 60 || interval > 7*86400 {
		return nil, errors.New("interval must be 60 to 604800 seconds")
	}
	if limit <= 0 || limit > maxCandles {
		limit = maxCandles
	}
	rng := new(collections.Range[uint64]).Descending()
	if before > 0 {
		before -= before % interval // whole candles only
		last, ok, err := k.lastFillBefore(ctx, before)
		if err != nil || !ok {
			return nil, err
		}
		rng = rng.EndInclusive(last)
	}
	iter, err := k.Fills.Iterate(ctx, rng)
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	var out []types.Candle // newest first while reading
	for n := 0; iter.Valid() && n < maxCandleFills; iter.Next() {
		n++
		f, err := iter.Value()
		if err != nil {
			return nil, err
		}
		start := f.Time - f.Time%interval
		p := FillPrice(f)
		if len(out) == 0 || out[len(out)-1].Time != start {
			if len(out) == limit {
				break
			}
			out = append(out, types.Candle{Time: start, Open: p, High: p, Low: p, Close: p})
		}
		c := &out[len(out)-1]
		c.Open = p // reading backwards: the oldest trade of the candle comes last
		c.High = max(c.High, p)
		c.Low = min(c.Low, p)
		c.Lock += f.Lock
		c.Usdc += f.Usdc
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// lastFillBefore finds the newest trade done before t (unix seconds). Trades
// are numbered from 1 in time order, so a binary search reads only a few of
// them however long the history.
func (k Keeper) lastFillBefore(ctx context.Context, t int64) (uint64, bool, error) {
	next, err := k.NextFill.Peek(ctx)
	if err != nil {
		return 0, false, err
	}
	lo, hi, found := uint64(1), next, uint64(0)
	for lo < hi {
		mid := lo + (hi-lo)/2
		f, err := k.Fills.Get(ctx, mid)
		if err != nil {
			return 0, false, err
		}
		if f.Time < t {
			found, lo = mid, mid+1
		} else {
			hi = mid
		}
	}
	return found, found != 0, nil
}
