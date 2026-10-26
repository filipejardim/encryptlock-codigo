// Package rotation carries out the founder's decision of 6 Oct: every
// LockKeeper that is switched on works and earns.
//
//   - The program next to each LockKeeper says "I am on" every 5 minutes
//     (MsgHeartbeat, free). The time between two heartbeats counts as time on.
//   - Every hour the earnings (new coins and the LockKeepers' share of fees)
//     are shared among everyone who was on, by time on × guarantee, with the
//     5% golden rule. Many fake 1-dollar computers gain nothing: 1.000 × 1 $
//     earns the same as 1 × 1.000 $.
//   - A drawn group of at most 100, always rotating, signs the blocks
//     (see committee.go).
package rotation

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	collcodec "cosmossdk.io/collections/codec"
	"cosmossdk.io/core/address"
	"cosmossdk.io/core/store"
	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"encryptlock/lockkeeper/protocol"
	"encryptlock/lockkeeper/x/powercap"
	"encryptlock/lockkeeper/x/rotation/types"
)

const daySeconds = 24 * 60 * 60

// StakingKeeper is the part of the staking keeper rotation reads.
type StakingKeeper interface {
	GetValidator(ctx context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error)
	PowerReduction(ctx context.Context) sdkmath.Int
}

// BankKeeper is the part of the bank keeper rotation uses.
type BankKeeper interface {
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
}

// SlashingKeeper takes a LockKeeper back into the set after it was out for
// not signing (the same as MsgUnjail, without needing the owner's key).
type SlashingKeeper interface {
	Unjail(ctx context.Context, validatorAddr sdk.ValAddress) error
}

// DistrKeeper credits a LockKeeper's earnings (its commission and its share
// for whoever put coins in its guarantee), ready to be collected.
type DistrKeeper interface {
	AllocateTokensToValidator(ctx context.Context, val stakingtypes.ValidatorI, tokens sdk.DecCoins) error
}

// Keeper stores who is on, the time on since the last payout and the group
// signing the blocks.
type Keeper struct {
	staking      StakingKeeper
	bank         BankKeeper
	distr        DistrKeeper
	slashing     SlashingKeeper
	addressCodec address.Codec
	denom        string
	feeCollector string
	// Seats and MaxSeats: the signing group starts with Seats and grows up
	// to MaxSeats (protocol.SigningSeats, MaxSigningSeats; smaller in tests).
	Seats, MaxSeats int

	// Online: per LockKeeper, the last heartbeat and the time on per day.
	Online collections.Map[sdk.ValAddress, types.Online]
	// Weights: per LockKeeper, guarantee (LOCK) × seconds on since the last payout.
	Weights collections.Map[sdk.ValAddress, int64]
	// PayPeriod: the hour (Unix seconds / 3600) of the last payout.
	PayPeriod collections.Item[int64]
	// Committee: the group signing the blocks, by consensus address, with
	// the rotation period each one got its seat in.
	Committee collections.Map[[]byte, int64]
	// RotatePeriod: the last rotation period (Unix seconds / 600).
	RotatePeriod collections.Item[int64]
	// Helpers: helper account → the LockKeeper it sends heartbeats for.
	Helpers collections.Map[sdk.AccAddress, []byte]
	// HelperOf: LockKeeper → its helper account.
	HelperOf collections.Map[sdk.ValAddress, []byte]
	// Announced: block-signing keys of computers waiting to be linked by QR.
	Announced collections.Map[sdk.AccAddress, types.Announced]
	// AnnouncedAt: (time, helper) of the announced keys, to delete them
	// after 24 hours (they would otherwise fill the blockchain for free).
	AnnouncedAt collections.KeySet[collections.Pair[int64, sdk.AccAddress]]
	// OnSince: per LockKeeper, when it was last switched on (or came back
	// after being taken out): on without a break since then.
	OnSince collections.Map[sdk.ValAddress, int64]
	// Chip proof (chip.go): the checkers' keys, the computers that started
	// the network, account → chip code and chip code → account.
	ChipCheckers collections.KeySet[[]byte]
	ChipFree     collections.KeySet[sdk.AccAddress]
	ChipOf       collections.Map[sdk.AccAddress, string]
	ChipUsed     collections.Map[string, sdk.AccAddress]
}

