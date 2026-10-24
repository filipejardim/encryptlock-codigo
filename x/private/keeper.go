// Package private is the optional private mode (PRIVACIDADE.md, Phase 3,
// option B: post-quantum proofs). LOCK put into private mode becomes hidden
// "notes": the network only stores a commitment to each note (a hash that
// hides value, owner and deadline), the set of spent notes ("nullifiers") and
// one public number, the turnstile.
//
// The turnstile (Zcash ZIP 209) is the total of LOCK inside private mode:
// entering adds to it, leaving subtracts, and nothing can ever leave beyond
// it. Even a forged proof could then never create LOCK outside private mode.
//
// Step 3a-1 (this file): notes tree, nullifiers, turnstile, entering and
// spending, with the proof check behind the Verifier interface. Until the
// post-quantum verifier exists the default one refuses every proof, so coins
// can go in but nothing can be spent or leave. The module is not yet wired
// into the network.
package private

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"
	sdkmath "cosmossdk.io/math"
	"github.com/consensys/gnark-crypto/field/koalabear"
	"github.com/consensys/gnark-crypto/field/koalabear/poseidon2"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ModuleName is also the name of the module account that holds the LOCK in
// private mode.
const ModuleName = "private"

// TreeDepth is the depth of the notes tree: room for 2^32 notes.
const TreeDepth = 32

// MaxNotesPerSpend limits the inputs and outputs of one private transfer.
const MaxNotesPerSpend = 4

var (
	ErrInvalid      = errors.New("private: invalid request")
	ErrSpent        = errors.New("private: note already spent")
	ErrUnknownRoot  = errors.New("private: unknown notes tree root")
	ErrBadProof     = errors.New("private: proof refused")
	ErrTurnstile    = errors.New("private: more would leave private mode than ever entered")
	ErrTreeFull     = errors.New("private: notes tree full")
	errNoVerifierOK = errors.New("private: post-quantum verifier not available yet")
)

// BankKeeper is the part of the bank module this module uses.
type BankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
}

// Spend is one private transfer: it spends notes (their nullifiers), creates
// new notes (their commitments) and may send Exit LOCK out of private mode to
// ExitTo. Root is the notes tree root the proof was made against; BlockTime
// is a public input so the proof can apply the 1-hour rule inside the notes
// (before a note's deadline only its receiver can spend it, after it only the
// sender, which is the transfer going back).
type Spend struct {
	Root        []byte
	Nullifiers  [][]byte
	Commitments [][]byte
	Exit        uint64
	ExitTo      sdk.AccAddress
	Proof       []byte
}

// Verifier checks the proof of a Spend: the spent notes are in the tree under
// Root, the nullifiers belong to them, the owner (or, after the deadline, the
// sender) signed, and values in = values out + Exit. It must be deterministic.
type Verifier interface {
	Verify(s Spend, blockTime int64) error
}

// RefuseAll is the verifier until the post-quantum one exists.
type RefuseAll struct{}

func (RefuseAll) Verify(Spend, int64) error { return errNoVerifierOK }

// Keeper stores private mode.
type Keeper struct {
	bank     BankKeeper
	denom    string
	verifier Verifier

	// Turnstile is the total of LOCK units inside private mode.
	Turnstile collections.Item[uint64]
	// Leaves is the number of notes in the tree.
	Leaves collections.Item[uint64]
	// Frontier holds, per level, the last left node still waiting for its
	// right neighbour (incremental Merkle tree: O(depth) storage).
	Frontier collections.Map[uint64, []byte]
	// Roots are all the tree roots there ever were: a proof may use any of
	// them, like Zcash anchors.
	Roots collections.KeySet[[]byte]
	// Nullifiers of the spent notes.
	Nullifiers collections.KeySet[[]byte]
}

// NewKeeper creates the private mode keeper.
func NewKeeper(storeService store.KVStoreService, bank BankKeeper, denom string, verifier Verifier) Keeper {
	if verifier == nil {
		verifier = RefuseAll{}
	}
	sb := collections.NewSchemaBuilder(storeService)
	k := Keeper{
		bank:       bank,
		denom:      denom,
		verifier:   verifier,
		Turnstile:  collections.NewItem(sb, collections.NewPrefix(0), "turnstile", collections.Uint64Value),
		Leaves:     collections.NewItem(sb, collections.NewPrefix(1), "leaves", collections.Uint64Value),
		Frontier:   collections.NewMap(sb, collections.NewPrefix(2), "frontier", collections.Uint64Key, collections.BytesValue),
		Roots:      collections.NewKeySet(sb, collections.NewPrefix(3), "roots", collections.BytesKey),
		Nullifiers: collections.NewKeySet(sb, collections.NewPrefix(4), "nullifiers", collections.BytesKey),
	}
	if _, err := sb.Build(); err != nil {
		panic(err)
	}
	return k
}

// Tree hash: Poseidon2 over the KoalaBear field (gnark-crypto, the same
// building blocks as Plonky3), the hash the post-quantum proofs compute
// cheaply. A node is 8 field elements (32 bytes); Compress(left, right) is the
// width-16 permutation with feed-forward. The phone's prover uses this same
// Go code, so both sides always agree.
var treeHash = poseidon2.NewDefaultPermutation()

func hashNode(l, r []byte) []byte {
	out, err := treeHash.Compress(l, r)
	if err != nil {
		panic(err) // inputs are checked by validHash; nodes are hash outputs
	}
	return out
}

// emptyNodes[i] is the root of an empty subtree of height i.
var emptyNodes = func() [TreeDepth + 1][]byte {
	var z [TreeDepth + 1][]byte
	z[0] = make([]byte, 32)
	for i := 1; i <= TreeDepth; i++ {
		z[i] = hashNode(z[i-1], z[i-1])
	}
	return z
}()

