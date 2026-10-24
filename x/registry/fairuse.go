package registry

import (
	"context"
	"errors"
	"time"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"encryptlock/lockkeeper/x/registry/types"
)

// Fair use (security review, 11 Oct 2026). Registering an account and most
// messages are free (only transfers and trades pay a fee), so nothing would
// stop one person from flooding the network for nothing:
//
//   - each account sends at most FreeTxPerHour transactions an hour (a
//     transaction with a transfer pays its fee and does not count);
//   - at most RegistrationsPerBlock new accounts per block;
//   - an account registered more than UnusedAfter ago that never held a coin
//     nor sent anything after registering is forgotten; the app registers it
//     again by itself when it is opened. Registered accounts cannot then pile
//     up for free.
const (
	FreeTxPerHour         = 120
	RegistrationsPerBlock = 4
	UnusedAfter           = 7 * 24 * time.Hour
	prunePerBlock         = 50
	hourSeconds           = 3600
)

// ErrBusy: too many registrations in this block, or too many transactions
// from one account this hour.
var ErrBusy = errorsmod.Register(types.ModuleName, 4, "too busy: try again in a moment")

// AccountKeeper is the part of the account keeper that forgets unused
// accounts.
type AccountKeeper interface {
	GetAccount(ctx context.Context, addr sdk.AccAddress) sdk.AccountI
	RemoveAccount(ctx context.Context, acc sdk.AccountI)
}

// BankKeeper tells whether an account holds anything.
type BankKeeper interface {
	GetAllBalances(ctx context.Context, addr sdk.AccAddress) sdk.Coins
}

// Incoming tells whether an account has transfers waiting to be accepted.
type Incoming interface {
	HasIncoming(ctx context.Context, addr sdk.AccAddress) (bool, error)
}

type pruneDeps struct {
	accounts AccountKeeper
	bank     BankKeeper
	incoming Incoming
}

// SetPruneDeps gives the registry what it needs to forget unused accounts
// (set once the other keepers exist).
func (k Keeper) SetPruneDeps(accounts AccountKeeper, bank BankKeeper, incoming Incoming) {
	*k.deps = pruneDeps{accounts: accounts, bank: bank, incoming: incoming}
}

func blockTime(ctx context.Context) int64 { return sdk.UnwrapSDKContext(ctx).BlockTime().Unix() }

// CountTx records one transaction of addr this hour and refuses it past the
// limit. paid: it carries a fee (a transfer), so it does not count.
func (k Keeper) CountTx(ctx context.Context, addr sdk.AccAddress, paid bool) error {
	if paid {
		return nil
	}
	key := collections.Join(blockTime(ctx)/hourSeconds, addr)
	n, err := k.TxCount.Get(ctx, key)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	if n >= FreeTxPerHour {
		return errorsmod.Wrapf(ErrBusy, "account %s sent %d transactions this hour", addr, n)
	}
	return k.TxCount.Set(ctx, key, n+1)
}

// CountRegistration refuses a new account past RegistrationsPerBlock in
// this block.
func (k Keeper) CountRegistration(ctx context.Context) error {
	height := sdk.UnwrapSDKContext(ctx).BlockHeight()
	last, err := k.RegHeight.Get(ctx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	var n uint64
	if last == height {
		if n, err = k.RegCount.Get(ctx); err != nil && !errors.Is(err, collections.ErrNotFound) {
			return err
		}
	}
	if n >= RegistrationsPerBlock {
		return errorsmod.Wrap(ErrBusy, "many new accounts in this block")
	}
	if err := k.RegHeight.Set(ctx, height); err != nil {
		return err
	}
	return k.RegCount.Set(ctx, n+1)
}

// EndBlock forgets the transaction counts of past hours and unused accounts
// (a few per block).
func (k Keeper) EndBlock(ctx context.Context) error {
	t := blockTime(ctx)
	// Counts of past hours.
	iter, err := k.TxCount.Iterate(ctx, collections.NewPrefixUntilPairRange[int64, sdk.AccAddress](t/hourSeconds-1))
	if err != nil {
		return err
	}
	var old []collections.Pair[int64, sdk.AccAddress]
	for ; iter.Valid() && len(old) < 1000; iter.Next() {
		key, err := iter.Key()
		if err != nil {
			iter.Close()
			return err
		}
		old = append(old, key)
	}
	iter.Close()
	for _, key := range old {
		if err := k.TxCount.Remove(ctx, key); err != nil {
			return err
		}
	}
	if k.deps.accounts == nil {
		return nil
	}
	// Unused accounts.
	riter, err := k.ByTime.Iterate(ctx, collections.NewPrefixUntilPairRange[int64, sdk.AccAddress](t-int64(UnusedAfter.Seconds())))
	if err != nil {
		return err
	}
	var due []collections.Pair[int64, sdk.AccAddress]
	for ; riter.Valid() && len(due) < prunePerBlock; riter.Next() {
		key, err := riter.Key()
		if err != nil {
			riter.Close()
			return err
		}
		due = append(due, key)
	}
	riter.Close()
	for _, key := range due {
		if err := k.ByTime.Remove(ctx, key); err != nil {
			return err
		}
		unused, err := k.unused(ctx, key.K2())
		if err != nil {
			return err
		}
		if !unused {
			continue // in use: kept for good
		}
		if acc := k.deps.accounts.GetAccount(ctx, key.K2()); acc != nil {
			k.deps.accounts.RemoveAccount(ctx, acc)
		}
		if err := k.Registered.Remove(ctx, key.K2()); err != nil {
			return err
		}
		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("account_forgotten", sdk.NewAttribute("account", key.K2().String())))
	}
	return nil
}

// unused: the account never sent anything after registering (sequence 1 at
// most), holds no coins and has no transfer waiting for it.
func (k Keeper) unused(ctx context.Context, addr sdk.AccAddress) (bool, error) {
	if acc := k.deps.accounts.GetAccount(ctx, addr); acc != nil && acc.GetSequence() > 1 {
		return false, nil
	}
	if !k.deps.bank.GetAllBalances(ctx, addr).IsZero() {
		return false, nil
	}
	waiting, err := k.deps.incoming.HasIncoming(ctx, addr)
	return !waiting, err
}
