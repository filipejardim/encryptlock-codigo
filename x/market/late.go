package market

import (
	"context"
	"errors"
	"time"

	"cosmossdk.io/collections"

	"encryptlock/lockkeeper/x/market/types"
)

// Late payments (security review, 11 Oct). The buyer pays the seller on
// Polygon the moment the secret is shown; if the LockKeepers could only
// confirm it after the reservation ended (Polygon servers down for a while,
// for instance), the seller would keep the USDC and the LOCK. So an unpaid
// trade is kept for LateWindow: a payment confirmed in that time still hands
// over the LOCK - from the offer if it is still open, otherwise from the
// seller, who got the USDC. Meanwhile its secret cannot be used again.
const LateWindow = 48 * time.Hour

// lateUntil: until when a trade waits for a late payment.
func lateUntil(tr types.Trade) int64 { return tr.ReservedUntil + int64(LateWindow.Seconds()) }

// toLate keeps an unpaid trade for LateWindow.
func (k Keeper) toLate(ctx context.Context, tr types.Trade) error {
	if err := k.Late.Set(ctx, tr.Id, tr); err != nil {
		return err
	}
	return k.LateByEnd.Set(ctx, collections.Join(lateUntil(tr), tr.Id))
}

// IsLate reports whether a trade ended unpaid less than LateWindow ago.
func (k Keeper) IsLate(ctx context.Context, id uint64) (types.Trade, bool, error) {
	tr, err := k.Late.Get(ctx, id)
	if errors.Is(err, collections.ErrNotFound) {
		return tr, false, nil
	}
	return tr, err == nil, err
}

// settleLate hands over the LOCK of a trade paid after its reservation.
func (k Keeper) settleLate(ctx context.Context, tr types.Trade) (paid uint64, err error) {
	buyer, err := k.addressCodec.StringToBytes(tr.Buyer)
	if err != nil {
		return 0, err
	}
	seller, err := k.addressCodec.StringToBytes(tr.Seller)
	if err != nil {
		return 0, err
	}
	need := tr.Lock + tr.Fee
	// The sell offer is still open with its LOCK: they pay.
	if o, err := k.Offers.Get(ctx, tr.OfferId); err == nil && o.Side == types.Side_SIDE_SELL && o.TradeId == 0 && o.Maker == tr.Seller && o.Lock == tr.Lock {
		if err := k.forgetOffer(ctx, o); err != nil {
			return 0, err
		}
	} else {
		// Otherwise from the seller's balance (it has the USDC), as much as
		// it holds.
		have := k.bank.SpendableCoins(ctx, seller).AmountOf(k.denom)
		if have.IsUint64() && have.Uint64() < need {
			need = have.Uint64()
		}
		if need > 0 {
			if err := k.bank.SendCoinsFromAccountToModule(ctx, seller, types.ModuleName, k.coins(need)); err != nil {
				return 0, err
			}
		}
	}
	lock, fee := min(need, tr.Lock), need-min(need, tr.Lock)
	if lock > 0 {
		if err := k.bank.SendCoinsFromModuleToAccount(ctx, types.ModuleName, buyer, k.coins(lock)); err != nil {
			return 0, err
		}
	}
	if fee > 0 {
		if _, _, err := k.payFee(ctx, fee); err != nil {
			return 0, err
		}
	}
	if err := k.forgetLate(ctx, tr); err != nil {
		return 0, err
	}
	if lock < tr.Lock {
		k.emit(ctx, "late_payment_short", "trade_id", u(tr.Id), "seller", tr.Seller, "buyer", tr.Buyer, "missing", u(tr.Lock-lock))
	}
	return lock, nil
}

func (k Keeper) forgetLate(ctx context.Context, tr types.Trade) error {
	if err := k.LateByEnd.Remove(ctx, collections.Join(lateUntil(tr), tr.Id)); err != nil {
		return err
	}
	return k.Late.Remove(ctx, tr.Id)
}

// purgeLate forgets unpaid trades older than LateWindow; their secrets are
// free again.
func (k Keeper) purgeLate(ctx context.Context) error {
	keys, err := firstKeys(ctx, k.LateByEnd, collections.NewPrefixUntilPairRange[int64, uint64](now(ctx).Unix()), maxPerBlock)
	if err != nil {
		return err
	}
	for _, key := range keys {
		tr, err := k.Late.Get(ctx, key.K2())
		if err == nil {
			if id, err := k.Hashes.Get(ctx, tr.Hash); err == nil && id == tr.Id {
				if err := k.Hashes.Remove(ctx, tr.Hash); err != nil {
					return err
				}
			}
		}
		if err := k.Late.Remove(ctx, key.K2()); err != nil {
			return err
		}
		if err := k.LateByEnd.Remove(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

// LateTrades lists the unpaid trades still waiting for a late payment.
func (k Keeper) LateTrades(ctx context.Context) ([]types.Trade, error) {
	var out []types.Trade
	err := k.Late.Walk(ctx, nil, func(_ uint64, tr types.Trade) (bool, error) {
		out = append(out, tr)
		return false, nil
	})
	return out, err
}
