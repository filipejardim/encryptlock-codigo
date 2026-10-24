// Package recovery implements inheritance and recovery by trusted people
// (white paper, section 7.3). Both are optional.
//
//   - Inheritance: if the account shows no sign of life for the time its owner
//     chose (at least 1 year), the heirs confirm (all of them, unless the owner
//     chose fewer, within 30 days); 30 days later the coins go to the heirs in
//     the chosen percentages. Any sign of life cancels it.
//   - Recovery: if the owner lost both the phone and the 24 words, enough
//     trusted people (at least 2) approve moving the coins to the owner's new
//     account; 30 days later they move. The owner can cancel meanwhile.
//
// Moving the coins at the end is a network movement, not a transfer between
// people, so it does not wait for acceptance (white paper, section 6.2).
package recovery

import (
	"context"
	"errors"
	"slices"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	"cosmossdk.io/core/store"
	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"encryptlock/lockkeeper/protocol"
	"encryptlock/lockkeeper/x/recovery/types"
)

const (
	KindInheritance = "inheritance"
	KindRecovery    = "recovery"
)

// BankKeeper is the part of the bank module this module uses.
type BankKeeper interface {
	SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins
	SendCoins(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) error
}

// DevFund is the development fund's address (x/transfers): when the fund's
// own account is inherited or recovered, its future fees follow.
type DevFund interface {
	DevFundAddress(ctx context.Context) (sdk.AccAddress, error)
	MoveDevFund(ctx context.Context, to sdk.AccAddress) error
}

// Staking is the part of the staking module this module uses: the guarantee
// of a LockKeeper whose account is inherited or recovered is given back.
type Staking interface {
	GetDelegatorDelegations(ctx context.Context, delegator sdk.AccAddress, maxRetrieve uint16) ([]stakingtypes.Delegation, error)
	Undelegate(ctx context.Context, delAddr sdk.AccAddress, valAddr sdk.ValAddress, sharesAmount sdkmath.LegacyDec) (time.Time, sdkmath.Int, error)
}

// Registry tells whether an account is registered.
type Registry interface {
	IsRegistered(ctx context.Context, addr sdk.AccAddress) (bool, error)
}

// Keeper stores each account's plan, last sign of life and running claim.
type Keeper struct {
	bank         BankKeeper
	registry     Registry
	addressCodec address.Codec
	denom        string

	devFund *DevFund
	staking *Staking

	Statuses collections.Map[sdk.AccAddress, types.Status]
	// Due orders running claims by execution time.
	Due collections.KeySet[collections.Pair[int64, sdk.AccAddress]]
	// Forwards: accounts already inherited or recovered. Whatever reaches them
	// later (the LockKeeper guarantee given back, locks and offers that
	// return) goes on to the heirs or the new account, in the same shares.
	Forwards collections.Map[sdk.AccAddress, types.Plan]
	// ForwardCursor: where the round of forward checks stopped.
	ForwardCursor collections.Item[[]byte]
}

// NewKeeper creates the recovery keeper.
func NewKeeper(storeService store.KVStoreService, cdc codec.BinaryCodec, bank BankKeeper, registry Registry, addressCodec address.Codec, denom string) Keeper {
	sb := collections.NewSchemaBuilder(storeService)
	k := Keeper{
		bank:          bank,
		registry:      registry,
		addressCodec:  addressCodec,
		denom:         denom,
		devFund:       new(DevFund),
		staking:       new(Staking),
		Statuses:      collections.NewMap(sb, collections.NewPrefix(0), "statuses", sdk.AccAddressKey, codec.CollValue[types.Status](cdc)),
		Due:           collections.NewKeySet(sb, collections.NewPrefix(1), "due", collections.PairKeyCodec(collections.Int64Key, sdk.AccAddressKey)),
		Forwards:      collections.NewMap(sb, collections.NewPrefix(2), "forwards", sdk.AccAddressKey, codec.CollValue[types.Plan](cdc)),
		ForwardCursor: collections.NewItem(sb, collections.NewPrefix(3), "forward_cursor", collections.BytesValue),
	}
	if _, err := sb.Build(); err != nil {
		panic(err)
	}
	return k
}

