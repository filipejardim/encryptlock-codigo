package rotation

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"encryptlock/lockkeeper/x/rotation/types"
)

func chipCode(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func TestOnlyProvenChipsSayIAmOn(t *testing.T) {
	e := setup(t, 10, 10)
	e.ctx = e.ctx.WithChainID("encryptlock-test")
	pub, priv, _ := ed25519.GenerateKey(nil)
	if err := e.k.InitGenesis(e.ctx, Genesis{ChipCheckers: [][]byte{pub}}); err != nil {
		t.Fatal(err)
	}
	ticket := func(code string, acc sdk.AccAddress) []byte {
		return ed25519.Sign(priv, TicketMessage("encryptlock-test", code, acc))
	}

	// No chip: no "I am on".
	if err := e.k.Heartbeat(e.ctx, e.accs[0]); !errors.Is(err, types.ErrNoChip) {
		t.Fatalf("heartbeat without a chip: %v, want ErrNoChip", err)
	}

	// The computer's helper proves its chip: the LockKeeper may say it is on.
	helper := sdk.AccAddress("helper______________")
	if err := e.k.SetHelper(e.ctx, e.accs[0], helper); err != nil {
		t.Fatal(err)
	}
	a := chipCode("chip A")
	if err := e.k.ProveChip(e.ctx, helper, a, ticket(a, e.accs[1])); !errors.Is(err, types.ErrBadChip) {
		t.Fatalf("ticket for another account: %v, want ErrBadChip", err)
	}
	if err := e.k.ProveChip(e.ctx, helper, a, []byte("forged")); !errors.Is(err, types.ErrBadChip) {
		t.Fatalf("forged ticket: %v, want ErrBadChip", err)
	}
	if err := e.k.ProveChip(e.ctx, helper, a, ticket(a, helper)); err != nil {
		t.Fatal(err)
	}
	e.beat(t, 0)

	// The same chip cannot serve a second LockKeeper.
	if err := e.k.ProveChip(e.ctx, e.accs[1], a, ticket(a, e.accs[1])); !errors.Is(err, types.ErrBadChip) {
		t.Fatalf("same chip for a second account: %v, want ErrBadChip", err)
	}
	if err := e.k.Heartbeat(e.ctx, e.accs[1]); !errors.Is(err, types.ErrNoChip) {
		t.Fatalf("second LockKeeper without a chip: %v", err)
	}

	// The helper moves to another computer: the old chip is free again.
	b := chipCode("chip B")
	if err := e.k.ProveChip(e.ctx, helper, b, ticket(b, helper)); err != nil {
		t.Fatal(err)
	}
	if err := e.k.ProveChip(e.ctx, e.accs[1], a, ticket(a, e.accs[1])); err != nil {
		t.Fatalf("freed chip: %v", err)
	}
	e.beat(t, 1)
}

func TestComputersThatStartTheNetworkAreExempt(t *testing.T) {
	e := setup(t, 10)
	pub, _, _ := ed25519.GenerateKey(nil)
	founder, _ := e.k.addressCodec.BytesToString(e.accs[0])
	if err := e.k.InitGenesis(e.ctx, Genesis{ChipCheckers: [][]byte{pub}, ChipFree: []string{founder}}); err != nil {
		t.Fatal(err)
	}
	e.beat(t, 0)
	g, err := e.k.ExportGenesis(e.ctx)
	if err != nil || len(g.ChipCheckers) != 1 || len(g.ChipFree) != 1 || g.ChipFree[0] != founder {
		t.Fatalf("exported genesis %+v, %v", g, err)
	}
}

func TestAfterAStopOnlyRealComputersSign(t *testing.T) {
	e := setup(t, 10, 10, 10)
	pub, _, _ := ed25519.GenerateKey(nil)
	founder, _ := e.k.addressCodec.BytesToString(e.accs[0])
	if err := e.k.InitGenesis(e.ctx, Genesis{ChipCheckers: [][]byte{pub}, ChipFree: []string{founder}}); err != nil {
		t.Fatal(err)
	}
	// Nobody on (start, or the network stood still): 1 and 2 have no chip.
	if got := e.selectNow(t); len(got) != 1 || !got[0] {
		t.Fatalf("group = %v, want only the computer with a chip", got)
	}
}
