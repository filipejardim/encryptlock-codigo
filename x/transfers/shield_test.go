package transfers

import (
	"context"
	"errors"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"encryptlock/lockkeeper/x/transfers/types"
)

var phone2 = sdk.AccAddress("alice_second_phone__")

func (e *env) str(addr sdk.AccAddress) string {
	s, _ := e.k.addressCodec.BytesToString(addr)
	return s
}

func (e *env) withDevice(t *testing.T) {
	t.Helper()
	if err := e.k.SetDevice(e.ctx, alice, e.str(phone2)); err != nil {
		t.Fatal(err)
	}
}

func (e *env) awaiting(t *testing.T, id uint64) bool {
	t.Helper()
	tr, err := e.k.get(e.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return tr.AwaitingApproval
}

func TestWithoutSecondDeviceNothingNeedsApproval(t *testing.T) {
	e := setup(t)
	id, _ := e.k.Send(e.ctx, alice, bob, 50*L)
	if e.awaiting(t, id) {
		t.Fatal("transfer waits for approval without a second device")
	}
}

func TestNewContactNeedsSecondDevice(t *testing.T) {
	e := setup(t)
	e.withDevice(t)

	id, _ := e.k.Send(e.ctx, alice, bob, 10*L)
	if !e.awaiting(t, id) {
		t.Fatal("first transfer to a new contact did not wait for approval")
	}
	if err := e.k.Accept(e.ctx, bob, id); !errors.Is(err, types.ErrAwaitingApproval) {
		t.Fatalf("recipient accepted before approval: err = %v", err)
	}
	if err := e.k.Approve(e.ctx, alice, id); !errors.Is(err, types.ErrNotDevice) {
		t.Fatalf("the main phone approved its own transfer: err = %v", err)
	}

	// Approved 23 hours later: the recipient then has 1 hour.
	e.after(23 * time.Hour)
	if err := e.k.Approve(e.ctx, phone2, id); err != nil {
		t.Fatal(err)
	}
	e.after(23*time.Hour + 59*time.Minute)
	if err := e.k.Accept(e.ctx, bob, id); err != nil {
		t.Fatalf("accept within 1 hour of approval: %v", err)
	}

	// bob is now a known contact: no approval needed.
	id2, _ := e.k.Send(e.ctx, alice, bob, 10*L)
	if e.awaiting(t, id2) {
		t.Fatal("transfer to a known contact waited for approval")
	}
}

func TestLargeAmountNeedsSecondDevice(t *testing.T) {
	e := setup(t)
	e.withDevice(t)
	if err := e.k.Contacts.Set(e.ctx, contactKey(alice, bob)); err != nil {
		t.Fatal(err)
	}
	if err := e.k.SetLimits(e.ctx, alice, 0, 20*L); err != nil {
		t.Fatal(err)
	}
	small, _ := e.k.Send(e.ctx, alice, bob, 20*L)
	large, _ := e.k.Send(e.ctx, alice, bob, 21*L)
	if e.awaiting(t, small) || !e.awaiting(t, large) {
		t.Fatal("only the transfer above 20 LOCK should wait for approval")
	}
}

func TestNotApprovedIn24HoursGoesBack(t *testing.T) {
	e := setup(t)
	e.withDevice(t)
	id, _ := e.k.Send(e.ctx, alice, bob, 10*L)

	e.after(23 * time.Hour)
	if n, _ := e.k.ExpireDue(e.ctx); n != 0 {
		t.Fatal("refunded before 24 hours")
	}
	e.after(24 * time.Hour)
	if n, _ := e.k.ExpireDue(e.ctx); n != 1 {
		t.Fatal("not refunded after 24 hours without approval")
	}
	if e.pending(id) {
		t.Fatal("still pending")
	}
	e.balance(t, alice, 100)
}

func TestNotMeFreezesAndCancelsPending(t *testing.T) {
	e := setup(t)
	e.withDevice(t)
	if _, err := e.k.Send(e.ctx, alice, bob, 30*L); err != nil {
		t.Fatal(err)
	}
	if _, err := e.k.Send(e.ctx, alice, bob, 20*L); err != nil {
		t.Fatal(err)
	}

	if _, err := e.k.Freeze(e.ctx, bob, alice); !errors.Is(err, types.ErrNotDevice) {
		t.Fatalf("a stranger froze the account: err = %v", err)
	}
	canceled, err := e.k.Freeze(e.ctx, phone2, alice)
	if err != nil {
		t.Fatal(err)
	}
	if canceled != 2 {
		t.Fatalf("canceled %d transfers, want 2", canceled)
	}
	e.balance(t, alice, 100) // everything back (minus fees)

	if _, err := e.k.Send(e.ctx, alice, bob, 1*L); !errors.Is(err, types.ErrFrozen) {
		t.Fatalf("frozen account sent: err = %v", err)
	}
	// Receiving still works.
	id, _ := e.k.Send(e.ctx, bob, alice, 5*L)
	if err := e.k.Accept(e.ctx, alice, id); err != nil {
		t.Fatalf("frozen account could not accept: %v", err)
	}
}

func TestUnfreezeWithBothKeysAtOnceOrOneKeyAfter7Days(t *testing.T) {
	e := setup(t)
	e.withDevice(t)

	// Both keys: at once.
	if _, err := e.k.Freeze(e.ctx, alice, alice); err != nil {
		t.Fatal(err)
	}
	if ok, _, _ := e.k.Unfreeze(e.ctx, alice, alice); ok {
		t.Fatal("unfrozen with one key at once")
	}
	if ok, _, err := e.k.Unfreeze(e.ctx, phone2, alice); err != nil || !ok {
		t.Fatalf("not unfrozen with both keys: %v", err)
	}

	// One key (e.g. a thief with the main phone): 7 days.
	if _, err := e.k.Freeze(e.ctx, phone2, alice); err != nil {
		t.Fatal(err)
	}
	_, at, err := e.k.Unfreeze(e.ctx, alice, alice)
	if err != nil {
		t.Fatal(err)
	}
	if want := e.ctx.BlockTime().Add(7 * 24 * time.Hour).Unix(); at != want {
		t.Fatalf("unfreezes at %d, want %d", at, want)
	}
	e.after(7*24*time.Hour - time.Second)
	if _, err := e.k.Send(e.ctx, alice, bob, 1*L); !errors.Is(err, types.ErrFrozen) {
		t.Fatal("unfrozen before 7 days")
	}
	e.after(7 * 24 * time.Hour)
	if _, err := e.k.Send(e.ctx, alice, bob, 1*L); err != nil {
		t.Fatalf("still frozen after 7 days: %v", err)
	}
}

func TestDailyLimit(t *testing.T) {
	e := setup(t)
	if err := e.k.SetLimits(e.ctx, alice, 10*L, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := e.k.Send(e.ctx, alice, bob, 6*L); err != nil {
		t.Fatal(err)
	}
	if _, err := e.k.Send(e.ctx, alice, bob, 5*L); !errors.Is(err, types.ErrDailyLimit) {
		t.Fatalf("went over the daily limit: err = %v", err)
	}
	if _, err := e.k.Send(e.ctx, alice, bob, 4*L); err != nil {
		t.Fatalf("within the limit: %v", err)
	}
	e.after(24 * time.Hour)
	if _, err := e.k.Send(e.ctx, alice, bob, 10*L); err != nil {
		t.Fatalf("new day: %v", err)
	}
}

func TestLooseningWaits48HoursTighteningIsImmediate(t *testing.T) {
	e := setup(t)
	if err := e.k.SetLimits(e.ctx, alice, 10*L, 0); err != nil {
		t.Fatal(err)
	}
	// Raise to 50: still 10 for 48 hours.
	if err := e.k.SetLimits(e.ctx, alice, 50*L, 0); err != nil {
		t.Fatal(err)
	}
	s, _ := e.k.GetShield(e.ctx, alice)
	if s.DailyLimit != 10*L {
		t.Fatalf("limit raised at once to %d", s.DailyLimit)
	}
	e.after(48 * time.Hour)
	s, _ = e.k.GetShield(e.ctx, alice)
	if s.DailyLimit != 50*L {
		t.Fatalf("limit after 48 h = %d, want 50 LOCK", s.DailyLimit)
	}
	// Lower to 5: at once.
	if err := e.k.SetLimits(e.ctx, alice, 5*L, 0); err != nil {
		t.Fatal(err)
	}
	s, _ = e.k.GetShield(e.ctx, alice)
	if s.DailyLimit != 5*L {
		t.Fatalf("lowering did not apply at once: %d", s.DailyLimit)
	}
	// Removing the limit (0) is loosening: 48 hours.
	if err := e.k.SetLimits(e.ctx, alice, 0, 0); err != nil {
		t.Fatal(err)
	}
	s, _ = e.k.GetShield(e.ctx, alice)
	if s.DailyLimit != 5*L {
		t.Fatal("limit removed at once")
	}
}

func TestRemovingTheSecondDeviceTakes7DaysAndFreezeStopsIt(t *testing.T) {
	e := setup(t)
	e.withDevice(t)

	// A thief with the main phone tries to remove the second device.
	if err := e.k.SetDevice(e.ctx, alice, ""); err != nil {
		t.Fatal(err)
	}
	s, _ := e.k.GetShield(e.ctx, alice)
	if s.Device == "" {
		t.Fatal("second device removed at once")
	}
	// The owner sees it and presses "Not me" on the second device.
	if _, err := e.k.Freeze(e.ctx, phone2, alice); err != nil {
		t.Fatal(err)
	}
	e.after(8 * 24 * time.Hour)
	s, _ = e.k.GetShield(e.ctx, alice)
	if s.Device != e.str(phone2) {
		t.Fatal("the removal went through after freezing")
	}
}

func TestDeviceMustBeAnotherAccount(t *testing.T) {
	e := setup(t)
	if err := e.k.SetDevice(e.ctx, alice, e.str(alice)); !errors.Is(err, types.ErrInvalidDevice) {
		t.Fatalf("account set itself as its second device: err = %v", err)
	}
}

func TestShieldSurvivesGenesisExport(t *testing.T) {
	e := setup(t)
	e.withDevice(t)
	if err := e.k.SetLimits(e.ctx, alice, 10*L, 0); err != nil {
		t.Fatal(err)
	}
	id, _ := e.k.Send(e.ctx, alice, bob, 1*L)
	if err := e.k.Approve(e.ctx, phone2, id); err != nil {
		t.Fatal(err)
	}
	if err := e.k.Accept(e.ctx, bob, id); err != nil {
		t.Fatal(err)
	}

	exported := NewAppModule(e.k).ExportGenesis(e.ctx, nil)
	e2 := setup(t)
	NewAppModule(e2.k).InitGenesis(e2.ctx, nil, exported)
	s, _ := e2.k.GetShield(e2.ctx, alice)
	if s.Device != e.str(phone2) || s.DailyLimit != 10*L {
		t.Fatalf("shield lost in export: %+v", s)
	}
	if known, _ := e2.k.Contacts.Has(e2.ctx, contactKey(alice, bob)); !known {
		t.Fatal("contact lost in export")
	}
}

// A thief cannot make "Not me" fail by first sending hundreds of tiny
// transfers: the freeze still happens, at most 100 come back at once, and the
// others cannot be accepted and come back when they expire.
func TestNotMeWorksWithManyPendingTransfers(t *testing.T) {
	e := setup(t)
	var ids []uint64
	for i := 0; i < 150; i++ {
		id, err := e.k.Send(e.ctx, alice, bob, L/10)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	canceled, err := e.k.Freeze(e.ctx, alice, alice)
	if err != nil {
		t.Fatal(err)
	}
	if canceled != freezeRefundsPerTx {
		t.Fatalf("canceled %d at once, want %d", canceled, freezeRefundsPerTx)
	}
	left := 0
	for _, id := range ids {
		if !e.pending(id) {
			continue
		}
		left++
		if err := e.k.Accept(e.ctx, bob, id); !errors.Is(err, types.ErrFrozen) {
			t.Fatalf("a transfer from a frozen account was accepted: err = %v", err)
		}
	}
	if left != 50 {
		t.Fatalf("%d still pending, want 50", left)
	}
	e.after(time.Hour)
	if _, err := e.k.ExpireDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if e.pending(id) {
			t.Fatal("a transfer did not come back when it expired")
		}
	}
}

// While frozen, protections can only get stricter: no new or different
// second device, no looser limits, no shorter vault.
func TestFrozenAccountCannotLoosenProtections(t *testing.T) {
	e := setup(t)
	e.withDevice(t)
	if err := e.k.SetLimits(e.ctx, alice, 10*L, 5*L); err != nil {
		t.Fatal(err)
	}
	if err := e.k.SetVault(e.ctx, alice, 30); err != nil {
		t.Fatal(err)
	}
	if _, err := e.k.Freeze(e.ctx, phone2, alice); err != nil {
		t.Fatal(err)
	}
	thief := sdk.AccAddress("thief_phone_________")
	if err := e.k.SetDevice(e.ctx, alice, e.str(thief)); !errors.Is(err, types.ErrFrozen) {
		t.Fatalf("second device changed while frozen: err = %v", err)
	}
	if err := e.k.SetDevice(e.ctx, alice, ""); !errors.Is(err, types.ErrFrozen) {
		t.Fatalf("second device removed while frozen: err = %v", err)
	}
	if err := e.k.SetLimits(e.ctx, alice, 0, 5*L); !errors.Is(err, types.ErrFrozen) {
		t.Fatalf("daily limit loosened while frozen: err = %v", err)
	}
	if err := e.k.SetVault(e.ctx, alice, 7); !errors.Is(err, types.ErrFrozen) {
		t.Fatalf("vault shortened while frozen: err = %v", err)
	}
	// Stricter is still fine.
	if err := e.k.SetLimits(e.ctx, alice, 5*L, 1*L); err != nil {
		t.Fatalf("stricter limits refused while frozen: %v", err)
	}
	if err := e.k.SetVault(e.ctx, alice, 60); err != nil {
		t.Fatalf("longer vault refused while frozen: %v", err)
	}
}

// With no second device, someone with the 24 words cannot add one of his own
// while the account is frozen and then unfreeze at once with "both keys".
func TestFrozenAccountCannotAddDeviceToUnfreeze(t *testing.T) {
	e := setup(t)
	if _, err := e.k.Freeze(e.ctx, alice, alice); err != nil {
		t.Fatal(err)
	}
	thief := sdk.AccAddress("thief_phone_________")
	if err := e.k.SetDevice(e.ctx, alice, e.str(thief)); !errors.Is(err, types.ErrFrozen) {
		t.Fatalf("device added while frozen: err = %v", err)
	}
	if _, _, err := e.k.Unfreeze(e.ctx, thief, alice); !errors.Is(err, types.ErrNotDevice) {
		t.Fatalf("a stranger asked to unfreeze: err = %v", err)
	}
}

// A thief with the words sends more vault transfers than "Not me" cancels at
// once; after the owner unfreezes, none of them can be accepted.
func TestTransfersSentBeforeNotMeStayDeadAfterUnfreeze(t *testing.T) {
	e := setup(t)
	if err := e.k.SetVault(e.ctx, alice, 8); err != nil {
		t.Fatal(err)
	}
	var ids []uint64
	for range freezeRefundsPerTx + 5 {
		id, err := e.k.Send(e.ctx, alice, bob, L/100)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	e.after(time.Hour)
	if _, err := e.k.Freeze(e.ctx, alice, alice); err != nil {
		t.Fatal(err)
	}
	left := ids[len(ids)-1]
	if !e.pending(left) {
		t.Fatal("expected some transfers left pending after one Not me")
	}
	// The owner unfreezes alone: 7 days later.
	e.after(2 * time.Hour)
	if _, _, err := e.k.Unfreeze(e.ctx, alice, alice); err != nil {
		t.Fatal(err)
	}
	e.after(8*24*time.Hour + 30*time.Minute)
	if s, _ := e.k.GetShield(e.ctx, alice); s.Frozen {
		t.Fatal("still frozen after 7 days")
	}
	if err := e.k.Accept(e.ctx, bob, left); !errors.Is(err, types.ErrFrozen) {
		t.Fatalf("accepting a transfer sent before Not me: %v, want ErrFrozen", err)
	}
	// What the owner sends after unfreezing works as usual.
	id, err := e.k.Send(e.ctx, alice, bob, L)
	if err != nil {
		t.Fatal(err)
	}
	e.after(16*24*time.Hour + time.Hour)
	if err := e.k.Accept(e.ctx, bob, id); err != nil {
		t.Fatalf("new transfer after unfreezing: %v", err)
	}
}

func TestSellingFollowsTheShield(t *testing.T) {
	e := setup(t)
	called := false
	e.k.SetFreezeHook(func(context.Context, sdk.AccAddress) error { called = true; return nil })

	if need, err := e.k.CheckSell(e.ctx, alice, 10*L); err != nil || need {
		t.Fatalf("plain account: need=%v err=%v", need, err)
	}
	if err := e.k.SetLimits(e.ctx, alice, 15*L, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := e.k.CheckSell(e.ctx, alice, 20*L); !errors.Is(err, types.ErrDailyLimit) {
		t.Fatalf("selling over the daily limit: %v", err)
	}
	e.withDevice(t)
	if need, _ := e.k.CheckSell(e.ctx, alice, L); !need {
		t.Fatal("with a second device a sale must need approval")
	}
	if _, err := e.k.Freeze(e.ctx, alice, alice); err != nil || !called {
		t.Fatalf("Not me did not take the market offers down (err %v)", err)
	}
	if _, err := e.k.CheckSell(e.ctx, alice, L); !errors.Is(err, types.ErrFrozen) {
		t.Fatalf("frozen account selling: %v", err)
	}
	if err := e.k.SetVault(e.ctx, bob, 30); err != nil {
		t.Fatal(err)
	}
	if _, err := e.k.CheckSell(e.ctx, bob, L); !errors.Is(err, types.ErrVaultCannotSell) {
		t.Fatalf("vault selling: %v", err)
	}
}
