package transfers

import (
	"context"
	"encoding/json"
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
	"encryptlock/lockkeeper/x/transfers/types"
)

const (
	denom        = "lockunit"
	feeCollector = "fee_collector"
	// L is one LOCK in lockunit.
	L = protocol.BaseUnitsPerCoin
)

// fakeBank keeps balances by address (module accounts included) and the
// fees each account paid.
type fakeBank struct {
	balances  map[string]uint64
	feesPaid  map[string]uint64
	devFund   uint64
	lockKeepr uint64
}

func (b *fakeBank) move(from, to string, amt sdk.Coins) error {
	n := amt.AmountOf(denom).Uint64()
	if b.balances[from] < n {
		return sdkerrors.ErrInsufficientFunds
	}
	b.balances[from] -= n
	b.balances[to] += n
	return nil
}

func (b *fakeBank) SendCoins(_ context.Context, from, to sdk.AccAddress, amt sdk.Coins) error {
	if err := b.move(string(from), string(to), amt); err != nil {
		return err
	}
	if to.Equals(devFund) {
		b.feesPaid[string(from)] += amt.AmountOf(denom).Uint64()
		b.devFund += amt.AmountOf(denom).Uint64()
	}
	return nil
}

func (b *fakeBank) SendCoinsFromAccountToModule(_ context.Context, from sdk.AccAddress, module string, amt sdk.Coins) error {
	if module == feeCollector {
		b.feesPaid[string(from)] += amt.AmountOf(denom).Uint64()
		b.lockKeepr += amt.AmountOf(denom).Uint64()
	}
	return b.move(string(from), "module:"+module, amt)
}

func (b *fakeBank) SendCoinsFromModuleToAccount(_ context.Context, module string, to sdk.AccAddress, amt sdk.Coins) error {
	return b.move("module:"+module, string(to), amt)
}

type fakeClock struct{}

func (fakeClock) GenesisTime(context.Context) (int64, error) { return start.Unix(), nil }

type fakeRegistry map[string]bool

func (r fakeRegistry) IsRegistered(_ context.Context, addr sdk.AccAddress) (bool, error) {
	return r[string(addr)], nil
}

var (
	alice    = sdk.AccAddress("alice_______________")
	bob      = sdk.AccAddress("bob_________________")
	stranger = sdk.AccAddress("not_registered______")
	devFund  = sdk.AccAddress("development_fund____")
	start    = time.Date(2026, time.October, 4, 12, 0, 0, 0, time.UTC)
)

type env struct {
	k    Keeper
	bank *fakeBank
	ctx  sdk.Context
}

func setup(t *testing.T) *env {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.ModuleName)
	ctx := testutil.DefaultContext(key, storetypes.NewTransientStoreKey("transient_test")).WithBlockTime(start)
	bank := &fakeBank{balances: map[string]uint64{string(alice): 100 * L, string(bob): 20 * L}, feesPaid: map[string]uint64{}}
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	registry := fakeRegistry{string(alice): true, string(bob): true}
	addressCodec := authcodec.NewBech32Codec("lock")
	k := NewKeeper(runtime.NewKVStoreService(key), cdc, bank, registry, fakeClock{}, addressCodec, denom, feeCollector)
	devFundStr, _ := addressCodec.BytesToString(devFund)
	genesis, _ := json.Marshal(GenesisState{DevFundAddress: devFundStr, NextID: 1})
	NewAppModule(k).InitGenesis(ctx, nil, genesis)
	return &env{k: k, bank: bank, ctx: ctx}
}

func (e *env) after(d time.Duration) { e.ctx = e.ctx.WithBlockTime(start.Add(d)) }

// balance checks addr has want LOCK minus the fees it paid.
func (e *env) balance(t *testing.T, addr sdk.AccAddress, want uint64) {
	t.Helper()
	want = want*L - e.bank.feesPaid[string(addr)]
	if got := e.bank.balances[string(addr)]; got != want {
		t.Errorf("balance of %s = %d, want %d", addr, got, want)
	}
}

func (e *env) pending(id uint64) bool {
	has, _ := e.k.Pending.Has(e.ctx, id)
	return has
}

func TestAcceptedTransferMovesHeldAmount(t *testing.T) {
	e := setup(t)
	id, err := e.k.Send(e.ctx, alice, bob, 40*L)
	if err != nil {
		t.Fatal(err)
	}
	e.balance(t, alice, 60) // held at once
	e.balance(t, bob, 20)   // not yet received

	e.after(59 * time.Minute)
	if err := e.k.Accept(e.ctx, bob, id); err != nil {
		t.Fatal(err)
	}
	e.balance(t, alice, 60)
	e.balance(t, bob, 60)
	if e.pending(id) {
		t.Error("accepted transfer still pending")
	}
}

func TestOnlyRecipientCanAccept(t *testing.T) {
	e := setup(t)
	id, _ := e.k.Send(e.ctx, alice, bob, 40*L)
	if err := e.k.Accept(e.ctx, alice, id); !errors.Is(err, types.ErrNotRecipient) {
		t.Fatalf("sender accepting: err = %v, want ErrNotRecipient", err)
	}
	if !e.pending(id) {
		t.Fatal("transfer no longer pending")
	}
}

