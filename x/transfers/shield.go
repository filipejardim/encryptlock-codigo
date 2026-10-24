package transfers

import (
	"context"
	"errors"
	"math"

	"cosmossdk.io/collections"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"encryptlock/lockkeeper/protocol"
	"encryptlock/lockkeeper/x/transfers/types"
)

// Security shield (white paper, section 7.2): a second device that can only
// approve and freeze, extra approval for new contacts and large amounts,
// "Not me" (freeze), and a daily limit. Loosening any protection takes time;
// tightening it is immediate, so a thief with the phone cannot quickly undo
// the owner's protections.

func seconds(d interface{ Seconds() float64 }) int64 { return int64(d.Seconds()) }

// unlimited reads 0 as "no limit" for comparisons.
func unlimited(v uint64) uint64 {
	if v == 0 {
		return math.MaxUint64
	}
	return v
}

// matured applies the pending changes whose time has come.
func matured(s types.Shield, now int64) types.Shield {
	if s.DeviceChangePending && now >= s.DeviceChangeAt {
		s.Device, s.PendingDevice, s.DeviceChangePending, s.DeviceChangeAt = s.PendingDevice, "", false, 0
	}
	if s.DailyLimitChangeAt != 0 && now >= s.DailyLimitChangeAt {
		s.DailyLimit, s.PendingDailyLimit, s.DailyLimitChangeAt = s.PendingDailyLimit, 0, 0
	}
	if s.ApprovalAboveChangeAt != 0 && now >= s.ApprovalAboveChangeAt {
		s.ApprovalAbove, s.PendingApprovalAbove, s.ApprovalAboveChangeAt = s.PendingApprovalAbove, 0, 0
	}
	if s.VaultDelayChangeAt != 0 && now >= s.VaultDelayChangeAt {
		s.VaultDelay, s.PendingVaultDelay, s.VaultDelayChangeAt = s.PendingVaultDelay, 0, 0
	}
	if s.Frozen {
		delay := seconds(protocol.UnfreezeDelay)
		both := s.UnfreezeOwnerAt != 0 && s.UnfreezeDeviceAt != 0
		ownerWaited := s.UnfreezeOwnerAt != 0 && now >= s.UnfreezeOwnerAt+delay
		deviceWaited := s.UnfreezeDeviceAt != 0 && now >= s.UnfreezeDeviceAt+delay
		if both || ownerWaited || deviceWaited {
			s.Frozen, s.FrozenAt, s.UnfreezeOwnerAt, s.UnfreezeDeviceAt = false, 0, 0, 0
		}
	}
	return s
}

func now(ctx context.Context) int64 { return sdk.UnwrapSDKContext(ctx).BlockTime().Unix() }

// GetShield returns an account's shield with due changes applied.
func (k Keeper) GetShield(ctx context.Context, addr sdk.AccAddress) (types.Shield, error) {
	s, err := k.Shields.Get(ctx, addr)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return s, err
	}
	return matured(s, now(ctx)), nil
}

func (k Keeper) isDevice(s types.Shield, signer sdk.AccAddress) bool {
	return s.Device != "" && k.sameAddress(s.Device, signer)
}

// SetDevice links (or changes, or removes with an empty device) the second
// device. The first device applies at once; any change after that takes 7
// days, so a thief cannot remove it quickly.
func (k Keeper) SetDevice(ctx context.Context, owner sdk.AccAddress, device string) error {
	if device != "" {
		dev, err := k.addressCodec.StringToBytes(device)
		if err != nil {
			return err
		}
		if owner.Equals(sdk.AccAddress(dev)) {
			return types.ErrInvalidDevice
		}
	}
	s, err := k.GetShield(ctx, owner)
	if err != nil {
		return err
	}
	// While frozen the device cannot be added, changed or removed: a thief
	// with the 24 words must not swap it, nor add one of his own and unfreeze
	// at once with "both keys".
	if s.Frozen {
		return types.ErrFrozen
	}
	if s.Device == "" && !s.DeviceChangePending {
		s.Device = device
	} else {
		s.PendingDevice, s.DeviceChangePending = device, true
		s.DeviceChangeAt = now(ctx) + seconds(protocol.DeviceChangeDelay)
	}
	return k.Shields.Set(ctx, owner, s)
}