func now(ctx context.Context) int64 { return sdk.UnwrapSDKContext(ctx).BlockTime().Unix() }

func seconds(d interface{ Seconds() float64 }) int64 { return int64(d.Seconds()) }

func hasPlan(p *types.Plan) bool { return p != nil && (len(p.Heirs) > 0 || len(p.Guardians) > 0) }

// GetStatus returns an account's status with a due plan change applied, and
// whether the account has one at all.
func (k Keeper) GetStatus(ctx context.Context, addr sdk.AccAddress) (types.Status, bool, error) {
	s, err := k.Statuses.Get(ctx, addr)
	if errors.Is(err, collections.ErrNotFound) {
		return types.Status{}, false, nil
	}
	if err != nil {
		return s, false, err
	}
	if s.PlanChangePending && now(ctx) >= s.PlanChangeAt {
		s.Plan, s.PendingPlan, s.PlanChangePending, s.PlanChangeAt = s.PendingPlan, nil, false, 0
	}
	return s, true, nil
}

func (k Keeper) validatePlan(ctx context.Context, owner sdk.AccAddress, p *types.Plan) error {
	if p == nil {
		return nil
	}
	invalid := func(format string, args ...any) error {
		return errorsmod.Wrapf(types.ErrInvalidPlan, format, args...)
	}
	seen := map[string]bool{}
	checkAccount := func(addrStr, role string) error {
		addr, err := k.addressCodec.StringToBytes(addrStr)
		if err != nil {
			return err
		}
		if owner.Equals(sdk.AccAddress(addr)) {
			return invalid("the owner cannot be its own %s", role)
		}
		if seen[role+addrStr] {
			return invalid("%s %s listed twice", role, addrStr)
		}
		seen[role+addrStr] = true
		registered, err := k.registry.IsRegistered(ctx, addr)
		if err != nil {
			return err
		}
		if !registered {
			return errorsmod.Wrapf(types.ErrNotRegistered, "%s %s", role, addrStr)
		}
		// An account already inherited or recovered signs nothing and only
		// passes coins on: it cannot inherit, approve or be the new account.
		if closed, err := k.IsClosed(ctx, addr); err != nil {
			return err
		} else if closed {
			return errorsmod.Wrapf(types.ErrClosed, "%s %s", role, addrStr)
		}
		return nil
	}

	if len(p.Heirs) > protocol.MaxHeirs {
		return invalid("at most %d heirs", protocol.MaxHeirs)
	}
	var total uint32
	for _, h := range p.Heirs {
		if err := checkAccount(h.Address, "heir"); err != nil {
			return err
		}
		if h.Percent == 0 {
			return invalid("heir %s has 0%%", h.Address)
		}
		total += h.Percent
	}
	if int(p.HeirThreshold) > len(p.Heirs) {
		return invalid("%d heirs cannot confirm with only %d heirs", p.HeirThreshold, len(p.Heirs))
	}
	if len(p.Heirs) > 0 {
		if total != 100 {
			return invalid("heirs add up to %d%%, must be 100%%", total)
		}
		if p.InactivityDays < protocol.MinInactivityDays {
			return invalid("at least %d days without a sign of life", protocol.MinInactivityDays)
		}
	}

	if len(p.Guardians) > 0 {
		if len(p.Guardians) < protocol.MinGuardians || len(p.Guardians) > protocol.MaxGuardians {
			return invalid("between %d and %d trusted people", protocol.MinGuardians, protocol.MaxGuardians)
		}
		if p.GuardianThreshold < protocol.MinGuardianThreshold || int(p.GuardianThreshold) > len(p.Guardians) {
			return invalid("between %d and %d trusted people must approve", protocol.MinGuardianThreshold, len(p.Guardians))
		}
		for _, g := range p.Guardians {
			if err := checkAccount(g, "trusted person"); err != nil {
				return err
			}
		}
	}
	return nil
}

