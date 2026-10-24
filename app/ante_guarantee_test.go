package app

import (
	"context"
	"testing"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"encryptlock/lockkeeper/x/market"
	markettypes "encryptlock/lockkeeper/x/market/types"
)

type fakePricer struct{ last *markettypes.Fill }

// The reference price of one trade (as when it is the only one of the day).
func (p fakePricer) ReferencePrice(context.Context) (uint64, error) {
	if p.last == nil {
		return 0, nil
	}
	return market.FillPrice(*p.last), nil
}

func runGuarantee(last *markettypes.Fill, value, minSelf int64) error {
	msg := &stakingtypes.MsgCreateValidator{
		Value:             sdk.NewInt64Coin(BaseDenom, value),
		MinSelfDelegation: sdkmath.NewInt(minSelf),
	}
	next := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil }
	_, err := MinGuaranteeDecorator{market: fakePricer{last}}.AnteHandle(sdk.Context{}, fakeTx{msgs: []sdk.Msg{msg}}, false, next)
	return err
}

func TestMinGuarantee(t *testing.T) {
	// Reference price: 4 LOCK for 2 USDC, so 1 dollar = 2 LOCK = 200.000.000 lockunit.
	trade := &markettypes.Fill{Lock: 400_000_000, Usdc: 2_000_000}
	if err := runGuarantee(trade, 200_000_000, 200_000_000); err != nil {
		t.Errorf("1 dollar of LOCK was rejected: %v", err)
	}
	if err := runGuarantee(trade, 199_999_999, 199_999_999); err == nil {
		t.Error("less than 1 dollar of LOCK was accepted")
	}
	if err := runGuarantee(trade, 500_000_000, 1); err == nil {
		t.Error("a minimum self-delegation below 1 dollar was accepted")
	}
	// Before the first trade there is no price, so no minimum.
	if err := runGuarantee(nil, 1, 1); err != nil {
		t.Errorf("without a price yet any guarantee must be accepted: %v", err)
	}
	// Rounded up: 3.000.001 lockunit for 2 USDC → 1 dollar = 1.500.000,5 → 1.500.001.
	need, _ := MinGuarantee(context.Background(), fakePricer{&markettypes.Fill{Lock: 3_000_001, Usdc: 2_000_000}})
	if !need.Equal(sdkmath.NewInt(1_500_001)) {
		t.Errorf("need = %s, want 1500001 (rounded up)", need)
	}
	// LOCK very expensive: never less than 0,01 LOCK.
	need, _ = MinGuarantee(context.Background(), fakePricer{&markettypes.Fill{Lock: 1, Usdc: 2_000_000}})
	if !need.Equal(sdkmath.NewInt(1_000_000)) {
		t.Errorf("need = %s, want the 0,01 LOCK floor", need)
	}
}