// NewKeeper creates the rotation keeper.
func NewKeeper(storeService store.KVStoreService, cdc codec.BinaryCodec, staking StakingKeeper, bank BankKeeper, distr DistrKeeper, slashing SlashingKeeper, addressCodec address.Codec, denom, feeCollector string) Keeper {
	sb := collections.NewSchemaBuilder(storeService)
	k := Keeper{
		staking:      staking,
		bank:         bank,
		distr:        distr,
		slashing:     slashing,
		addressCodec: addressCodec,
		denom:        denom,
		feeCollector: feeCollector,
		Seats:        protocol.SigningSeats,
		MaxSeats:     protocol.MaxSigningSeats,
		Online:       collections.NewMap(sb, collections.NewPrefix(0), "online", sdk.ValAddressKey, codec.CollValue[types.Online](cdc)),
		Weights:      collections.NewMap(sb, collections.NewPrefix(1), "weights", sdk.ValAddressKey, collections.Int64Value),
		PayPeriod:    collections.NewItem(sb, collections.NewPrefix(2), "pay_period", collections.Int64Value),
		Committee:    collections.NewMap(sb, collections.NewPrefix(3), "committee", collections.BytesKey, collections.Int64Value),
		RotatePeriod: collections.NewItem(sb, collections.NewPrefix(4), "rotate_period", collections.Int64Value),
		Helpers:      collections.NewMap(sb, collections.NewPrefix(5), "helpers", sdk.AccAddressKey, collections.BytesValue),
		HelperOf:     collections.NewMap(sb, collections.NewPrefix(6), "helper_of", sdk.ValAddressKey, collections.BytesValue),
		Announced:    collections.NewMap(sb, collections.NewPrefix(7), "announced", sdk.AccAddressKey, codec.CollValue[types.Announced](cdc)),
		AnnouncedAt:  collections.NewKeySet(sb, collections.NewPrefix(8), "announced_at", collections.PairKeyCodec(collections.Int64Key, sdk.AccAddressKey)),
		OnSince:      collections.NewMap(sb, collections.NewPrefix(9), "on_since", sdk.ValAddressKey, collections.Int64Value),
		ChipCheckers: collections.NewKeySet(sb, collections.NewPrefix(10), "chip_checkers", collections.BytesKey),
		ChipFree:     collections.NewKeySet(sb, collections.NewPrefix(11), "chip_free", sdk.AccAddressKey),
		ChipOf:       collections.NewMap(sb, collections.NewPrefix(12), "chip_of", sdk.AccAddressKey, collections.StringValue),
		ChipUsed:     collections.NewMap(sb, collections.NewPrefix(13), "chip_used", collections.StringKey, collcodec.KeyToValueCodec(sdk.AccAddressKey)),
	}
	if _, err := sb.Build(); err != nil {
		panic(err)
	}
	return k
}

func now(ctx context.Context) int64 { return sdk.UnwrapSDKContext(ctx).BlockTime().Unix() }