// SetPlan sets heirs and trusted people. The first plan applies at once; any
// later change (including removing it) takes 7 days.
func (k Keeper) SetPlan(ctx context.Context, owner sdk.AccAddress, plan *types.Plan) error {
	if err := k.validatePlan(ctx, owner, plan); err != nil {
		return err
	}
	s, _, err := k.GetStatus(ctx, owner)
	if err != nil {
		return err
	}
	if !hasPlan(s.Plan) && !s.PlanChangePending {
		s.Plan = plan
	} else {
		s.PendingPlan, s.PlanChangePending = plan, true
		s.PlanChangeAt = now(ctx) + seconds(protocol.PlanChangeDelay)
	}
	s.LastActive = now(ctx)
	return k.Statuses.Set(ctx, owner, s)
}

// RecordActivity is called for every transaction an account signs: it is a
// sign of life and cancels a running inheritance claim. Accounts without a
// plan are not tracked.
func (k Keeper) RecordActivity(ctx context.Context, addr sdk.AccAddress) error {
	s, ok, err := k.GetStatus(ctx, addr)
	if err != nil || !ok {
		return err
	}
	s.LastActive = now(ctx)
	s.HeirConfirmations, s.ConfirmationsStartedAt = nil, 0
	if s.Claim != nil && s.Claim.Kind == KindInheritance {
		if err := k.Due.Remove(ctx, collections.Join(s.Claim.ExecutesAt, addr)); err != nil {
			return err
		}
		s.Claim = nil
	}
	return k.Statuses.Set(ctx, addr, s)
}

// ClaimInheritance, by an heir, after the chosen time without a sign of life.
func (k Keeper) ClaimInheritance(ctx context.Context, heir, account sdk.AccAddress) (int64, error) {
	s, ok, err := k.GetStatus(ctx, account)
	if err != nil {
		return 0, err
	}
	if !ok || s.Plan == nil || len(s.Plan.Heirs) == 0 {
		return 0, types.ErrNoPlan
	}
	isHeir := false
	for _, h := range s.Plan.Heirs {
		if k.same(h.Address, heir) {
			isHeir = true
		}
	}
	if !isHeir {
		return 0, types.ErrNotHeir
	}
	if now(ctx)-s.LastActive < int64(s.Plan.InactivityDays)*24*60*60 {
		return 0, types.ErrStillActive
	}
	if s.Claim != nil {
		return 0, types.ErrClaimRunning
	}

	// Confirmations older than 30 days lapse: the claim starts again.
	if s.ConfirmationsStartedAt != 0 && now(ctx) >= s.ConfirmationsStartedAt+seconds(protocol.HeirConfirmWindow) {
		s.HeirConfirmations, s.ConfirmationsStartedAt = nil, 0
	}
	heirStr, err := k.addressCodec.BytesToString(heir)
	if err != nil {
		return 0, err
	}
	if !slices.Contains(s.HeirConfirmations, heirStr) {
		s.HeirConfirmations = append(s.HeirConfirmations, heirStr)
	}
	if s.ConfirmationsStartedAt == 0 {
		s.ConfirmationsStartedAt = now(ctx)
		// The owner's app shows an alert as soon as this event appears.
		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("inheritance_requested",
			sdk.NewAttribute("account", account.String()),
			sdk.NewAttribute("heir", heirStr),
		))
	}

	required, err := k.heirsRequired(ctx, s.Plan)
	if err != nil {
		return 0, err
	}
	if len(s.HeirConfirmations) < required {
		return 0, k.Statuses.Set(ctx, account, s)
	}
	s.HeirConfirmations, s.ConfirmationsStartedAt = nil, 0
	return k.startClaim(ctx, account, &s, &types.Claim{Kind: KindInheritance})
}

// HeirsRequired is how many heirs must confirm: the owner's choice, or all.
func HeirsRequired(p *types.Plan) int {
	if p.HeirThreshold == 0 {
		return len(p.Heirs)
	}
	return int(p.HeirThreshold)
}

