package transfers

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"encryptlock/lockkeeper/protocol"
)

// TransferFee is 0,1% of the amount, at least TransferFeeMin and at most
// TransferFeeMax (lockunit).
func TransferFee(amount uint64) uint64 {
	// amount / 10.000 * 10, without overflow: amount ≤ 4·10^15.
	fee := amount * protocol.TransferFeeBasisPoints / 10_000
	if fee < protocol.TransferFeeMin {
		return protocol.TransferFeeMin
	}
	if fee > protocol.TransferFeeMax {
		return protocol.TransferFeeMax
	}
	return fee
}

// SplitFee divides a fee between the development fund and the LockKeepers,
// according to how long the network has existed (elapsedSeconds since
// genesis); once the final milestone is reached it is the last row (50/50)
// whatever the age. Rounding favours the LockKeepers.
func SplitFee(fee uint64, elapsedSeconds int64, finalMilestone bool) (devFund, lockKeepers uint64) {
	percent := protocol.FeeSplit[len(protocol.FeeSplit)-1].DevFundPercent
	for _, period := range protocol.FeeSplit {
		if finalMilestone {
			break
		}
		if period.UntilYear == 0 || elapsedSeconds < period.UntilYear*protocol.YearSeconds {
			percent = period.DevFundPercent
			break
		}
	}
	devFund = fee * percent / 100
	return devFund, fee - devFund
}

// FinalMilestoneReached reports whether the network has 10.000.000 active
// wallets (white paper, section 5.4): from then the fee split is 50/50. How
// the network counts active wallets by itself, without a central party and
// without seeing private balances, is decided in the privacy phase; until
// then it is never reached.
func (k Keeper) FinalMilestoneReached(context.Context) bool { return false }

// DevFundAddress is where the development fund's share of every fee goes.
func (k Keeper) DevFundAddress(ctx context.Context) (sdk.AccAddress, error) {
	bz, err := k.DevFund.Get(ctx)
	if err != nil {
		return nil, err
	}
	return sdk.AccAddress(bz), nil
}

// MoveDevFund makes [to] the development fund: only when the fund's own
// account is inherited or recovered (x/recovery), so its future fees reach
// the heir or the owner's new account instead of an address nobody holds.
func (k Keeper) MoveDevFund(ctx context.Context, to sdk.AccAddress) error {
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("dev_fund_moved", sdk.NewAttribute("to", to.String())))
	return k.DevFund.Set(ctx, to)
}