// Heartbeat records that the LockKeeper of account acc is on. The time since
// its previous heartbeat counts as time on, unless it was off in between.
//
// acc is the LockKeeper's account or its helper. A LockKeeper that was taken
// out of the set (switched off while drawn to sign) comes back here by
// itself, once the 1-minute wait is over.
func (k Keeper) Heartbeat(ctx context.Context, acc sdk.AccAddress) error {
	valAddr := sdk.ValAddress(acc)
	if owner, err := k.Helpers.Get(ctx, acc); err == nil {
		valAddr = owner
	} else if !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	val, err := k.staking.GetValidator(ctx, valAddr)
	if err != nil {
		return errorsmod.Wrapf(types.ErrNotLockKeeper, "%s", acc)
	}
	if ok, err := k.ChipOK(ctx, valAddr); err != nil {
		return err
	} else if !ok {
		return errorsmod.Wrapf(types.ErrNoChip, "%s", acc)
	}
	cameBack := false
	if val.IsJailed() {
		cameBack = true
		if err := k.slashing.Unjail(ctx, valAddr); err != nil {
			return errorsmod.Wrapf(types.ErrNotLockKeeper, "%s is out and cannot come back yet: %s", acc, err)
		}
		if val, err = k.staking.GetValidator(ctx, valAddr); err != nil {
			return err
		}
	}
	t := now(ctx)
	o, err := k.Online.Get(ctx, valAddr)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	if o.Last > 0 && t-o.Last < int64(protocol.HeartbeatMinGap.Seconds()) {
		return types.ErrTooSoon
	}
	var on int64
	// Taken out for missing blocks since the last heartbeat: that stretch was
	// not work and does not count (only it: the rest of the hour still pays).
	// A computer that only says "I am on" without signing is out at every
	// heartbeat and so earns nothing.
	if o.Last > 0 && t-o.Last <= int64(protocol.OnlineWindow.Seconds()) && !cameBack {
		on = t - o.Last
	}
	if day := t / daySeconds; o.Day != day {
		o = rollDays(o, day)
	}
	if on == 0 || cameBack {
		// Just switched on, or back after being out: stable again only
		// after protocol.StableAfter without a break.
		if err := k.OnSince.Set(ctx, valAddr, t); err != nil {
			return err
		}
	}
	o.Today += on
	o.Last = t
	if err := k.Online.Set(ctx, valAddr, o); err != nil {
		return err
	}
	if on == 0 {
		return nil
	}
	w, err := k.Weights.Get(ctx, valAddr)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	return k.Weights.Set(ctx, valAddr, w+GuaranteeWeight(val)*on)
}

// weightUnit: the guarantee counts for the earnings in steps of 0,01 LOCK
// (not whole LOCK like the voting power), so a 1-dollar guarantee earns its
// share even when 1 LOCK is worth more than 1 dollar.
const weightUnit = 1_000_000

// GuaranteeWeight is the guarantee of a LockKeeper in steps of 0,01 LOCK.
func GuaranteeWeight(val stakingtypes.Validator) int64 {
	return val.Tokens.QuoRaw(weightUnit).Int64()
}

// IsOnline reports whether the LockKeeper sent a heartbeat recently.
func (k Keeper) IsOnline(ctx context.Context, valAddr sdk.ValAddress) (bool, types.Online, error) {
	o, err := k.Online.Get(ctx, valAddr)
	if errors.Is(err, collections.ErrNotFound) {
		return false, o, nil
	}
	if err != nil {
		return false, o, err
	}
	return now(ctx)-o.Last <= int64(protocol.OnlineWindow.Seconds()), o, nil
}

// SecondsOn is the time on today plus yesterday, as of now.
// logDays: how many days of hours the log keeps (today included).
const logDays = 30

// history is the seconds on per day, newest (o.Day) first.
func history(o types.Online) []int64 {
	return append([]int64{o.Today, o.Yesterday}, o.Earlier...)
}

// rollDays moves the log forward to day (days with no heartbeat count 0).
func rollDays(o types.Online, day int64) types.Online {
	gap := day - o.Day
	if gap <= 0 {
		return o
	}
	h := history(o)
	if gap >= logDays {
		h = nil
	} else {
		h = append(make([]int64, gap), h...)
	}
	for len(h) < 2 {
		h = append(h, 0)
	}
	if len(h) > logDays {
		h = h[:logDays]
	}
	o.Day, o.Today, o.Yesterday, o.Earlier = day, h[0], h[1], append([]int64(nil), h[2:]...)
	return o
}

// HoursLog returns the seconds on today, yesterday, in the last 7 days and
// in the last 30 days, as of time t.
func HoursLog(o types.Online, t int64) (today, yesterday, week, month int64) {
	h := history(rollDays(o, t/daySeconds))
	for i, s := range h {
		if i < 7 {
			week += s
		}
		month += s
	}
	return h[0], h[1], week, month
}