// heirsRequired is HeirsRequired without the heirs whose own account was
// inherited or recovered since: they can sign nothing, so they cannot
// confirm (their share still reaches whoever their coins go to).
func (k Keeper) heirsRequired(ctx context.Context, p *types.Plan) (int, error) {
	open := 0
	for _, h := range p.Heirs {
		addr, err := k.addressCodec.StringToBytes(h.Address)
		if err != nil {
			return 0, err
		}
		closed, err := k.IsClosed(ctx, addr)
		if err != nil {
			return 0, err
		}
		if !closed {
			open++
		}
	}
	return min(HeirsRequired(p), open), nil
}

// ApproveRecovery, by a trusted person, to move the coins to newAddress. Once
// enough trusted people approve the same new account, the claim starts.
func (k Keeper) ApproveRecovery(ctx context.Context, guardian, account, newAddress sdk.AccAddress) (uint32, int64, error) {
	s, ok, err := k.GetStatus(ctx, account)
	if err != nil {
		return 0, 0, err
	}
	if !ok || s.Plan == nil || len(s.Plan.Guardians) == 0 {
		return 0, 0, types.ErrNoPlan
	}
	isGuardian := false
	for _, g := range s.Plan.Guardians {
		if k.same(g, guardian) {
			isGuardian = true
		}
	}
	if !isGuardian {
		return 0, 0, types.ErrNotGuardian
	}
	if s.Claim != nil {
		return 0, 0, types.ErrClaimRunning
	}
	if newAddress.Equals(account) {
		return 0, 0, errorsmod.Wrap(types.ErrInvalidPlan, "the new account must be another account")
	}
	registered, err := k.registry.IsRegistered(ctx, newAddress)
	if err != nil {
		return 0, 0, err
	}
	if !registered {
		return 0, 0, types.ErrNotRegistered
	}
	if closed, err := k.IsClosed(ctx, newAddress); err != nil {
		return 0, 0, err
	} else if closed {
		return 0, 0, types.ErrClosed
	}

	guardianStr, err := k.addressCodec.BytesToString(guardian)
	if err != nil {
		return 0, 0, err
	}
	newStr, err := k.addressCodec.BytesToString(newAddress)
	if err != nil {
		return 0, 0, err
	}
	approvals := s.Approvals[:0]
	for _, a := range s.Approvals {
		if a.Guardian != guardianStr {
			approvals = append(approvals, a)
		}
	}
	s.Approvals = append(approvals, &types.RecoveryApproval{Guardian: guardianStr, NewAddress: newStr})

	// Only people who are trusted now count: approvals given before the owner
	// removed someone from the plan do not.
	var count uint32
	for _, a := range s.Approvals {
		if a.NewAddress == newStr && slices.Contains(s.Plan.Guardians, a.Guardian) {
			count++
		}
	}
	if count < s.Plan.GuardianThreshold {
		return count, 0, k.Statuses.Set(ctx, account, s)
	}
	at, err := k.startClaim(ctx, account, &s, &types.Claim{Kind: KindRecovery, NewAddress: newStr})
	return count, at, err
}

func (k Keeper) startClaim(ctx context.Context, account sdk.AccAddress, s *types.Status, claim *types.Claim) (int64, error) {
	if s.Claim != nil {
		return 0, types.ErrClaimRunning
	}
	claim.StartedAt = now(ctx)
	claim.ExecutesAt = claim.StartedAt + seconds(protocol.ClaimDelay)
	s.Claim = claim
	if err := k.Statuses.Set(ctx, account, *s); err != nil {
		return 0, err
	}
	if err := k.Due.Set(ctx, collections.Join(claim.ExecutesAt, account)); err != nil {
		return 0, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(claim.Kind+"_started",
		sdk.NewAttribute("account", account.String()),
		sdk.NewAttribute("executes_at", sdkmath.NewInt(claim.ExecutesAt).String()),
	))
	return claim.ExecutesAt, nil
}

