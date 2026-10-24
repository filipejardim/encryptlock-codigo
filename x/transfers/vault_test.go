package transfers

import (
	"errors"
	"testing"
	"time"

	"encryptlock/lockkeeper/x/transfers/types"
)

const week = 7 * 24 * time.Hour

func TestVaultTransfersWaitBeforeTheyCanBeAccepted(t *testing.T) {
	e := setup(t)
	if err := e.k.SetVault(e.ctx, alice, 7); err != nil {
		t.Fatal(err)
	}
	id, err := e.k.Send(e.ctx, alice, bob, 50*L)
	if err != nil {
		t.Fatal(err)
	}

	e.after(week - time.Minute)
	if err := e.k.Accept(e.ctx, bob, id); !errors.Is(err, types.ErrNotReleased) {
		t.Fatalf("accepted before 7 days: err = %v", err)
	}
	if n, _ := e.k.ExpireDue(e.ctx); n != 0 {
		t.Fatal("refunded during the vault delay")
	}
	e.after(week + 59*time.Minute)
	if err := e.k.Accept(e.ctx, bob, id); err != nil {
		t.Fatalf("not accepted after 7 days: %v", err)
	}
	e.balance(t, bob, 70)
}

func TestVaultCanCancelDuringTheDelay(t *testing.T) {
	e := setup(t)
	if err := e.k.SetVault(e.ctx, alice, 7); err != nil {
		t.Fatal(err)
	}
	id, _ := e.k.Send(e.ctx, alice, bob, 50*L)
	e.after(6 * 24 * time.Hour)
	if err := e.k.Cancel(e.ctx, alice, id); err != nil {
		t.Fatalf("vault could not cancel during the delay: %v", err)
	}
	e.balance(t, alice, 100)
}

func TestVaultTransferNotAcceptedReturnsAfterDelayPlus1Hour(t *testing.T) {
	e := setup(t)
	if err := e.k.SetVault(e.ctx, alice, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := e.k.Send(e.ctx, alice, bob, 50*L); err != nil {
		t.Fatal(err)
	}
	e.after(week + time.Hour)
	if n, _ := e.k.ExpireDue(e.ctx); n != 1 {
		t.Fatal("not refunded 1 hour after the release")
	}
	e.balance(t, alice, 100)
}

func TestVaultDelayRules(t *testing.T) {
	e := setup(t)
	for _, days := range []uint32{1, 6, 366} {
		if err := e.k.SetVault(e.ctx, alice, days); !errors.Is(err, types.ErrInvalidVault) {
			t.Errorf("delay of %d days accepted", days)
		}
	}
	if err := e.k.SetVault(e.ctx, alice, 30); err != nil {
		t.Fatal(err)
	}
	// Shortening (or stopping) waits the current delay: 30 days.
	if err := e.k.SetVault(e.ctx, alice, 0); err != nil {
		t.Fatal(err)
	}
	s, _ := e.k.GetShield(e.ctx, alice)
	if s.VaultDelay != 30*24*60*60 {
		t.Fatal("vault stopped at once")
	}
	e.after(29 * 24 * time.Hour)
	s, _ = e.k.GetShield(e.ctx, alice)
	if s.VaultDelay == 0 {
		t.Fatal("vault stopped before 30 days")
	}
	e.after(30 * 24 * time.Hour)
	s, _ = e.k.GetShield(e.ctx, alice)
	if s.VaultDelay != 0 {
		t.Fatal("vault did not stop after 30 days")
	}
}

func TestNotMeStopsAVaultBeingWeakened(t *testing.T) {
	e := setup(t)
	if err := e.k.SetVault(e.ctx, alice, 30); err != nil {
		t.Fatal(err)
	}
	if err := e.k.SetVault(e.ctx, alice, 7); err != nil { // a thief tries to shorten it
		t.Fatal(err)
	}
	if _, err := e.k.Freeze(e.ctx, alice, alice); err != nil {
		t.Fatal(err)
	}
	e.after(40 * 24 * time.Hour)
	s, _ := e.k.GetShield(e.ctx, alice)
	if s.VaultDelay != 30*24*60*60 {
		t.Fatalf("vault delay = %d after freezing, want 30 days", s.VaultDelay)
	}
}
