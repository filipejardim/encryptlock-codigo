package rotation

import (
	"context"
	"errors"

	"cosmossdk.io/collections"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"encryptlock/lockkeeper/protocol"
)

// SigningPowers gives the power to sign of each member of the signing group
// (founder's decision, 20 Oct 2026).
//
// With 1 chip = 1 vote, more than 1/3 of the group switched off at once
// stops the blocks, and the network's clock with them, so they never even
// count as off: one home computer next to the founder's, switched off for
// the night, would stop the network until it came back. So, in the start
// phase (fewer than protocol.StableKeepers LockKeepers on and stable), each
// computer that started the network signs with 2n+1, n being the other
// members (each 1): more than 2/3 of the total. The others come and go
// freely. Alone it signs with 1, as before.
//
// startPhase reports that the start phase is on (the 5% cap then does not
// apply: it would take the 2/3 away). Earnings never depend on this (pay).
func (k Keeper) SigningPowers(ctx context.Context, group []stakingtypes.Validator, equal int64) (powers []int64, startPhase bool, err error) {
	t := now(ctx)
	powers = make([]int64, len(group))
	starter := make([]bool, len(group))
	others, stable, starters := 0, 0, 0
	for i, v := range group {
		powers[i] = equal
		valAddr, err := sdk.ValAddressFromBech32(v.GetOperator())
		if err != nil {
			return nil, false, err
		}
		if starter[i], err = k.ChipFree.Has(ctx, sdk.AccAddress(valAddr)); err != nil {
			return nil, false, err
		}
		if starter[i] {
			starters++
			continue
		}
		others++
		o, err := k.Online.Get(ctx, valAddr)
		if errors.Is(err, collections.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		if _, _, week, _ := HoursLog(o, t); week >= int64(protocol.StableWeek.Seconds()) {
			stable++
		}
	}
	if starters == 0 || stable >= protocol.StableKeepers {
		return powers, false, nil
	}
	for i := range group {
		if starter[i] {
			powers[i] = equal * int64(2*others+1)
		}
	}
	return powers, true, nil
}