// CancelClaims, by the owner: cancels a running claim, recovery approvals and
// a pending plan change.
func (k Keeper) CancelClaims(ctx context.Context, owner sdk.AccAddress) error {
	s, ok, err := k.GetStatus(ctx, owner)
	if err != nil {
		return err
	}
	if !ok || (s.Claim == nil && len(s.Approvals) == 0 && len(s.HeirConfirmations) == 0 && !s.PlanChangePending) {
		return types.ErrNothingToCancel
	}
	if s.Claim != nil {
		if err := k.Due.Remove(ctx, collections.Join(s.Claim.ExecutesAt, owner)); err != nil {
			return err
		}
	}
	s.Claim, s.Approvals = nil, nil
	s.HeirConfirmations, s.ConfirmationsStartedAt = nil, 0
	s.PendingPlan, s.PlanChangePending, s.PlanChangeAt = nil, false, 0
	return k.Statuses.Set(ctx, owner, s)
}

// ExecuteDue moves the coins of every claim whose 30 days have passed. It
// runs at the end of every block.
func (k Keeper) ExecuteDue(ctx context.Context) error {
	// Every claim whose execution time is now or earlier.
	iter, err := k.Due.Iterate(ctx, collections.NewPrefixUntilPairRange[int64, sdk.AccAddress](now(ctx)))
	if err != nil {
		return err
	}
	// At most maxDuePerBlock per block, read one by one; the rest in the next
	// blocks.
	var keys []collections.Pair[int64, sdk.AccAddress]
	for ; iter.Valid() && len(keys) < maxDuePerBlock; iter.Next() {
		key, err := iter.Key()
		if err != nil {
			iter.Close()
			return err
		}
		keys = append(keys, key)
	}
	iter.Close()
	for _, key := range keys {
		// A claim that cannot be paid never stops the blockchain: it is
		// dropped (with an event) and the others go on.
		cache, write := sdk.UnwrapSDKContext(ctx).CacheContext()
		if err := k.execute(cache, key.K2()); err != nil {
			sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("claim_failed",
				sdk.NewAttribute("account", key.K2().String()),
				sdk.NewAttribute("error", err.Error()),
			))
			if err := k.dropClaim(ctx, key.K2()); err != nil {
				return err
			}
		} else {
			write()
		}
		if err := k.Due.Remove(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

// maxDuePerBlock: claims executed in one block at most.
const maxDuePerBlock = 1000

// dropClaim forgets a claim that could not be paid.
func (k Keeper) dropClaim(ctx context.Context, account sdk.AccAddress) error {
	s, ok, err := k.GetStatus(ctx, account)
	if err != nil || !ok {
		return err
	}
	s.Claim, s.Approvals = nil, nil
	return k.Statuses.Set(ctx, account, s)
}

func (k Keeper) execute(ctx context.Context, account sdk.AccAddress) error {
	s, ok, err := k.GetStatus(ctx, account)
	if err != nil || !ok || s.Claim == nil {
		return err
	}
	balance := k.bank.SpendableCoins(ctx, account).AmountOf(k.denom)

	// Who gets the coins, in which shares: kept as the account's forward.
	var to types.Plan
	switch s.Claim.Kind {
	case KindRecovery:
		to.Heirs = []*types.Heir{{Address: s.Claim.NewAddress, Percent: 100}}
	case KindInheritance:
		to.Heirs = s.Plan.Heirs
	}
	payments, err := k.pay(ctx, account, &to, balance)
	if err != nil {
		return err
	}
	if len(to.Heirs) > 0 {
		if err := k.Forwards.Set(ctx, account, to); err != nil {
			return err
		}
	}
	k.releaseGuarantee(ctx, account)

	// The development fund's own account: its future fees go to the first
	// heir, or to the owner's new account after a recovery.
	if fund := *k.devFund; fund != nil && len(payments) > 0 {
		current, err := fund.DevFundAddress(ctx)
		if err != nil {
			return err
		}
		if current.Equals(account) {
			if err := fund.MoveDevFund(ctx, payments[0].to); err != nil {
				return err
			}
		}
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(s.Claim.Kind+"_executed",
		sdk.NewAttribute("account", account.String()),
		sdk.NewAttribute("amount", balance.String()),
	))
	s.Claim, s.Approvals = nil, nil
	return k.Statuses.Set(ctx, account, s)
}

type payment struct {
	to     sdk.AccAddress
	amount sdkmath.Int
}

// Limits of following heirs who were inherited or recovered themselves.
const (
	maxForwardDepth = 5
	maxDestinations = 100
	// maxHeirSteps: heirs looked at in one payment at most, however the
	// accounts are chained (the work of a block stays small).
	maxHeirSteps = 300
)

type destination struct {
	addr   sdk.AccAddress
	weight sdkmath.Int
}

// destinations: where the plan's coins really go. An heir (or new account)
// that was itself inherited or recovered passes its share on to its own
// heirs, and so on; a way that comes back to an account already on the path
// (two people who are each other's heir) is dropped and its share goes to
// the others, so coins never go round in circles. Weights multiply the
// percentages along the way.
func (k Keeper) destinations(ctx context.Context, from sdk.AccAddress, to *types.Plan) ([]destination, error) {
	var out []destination
	steps := 0
	var walk func(plan *types.Plan, weight sdkmath.Int, path []sdk.AccAddress, depth int) error
	walk = func(plan *types.Plan, weight sdkmath.Int, path []sdk.AccAddress, depth int) error {
		for _, h := range plan.Heirs {
			if steps++; steps > maxHeirSteps {
				return nil
			}
			addr, err := k.addressCodec.StringToBytes(h.Address)
			if err != nil {
				return err
			}
			if slices.ContainsFunc(path, func(a sdk.AccAddress) bool { return a.Equals(sdk.AccAddress(addr)) }) {
				continue
			}
			w := weight.MulRaw(int64(h.Percent))
			next, closed, err := k.forwardOf(ctx, addr)
			if err != nil {
				return err
			}
			if closed {
				if depth < maxForwardDepth {
					if err := walk(&next, w, append(slices.Clone(path), addr), depth+1); err != nil {
						return err
					}
				}
				continue
			}
			if i := slices.IndexFunc(out, func(d destination) bool { return d.addr.Equals(sdk.AccAddress(addr)) }); i >= 0 {
				out[i].weight = out[i].weight.Add(w)
			} else if len(out) < maxDestinations {
				out = append(out, destination{addr, w})
			}
		}
		return nil
	}
	if err := walk(to, sdkmath.OneInt(), []sdk.AccAddress{from}, 0); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, types.ErrNoLivingHeir
	}
	return out, nil
}

// forwardOf: where an inherited or recovered account passes coins on.
func (k Keeper) forwardOf(ctx context.Context, addr sdk.AccAddress) (types.Plan, bool, error) {
	p, err := k.Forwards.Get(ctx, addr)
	if errors.Is(err, collections.ErrNotFound) {
		return p, false, nil
	}
	return p, err == nil, err
}

// pay sends amount from account to the plan's heirs in their shares (the
// rounding remainder to the first one), following heirs who were inherited
// or recovered themselves (destinations).
func (k Keeper) pay(ctx context.Context, account sdk.AccAddress, to *types.Plan, amount sdkmath.Int) ([]payment, error) {
	dests, err := k.destinations(ctx, account, to)
	if err != nil {
		return nil, err
	}
	total := sdkmath.ZeroInt()
	for _, d := range dests {
		total = total.Add(d.weight)
	}
	var payments []payment
	paid := sdkmath.ZeroInt()
	for _, d := range dests {
		share := amount.Mul(d.weight).Quo(total)
		payments = append(payments, payment{d.addr, share})
		paid = paid.Add(share)
	}
	if len(payments) > 0 {
		payments[0].amount = payments[0].amount.Add(amount.Sub(paid))
	}
	for _, p := range payments {
		if !p.amount.IsPositive() {
			continue
		}
		if err := k.bank.SendCoins(ctx, account, p.to, sdk.NewCoins(sdk.NewCoin(k.denom, p.amount))); err != nil {
			return nil, err
		}
	}
	return payments, nil
}

// releaseGuarantee gives back the LockKeeper guarantee of an inherited or
// recovered account: it comes back to the account when the network's
// waiting time ends (21 days) and is then forwarded like the rest. It never
// stops the claim: what cannot be released is reported with an event.
func (k Keeper) releaseGuarantee(ctx context.Context, account sdk.AccAddress) {
	st := *k.staking
	if st == nil {
		return
	}
	dels, err := st.GetDelegatorDelegations(ctx, account, 50)
	for _, d := range dels {
		if err != nil {
			break
		}
		val, verr := sdk.ValAddressFromBech32(d.ValidatorAddress)
		if verr == nil {
			cache, write := sdk.UnwrapSDKContext(ctx).CacheContext()
			if _, _, verr = st.Undelegate(cache, account, val, d.Shares); verr == nil {
				write()
				continue
			}
		}
		err = verr
	}
	if err != nil {
		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("guarantee_not_released",
			sdk.NewAttribute("account", account.String()),
			sdk.NewAttribute("error", err.Error()),
		))
	}
}