// SetLimits sets the daily limit and the approval amount (0 = none). Stricter
// values apply at once; looser ones 48 hours later.
func (k Keeper) SetLimits(ctx context.Context, owner sdk.AccAddress, dailyLimit, approvalAbove uint64) error {
	s, err := k.GetShield(ctx, owner)
	if err != nil {
		return err
	}
	loosen := unlimited(dailyLimit) > unlimited(s.DailyLimit) || unlimited(approvalAbove) > unlimited(s.ApprovalAbove)
	if s.Frozen && loosen {
		return types.ErrFrozen // while frozen, protections only get stricter
	}
	later := now(ctx) + seconds(protocol.LimitLoosenDelay)

	if unlimited(dailyLimit) <= unlimited(s.DailyLimit) {
		s.DailyLimit, s.PendingDailyLimit, s.DailyLimitChangeAt = dailyLimit, 0, 0
	} else {
		s.PendingDailyLimit, s.DailyLimitChangeAt = dailyLimit, later
	}
	if unlimited(approvalAbove) <= unlimited(s.ApprovalAbove) {
		s.ApprovalAbove, s.PendingApprovalAbove, s.ApprovalAboveChangeAt = approvalAbove, 0, 0
	} else {
		s.PendingApprovalAbove, s.ApprovalAboveChangeAt = approvalAbove, later
	}
	return k.Shields.Set(ctx, owner, s)
}

// SetVault makes the account a Sovereign Vault (or changes its delay; 0 stops
// it). A longer delay applies at once; a shorter one only after the current
// delay, so a thief cannot quickly remove the protection.
func (k Keeper) SetVault(ctx context.Context, owner sdk.AccAddress, delayDays uint32) error {
	if delayDays != 0 && (delayDays < protocol.VaultMinDelayDays || delayDays > protocol.VaultMaxDelayDays) {
		return types.ErrInvalidVault
	}
	s, err := k.GetShield(ctx, owner)
	if err != nil {
		return err
	}
	delay := int64(delayDays) * 24 * 60 * 60
	if s.Frozen && delay < s.VaultDelay {
		return types.ErrFrozen // while frozen, protections only get stricter
	}
	if delay >= s.VaultDelay {
		s.VaultDelay, s.PendingVaultDelay, s.VaultDelayChangeAt = delay, 0, 0
	} else {
		s.PendingVaultDelay, s.VaultDelayChangeAt = delay, now(ctx)+s.VaultDelay
	}
	return k.Shields.Set(ctx, owner, s)
}

// freezeRefundsPerTx bounds the work of one "Not me", so a thief cannot make
// it fail by first sending hundreds of tiny transfers. The ones left over
// cannot be accepted or approved while the account is frozen and come back
// by themselves when they expire.
const freezeRefundsPerTx = 100

