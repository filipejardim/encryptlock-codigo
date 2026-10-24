// Package vault implements time locks for the Sovereign Vault (white paper,
// section 7.4): coins locked until a date cannot be moved by anyone before it
// and go back to the owner automatically at that date. A lock proves that the
// account holds at least that amount during the whole period.
package vault

import (
	"context"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	"cosmossdk.io/core/store"
	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"encryptlock/lockkeeper/protocol"
	"encryptlock/lockkeeper/x/vault/types"
)

// BankKeeper is the part of the bank module this module uses.
type BankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
}

// Keeper stores the locks.
type Keeper struct {
	bank         BankKeeper
	addressCodec address.Codec
	denom        string

	NextID  collections.Sequence
	Locks   collections.Map[uint64, types.Lock]
	ByOwner collections.KeySet[collections.Pair[sdk.AccAddress, uint64]]
	ByUntil collections.KeySet[collections.Pair[int64, uint64]]
}

// NewKeeper creates the vault keeper.
func NewKeeper(storeService store.KVStoreService, cdc codec.BinaryCodec, bank BankKeeper, addressCodec address.Codec, denom string) Keeper {
	sb := collections.NewSchemaBuilder(storeService)
	k := Keeper{
		bank:         bank,
		addressCodec: addressCodec,
		denom:        denom,
		NextID:       collections.NewSequence(sb, collections.NewPrefix(0), "next_id"),
		Locks:        collections.NewMap(sb, collections.NewPrefix(1), "locks", collections.Uint64Key, codec.CollValue[types.Lock](cdc)),
		ByOwner:      collections.NewKeySet(sb, collections.NewPrefix(2), "by_owner", collections.PairKeyCodec(sdk.AccAddressKey, collections.Uint64Key)),
		ByUntil:      collections.NewKeySet(sb, collections.NewPrefix(3), "by_until", collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key)),
	}
	if _, err := sb.Build(); err != nil {
		panic(err)
	}
	return k
}

func (k Keeper) coins(amount uint64) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin(k.denom, sdkmath.NewIntFromUint64(amount)))
}

// Lock locks amount of the owner's coins until the given time.
func (k Keeper) Lock(ctx context.Context, owner sdk.AccAddress, amount uint64, until int64) (uint64, error) {
	now := sdk.UnwrapSDKContext(ctx).BlockTime()
	if amount < protocol.MinLockUnits || until <= now.Unix() || until > now.AddDate(protocol.MaxLockYears, 0, 0).Unix() {
		return 0, types.ErrInvalidLock
	}
	// At most MaxLocksPerAccount locks at once.
	iter, err := k.ByOwner.Iterate(ctx, collections.NewPrefixedPairRange[sdk.AccAddress, uint64](owner))
	if err != nil {
		return 0, err
	}
	n := 0
	for ; iter.Valid() && n < protocol.MaxLocksPerAccount; iter.Next() {
		n++
	}
	iter.Close()
	if n >= protocol.MaxLocksPerAccount {
		return 0, types.ErrInvalidLock
	}
	if err := k.bank.SendCoinsFromAccountToModule(ctx, owner, types.ModuleName, k.coins(amount)); err != nil {
		return 0, err
	}
	id, err := k.NextID.Next(ctx)
	if err != nil {
		return 0, err
	}
	ownerStr, err := k.addressCodec.BytesToString(owner)
	if err != nil {
		return 0, err
	}
	l := types.Lock{Id: id, Owner: ownerStr, Amount: amount, CreatedAt: now.Unix(), Until: until}
	if err := k.save(ctx, l, owner); err != nil {
		return 0, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("lock_created",
		sdk.NewAttribute("id", sdkmath.NewIntFromUint64(id).String()),
		sdk.NewAttribute("owner", ownerStr),
		sdk.NewAttribute("amount", sdkmath.NewIntFromUint64(amount).String()),
		sdk.NewAttribute("until", sdkmath.NewInt(until).String()),
	))
	return id, nil
}

func (k Keeper) save(ctx context.Context, l types.Lock, owner sdk.AccAddress) error {
	if err := k.Locks.Set(ctx, l.Id, l); err != nil {
		return err
	}
	if err := k.ByOwner.Set(ctx, collections.Join(owner, l.Id)); err != nil {
		return err
	}
	return k.ByUntil.Set(ctx, collections.Join(l.Until, l.Id))
}

// maxReleasesPerBlock: locks released in one block at most.
const maxReleasesPerBlock = 1000

// ReleaseDue gives back every lock whose date has come. It runs at the end of
// every block.
func (k Keeper) ReleaseDue(ctx context.Context) error {
	now := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	iter, err := k.ByUntil.Iterate(ctx, collections.NewPrefixUntilPairRange[int64, uint64](now))
	if err != nil {
		return err
	}
	// At most maxReleasesPerBlock per block, read one by one (many locks
	// ending at the same second cannot slow the blocks down); the rest in
	// the next blocks.
	var keys []collections.Pair[int64, uint64]
	for ; iter.Valid() && len(keys) < maxReleasesPerBlock; iter.Next() {
		key, err := iter.Key()
		if err != nil {
			iter.Close()
			return err
		}
		keys = append(keys, key)
	}
	iter.Close()
	for _, key := range keys {
		l, err := k.Locks.Get(ctx, key.K2())
		if err != nil {
			return err
		}
		owner, err := k.addressCodec.StringToBytes(l.Owner)
		if err != nil {
			return err
		}
		if err := k.bank.SendCoinsFromModuleToAccount(ctx, types.ModuleName, owner, k.coins(l.Amount)); err != nil {
			return err
		}
		if err := k.Locks.Remove(ctx, l.Id); err != nil {
			return err
		}
		if err := k.ByOwner.Remove(ctx, collections.Join(sdk.AccAddress(owner), l.Id)); err != nil {
			return err
		}
		if err := k.ByUntil.Remove(ctx, key); err != nil {
			return err
		}
		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("lock_released",
			sdk.NewAttribute("id", sdkmath.NewIntFromUint64(l.Id).String()),
			sdk.NewAttribute("owner", l.Owner),
			sdk.NewAttribute("amount", sdkmath.NewIntFromUint64(l.Amount).String()),
		))
	}
	return nil
}

// LocksOf returns an account's locks.
func (k Keeper) LocksOf(ctx context.Context, owner sdk.AccAddress) ([]types.Lock, error) {
	iter, err := k.ByOwner.Iterate(ctx, collections.NewPrefixedPairRange[sdk.AccAddress, uint64](owner))
	if err != nil {
		return nil, err
	}
	keys, err := iter.Keys()
	if err != nil {
		return nil, err
	}
	out := make([]types.Lock, 0, len(keys))
	for _, key := range keys {
		l, err := k.Locks.Get(ctx, key.K2())
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}

// Reserves returns how much the owner has locked until at least `until`:
// coins the account is guaranteed to hold from now until then.
func (k Keeper) Reserves(ctx context.Context, owner sdk.AccAddress, until int64) (uint64, error) {
	locks, err := k.LocksOf(ctx, owner)
	if err != nil {
		return 0, err
	}
	var total uint64
	for _, l := range locks {
		if l.Until >= until {
			total += l.Amount
		}
	}
	return total, nil
}