func SecondsOn(o types.Online, t int64) int64 {
	switch t / daySeconds {
	case o.Day:
		return o.Today + o.Yesterday
	case o.Day + 1:
		return o.Today
	}
	return 0
}

// BeginBlock moves the fees waiting for the LockKeepers into the earnings
// pool (before distribution would pay them to the signers only) and, once an
// hour, shares the pool among everyone who was on.
func (k Keeper) BeginBlock(ctx context.Context) error {
	if err := k.pruneAnnounced(ctx); err != nil {
		return err
	}
	fees := k.bank.GetBalance(ctx, authtypes.NewModuleAddress(k.feeCollector), k.denom)
	if fees.IsPositive() {
		if err := k.bank.SendCoinsFromModuleToModule(ctx, k.feeCollector, types.ModuleName, sdk.NewCoins(fees)); err != nil {
			return err
		}
	}
	period := now(ctx) / int64(protocol.PayEvery.Seconds())
	last, err := k.PayPeriod.Get(ctx)
	if errors.Is(err, collections.ErrNotFound) {
		return k.PayPeriod.Set(ctx, period)
	}
	if err != nil || last == period {
		return err
	}
	if err := k.PayPeriod.Set(ctx, period); err != nil {
		return err
	}
	return k.pay(ctx)
}