// Freeze ("Not me"), by the account or its second device: the account can no
// longer send, pending transfers it sent go back to it (up to
// freezeRefundsPerTx now, the rest when they expire; none of them can be
// accepted meanwhile), and pending loosening changes (device, limits) are
// dropped. Returns how many transfers were canceled now.
func (k Keeper) Freeze(ctx context.Context, signer, account sdk.AccAddress) (uint64, error) {
	s, err := k.GetShield(ctx, account)
	if err != nil {
		return 0, err
	}
	if !signer.Equals(account) && !k.isDevice(s, signer) {
		return 0, types.ErrNotDevice
	}
	s.Frozen, s.FrozenAt, s.UnfreezeOwnerAt, s.UnfreezeDeviceAt = true, now(ctx), 0, 0
	s.PendingDevice, s.DeviceChangePending, s.DeviceChangeAt = "", false, 0
	s.PendingDailyLimit, s.DailyLimitChangeAt = 0, 0
	s.PendingApprovalAbove, s.ApprovalAboveChangeAt = 0, 0
	s.PendingVaultDelay, s.VaultDelayChangeAt = 0, 0
	if err := k.Shields.Set(ctx, account, s); err != nil {
		return 0, err
	}
	if err := k.LastFrozen.Set(ctx, account, now(ctx)); err != nil {
		return 0, err
	}
	if h := *k.onFreeze; h != nil {
		if err := h(ctx, account); err != nil {
			return 0, err
		}
	}

	iter, err := k.BySender.Iterate(ctx, collections.NewPrefixedPairRange[sdk.AccAddress, uint64](account))
	if err != nil {
		return 0, err
	}
	var ids []uint64
	for ; iter.Valid() && len(ids) < freezeRefundsPerTx; iter.Next() {
		key, err := iter.Key()
		if err != nil {
			iter.Close()
			return 0, err
		}
		ids = append(ids, key.K2())
	}
	iter.Close()
	for _, id := range ids {
		t, err := k.get(ctx, id)
		if err != nil {
			return 0, err
		}
		if err := k.refund(ctx, t, "transfer_canceled"); err != nil {
			return 0, err
		}
	}
	return uint64(len(ids)), nil
}

// Unfreeze records a request by the account or its second device. With both
// requests the account unfreezes at once; with one, 7 days after it.
func (k Keeper) Unfreeze(ctx context.Context, signer, account sdk.AccAddress) (unfrozen bool, unfreezesAt int64, err error) {
	s, err := k.GetShield(ctx, account)
	if err != nil {
		return false, 0, err
	}
	if !s.Frozen {
		return false, 0, types.ErrNotFrozen
	}
	t := now(ctx)
	switch {
	case signer.Equals(account):
		if s.UnfreezeOwnerAt == 0 {
			s.UnfreezeOwnerAt = t
		}
	case k.isDevice(s, signer):
		if s.UnfreezeDeviceAt == 0 {
			s.UnfreezeDeviceAt = t
		}
	default:
		return false, 0, types.ErrNotDevice
	}
	s = matured(s, t)
	if err := k.Shields.Set(ctx, account, s); err != nil {
		return false, 0, err
	}
	if !s.Frozen {
		return true, 0, nil
	}
	first := s.UnfreezeOwnerAt
	if first == 0 || (s.UnfreezeDeviceAt != 0 && s.UnfreezeDeviceAt < first) {
		first = s.UnfreezeDeviceAt
	}
	return false, first + seconds(protocol.UnfreezeDelay), nil
}

// checkSend applies the shield to a new transfer: frozen accounts cannot
// send, the daily limit is counted, and it returns whether the second device
// must approve.
func (k Keeper) checkSend(ctx context.Context, sender, recipient sdk.AccAddress, amount uint64) (needsApproval bool, vaultDelay int64, err error) {
	s, err := k.GetShield(ctx, sender)
	if err != nil {
		return false, 0, err
	}
	if s.Frozen {
		return false, 0, types.ErrFrozen
	}
	vaultDelay = s.VaultDelay
	if err := k.countDaily(ctx, sender, s, amount); err != nil {
		return false, 0, err
	}

	if s.Device == "" {
		return false, vaultDelay, nil
	}
	known, err := k.Contacts.Has(ctx, contactKey(sender, recipient))
	if err != nil {
		return false, 0, err
	}
	large := s.ApprovalAbove != 0 && amount > s.ApprovalAbove
	return !known || large, vaultDelay, nil
}

