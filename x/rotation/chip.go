package rotation

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"encryptlock/lockkeeper/x/rotation/types"
)

// Real computers only (founder's decision, 8 Oct; steps C3 and C4).
//
// The network's chip checker (chip.Checker, run by a LockKeeper of the
// network) does the secret test with a computer's security chip and signs a
// ticket "chip code -> account". The account (usually the computer's helper)
// sends the ticket here. Then:
//   - each chip serves one account at a time (proving another chip frees the
//     old one, so a computer can change hands, but never be two LockKeepers);
//   - a LockKeeper says "I am on" only with a proven chip, on its own account
//     or on its helper's. Without "I am on" it never signs nor earns.
//
// The checkers' keys and the computers that start the network (exempt: they
// are in the first block for everyone to see) come from genesis. With no
// checker in genesis the rule is off (test networks of the modules).

// TicketDomain separates chip tickets from anything else signed with the
// checker's key.
const TicketDomain = "encryptlock-chip-v1"

// TicketMessage is what the checker signs: the chip's code for an account on
// one network.
func TicketMessage(chainID, code string, account sdk.AccAddress) []byte {
	h := sha256.Sum256([]byte(TicketDomain + "\n" + chainID + "\n" + code + "\n" + hex.EncodeToString(account)))
	return h[:]
}

// ProveChip records that account runs on the real computer whose chip has
// code, checked by one of the network's checkers (ticket).
func (k Keeper) ProveChip(ctx context.Context, account sdk.AccAddress, code string, ticket []byte) error {
	if raw, err := hex.DecodeString(code); err != nil || len(raw) != sha256.Size || hex.EncodeToString(raw) != code {
		return errorsmod.Wrap(types.ErrBadChip, "not a chip code")
	}
	msg := TicketMessage(sdk.UnwrapSDKContext(ctx).ChainID(), code, account)
	ok := false
	err := k.ChipCheckers.Walk(ctx, nil, func(pub []byte) (bool, error) {
		ok = len(pub) == ed25519.PublicKeySize && ed25519.Verify(pub, msg, ticket)
		return ok, nil
	})
	if err != nil {
		return err
	}
	if !ok {
		return errorsmod.Wrap(types.ErrBadChip, "not signed by the network's chip checker")
	}
	if owner, err := k.ChipUsed.Get(ctx, code); err == nil && !account.Equals(owner) {
		return errorsmod.Wrap(types.ErrBadChip, "this computer already serves another account")
	} else if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	if old, err := k.ChipOf.Get(ctx, account); err == nil && old != code {
		if err := k.ChipUsed.Remove(ctx, old); err != nil {
			return err
		}
	}
	if err := k.ChipUsed.Set(ctx, code, account); err != nil {
		return err
	}
	return k.ChipOf.Set(ctx, account, code)
}

// ChipOK reports whether the LockKeeper valAddr may say "I am on": its chip
// is proven (on its account or its helper's), it started the network, or the
// network has no chip checker.
func (k Keeper) ChipOK(ctx context.Context, valAddr sdk.ValAddress) (bool, error) {
	iter, err := k.ChipCheckers.Iterate(ctx, nil)
	if err != nil {
		return false, err
	}
	none := !iter.Valid()
	iter.Close()
	if none {
		return true, nil
	}
	acc := sdk.AccAddress(valAddr)
	if free, err := k.ChipFree.Has(ctx, acc); err != nil || free {
		return free, err
	}
	accounts := []sdk.AccAddress{acc}
	if helper, err := k.HelperOf.Get(ctx, valAddr); err == nil {
		accounts = append(accounts, helper)
	} else if !errors.Is(err, collections.ErrNotFound) {
		return false, err
	}
	for _, a := range accounts {
		if has, err := k.ChipOf.Has(ctx, a); err != nil || has {
			return has, err
		}
	}
	return false, nil
}

func (s msgServer) ProveChip(ctx context.Context, msg *types.MsgProveChip) (*types.MsgProveChipResponse, error) {
	acc, err := s.k.addressCodec.StringToBytes(msg.Account)
	if err != nil {
		return nil, err
	}
	if err := s.k.ProveChip(ctx, acc, msg.Code, msg.Ticket); err != nil {
		return nil, err
	}
	return &types.MsgProveChipResponse{}, nil
}

// Genesis is the rotation module's genesis: the chip checkers' public keys
// and the computers that start the network (exempt from the chip proof).
type Genesis struct {
	// ChipCheckers: Ed25519 public keys, base64.
	ChipCheckers [][]byte `json:"chip_checkers,omitempty"`
	// ChipFree: LockKeeper accounts (lock1...) that started the network.
	ChipFree []string `json:"chip_free,omitempty"`
}

// ParseGenesis reads and checks the module's genesis.
func ParseGenesis(bz json.RawMessage) (Genesis, error) {
	var g Genesis
	if len(bz) == 0 {
		return g, nil
	}
	if err := json.Unmarshal(bz, &g); err != nil {
		return g, err
	}
	for _, pub := range g.ChipCheckers {
		if len(pub) != ed25519.PublicKeySize {
			return g, errors.New("rotation genesis: a chip checker key must be 32 bytes")
		}
	}
	return g, nil
}

// InitGenesis stores the chip checkers and the exempt computers.
func (k Keeper) InitGenesis(ctx context.Context, g Genesis) error {
	for _, pub := range g.ChipCheckers {
		if err := k.ChipCheckers.Set(ctx, pub); err != nil {
			return err
		}
	}
	for _, a := range g.ChipFree {
		acc, err := k.addressCodec.StringToBytes(a)
		if err != nil {
			return err
		}
		if err := k.ChipFree.Set(ctx, acc); err != nil {
			return err
		}
	}
	return nil
}

// ExportGenesis returns the chip checkers and the exempt computers.
func (k Keeper) ExportGenesis(ctx context.Context) (Genesis, error) {
	var g Genesis
	err := k.ChipCheckers.Walk(ctx, nil, func(pub []byte) (bool, error) {
		g.ChipCheckers = append(g.ChipCheckers, pub)
		return false, nil
	})
	if err != nil {
		return g, err
	}
	err = k.ChipFree.Walk(ctx, nil, func(acc sdk.AccAddress) (bool, error) {
		s, err := k.addressCodec.BytesToString(acc)
		g.ChipFree = append(g.ChipFree, s)
		return err != nil, err
	})
	return g, err
}
