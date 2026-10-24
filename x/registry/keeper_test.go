package registry

import (
	"errors"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authcodec "github.com/cosmos/cosmos-sdk/x/auth/codec"

	"encryptlock/lockkeeper/x/registry/types"
)

func setup(t *testing.T) (Keeper, sdk.Context) {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.ModuleName)
	ctx := testutil.DefaultContext(key, storetypes.NewTransientStoreKey("transient_test")).
		WithBlockTime(time.Date(2026, time.October, 4, 12, 0, 0, 0, time.UTC))
	return NewKeeper(runtime.NewKVStoreService(key), authcodec.NewBech32Codec("lock")), ctx
}

func TestRegisterOnceThenRegistered(t *testing.T) {
	k, ctx := setup(t)
	addr := sdk.AccAddress("ana_________________")
	addrStr, _ := k.addressCodec.BytesToString(addr)

	if ok, _ := k.IsRegistered(ctx, addr); ok {
		t.Fatal("account registered before registering")
	}
	if _, err := (msgServer{k}).Register(ctx, &types.MsgRegister{Address: addrStr}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := k.IsRegistered(ctx, addr); !ok {
		t.Fatal("account not registered after registering")
	}

	res, err := (queryServer{k}).Registered(ctx, &types.QueryRegisteredRequest{Address: addrStr})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Registered || res.RegisteredAt != ctx.BlockTime().Unix() {
		t.Fatalf("query = %+v, want registered at %d", res, ctx.BlockTime().Unix())
	}
}

func TestSameAccountCannotRegisterTwice(t *testing.T) {
	k, ctx := setup(t)
	addr := sdk.AccAddress("ana_________________")
	if err := k.Register(ctx, addr); err != nil {
		t.Fatal(err)
	}
	if err := k.Register(ctx, addr); !errors.Is(err, types.ErrAlreadyRegistered) {
		t.Fatalf("second registration: err = %v, want ErrAlreadyRegistered", err)
	}
}

func TestUnknownAccountIsNotRegistered(t *testing.T) {
	k, ctx := setup(t)
	addrStr, _ := k.addressCodec.BytesToString(sdk.AccAddress("nobody______________"))
	res, err := (queryServer{k}).Registered(ctx, &types.QueryRegisteredRequest{Address: addrStr})
	if err != nil {
		t.Fatal(err)
	}
	if res.Registered {
		t.Fatal("unknown account reported as registered")
	}
}

func TestGenesisRoundTrip(t *testing.T) {
	k, ctx := setup(t)
	am := NewAppModule(k)
	for _, name := range []string{"ana_________________", "rui_________________"} {
		if err := k.Register(ctx, sdk.AccAddress(name)); err != nil {
			t.Fatal(err)
		}
	}
	exported := am.ExportGenesis(ctx, nil)
	if err := am.ValidateGenesis(nil, nil, exported); err != nil {
		t.Fatal(err)
	}

	k2, ctx2 := setup(t)
	NewAppModule(k2).InitGenesis(ctx2, nil, exported)
	for _, name := range []string{"ana_________________", "rui_________________"} {
		if ok, _ := k2.IsRegistered(ctx2, sdk.AccAddress(name)); !ok {
			t.Fatalf("%s lost after genesis export/import", name)
		}
	}
}
