// Package transfers implements EncryptLock's pending transfers (white paper,
// section 6.2): the recipient has 1 hour to accept, otherwise the amount goes
// back to the sender automatically; the sender can cancel while pending; only
// registered accounts can receive.
package transfers

import (
	"context"
	"errors"
	"strconv"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	"cosmossdk.io/core/store"
	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"encryptlock/lockkeeper/protocol"
	"encryptlock/lockkeeper/x/transfers/types"
)

// BankKeeper is the part of the bank module this module uses.
type BankKeeper interface {
	SendCoins(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
}

// Clock gives the genesis time (Unix seconds), to know which fee split
// period applies.
type Clock interface {
	GenesisTime(ctx context.Context) (int64, error)
}

// Registry tells whether an account is registered.
type Registry interface {
	IsRegistered(ctx context.Context, addr sdk.AccAddress) (bool, error)
}

// Keeper stores pending transfers.
type Keeper struct {
	bank         BankKeeper
	registry     Registry
	clock        Clock
	feeCollector string
	addressCodec address.Codec
	denom        string

	// DevFund is the development fund address, fixed at genesis.
	DevFund collections.Item[[]byte]
	NextID  collections.Sequence
	Pending collections.Map[uint64, types.Transfer]
	// ByExpiry orders pending transfers by expiry time, for automatic refunds.
	ByExpiry collections.KeySet[collections.Pair[int64, uint64]]
	// BySender and ByRecipient list each account's pending transfers.
	BySender    collections.KeySet[collections.Pair[sdk.AccAddress, uint64]]
	ByRecipient collections.KeySet[collections.Pair[sdk.AccAddress, uint64]]
	// Shields holds each account's security settings (shield.go).
	Shields collections.Map[sdk.AccAddress, types.Shield]
	// Contacts records (sender, recipient) pairs of accepted transfers.
	Contacts collections.KeySet[collections.Pair[sdk.AccAddress, sdk.AccAddress]]
	// LastFrozen: when each account last pressed "Not me" (kept after it
	// unfreezes): what it sent before can never be accepted (shield.go).
	LastFrozen collections.Map[sdk.AccAddress, int64]
	onFreeze   *FreezeHook
}

// NewKeeper creates the transfers keeper.
// feeCollector is the module account the distribution module pays LockKeepers
// from.
func NewKeeper(storeService store.KVStoreService, cdc codec.BinaryCodec, bank BankKeeper, registry Registry, clock Clock, addressCodec address.Codec, denom, feeCollector string) Keeper {
	sb := collections.NewSchemaBuilder(storeService)
	k := Keeper{
		bank:         bank,
		registry:     registry,
		clock:        clock,
		feeCollector: feeCollector,
		addressCodec: addressCodec,
		denom:        denom,
		DevFund:      collections.NewItem(sb, collections.NewPrefix(5), "dev_fund", collections.BytesValue),
		NextID:       collections.NewSequence(sb, collections.NewPrefix(0), "next_id"),
		Pending:      collections.NewMap(sb, collections.NewPrefix(1), "pending", collections.Uint64Key, codec.CollValue[types.Transfer](cdc)),
		ByExpiry:     collections.NewKeySet(sb, collections.NewPrefix(2), "by_expiry", collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key)),
		BySender:     collections.NewKeySet(sb, collections.NewPrefix(3), "by_sender", collections.PairKeyCodec(sdk.AccAddressKey, collections.Uint64Key)),
		ByRecipient:  collections.NewKeySet(sb, collections.NewPrefix(4), "by_recipient", collections.PairKeyCodec(sdk.AccAddressKey, collections.Uint64Key)),
		Shields:      collections.NewMap(sb, collections.NewPrefix(6), "shields", sdk.AccAddressKey, codec.CollValue[types.Shield](cdc)),
		Contacts:     collections.NewKeySet(sb, collections.NewPrefix(7), "contacts", collections.PairKeyCodec(sdk.AccAddressKey, sdk.AccAddressKey)),
		LastFrozen:   collections.NewMap(sb, collections.NewPrefix(8), "last_frozen", sdk.AccAddressKey, collections.Int64Value),
		onFreeze:     new(FreezeHook),
	}
	if _, err := sb.Build(); err != nil {
		panic(err)
	}
	return k
}

func (k Keeper) coins(amount uint64) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin(k.denom, sdkmath.NewIntFromUint64(amount)))
}

