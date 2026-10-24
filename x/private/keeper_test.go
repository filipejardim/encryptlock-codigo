package private

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const (
	denom = "lockunit"
	m     = 100_000_000 // 1 LOCK in units
)

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

// acceptAll stands for a forged proof: the verifier was fooled.
type acceptAll struct{}

func (acceptAll) Verify(Spend, int64) error { return nil }

var (
	alice = sdk.AccAddress("alice_______________")
	bob   = sdk.AccAddress("bob_________________")
	thief = sdk.AccAddress("thief_______________")
	pool  = "module:" + ModuleName
)

func setup(t *testing.T, v Verifier) (Keeper, *fakeBank, sdk.Context) {
	t.Helper()
	key := storetypes.NewKVStoreKey(ModuleName)
	ctx := testutil.DefaultContext(key, storetypes.NewTransientStoreKey("transient_test")).
		WithBlockTime(time.Date(2026, time.October, 15, 18, 0, 0, 0, time.UTC))
	bank := &fakeBank{balances: map[string]uint64{string(alice): 100 * m, string(bob): 50 * m}}
	return NewKeeper(runtime.NewKVStoreService(key), bank, denom, v), bank, ctx
}

// h makes a test commitment: 8 field elements below the KoalaBear size.
func h(s string) []byte {
	x := sha256.Sum256([]byte(s))
	for i := 0; i < 32; i += 4 {
		x[i] &= 0x3f
	}
	return x[:]
}

// naiveRoot builds the whole tree level by level.
func naiveRoot(leaves [][]byte) []byte {
	level := leaves
	for lvl := 0; lvl < TreeDepth; lvl++ {
		var next [][]byte
		for i := 0; i < len(level); i += 2 {
			r := emptyNodes[lvl]
			if i+1 < len(level) {
				r = level[i+1]
			}
			next = append(next, hashNode(level[i], r))
		}
		level = next
	}
	if len(level) == 0 {
		return emptyNodes[TreeDepth]
	}
	return level[0]
}

