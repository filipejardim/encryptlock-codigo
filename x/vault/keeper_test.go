package vault

import (
	"context"
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
	"encryptlock/lockkeeper/x/vault/types"
)

const denom = "lockunit"

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

var (
	fund  = sdk.AccAddress("investment_fund_____")
	start = time.Date(2026, time.October, 4, 12, 0, 0, 0, time.UTC)
	q1End = time.Date(2026, time.December, 31, 23, 59, 59, 0, time.UTC).Unix()
)

func setup(t *testing.T) (Keeper, *fakeBank, sdk.Context) {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.ModuleName)
	ctx := testutil.DefaultContext(key, storetypes.NewTransientStoreKey("transient_test")).WithBlockTime(start)
	bank := &fakeBank{balances: map[string]uint64{string(fund): 1_000 * m}}
	k := NewKeeper(runtime.NewKVStoreService(key), codec.NewProtoCodec(codectypes.NewInterfaceRegistry()), bank, authcodec.NewBech32Codec("lock"), denom)
	NewAppModule(k).InitGenesis(ctx, nil, NewAppModule(k).DefaultGenesis(nil))
	return k, bank, ctx
}

func TestLockedCoinsCannotBeSpentAndProveReserves(t *testing.T) {
	k, bank, ctx := setup(t)
	if _, err := k.Lock(ctx, fund, 800*m, q1End); err != nil {
		t.Fatal(err)
	}
	if bank.balances[string(fund)] != 200*m {
		t.Fatalf("spendable balance = %d, want 200 (800 locked)", bank.balances[string(fund)])
	}
	// Auditor: "does the fund hold at least 800 until the end of the quarter?"
	if got, _ := k.Reserves(ctx, fund, q1End); got != 800*m {
		t.Fatalf("reserves until end of quarter = %d, want 800", got)
	}
	// Not after the lock ends.
	if got, _ := k.Reserves(ctx, fund, q1End+1); got != 0 {
		t.Fatalf("reserves after the lock = %d, want 0", got)
	}
}

func TestLockReturnsAutomaticallyAtItsDate(t *testing.T) {
	k, bank, ctx := setup(t)
	if _, err := k.Lock(ctx, fund, 800*m, q1End); err != nil {
		t.Fatal(err)
	}
	if err := k.ReleaseDue(ctx.WithBlockTime(time.Unix(q1End-1, 0))); err != nil {
		t.Fatal(err)
	}
	if bank.balances[string(fund)] != 200*m {
		t.Fatal("released early")
	}
	if err := k.ReleaseDue(ctx.WithBlockTime(time.Unix(q1End, 0))); err != nil {
		t.Fatal(err)
	}
	if bank.balances[string(fund)] != 1_000*m {
		t.Fatalf("balance after release = %d, want 1000", bank.balances[string(fund)])
	}
	if locks, _ := k.LocksOf(ctx, fund); len(locks) != 0 {
		t.Fatal("released lock still listed")
	}
}

// m: 0,01 LOCK, the smallest lock.
const m = protocol.MinLockUnits

func TestTooManyLocks(t *testing.T) {
	k, _, ctx := setup(t)
	for range protocol.MaxLocksPerAccount {
		if _, err := k.Lock(ctx, fund, m, q1End); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := k.Lock(ctx, fund, m, q1End); !errors.Is(err, types.ErrInvalidLock) {
		t.Fatalf("lock over the limit: %v", err)
	}
}

func TestInvalidLocks(t *testing.T) {
	k, _, ctx := setup(t)
	cases := map[string]struct {
		amount uint64
		until  int64
	}{
		"zero":            {0, q1End},
		"below 0,01 LOCK": {m - 1, q1End},
		"in the past":     {m, start.Unix() - 1},
		"now":             {m, start.Unix()},
		"over 10 years":   {m, start.AddDate(10, 0, 1).Unix()},
	}
	for name, c := range cases {
		if _, err := k.Lock(ctx, fund, c.amount, c.until); !errors.Is(err, types.ErrInvalidLock) {
			t.Errorf("%s: err = %v, want ErrInvalidLock", name, err)
		}
	}
	if _, err := k.Lock(ctx, fund, 1_001*m, q1End); !errors.Is(err, sdkerrors.ErrInsufficientFunds) {
		t.Errorf("locking more than the balance: err = %v", err)
	}
}

func TestLocksSurviveGenesisExport(t *testing.T) {
	k, _, ctx := setup(t)
	if _, err := k.Lock(ctx, fund, 500*m, q1End); err != nil {
		t.Fatal(err)
	}
	exported := NewAppModule(k).ExportGenesis(ctx, nil)
	k2, _, ctx2 := setup(t)
	NewAppModule(k2).InitGenesis(ctx2, nil, exported)
	if got, _ := k2.Reserves(ctx2, fund, q1End); got != 500*m {
		t.Fatalf("reserves after export = %d, want 500", got)
	}
}