// Send creates a pending transfer and holds the amount.
func (k Keeper) Send(ctx context.Context, sender, recipient sdk.AccAddress, amount uint64) (uint64, error) {
	if amount == 0 {
		return 0, types.ErrZeroAmount
	}
	if sender.Equals(recipient) {
		return 0, types.ErrSelfTransfer
	}
	registered, err := k.registry.IsRegistered(ctx, recipient)
	if err != nil {
		return 0, err
	}
	if !registered {
		return 0, types.ErrRecipientNotRegistered
	}
	needsApproval, vaultDelay, err := k.checkSend(ctx, sender, recipient, amount)
	if err != nil {
		return 0, err
	}

	if err := k.bank.SendCoinsFromAccountToModule(ctx, sender, types.ModuleName, k.coins(amount)); err != nil {
		return 0, err
	}
	fee, devShare, keepersShare, err := k.chargeFee(ctx, sender, amount)
	if err != nil {
		return 0, err
	}

	id, err := k.NextID.Next(ctx)
	if err != nil {
		return 0, err
	}
	senderStr, err := k.addressCodec.BytesToString(sender)
	if err != nil {
		return 0, err
	}
	recipientStr, err := k.addressCodec.BytesToString(recipient)
	if err != nil {
		return 0, err
	}
	created := sdk.UnwrapSDKContext(ctx).BlockTime()
	// Waiting for the second device: 24 hours to approve; then the recipient
	// gets its 1 hour to accept.
	// From a Sovereign Vault, the recipient can only accept after the delay.
	releaseAt := created.Unix() + vaultDelay
	expiresAt := releaseAt + int64(protocol.TransferAcceptanceWindow.Seconds())
	if needsApproval {
		expiresAt = created.Add(protocol.ApprovalWindow).Unix()
	}
	t := types.Transfer{
		Id:               id,
		Sender:           senderStr,
		Recipient:        recipientStr,
		Amount:           amount,
		CreatedAt:        created.Unix(),
		ExpiresAt:        expiresAt,
		AwaitingApproval: needsApproval,
		ReleaseAt:        releaseAt,
	}
	if err := k.save(ctx, t, sender, recipient); err != nil {
		return 0, err
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(transferEvent("transfer_created", t).AppendAttributes(
		sdk.NewAttribute("fee", sdkmath.NewIntFromUint64(fee).String()),
		sdk.NewAttribute("fee_dev_fund", sdkmath.NewIntFromUint64(devShare).String()),
		sdk.NewAttribute("fee_lockkeepers", sdkmath.NewIntFromUint64(keepersShare).String()),
		sdk.NewAttribute("awaiting_approval", strconv.FormatBool(needsApproval)),
		sdk.NewAttribute("release_at", strconv.FormatInt(releaseAt, 10)),
		sdk.NewAttribute("created_at", strconv.FormatInt(created.Unix(), 10)),
	))
	return id, nil
}

// chargeFee takes the transfer fee from the sender: the development fund's
// share goes to its address, the LockKeepers' share to the fee collector
// (paid out by the distribution module like the emission). The fee is never
// refunded, even if the transfer is canceled or expires.
func (k Keeper) chargeFee(ctx context.Context, sender sdk.AccAddress, amount uint64) (fee, devShare, keepersShare uint64, err error) {
	genesis, err := k.clock.GenesisTime(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	devFund, err := k.DevFund.Get(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	fee = TransferFee(amount)
	devShare, keepersShare = SplitFee(fee, sdk.UnwrapSDKContext(ctx).BlockTime().Unix()-genesis, k.FinalMilestoneReached(ctx))
	if devShare > 0 {
		if err := k.bank.SendCoins(ctx, sender, devFund, k.coins(devShare)); err != nil {
			return 0, 0, 0, err
		}
	}
	if keepersShare > 0 {
		if err := k.bank.SendCoinsFromAccountToModule(ctx, sender, k.feeCollector, k.coins(keepersShare)); err != nil {
			return 0, 0, 0, err
		}
	}
	return fee, devShare, keepersShare, nil
}

// HasIncoming reports whether addr has transfers waiting to be accepted.
func (k Keeper) HasIncoming(ctx context.Context, addr sdk.AccAddress) (bool, error) {
	iter, err := k.ByRecipient.Iterate(ctx, collections.NewPrefixedPairRange[sdk.AccAddress, uint64](addr))
	if err != nil {
		return false, err
	}
	defer iter.Close()
	return iter.Valid(), nil
}

// Accept pays a pending transfer to its recipient, within the 1 hour window.
func (k Keeper) Accept(ctx context.Context, recipient sdk.AccAddress, id uint64) error {
	t, err := k.get(ctx, id)
	if err != nil {
		return err
	}
	if !k.sameAddress(t.Recipient, recipient) {
		return types.ErrNotRecipient
	}
	if t.AwaitingApproval {
		return types.ErrAwaitingApproval
	}
	if sdk.UnwrapSDKContext(ctx).BlockTime().Unix() < t.ReleaseAt {
		return types.ErrNotReleased
	}
	if sdk.UnwrapSDKContext(ctx).BlockTime().Unix() >= t.ExpiresAt {
		// The end of this block refunds it.
		return types.ErrExpired
	}
	sender, err := k.addressCodec.StringToBytes(t.Sender)
	if err != nil {
		return err
	}
	// "Not me" on the sender: nothing it sent can be taken any more; it goes
	// back when it expires (or at once, if the freeze canceled it).
	s, err := k.GetShield(ctx, sender)
	if err != nil {
		return err
	}
	if s.Frozen {
		return types.ErrFrozen
	}
	if err := k.sentBeforeFreeze(ctx, sender, t); err != nil {
		return err
	}
	if err := k.bank.SendCoinsFromModuleToAccount(ctx, types.ModuleName, recipient, k.coins(t.Amount)); err != nil {
		return err
	}
	// The recipient is now a known contact of the sender (no extra approval
	// needed next time).
	if err := k.Contacts.Set(ctx, contactKey(sender, recipient)); err != nil {
		return err
	}
	return k.finish(ctx, t, "transfer_accepted")
}

// Cancel returns a pending transfer to its sender.
func (k Keeper) Cancel(ctx context.Context, sender sdk.AccAddress, id uint64) error {
	t, err := k.get(ctx, id)
	if err != nil {
		return err
	}
	if !k.sameAddress(t.Sender, sender) {
		return types.ErrNotSender
	}
	if sdk.UnwrapSDKContext(ctx).BlockTime().Unix() >= t.ExpiresAt {
		return types.ErrExpired
	}
	return k.refund(ctx, t, "transfer_canceled")
}

// maxRefundsPerBlock: expired transfers refunded in one block at most.
const maxRefundsPerBlock = 1000

// ExpireDue refunds every pending transfer whose window has ended. It runs at
// the end of every block. Returns how many were refunded.
func (k Keeper) ExpireDue(ctx context.Context) (int, error) {
	now := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	// All (expiresAt, id) with expiresAt <= now.
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndInclusive(collections.Join(now, uint64(1<<64-1)))
	iter, err := k.ByExpiry.Iterate(ctx, rng)
	if err != nil {
		return 0, err
	}
	// At most maxRefundsPerBlock per block, read one by one, so a flood of
	// transfers expiring at once cannot slow the blocks down; the rest go in
	// the next blocks.
	var keys []collections.Pair[int64, uint64]
	for ; iter.Valid() && len(keys) < maxRefundsPerBlock; iter.Next() {
		key, err := iter.Key()
		if err != nil {
			iter.Close()
			return 0, err
		}
		keys = append(keys, key)
	}
	iter.Close()
	for _, key := range keys {
		t, err := k.get(ctx, key.K2())
		if err != nil {
			return 0, err
		}
		if err := k.refund(ctx, t, "transfer_expired"); err != nil {
			return 0, err
		}
	}
	return len(keys), nil
}

func (k Keeper) refund(ctx context.Context, t types.Transfer, event string) error {
	sender, err := k.addressCodec.StringToBytes(t.Sender)
	if err != nil {
		return err
	}
	if err := k.bank.SendCoinsFromModuleToAccount(ctx, types.ModuleName, sender, k.coins(t.Amount)); err != nil {
		return err
	}
	return k.finish(ctx, t, event)
}

func (k Keeper) get(ctx context.Context, id uint64) (types.Transfer, error) {
	t, err := k.Pending.Get(ctx, id)
	if errors.Is(err, collections.ErrNotFound) {
		return t, types.ErrTransferNotFound
	}
	return t, err
}

func (k Keeper) sameAddress(stored string, addr sdk.AccAddress) bool {
	bz, err := k.addressCodec.StringToBytes(stored)
	return err == nil && addr.Equals(sdk.AccAddress(bz))
}

func (k Keeper) save(ctx context.Context, t types.Transfer, sender, recipient sdk.AccAddress) error {
	if err := k.Pending.Set(ctx, t.Id, t); err != nil {
		return err
	}
	if err := k.ByExpiry.Set(ctx, collections.Join(t.ExpiresAt, t.Id)); err != nil {
		return err
	}
	if err := k.BySender.Set(ctx, collections.Join(sender, t.Id)); err != nil {
		return err
	}
	return k.ByRecipient.Set(ctx, collections.Join(recipient, t.Id))
}

// finish removes a transfer from the pending state and emits its event.
func (k Keeper) finish(ctx context.Context, t types.Transfer, event string) error {
	sender, err := k.addressCodec.StringToBytes(t.Sender)
	if err != nil {
		return err
	}
	recipient, err := k.addressCodec.StringToBytes(t.Recipient)
	if err != nil {
		return err
	}
	if err := k.Pending.Remove(ctx, t.Id); err != nil {
		return err
	}
	if err := k.ByExpiry.Remove(ctx, collections.Join(t.ExpiresAt, t.Id)); err != nil {
		return err
	}
	if err := k.BySender.Remove(ctx, collections.Join(sdk.AccAddress(sender), t.Id)); err != nil {
		return err
	}
	if err := k.ByRecipient.Remove(ctx, collections.Join(sdk.AccAddress(recipient), t.Id)); err != nil {
		return err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(transferEvent(event, t))
	return nil
}

func transferEvent(name string, t types.Transfer) sdk.Event {
	return sdk.NewEvent(name,
		sdk.NewAttribute("id", sdkmath.NewIntFromUint64(t.Id).String()),
		sdk.NewAttribute("sender", t.Sender),
		sdk.NewAttribute("recipient", t.Recipient),
		sdk.NewAttribute("amount", sdkmath.NewIntFromUint64(t.Amount).String()),
	)
}
