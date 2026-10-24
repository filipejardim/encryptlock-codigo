package powercap

import (
	"context"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"

	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

type fakeStaking struct{ validators []stakingtypes.Validator }

func (f *fakeStaking) GetLastValidators(context.Context) ([]stakingtypes.Validator, error) {
	return f.validators, nil
}

func (f *fakeStaking) PowerReduction(context.Context) sdkmath.Int { return sdkmath.OneInt() }

func newValidator(t *testing.T, power int64) stakingtypes.Validator {
	t.Helper()
	pk := ed25519.GenPrivKey().PubKey()
	v, err := stakingtypes.NewValidator(sdk.ValAddress(pk.Address()).String(), pk, stakingtypes.Description{})
	if err != nil {
		t.Fatal(err)
	}
	v.Status = stakingtypes.Bonded
	v.Tokens = sdkmath.NewInt(power)
	return v
}

func setup(t *testing.T) (CappedStaking, *fakeStaking, sdk.Context) {
	t.Helper()
	key := storetypes.NewKVStoreKey(ModuleName)
	ctx := testutil.DefaultContext(key, storetypes.NewTransientStoreKey("transient_test"))
	fake := &fakeStaking{}
	m := NewCappedStaking(staking.AppModule{}, fake, runtime.NewKVStoreService(key))
	m.innerEndBlock = func(context.Context) ([]abci.ValidatorUpdate, error) { return nil, nil }
	return m, fake, ctx
}

func powerOf(t *testing.T, updates []abci.ValidatorUpdate, v stakingtypes.Validator) (int64, bool) {
	t.Helper()
	want := v.ABCIValidatorUpdate(sdkmath.OneInt())
	for _, u := range updates {
		if u.PubKey.Equal(want.PubKey) {
			return u.Power, true
		}
	}
	return 0, false
}

func TestFounderAloneSendsNoChanges(t *testing.T) {
	m, fake, ctx := setup(t)
	fake.validators = []stakingtypes.Validator{newValidator(t, 1000)}
	updates, err := m.EndBlock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 0 {
		t.Fatalf("got %d updates, want none (genesis set unchanged)", len(updates))
	}
}

func TestCapSwitchesOnAt20AndRemovalsAreSent(t *testing.T) {
	m, fake, ctx := setup(t)
	founder := newValidator(t, 2000)
	fake.validators = []stakingtypes.Validator{founder}
	if _, err := m.EndBlock(ctx); err != nil {
		t.Fatal(err)
	}

	// 20 other LockKeepers join: 21 in total, the founder must drop to 5%.
	others := make([]stakingtypes.Validator, 20)
	for i := range others {
		others[i] = newValidator(t, 100)
	}
	fake.validators = append([]stakingtypes.Validator{founder}, others...)
	updates, err := m.EndBlock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	founderPower, ok := powerOf(t, updates, founder)
	if !ok {
		t.Fatal("founder power was not updated")
	}
	if 20*founderPower > founderPower+20*100 {
		t.Fatalf("founder voting power %d is above 5%%", founderPower)
	}
	if len(updates) != 21 {
		t.Fatalf("got %d updates, want 21 (founder + 20 new)", len(updates))
	}

	// Nothing changes: nothing is sent.
	if updates, _ := m.EndBlock(ctx); len(updates) != 0 {
		t.Fatalf("got %d updates for an unchanged set", len(updates))
	}

	// One LockKeeper leaves: it gets power 0; back to 20, the cap stays on.
	fake.validators = fake.validators[:20]
	updates, err = m.EndBlock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := powerOf(t, updates, others[19]); !ok || p != 0 {
		t.Fatalf("leaving LockKeeper got power %d (sent=%v), want 0", p, ok)
	}

	// Down to 19: the cap switches off and the founder gets full power back.
	fake.validators = fake.validators[:19]
	updates, err = m.EndBlock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := powerOf(t, updates, founder); p != 2000 {
		t.Fatalf("founder power = %d with 19 LockKeepers, want 2000 (cap off)", p)
	}
}

type allSign struct{}

func (allSign) Select(_ context.Context, vs []stakingtypes.Validator, _ func(stakingtypes.Validator) int64) ([]stakingtypes.Validator, error) {
	return vs, nil
}

func TestEveryMemberOfTheGroupCountsTheSame(t *testing.T) {
	m, fake, ctx := setup(t)
	m = m.WithSelector(allSign{})
	rich, small := newValidator(t, 1_000_000), newValidator(t, 1)
	fake.validators = []stakingtypes.Validator{rich, small}
	if _, err := m.EndBlock(ctx); err != nil {
		t.Fatal(err)
	}
	want, err := m.desired(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, upd := range want {
		if upd.Power != EqualPower {
			t.Fatalf("power %d, want %d for every member (1 chip = 1 vote)", upd.Power, EqualPower)
		}
	}
}
