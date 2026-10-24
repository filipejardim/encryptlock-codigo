package app

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"encryptlock/lockkeeper/x/emission"
	"encryptlock/lockkeeper/x/transfers"
)

// marketFees gives the market what the fee split needs: the genesis time
// (emission) and the development fund's address (fixed in transfers).
type marketFees struct {
	emission  emission.Keeper
	transfers transfers.Keeper
}

func (f marketFees) GenesisTime(ctx context.Context) (int64, error) {
	return f.emission.GenesisTime(ctx)
}

func (f marketFees) FinalMilestoneReached(ctx context.Context) bool {
	return f.transfers.FinalMilestoneReached(ctx)
}

func (f marketFees) DevFundAddress(ctx context.Context) (sdk.AccAddress, error) {
	return f.transfers.DevFundAddress(ctx)
}