func TestNotesTreeMatchesTheFullTree(t *testing.T) {
	k, _, ctx := setup(t, nil)
	var leaves [][]byte
	for i := 0; i <= 17; i++ {
		got, err := k.Root(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if want := naiveRoot(leaves); !bytes.Equal(got, want) {
			t.Fatalf("root with %d notes differs from the full tree", i)
		}
		if i > 0 {
			if ok, _ := k.Roots.Has(ctx, got); !ok {
				t.Fatalf("root with %d notes not remembered", i)
			}
		}
		cm := h(fmt.Sprint("note", i))
		leaves = append(leaves, cm)
		if err := k.Enter(ctx, alice, 1*m, cm); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEnterMovesCoinsAndCountsTheTurnstile(t *testing.T) {
	k, bank, ctx := setup(t, nil)
	if err := k.Enter(ctx, alice, 30*m, h("a")); err != nil {
		t.Fatal(err)
	}
	if err := k.Enter(ctx, bob, 20*m, h("b")); err != nil {
		t.Fatal(err)
	}
	if bank.balances[string(alice)] != 70*m || bank.balances[pool] != 50*m {
		t.Fatalf("balances: alice %d, private pool %d", bank.balances[string(alice)], bank.balances[pool])
	}
	if got, _ := k.Turnstile.Get(ctx); got != 50*m {
		t.Fatalf("turnstile = %d, want 50 LOCK", got)
	}
	// Bad requests change nothing.
	for _, bad := range []struct {
		who    sdk.AccAddress
		amount uint64
		cm     []byte
	}{{alice, 0, h("c")}, {alice, 1 * m, []byte("short")}, {alice, 1 * m, bytes.Repeat([]byte{0xff}, 32)}, {alice, 1_000 * m, h("d")}} {
		if err := k.Enter(ctx, bad.who, bad.amount, bad.cm); err == nil {
			t.Fatalf("enter %d accepted", bad.amount)
		}
	}
	if n, _ := k.Leaves.Get(ctx); n != 2 {
		t.Fatalf("notes = %d after refused requests, want 2", n)
	}
	if got, _ := k.Turnstile.Get(ctx); got != 50*m {
		t.Fatalf("turnstile = %d after refused requests", got)
	}
}

// Until the post-quantum verifier exists, nothing can be spent or leave.
func TestNothingLeavesWithoutTheRealVerifier(t *testing.T) {
	k, bank, ctx := setup(t, nil)
	if err := k.Enter(ctx, alice, 30*m, h("a")); err != nil {
		t.Fatal(err)
	}
	root, _ := k.Root(ctx)
	err := k.Submit(ctx, Spend{Root: root, Nullifiers: [][]byte{h("nf")}, Exit: 30 * m, ExitTo: alice, Proof: []byte("x")})
	if !errors.Is(err, ErrBadProof) {
		t.Fatalf("spend accepted without a verifier: %v", err)
	}
	if bank.balances[pool] != 30*m {
		t.Fatal("coins left private mode")
	}
	if spent, _ := k.Nullifiers.Has(ctx, h("nf")); spent {
		t.Fatal("note marked spent by a refused proof")
	}
}

// Attacks with forged proofs (the verifier accepts anything): the turnstile
// and the nullifiers still hold.
func TestForgedProofsCannotCreateLockOrSpendTwice(t *testing.T) {
	k, bank, ctx := setup(t, acceptAll{})
	if err := k.Enter(ctx, alice, 30*m, h("a")); err != nil {
		t.Fatal(err)
	}
	if err := k.Enter(ctx, bob, 20*m, h("b")); err != nil {
		t.Fatal(err)
	}
	root, _ := k.Root(ctx)

	// More out than ever went in: refused, nothing changes.
	err := k.Submit(ctx, Spend{Root: root, Nullifiers: [][]byte{h("nf1")}, Exit: 51 * m, ExitTo: thief})
	if !errors.Is(err, ErrTurnstile) {
		t.Fatalf("exit above the turnstile: %v", err)
	}
	if bank.balances[string(thief)] != 0 {
		t.Fatal("thief got coins")
	}
	if spent, _ := k.Nullifiers.Has(ctx, h("nf1")); spent {
		t.Fatal("nullifier kept after a refused spend")
	}

	// Unknown root, repeated nullifier, bad sizes, exit without receiver.
	refused := []Spend{
		{Root: h("made up"), Nullifiers: [][]byte{h("nf1")}},
		{Root: root, Nullifiers: [][]byte{h("nf1"), h("nf1")}},
		{Root: root},
		{Root: root, Nullifiers: [][]byte{h("1"), h("2"), h("3"), h("4"), h("5")}},
		{Root: root, Nullifiers: [][]byte{h("nf1")}, Commitments: [][]byte{[]byte("short")}},
		{Root: root, Nullifiers: [][]byte{h("nf1")}, Exit: 1 * m},
		{Root: root, Nullifiers: [][]byte{h("nf1")}, ExitTo: thief},
	}
	for i, s := range refused {
		if err := k.Submit(ctx, s); err == nil {
			t.Fatalf("bad spend %d accepted", i)
		}
	}

	// A valid-looking private transfer: one note in, two notes out, 5 LOCK leave.
	if err := k.Submit(ctx, Spend{Root: root, Nullifiers: [][]byte{h("nf1")}, Commitments: [][]byte{h("c1"), h("c2")}, Exit: 5 * m, ExitTo: bob}); err != nil {
		t.Fatal(err)
	}
	if bank.balances[string(bob)] != 35*m || bank.balances[pool] != 45*m {
		t.Fatalf("after exit: bob %d, pool %d", bank.balances[string(bob)], bank.balances[pool])
	}
	if got, _ := k.Turnstile.Get(ctx); got != 45*m {
		t.Fatalf("turnstile = %d, want 45 LOCK", got)
	}
	if n, _ := k.Leaves.Get(ctx); n != 4 {
		t.Fatalf("notes = %d, want 4", n)
	}

	// The same note again: refused (old root still valid, nullifier spent).
	if err := k.Submit(ctx, Spend{Root: root, Nullifiers: [][]byte{h("nf1")}, Exit: 1 * m, ExitTo: thief}); !errors.Is(err, ErrSpent) {
		t.Fatalf("double spend: %v", err)
	}

	// Draining everything that is left works, one unit more never does.
	root, _ = k.Root(ctx)
	if err := k.Submit(ctx, Spend{Root: root, Nullifiers: [][]byte{h("nf2")}, Exit: 45 * m, ExitTo: thief}); err != nil {
		t.Fatal(err)
	}
	if err := k.Submit(ctx, Spend{Root: root, Nullifiers: [][]byte{h("nf3")}, Exit: 1, ExitTo: thief}); !errors.Is(err, ErrTurnstile) {
		t.Fatalf("exit from an empty private mode: %v", err)
	}
	if bank.balances[pool] != 0 {
		t.Fatalf("pool = %d", bank.balances[pool])
	}
}