// validHash accepts 32 bytes that are 8 canonical KoalaBear elements (each
// 4-byte number below the field size), like every Poseidon2 output.
func validHash(b []byte) bool {
	if len(b) != 32 {
		return false
	}
	var e koalabear.Element
	for i := 0; i < 32; i += koalabear.Bytes {
		if e.SetBytesCanonical(b[i:i+koalabear.Bytes]) != nil {
			return false
		}
	}
	return true
}

// Root is the current notes tree root.
func (k Keeper) Root(ctx context.Context) ([]byte, error) {
	n, err := k.Leaves.Get(ctx)
	if errors.Is(err, collections.ErrNotFound) {
		return emptyNodes[TreeDepth], nil
	} else if err != nil {
		return nil, err
	}
	return k.rootOf(ctx, n)
}

// rootOf recomputes the root for n leaves from the frontier (same method as
// the Ethereum deposit contract).
func (k Keeper) rootOf(ctx context.Context, n uint64) ([]byte, error) {
	node := emptyNodes[0]
	for lvl := uint64(0); lvl < TreeDepth; lvl++ {
		if n>>lvl&1 == 1 {
			left, err := k.Frontier.Get(ctx, lvl)
			if err != nil {
				return nil, err
			}
			node = hashNode(left, node)
		} else {
			node = hashNode(node, emptyNodes[lvl])
		}
	}
	return node, nil
}

// append adds a commitment to the tree and remembers the new root.
func (k Keeper) append(ctx context.Context, cm []byte) error {
	n, err := k.Leaves.Get(ctx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	if n >= 1<<TreeDepth {
		return ErrTreeFull
	}
	node := cm
	for lvl := uint64(0); lvl < TreeDepth; lvl++ {
		if n>>lvl&1 == 0 {
			if err := k.Frontier.Set(ctx, lvl, node); err != nil {
				return err
			}
			break
		}
		left, err := k.Frontier.Get(ctx, lvl)
		if err != nil {
			return err
		}
		node = hashNode(left, node)
	}
	if err := k.Leaves.Set(ctx, n+1); err != nil {
		return err
	}
	root, err := k.rootOf(ctx, n+1)
	if err != nil {
		return err
	}
	return k.Roots.Set(ctx, root)
}

func (k Keeper) coins(amount uint64) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin(k.denom, sdkmath.NewIntFromUint64(amount)))
}

func (k Keeper) turnstile(ctx context.Context) (uint64, error) {
	t, err := k.Turnstile.Get(ctx)
	if errors.Is(err, collections.ErrNotFound) {
		return 0, nil
	}
	return t, err
}

// Enter moves amount of the owner's LOCK into private mode as one new note.
// The amount is public here (it leaves a public account); the note's owner
// and deadline stay hidden inside the commitment. The app checks the
// commitment it makes; a wrong one only loses the owner's own coins.
func (k Keeper) Enter(ctx context.Context, owner sdk.AccAddress, amount uint64, commitment []byte) error {
	if amount == 0 || !validHash(commitment) {
		return ErrInvalid
	}
	t, err := k.turnstile(ctx)
	if err != nil {
		return err
	}
	if t+amount < t {
		return ErrInvalid
	}
	if err := k.bank.SendCoinsFromAccountToModule(ctx, owner, ModuleName, k.coins(amount)); err != nil {
		return err
	}
	if err := k.append(ctx, commitment); err != nil {
		return err
	}
	return k.Turnstile.Set(ctx, t+amount)
}

// Submit applies a private transfer: checks the root, the nullifiers, the
// proof and the turnstile, then marks the notes spent, adds the new ones and
// pays Exit out of private mode.
func (k Keeper) Submit(ctx context.Context, s Spend) error {
	if len(s.Nullifiers) == 0 || len(s.Nullifiers) > MaxNotesPerSpend || len(s.Commitments) > MaxNotesPerSpend ||
		!validHash(s.Root) || (s.Exit > 0) != (len(s.ExitTo) > 0) {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, nf := range s.Nullifiers {
		if !validHash(nf) || seen[string(nf)] {
			return ErrInvalid
		}
		seen[string(nf)] = true
		if spent, err := k.Nullifiers.Has(ctx, nf); err != nil {
			return err
		} else if spent {
			return ErrSpent
		}
	}
	for _, cm := range s.Commitments {
		if !validHash(cm) {
			return ErrInvalid
		}
	}
	if known, err := k.Roots.Has(ctx, s.Root); err != nil {
		return err
	} else if !known {
		return ErrUnknownRoot
	}
	if err := k.verifier.Verify(s, sdk.UnwrapSDKContext(ctx).BlockTime().Unix()); err != nil {
		return errors.Join(ErrBadProof, err)
	}
	// The turnstile is checked whatever the proof says.
	t, err := k.turnstile(ctx)
	if err != nil {
		return err
	}
	if s.Exit > t {
		return ErrTurnstile
	}
	for _, nf := range s.Nullifiers {
		if err := k.Nullifiers.Set(ctx, nf); err != nil {
			return err
		}
	}
	for _, cm := range s.Commitments {
		if err := k.append(ctx, cm); err != nil {
			return err
		}
	}
	if s.Exit > 0 {
		if err := k.Turnstile.Set(ctx, t-s.Exit); err != nil {
			return err
		}
		return k.bank.SendCoinsFromModuleToAccount(ctx, ModuleName, s.ExitTo, k.coins(s.Exit))
	}
	return nil
}