// Approve, by the sender's second device: the transfer no longer waits for
// approval and the recipient has 1 hour from now to accept.
func (k Keeper) Approve(ctx context.Context, device sdk.AccAddress, id uint64) error {
	t, err := k.get(ctx, id)
	if err != nil {
		return err
	}
	if !t.AwaitingApproval {
		return types.ErrNotAwaiting
	}
	if now(ctx) >= t.ExpiresAt {
		return types.ErrExpired
	}
	sender, err := k.addressCodec.StringToBytes(t.Sender)
	if err != nil {
		return err
	}
	s, err := k.GetShield(ctx, sender)
	if err != nil {
		return err
	}
	if !k.isDevice(s, device) {
		return types.ErrNotDevice
	}
	if s.Frozen {
		return types.ErrFrozen
	}
	if err := k.sentBeforeFreeze(ctx, sender, t); err != nil {
		return err
	}

	if err := k.ByExpiry.Remove(ctx, collections.Join(t.ExpiresAt, t.Id)); err != nil {
		return err
	}
	t.AwaitingApproval = false
	// The recipient gets 1 hour from now, or from the vault release time.
	t.ExpiresAt = max(now(ctx), t.ReleaseAt) + seconds(protocol.TransferAcceptanceWindow)
	if err := k.Pending.Set(ctx, t.Id, t); err != nil {
		return err
	}
	if err := k.ByExpiry.Set(ctx, collections.Join(t.ExpiresAt, t.Id)); err != nil {
		return err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(transferEvent("transfer_approved", t))
	return nil
}

// sentBeforeFreeze refuses a transfer sent before the sender's last "Not
// me": a thief may have sent more than one "Not me" cancels at once (or with
// a long vault delay); after the owner unfreezes, those must stay dead. They
// go back to the sender when they expire, or the sender cancels them.
func (k Keeper) sentBeforeFreeze(ctx context.Context, sender sdk.AccAddress, t types.Transfer) error {
	at, err := k.LastFrozen.Get(ctx, sender)
	if errors.Is(err, collections.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if t.CreatedAt <= at {
		return types.ErrFrozen
	}
	return nil
}

// countDaily counts amount against the daily limit (if any).
func (k Keeper) countDaily(ctx context.Context, owner sdk.AccAddress, s types.Shield, amount uint64) error {
	if s.DailyLimit == 0 {
		return nil
	}
	t := now(ctx)
	if t-s.WindowStart >= seconds(protocol.DailyLimitWindow) {
		s.WindowStart, s.WindowSpent = t, 0
	}
	if s.WindowSpent+amount > s.DailyLimit || s.WindowSpent+amount < s.WindowSpent {
		return types.ErrDailyLimit
	}
	s.WindowSpent += amount
	return k.Shields.Set(ctx, owner, s)
}

// CheckSell applies the shield to LOCK leaving through the market (security
// review, 11 Oct: selling to oneself must not get around the protections):
// a frozen account cannot sell, a Sovereign Vault cannot sell directly, the
// amount counts for the daily limit, and with a second device the sale
// needs its approval (the buyer is unknown, like a new contact).
func (k Keeper) CheckSell(ctx context.Context, seller sdk.AccAddress, amount uint64) (needsApproval bool, err error) {
	s, err := k.GetShield(ctx, seller)
	if err != nil {
		return false, err
	}
	if s.Frozen {
		return false, types.ErrFrozen
	}
	if s.VaultDelay > 0 {
		return false, types.ErrVaultCannotSell
	}
	if err := k.countDaily(ctx, seller, s, amount); err != nil {
		return false, err
	}
	return s.Device != "", nil
}

// IsFrozen reports whether "Not me" froze the account.
func (k Keeper) IsFrozen(ctx context.Context, addr sdk.AccAddress) (bool, error) {
	s, err := k.GetShield(ctx, addr)
	return s.Frozen, err
}

// IsDeviceOf reports whether signer is the second device of owner.
func (k Keeper) IsDeviceOf(ctx context.Context, owner, signer sdk.AccAddress) (bool, error) {
	s, err := k.GetShield(ctx, owner)
	if err != nil {
		return false, err
	}
	return k.isDevice(s, signer), nil
}

// FreezeHook runs when an account presses "Not me" (the market takes its
// open sell offers down).
type FreezeHook func(ctx context.Context, account sdk.AccAddress) error

// SetFreezeHook sets what else "Not me" does (once, when the app starts).
func (k Keeper) SetFreezeHook(h FreezeHook) { *k.onFreeze = h }

func contactKey(sender, recipient sdk.AccAddress) collections.Pair[sdk.AccAddress, sdk.AccAddress] {
	return collections.Join(sender, recipient)
}
