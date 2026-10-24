package app

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// MinGuaranteeUSDC: to become a LockKeeper a person locks at least 1 dollar
// of LOCK as guarantee (founder's decision, 5 Oct), in USDC base units
// (6 decimals). The coins stay the person's.
const MinGuaranteeUSDC uint64 = 1_000_000

// minGuaranteeUnits: never less than 0,01 LOCK (lockunit), however high the
// price.
const minGuaranteeUnits = 1_000_000

// referencePricer is the part of the market keeper the guarantee rule uses.
type referencePricer interface {
	ReferencePrice(ctx context.Context) (uint64, error)
}

// MinGuarantee returns how many lockunit are worth MinGuaranteeUSDC at the
// reference price (the 24-hour median weighed by volume, or the last trade
// when there was none in 24 hours; rounded up), or zero before the first
// trade. Not the last trade alone: one trade between two accounts of the
// same person at a made-up price could otherwise make joining cost almost
// nothing or thousands of LOCK.
func MinGuarantee(ctx context.Context, market referencePricer) (sdkmath.Int, error) {
	price, err := market.ReferencePrice(ctx)
	if err != nil || price == 0 {
		return sdkmath.ZeroInt(), err
	}
	// price is in billionths of a dollar per LOCK; 1 LOCK = 10^8 lockunit,
	// 1 USDC = 10^6: lockunit = usdc · 10^11 / price.
	num := sdkmath.NewIntFromUint64(MinGuaranteeUSDC).Mul(sdkmath.NewInt(100_000_000_000))
	p := sdkmath.NewIntFromUint64(price)
	need := num.Add(p).SubRaw(1).Quo(p)
	// At least 0,01 LOCK, the step in which the guarantee counts for earnings.
	return sdkmath.MaxInt(need, sdkmath.NewInt(minGuaranteeUnits)), nil
}

// MinGuaranteeDecorator rejects a new LockKeeper whose guarantee is worth
// less than 1 dollar at the reference price. It also requires the LockKeeper's
// minimum self-guarantee to be at least that much, so taking the coins out
// below it later takes the LockKeeper out of the set (Cosmos rule). Before
// the first trade there is no price and no minimum.
type MinGuaranteeDecorator struct {
	market referencePricer
}

func (d MinGuaranteeDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	for _, msg := range tx.GetMsgs() {
		create, ok := msg.(*stakingtypes.MsgCreateValidator)
		if !ok {
			continue
		}
		need, err := MinGuarantee(ctx, d.market)
		if err != nil {
			return ctx, err
		}
		if create.Value.Amount.LT(need) || create.MinSelfDelegation.LT(need) {
			return ctx, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
				"a LockKeeper needs at least %s lockunit in guarantee (1 dollar at the reference price), also as minimum self-delegation", need)
		}
	}
	return next(ctx, tx, simulate)
}