func TestSenderCanCancelAndGetsEverythingBack(t *testing.T) {
	e := setup(t)
	id, _ := e.k.Send(e.ctx, alice, bob, 40*L)
	if err := e.k.Cancel(e.ctx, bob, id); !errors.Is(err, types.ErrNotSender) {
		t.Fatalf("recipient canceling: err = %v, want ErrNotSender", err)
	}
	if err := e.k.Cancel(e.ctx, alice, id); err != nil {
		t.Fatal(err)
	}
	e.balance(t, alice, 100)
	e.balance(t, bob, 20)
	if err := e.k.Accept(e.ctx, bob, id); !errors.Is(err, types.ErrTransferNotFound) {
		t.Fatalf("accepting a canceled transfer: err = %v, want ErrTransferNotFound", err)
	}
}

func TestNotAcceptedWithin1HourReturnsAutomatically(t *testing.T) {
	e := setup(t)
	id, _ := e.k.Send(e.ctx, alice, bob, 40*L)

	// 59 minutes: nothing happens yet.
	e.after(59 * time.Minute)
	if n, err := e.k.ExpireDue(e.ctx); err != nil || n != 0 {
		t.Fatalf("ExpireDue at 59 min = %d, %v; want 0", n, err)
	}

	// Exactly 1 hour: too late to accept; the end of the block refunds.
	e.after(protocol.TransferAcceptanceWindow)
	if err := e.k.Accept(e.ctx, bob, id); !errors.Is(err, types.ErrExpired) {
		t.Fatalf("accepting at 1 h: err = %v, want ErrExpired", err)
	}
	if n, err := e.k.ExpireDue(e.ctx); err != nil || n != 1 {
		t.Fatalf("ExpireDue at 1 h = %d, %v; want 1", n, err)
	}
	e.balance(t, alice, 100)
	e.balance(t, bob, 20)

	// Refunded only once.
	if n, _ := e.k.ExpireDue(e.ctx); n != 0 {
		t.Fatalf("second ExpireDue refunded %d more", n)
	}
	e.balance(t, alice, 100)
}

func TestOnlyDueTransfersExpire(t *testing.T) {
	e := setup(t)
	first, _ := e.k.Send(e.ctx, alice, bob, 10*L)
	e.after(30 * time.Minute)
	second, _ := e.k.Send(e.ctx, alice, bob, 10*L)

	e.after(time.Hour) // first is due, second has 30 minutes left
	if n, _ := e.k.ExpireDue(e.ctx); n != 1 {
		t.Fatalf("expired %d, want 1", n)
	}
	if e.pending(first) || !e.pending(second) {
		t.Fatal("wrong transfer expired")
	}
	if err := e.k.Accept(e.ctx, bob, second); err != nil {
		t.Fatalf("second transfer could not be accepted: %v", err)
	}
}

func TestCannotSendToUnregisteredAccount(t *testing.T) {
	e := setup(t)
	if _, err := e.k.Send(e.ctx, alice, stranger, 10*L); !errors.Is(err, types.ErrRecipientNotRegistered) {
		t.Fatalf("err = %v, want ErrRecipientNotRegistered", err)
	}
	e.balance(t, alice, 100)
}

func TestInvalidTransfersAreRejected(t *testing.T) {
	e := setup(t)
	if _, err := e.k.Send(e.ctx, alice, alice, 10*L); !errors.Is(err, types.ErrSelfTransfer) {
		t.Errorf("to self: err = %v, want ErrSelfTransfer", err)
	}
	if _, err := e.k.Send(e.ctx, alice, bob, 0); !errors.Is(err, types.ErrZeroAmount) {
		t.Errorf("zero: err = %v, want ErrZeroAmount", err)
	}
	if _, err := e.k.Send(e.ctx, alice, bob, 101*L); !errors.Is(err, sdkerrors.ErrInsufficientFunds) {
		t.Errorf("more than balance: err = %v, want ErrInsufficientFunds", err)
	}
	e.balance(t, alice, 100)
}

func TestPendingQueryListsBothSides(t *testing.T) {
	e := setup(t)
	if _, err := e.k.Send(e.ctx, alice, bob, 10*L); err != nil {
		t.Fatal(err)
	}
	if _, err := e.k.Send(e.ctx, bob, alice, 5*L); err != nil {
		t.Fatal(err)
	}
	aliceStr, _ := e.k.addressCodec.BytesToString(alice)
	res, err := (queryServer{e.k}).Pending(e.ctx, &types.QueryPendingRequest{Address: aliceStr})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Sent) != 1 || res.Sent[0].Amount != 10*L || len(res.Received) != 1 || res.Received[0].Amount != 5*L {
		t.Fatalf("pending for alice = %+v", res)
	}
}

func TestTransferNumbersStartAt1(t *testing.T) {
	e := setup(t)
	id, err := e.k.Send(e.ctx, alice, bob, 10*L)
	if err != nil {
		t.Fatal(err)
	}
	if id != 1 {
		t.Fatalf("first transfer id = %d, want 1", id)
	}
}

// Dust cannot hide a real transfer: the largest come first.
func TestRealTransferShowsAmongDust(t *testing.T) {
	e := setup(t)
	for range maxListed + 5 {
		if _, err := e.k.Send(e.ctx, bob, alice, 1); err != nil {
			t.Fatal(err)
		}
	}
	real, err := e.k.Send(e.ctx, bob, alice, L)
	if err != nil {
		t.Fatal(err)
	}
	res, err := (queryServer{e.k}).Pending(e.ctx, &types.QueryPendingRequest{Address: e.str(alice)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Received) != maxListed || res.Received[0].Id != real {
		t.Fatalf("%d listed, first %d: the real transfer must come first", len(res.Received), res.Received[0].Id)
	}
}