// maxForwardChecks: forwards looked at in one block at most (in turns).
const maxForwardChecks = 50

// ForwardDue passes on what reached inherited or recovered accounts. It
// looks at a few accounts per block, in turns, so the work per block stays
// small however many there are.
func (k Keeper) ForwardDue(ctx context.Context) error {
	cursor, err := k.ForwardCursor.Get(ctx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	var rng collections.Ranger[sdk.AccAddress]
	if len(cursor) > 0 {
		rng = new(collections.Range[sdk.AccAddress]).StartExclusive(sdk.AccAddress(cursor))
	}
	iter, err := k.Forwards.Iterate(ctx, rng)
	if err != nil {
		return err
	}
	type entry struct {
		addr sdk.AccAddress
		plan types.Plan
	}
	var batch []entry
	for ; iter.Valid() && len(batch) < maxForwardChecks; iter.Next() {
		kv, err := iter.KeyValue()
		if err != nil {
			iter.Close()
			return err
		}
		batch = append(batch, entry{kv.Key, kv.Value})
	}
	iter.Close()
	for _, e := range batch {
		amount := k.bank.SpendableCoins(ctx, e.addr).AmountOf(k.denom)
		if !amount.IsPositive() {
			continue
		}
		cache, write := sdk.UnwrapSDKContext(ctx).CacheContext()
		if _, err := k.pay(cache, e.addr, &e.plan, amount); err != nil {
			continue // tried again in the next round
		}
		write()
		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("forwarded",
			sdk.NewAttribute("account", e.addr.String()),
			sdk.NewAttribute("amount", amount.String()),
		))
	}
	if len(batch) < maxForwardChecks {
		// Round finished: start again (nothing written when there was no
		// cursor, so blocks stay the same where nobody is forwarded).
		if len(cursor) == 0 {
			return nil
		}
		return k.ForwardCursor.Remove(ctx)
	}
	return k.ForwardCursor.Set(ctx, batch[len(batch)-1].addr)
}

// IsClosed tells whether the account was already inherited or recovered:
// such an account signs nothing any more (app/ante_hybrid.go).
func (k Keeper) IsClosed(ctx context.Context, addr sdk.AccAddress) (bool, error) {
	return k.Forwards.Has(ctx, addr)
}

// SetStaking connects the staking module (once, when the app starts).
func (k Keeper) SetStaking(s Staking) { *k.staking = s }

func (k Keeper) same(stored string, addr sdk.AccAddress) bool {
	bz, err := k.addressCodec.StringToBytes(stored)
	return err == nil && addr.Equals(sdk.AccAddress(bz))
}

// SetDevFund connects the development fund (once, when the app starts).
func (k Keeper) SetDevFund(f DevFund) { *k.devFund = f }
