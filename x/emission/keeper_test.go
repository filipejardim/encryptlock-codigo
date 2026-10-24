package emission

import (
	"context"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"encryptlock/lockkeeper/protocol"
)

const testDenom = "lockunit"

// fakeBank records minted coins and where they went.
type fakeBank struct {
	supply      sdkmath.Int
	collected   sdkmath.Int
	feeReceiver string
}

func (b *fakeBank) MintCoins(_ context.Context, _ string, amt sdk.Coins) error {
	b.supply = b.supply.Add(amt.AmountOf(testDenom))
	return nil
}

func (b *fakeBank) SendCoinsFromModuleToModule(_ context.Context, _, recipient string, amt sdk.Coins) error {
	b.feeReceiver = recipient
	b.collected = b.collected.Add(amt.AmountOf(testDenom))
	return nil
}

func (b *fakeBank) GetSupply(context.Context, string) sdk.Coin {
	return sdk.NewCoin(testDenom, b.supply)
}

var genesisTime = time.Date(2026, time.October, 4, 12, 0, 0, 0, time.UTC)

func setup(t *testing.T) (Keeper, *fakeBank, sdk.Context) {
	t.Helper()
	key := storetypes.NewKVStoreKey(ModuleName)
	ctx := testutil.DefaultContext(key, storetypes.NewTransientStoreKey("transient_test")).WithBlockTime(genesisTime)
	bank := &fakeBank{supply: sdkmath.NewIntFromUint64(GenesisAllocation), collected: sdkmath.ZeroInt()}
	k := NewKeeper(runtime.NewKVStoreService(key), bank, testDenom, "fee_collector")
	NewAppModule(k).InitGenesis(ctx, nil, NewAppModule(k).DefaultGenesis(nil))
	return k, bank, ctx
}

func at(ctx sdk.Context, elapsed time.Duration) sdk.Context {
	return ctx.WithBlockTime(genesisTime.Add(elapsed))
}

func TestNothingIsEmittedAtGenesis(t *testing.T) {
	k, bank, ctx := setup(t)
	if err := k.BeginBlock(ctx); err != nil {
		t.Fatal(err)
	}
	if !bank.supply.Equal(sdkmath.NewIntFromUint64(GenesisAllocation)) {
		t.Fatalf("supply at genesis = %s, want 1.000 LOCK", bank.supply)
	}
}

func TestEmissionGoesToLockKeepers(t *testing.T) {
	k, bank, ctx := setup(t)
	if err := k.BeginBlock(at(ctx, 30*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if bank.feeReceiver != "fee_collector" {
		t.Errorf("new coins went to %q, want the fee collector (paid out to LockKeepers)", bank.feeReceiver)
	}
	if !bank.collected.Equal(sdkmath.NewIntFromUint64(MonthlyEmission[0])) {
		t.Errorf("after month 1 = %s, want %d", bank.collected, MonthlyEmission[0])
	}
}

func TestSameBlockTimeEmitsOnlyOnce(t *testing.T) {
	k, bank, ctx := setup(t)
	block := at(ctx, 5*time.Second)
	for range 3 {
		if err := k.BeginBlock(block); err != nil {
			t.Fatal(err)
		}
	}
	if want := CumulativeTarget(5); !bank.collected.Equal(sdkmath.NewIntFromUint64(want)) {
		t.Fatalf("emitted %s, want %d", bank.collected, want)
	}
}

func TestMissedTimeIsCaughtUpNotLost(t *testing.T) {
	k, bank, ctx := setup(t)
	// the network stops for 10 days in month 2, then resumes
	for _, elapsed := range []time.Duration{24 * time.Hour, 35 * 24 * time.Hour, 45 * 24 * time.Hour} {
		if err := k.BeginBlock(at(ctx, elapsed)); err != nil {
			t.Fatal(err)
		}
	}
	want := CumulativeTarget(int64((45 * 24 * time.Hour).Seconds()))
	if !bank.collected.Equal(sdkmath.NewIntFromUint64(want)) {
		t.Fatalf("emitted %s, want %d", bank.collected, want)
	}
}

func TestSupplyAfter30YearsAndAt40MillionOnlyAtTheEnd(t *testing.T) {
	k, bank, ctx := setup(t)
	month := 30 * 24 * time.Hour
	for m := 1; m <= 360; m++ { // the 30 years
		if err := k.BeginBlock(at(ctx, time.Duration(m)*month)); err != nil {
			t.Fatal(err)
		}
	}
	// 40.000.000 minus the 800.000 of the everlasting reserve.
	if want := sdkmath.NewIntFromUint64(protocol.MaxSupplyBaseUnits - ReserveAllocation); !bank.supply.Equal(want) {
		t.Fatalf("supply after 30 years = %s, want %s (39.200.000 LOCK)", bank.supply, want)
	}
	// Then the reserve, every 30 years half of what is left, until the end.
	// (Seconds: 1.500 years in time.Duration would overflow.)
	for y := int64(60); y <= 1_500; y += 30 {
		if err := k.BeginBlock(ctx.WithBlockTime(time.Unix(genesisTime.Unix()+y*12*MonthSeconds, 0))); err != nil {
			t.Fatal(err)
		}
	}
	if want := sdkmath.NewIntFromUint64(protocol.MaxSupplyBaseUnits); !bank.supply.Equal(want) {
		t.Fatalf("final supply = %s, want exactly %s (40.000.000 LOCK)", bank.supply, want)
	}
}

func TestGenesisWithExtraCoinsIsRejected(t *testing.T) {
	key := storetypes.NewKVStoreKey(ModuleName)
	ctx := testutil.DefaultContext(key, storetypes.NewTransientStoreKey("transient_test")).WithBlockTime(genesisTime)
	bank := &fakeBank{supply: sdkmath.NewIntFromUint64(GenesisAllocation + 1), collected: sdkmath.ZeroInt()}
	k := NewKeeper(runtime.NewKVStoreService(key), bank, testDenom, "fee_collector")

	defer func() {
		if recover() == nil {
			t.Fatal("genesis with more than 1.000 LOCK was accepted")
		}
	}()
	NewAppModule(k).InitGenesis(ctx, nil, NewAppModule(k).DefaultGenesis(nil))
}