// pay shares the pool by time on × guarantee (5% cap applied), credits each
// LockKeeper and starts counting again. Rounding leftovers stay for the next
// hour; so does everything when nobody was on.
func (k Keeper) pay(ctx context.Context) error {
	var vals []stakingtypes.Validator
	var weights []int64
	err := k.Weights.Walk(ctx, nil, func(addr sdk.ValAddress, w int64) (bool, error) {
		// Time out for not signing was already left out at the heartbeat
		// (Heartbeat): a computer that froze for a minute keeps the rest of
		// its hour.
		if val, err := k.staking.GetValidator(ctx, addr); err == nil && w > 0 {
			vals = append(vals, val)
			weights = append(weights, w)
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	if err := k.Weights.Clear(ctx, nil); err != nil {
		return err
	}
	pool := k.bank.GetBalance(ctx, authtypes.NewModuleAddress(types.ModuleName), k.denom).Amount
	if len(vals) == 0 || !pool.IsPositive() {
		return nil
	}
	weights = powercap.CapPowers(weights)
	var total int64
	for _, w := range weights {
		total += w
	}
	shares := make([]sdkmath.Int, len(vals))
	paid := sdkmath.ZeroInt()
	for i, w := range weights {
		shares[i] = pool.Mul(sdkmath.NewInt(w)).Quo(sdkmath.NewInt(total))
		paid = paid.Add(shares[i])
	}
	if !paid.IsPositive() {
		return nil
	}
	if err := k.bank.SendCoinsFromModuleToModule(ctx, types.ModuleName, distrtypes.ModuleName, sdk.NewCoins(sdk.NewCoin(k.denom, paid))); err != nil {
		return err
	}
	for i, val := range vals {
		if !shares[i].IsPositive() {
			continue
		}
		if err := k.distr.AllocateTokensToValidator(ctx, val, sdk.NewDecCoins(sdk.NewDecCoin(k.denom, shares[i]))); err != nil {
			return err
		}
	}
	return nil
}

type msgServer struct{ k Keeper }

func (s msgServer) Heartbeat(ctx context.Context, msg *types.MsgHeartbeat) (*types.MsgHeartbeatResponse, error) {
	acc, err := s.k.addressCodec.StringToBytes(msg.Keeper)
	if err != nil {
		return nil, err
	}
	if err := s.k.Heartbeat(ctx, acc); err != nil {
		return nil, err
	}
	return &types.MsgHeartbeatResponse{}, nil
}

func (s msgServer) SetHelper(ctx context.Context, msg *types.MsgSetHelper) (*types.MsgSetHelperResponse, error) {
	keeper, err := s.k.addressCodec.StringToBytes(msg.Keeper)
	if err != nil {
		return nil, err
	}
	var helper sdk.AccAddress
	if msg.Helper != "" {
		if helper, err = s.k.addressCodec.StringToBytes(msg.Helper); err != nil {
			return nil, err
		}
	}
	if err := s.k.SetHelper(ctx, keeper, helper); err != nil {
		return nil, err
	}
	return &types.MsgSetHelperResponse{}, nil
}

// SetHelper lets helper send the heartbeats of the LockKeeper of account
// keeper (nil removes the helper). A helper serves one LockKeeper only and
// cannot be a LockKeeper itself.
func (k Keeper) SetHelper(ctx context.Context, keeper, helper sdk.AccAddress) error {
	valAddr := sdk.ValAddress(keeper)
	if _, err := k.staking.GetValidator(ctx, valAddr); err != nil {
		return errorsmod.Wrapf(types.ErrNotLockKeeper, "%s", keeper)
	}
	if old, err := k.HelperOf.Get(ctx, valAddr); err == nil {
		if err := k.Helpers.Remove(ctx, old); err != nil {
			return err
		}
		if err := k.HelperOf.Remove(ctx, valAddr); err != nil {
			return err
		}
	} else if !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	if len(helper) == 0 {
		return nil
	}
	if _, err := k.staking.GetValidator(ctx, sdk.ValAddress(helper)); err == nil {
		return errorsmod.Wrap(types.ErrHelperTaken, "a LockKeeper cannot be a helper")
	}
	if has, err := k.Helpers.Has(ctx, helper); err != nil || has {
		return errorsmod.Wrap(types.ErrHelperTaken, "already the helper of another LockKeeper")
	}
	if err := k.Helpers.Set(ctx, helper, valAddr); err != nil {
		return err
	}
	return k.HelperOf.Set(ctx, valAddr, helper)
}

type queryServer struct{ k Keeper }

func (s queryServer) Status(ctx context.Context, req *types.QueryStatusRequest) (*types.QueryStatusResponse, error) {
	acc, err := s.k.addressCodec.StringToBytes(req.Keeper)
	if err != nil {
		return nil, err
	}
	valAddr := sdk.ValAddress(acc)
	online, o, err := s.k.IsOnline(ctx, valAddr)
	if err != nil {
		return nil, err
	}
	chipOK, err := s.k.ChipOK(ctx, valAddr)
	if err != nil {
		return nil, err
	}
	signing := false
	if val, err := s.k.staking.GetValidator(ctx, valAddr); err == nil {
		if cons, err := val.GetConsAddr(); err == nil {
			signing, _ = s.k.Committee.Has(ctx, cons)
		}
	}
	today, yesterday, week, month := HoursLog(o, now(ctx))
	return &types.QueryStatusResponse{
		Online: online, Signing: signing, SecondsOn: SecondsOn(o, now(ctx)), LastHeartbeat: o.Last,
		SecondsToday: today, SecondsYesterday: yesterday, SecondsWeek: week, SecondsMonth: month, Chip: chipOK,
	}, nil
}

func (s queryServer) Overview(ctx context.Context, _ *types.QueryOverviewRequest) (*types.QueryOverviewResponse, error) {
	var online, signing uint64
	var keepers, signers []string
	t := now(ctx)
	err := s.k.Online.Walk(ctx, nil, func(v sdk.ValAddress, o types.Online) (bool, error) {
		if t-o.Last <= int64(protocol.OnlineWindow.Seconds()) {
			online++
			keepers = append(keepers, v.String())
			if val, err := s.k.staking.GetValidator(ctx, v); err == nil {
				if cons, err := val.GetConsAddr(); err == nil {
					if in, _ := s.k.Committee.Has(ctx, cons); in {
						signers = append(signers, v.String())
					}
				}
			}
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	err = s.k.Committee.Walk(ctx, nil, func([]byte, int64) (bool, error) {
		signing++
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	return &types.QueryOverviewResponse{Online: online, Signing: signing, Seats: uint64(s.k.Seats), OnlineKeepers: keepers, SigningKeepers: signers}, nil
}
