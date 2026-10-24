package registry

import (
	"context"
	"errors"
	"testing"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

type fakeAccounts map[string]sdk.AccountI

func (f fakeAccounts) GetAccount(_ context.Context, a sdk.AccAddress) sdk.AccountI { return f[string(a)] }
func (f fakeAccounts) RemoveAccount(_ context.Context, acc sdk.AccountI)           { delete(f, string(acc.GetAddress())) }

type fakeBalances map[string]int64

func (f fakeBalances) GetAllBalances(_ context.Context, a sdk.AccAddress) sdk.Coins {
	if n := f[string(a)]; n > 0 {
		return sdk.NewCoins(sdk.NewCoin("lockunit", sdkmath.NewInt(n)))
	}
	return sdk.NewCoins()
}

type fakeIncoming map[string]bool

func (f fakeIncoming) HasIncoming(_ context.Context, a sdk.AccAddress) (bool, error) {
	return f[string(a)], nil
}

func TestFreeTransactionsPerHour(t *testing.T) {
	k, ctx := setup(t)
	ana := sdk.AccAddress("ana_________________")
	for range FreeTxPerHour {
		if err := k.CountTx(ctx, ana, false); err != nil {
			t.Fatal(err)
		}
	}
	if err := k.CountTx(ctx, ana, false); !errors.Is(err, ErrBusy) {
		t.Fatalf("one more this hour: %v, want ErrBusy", err)
	}
	if err := k.CountTx(ctx, ana, true); err != nil {
		t.Fatalf("a transfer pays its fee and always goes: %v", err)
	}
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(hourSeconds * 1e9))
	if err := k.CountTx(ctx, ana, false); err != nil {
		t.Fatalf("next hour: %v", err)
	}
	if err := k.EndBlock(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestNewAccountsPerBlock(t *testing.T) {
	k, ctx := setup(t)
	ctx = ctx.WithBlockHeight(10)
	for range RegistrationsPerBlock {
		if err := k.CountRegistration(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := k.CountRegistration(ctx); !errors.Is(err, ErrBusy) {
		t.Fatalf("one more in the block: %v, want ErrBusy", err)
	}
	if err := k.CountRegistration(ctx.WithBlockHeight(11)); err != nil {
		t.Fatalf("next block: %v", err)
	}
}

func TestOnlyNeverUsedAccountsAreForgotten(t *testing.T) {
	k, ctx := setup(t)
	unused, rich, used, waiting := sdk.AccAddress("unused______________"), sdk.AccAddress("rich________________"),
		sdk.AccAddress("used________________"), sdk.AccAddress("waiting_____________")
	accounts := fakeAccounts{}
	for _, a := range []sdk.AccAddress{unused, rich, used, waiting} {
		acc := authtypes.NewBaseAccountWithAddress(a)
		_ = acc.SetSequence(1)
		accounts[string(a)] = acc
		if err := k.Register(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	_ = accounts[string(used)].SetSequence(5)
	k.SetPruneDeps(accounts, fakeBalances{string(rich): 1}, fakeIncoming{string(waiting): true})

	// Not yet 7 days: all kept.
	if err := k.EndBlock(ctx.WithBlockTime(ctx.BlockTime().Add(UnusedAfter / 2))); err != nil {
		t.Fatal(err)
	}
	if ok, _ := k.IsRegistered(ctx, unused); !ok {
		t.Fatal("forgotten too early")
	}
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(UnusedAfter + 1e9))
	if err := k.EndBlock(ctx); err != nil {
		t.Fatal(err)
	}
	if ok, _ := k.IsRegistered(ctx, unused); ok || accounts[string(unused)] != nil {
		t.Fatal("a never used account was kept")
	}
	for _, a := range []sdk.AccAddress{rich, used, waiting} {
		if ok, _ := k.IsRegistered(ctx, a); !ok || accounts[string(a)] == nil {
			t.Fatalf("%s is in use and was forgotten", a)
		}
	}
	// Registering again works.
	if err := k.Register(ctx, unused); err != nil {
		t.Fatal(err)
	}
}
