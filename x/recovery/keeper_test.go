package recovery

import (
	"context"
	"errors"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authcodec "github.com/cosmos/cosmos-sdk/x/auth/codec"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"encryptlock/lockkeeper/x/recovery/types"
)

const denom = "lockunit"

type fakeBank struct{ balances map[string]int64 }

func (b *fakeBank) SpendableCoins(_ context.Context, addr sdk.AccAddress) sdk.Coins {
	return sdk.NewCoins(sdk.NewInt64Coin(denom, b.balances[string(addr)]))
}

func (b *fakeBank) SendCoins(_ context.Context, from, to sdk.AccAddress, amt sdk.Coins) error {
	n := amt.AmountOf(denom).Int64()
	b.balances[string(from)] -= n
	b.balances[string(to)] += n
	return nil
}

type fakeRegistry map[string]bool

func (r fakeRegistry) IsRegistered(_ context.Context, addr sdk.AccAddress) (bool, error) {
	return r[string(addr)], nil
}

var (
	owner    = sdk.AccAddress("owner_______________")
	son      = sdk.AccAddress("son_________________")
	daughter = sdk.AccAddress("daughter____________")
	wife     = sdk.AccAddress("wife________________")
	brother  = sdk.AccAddress("brother_____________")
	friend   = sdk.AccAddress("friend______________")
	newPhone = sdk.AccAddress("owner_new_account___")
	stranger = sdk.AccAddress("stranger____________")
	start    = time.Date(2026, time.October, 4, 12, 0, 0, 0, time.UTC)
	day      = 24 * time.Hour
)

type env struct {
	k    Keeper
	bank *fakeBank
	ctx  sdk.Context
}

func setup(t *testing.T) *env {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.ModuleName)
	ctx := testutil.DefaultContext(key, storetypes.NewTransientStoreKey("transient_test")).WithBlockTime(start)
	bank := &fakeBank{balances: map[string]int64{string(owner): 1_000}}
	registry := fakeRegistry{}
	for _, a := range []sdk.AccAddress{owner, son, daughter, wife, brother, friend, newPhone} {
		registry[string(a)] = true
	}
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	k := NewKeeper(runtime.NewKVStoreService(key), cdc, bank, registry, authcodec.NewBech32Codec("lock"), denom)
	return &env{k: k, bank: bank, ctx: ctx}
}

func (e *env) at(d time.Duration) { e.ctx = e.ctx.WithBlockTime(start.Add(d)) }

func (e *env) s(a sdk.AccAddress) string {
	str, _ := e.k.addressCodec.BytesToString(a)
	return str
}

func (e *env) heirsPlan() *types.Plan {
	return &types.Plan{
		Heirs:          []*types.Heir{{Address: e.s(son), Percent: 60}, {Address: e.s(daughter), Percent: 40}},
		InactivityDays: 730,
	}
}

func (e *env) guardiansPlan() *types.Plan {
	return &types.Plan{Guardians: []string{e.s(wife), e.s(brother), e.s(friend)}, GuardianThreshold: 2}
}

func (e *env) balance(t *testing.T, a sdk.AccAddress, want int64) {
	t.Helper()
	if got := e.bank.balances[string(a)]; got != want {
		t.Errorf("balance of %s = %d, want %d", a, got, want)
	}
}

func TestInvalidPlansAreRejected(t *testing.T) {
	e := setup(t)
	bad := map[string]*types.Plan{
		"heirs add up to 90%":     {Heirs: []*types.Heir{{Address: e.s(son), Percent: 90}}, InactivityDays: 730},
		"less than 1 year":        {Heirs: []*types.Heir{{Address: e.s(son), Percent: 100}}, InactivityDays: 300},
		"3 of 2 heirs confirm":    {Heirs: []*types.Heir{{Address: e.s(son), Percent: 50}, {Address: e.s(daughter), Percent: 50}}, InactivityDays: 730, HeirThreshold: 3},
		"only 1 trusted person":   {Guardians: []string{e.s(wife)}, GuardianThreshold: 1},
		"1 approval is enough":    {Guardians: []string{e.s(wife), e.s(brother)}, GuardianThreshold: 1},
		"unregistered heir":       {Heirs: []*types.Heir{{Address: e.s(stranger), Percent: 100}}, InactivityDays: 730},
		"owner as its own heir":   {Heirs: []*types.Heir{{Address: e.s(owner), Percent: 100}}, InactivityDays: 730},
		"same trusted person x2":  {Guardians: []string{e.s(wife), e.s(wife)}, GuardianThreshold: 2},
		"more approvals than ppl": {Guardians: []string{e.s(wife), e.s(brother)}, GuardianThreshold: 3},
	}
	for name, plan := range bad {
		if err := e.k.SetPlan(e.ctx, owner, plan); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestInheritanceAfterInactivityAnd30Days(t *testing.T) {
	e := setup(t)
	if err := e.k.SetPlan(e.ctx, owner, e.heirsPlan()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.k.ClaimInheritance(e.ctx, stranger, owner); !errors.Is(err, types.ErrNotHeir) {
		t.Fatalf("a stranger claimed: err = %v", err)
	}

	e.at(729 * day)
	if _, err := e.k.ClaimInheritance(e.ctx, son, owner); !errors.Is(err, types.ErrStillActive) {
		t.Fatalf("claimed before 2 years: err = %v", err)
	}

	e.at(730 * day)
	if at, err := e.k.ClaimInheritance(e.ctx, son, owner); err != nil || at != 0 {
		t.Fatalf("one heir of two started the claim alone: %d, %v", at, err)
	}
	if _, err := e.k.ClaimInheritance(e.ctx, daughter, owner); err != nil {
		t.Fatal(err)
	}
	e.at(730*day + 29*day)
	if err := e.k.ExecuteDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	e.balance(t, owner, 1_000) // not yet

	e.at(730*day + 30*day)
	if err := e.k.ExecuteDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	e.balance(t, owner, 0)
	e.balance(t, son, 600)
	e.balance(t, daughter, 400)
}

func TestSignOfLifeCancelsInheritance(t *testing.T) {
	e := setup(t)
	if err := e.k.SetPlan(e.ctx, owner, e.heirsPlan()); err != nil {
		t.Fatal(err)
	}
	e.at(800 * day)
	if _, err := e.k.ClaimInheritance(e.ctx, daughter, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := e.k.ClaimInheritance(e.ctx, son, owner); err != nil {
		t.Fatal(err)
	}
	// The owner was not dead (e.g. in hospital): any transaction cancels it.
	e.at(810 * day)
	if err := e.k.RecordActivity(e.ctx, owner); err != nil {
		t.Fatal(err)
	}
	e.at(900 * day)
	if err := e.k.ExecuteDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	e.balance(t, owner, 1_000)
	// And the 2 years start again.
	if _, err := e.k.ClaimInheritance(e.ctx, daughter, owner); !errors.Is(err, types.ErrStillActive) {
		t.Fatalf("claimed again right after a sign of life: err = %v", err)
	}
}

func TestInheritanceRoundingGoesToFirstHeir(t *testing.T) {
	e := setup(t)
	e.bank.balances[string(owner)] = 1_001
	plan := &types.Plan{
		Heirs:          []*types.Heir{{Address: e.s(son), Percent: 50}, {Address: e.s(daughter), Percent: 50}},
		InactivityDays: 730,
	}
	if err := e.k.SetPlan(e.ctx, owner, plan); err != nil {
		t.Fatal(err)
	}
	e.at(730 * day)
	for _, heir := range []sdk.AccAddress{son, daughter} {
		if _, err := e.k.ClaimInheritance(e.ctx, heir, owner); err != nil {
			t.Fatal(err)
		}
	}
	e.at(760 * day)
	if err := e.k.ExecuteDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	e.balance(t, son, 501)
	e.balance(t, daughter, 500)
	e.balance(t, owner, 0)
}

func TestRecoveryByTwoOfThreeTrustedPeople(t *testing.T) {
	e := setup(t)
	if err := e.k.SetPlan(e.ctx, owner, e.guardiansPlan()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.k.ApproveRecovery(e.ctx, stranger, owner, newPhone); !errors.Is(err, types.ErrNotGuardian) {
		t.Fatalf("a stranger approved: err = %v", err)
	}

	n, at, err := e.k.ApproveRecovery(e.ctx, wife, owner, newPhone)
	if err != nil || n != 1 || at != 0 {
		t.Fatalf("first approval: %d, %d, %v", n, at, err)
	}
	// The second trusted person names a different account: no agreement.
	n, at, _ = e.k.ApproveRecovery(e.ctx, brother, owner, son)
	if n != 1 || at != 0 {
		t.Fatalf("two different new accounts started a recovery")
	}
	n, at, err = e.k.ApproveRecovery(e.ctx, brother, owner, newPhone)
	if err != nil || n != 2 || at == 0 {
		t.Fatalf("second approval: %d, %d, %v", n, at, err)
	}

	// A transaction of the account (e.g. a thief with the phone) does not
	// stop a recovery; only an explicit cancel does.
	if err := e.k.RecordActivity(e.ctx, owner); err != nil {
		t.Fatal(err)
	}
	e.at(30 * day)
	if err := e.k.ExecuteDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	e.balance(t, owner, 0)
	e.balance(t, newPhone, 1_000)
}

func TestOwnerCancelsARecoveryHeDidNotAskFor(t *testing.T) {
	e := setup(t)
	if err := e.k.SetPlan(e.ctx, owner, e.guardiansPlan()); err != nil {
		t.Fatal(err)
	}
	// Two trusted people plot to take the coins.
	if _, _, err := e.k.ApproveRecovery(e.ctx, wife, owner, son); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.k.ApproveRecovery(e.ctx, brother, owner, son); err != nil {
		t.Fatal(err)
	}
	e.at(20 * day)
	if err := e.k.CancelClaims(e.ctx, owner); err != nil {
		t.Fatal(err)
	}
	e.at(40 * day)
	if err := e.k.ExecuteDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	e.balance(t, owner, 1_000)
	e.balance(t, son, 0)
}

func TestChangingThePlanTakes7DaysAndCanBeCanceled(t *testing.T) {
	e := setup(t)
	if err := e.k.SetPlan(e.ctx, owner, e.heirsPlan()); err != nil {
		t.Fatal(err)
	}
	// A thief with the phone tries to make a friend the only heir.
	thiefPlan := &types.Plan{Heirs: []*types.Heir{{Address: e.s(friend), Percent: 100}}, InactivityDays: 730}
	if err := e.k.SetPlan(e.ctx, owner, thiefPlan); err != nil {
		t.Fatal(err)
	}
	s, _, _ := e.k.GetStatus(e.ctx, owner)
	if s.Plan.Heirs[0].Address != e.s(son) {
		t.Fatal("plan changed at once")
	}
	e.at(6 * day)
	if err := e.k.CancelClaims(e.ctx, owner); err != nil {
		t.Fatal(err)
	}
	e.at(8 * day)
	s, _, _ = e.k.GetStatus(e.ctx, owner)
	if s.Plan.Heirs[0].Address != e.s(son) || s.PlanChangePending {
		t.Fatal("canceled plan change still applied")
	}

	// Without cancel, it applies after 7 days.
	if err := e.k.SetPlan(e.ctx, owner, thiefPlan); err != nil {
		t.Fatal(err)
	}
	e.at(15 * day)
	s, _, _ = e.k.GetStatus(e.ctx, owner)
	if s.Plan.Heirs[0].Address != e.s(friend) {
		t.Fatal("plan change not applied after 7 days")
	}
}

func TestAccountsWithoutPlanAreNotTracked(t *testing.T) {
	e := setup(t)
	if err := e.k.RecordActivity(e.ctx, stranger); err != nil {
		t.Fatal(err)
	}
	if has, _ := e.k.Statuses.Has(e.ctx, stranger); has {
		t.Fatal("an account without a plan was stored")
	}
	if _, err := e.k.ClaimInheritance(e.ctx, son, stranger); !errors.Is(err, types.ErrNoPlan) {
		t.Fatalf("claim on an account without a plan: err = %v", err)
	}
}

func TestHeirConfirmationsLapseAfter30Days(t *testing.T) {
	e := setup(t)
	if err := e.k.SetPlan(e.ctx, owner, e.heirsPlan()); err != nil {
		t.Fatal(err)
	}
	e.at(730 * day)
	if _, err := e.k.ClaimInheritance(e.ctx, son, owner); err != nil {
		t.Fatal(err)
	}
	// The daughter only confirms 31 days later: the son's confirmation lapsed.
	e.at(761 * day)
	if at, err := e.k.ClaimInheritance(e.ctx, daughter, owner); err != nil || at != 0 {
		t.Fatalf("claim started with a lapsed confirmation: %d, %v", at, err)
	}
	// The son confirms again within 30 days of the daughter: it starts.
	e.at(770 * day)
	if at, err := e.k.ClaimInheritance(e.ctx, son, owner); err != nil || at == 0 {
		t.Fatalf("claim did not start: %d, %v", at, err)
	}
}

func TestOwnerCanChooseFewerHeirsToConfirm(t *testing.T) {
	e := setup(t)
	plan := e.heirsPlan()
	plan.HeirThreshold = 1 // e.g. in case an heir is no longer reachable
	if err := e.k.SetPlan(e.ctx, owner, plan); err != nil {
		t.Fatal(err)
	}
	e.at(730 * day)
	if at, err := e.k.ClaimInheritance(e.ctx, daughter, owner); err != nil || at == 0 {
		t.Fatalf("1 of 2 heirs did not start the claim: %d, %v", at, err)
	}
	e.at(760 * day)
	if err := e.k.ExecuteDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	// The coins still go to both heirs in their percentages.
	e.balance(t, son, 600)
	e.balance(t, daughter, 400)
}

func TestOneYearIsTheMinimum(t *testing.T) {
	e := setup(t)
	plan := &types.Plan{Heirs: []*types.Heir{{Address: e.s(son), Percent: 100}}, InactivityDays: 365}
	if err := e.k.SetPlan(e.ctx, owner, plan); err != nil {
		t.Fatalf("1 year rejected: %v", err)
	}
}

// The owner removes someone from the trusted people: that person's approval
// no longer counts.
func TestApprovalOfARemovedTrustedPersonDoesNotCount(t *testing.T) {
	e := setup(t)
	if err := e.k.SetPlan(e.ctx, owner, e.guardiansPlan()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.k.ApproveRecovery(e.ctx, wife, owner, newPhone); err != nil {
		t.Fatal(err)
	}
	// Wife is replaced by son (takes 7 days).
	plan := &types.Plan{Guardians: []string{e.s(son), e.s(brother), e.s(friend)}, GuardianThreshold: 2}
	if err := e.k.SetPlan(e.ctx, owner, plan); err != nil {
		t.Fatal(err)
	}
	e.at(8 * 24 * time.Hour)
	n, at, err := e.k.ApproveRecovery(e.ctx, brother, owner, newPhone)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || at != 0 {
		t.Fatalf("approvals = %d (claim at %d): the removed person's approval was counted", n, at)
	}
}

// fakeFund is the development fund's address (x/transfers).
type fakeFund struct{ addr sdk.AccAddress }

func (f *fakeFund) DevFundAddress(context.Context) (sdk.AccAddress, error) { return f.addr, nil }
func (f *fakeFund) MoveDevFund(_ context.Context, to sdk.AccAddress) error {
	f.addr = to
	return nil
}

func TestInheritingTheDevelopmentFundMovesItsFutureFeesToTheFirstHeir(t *testing.T) {
	e := setup(t)
	fund := &fakeFund{addr: owner}
	e.k.SetDevFund(fund)
	if err := e.k.SetPlan(e.ctx, owner, e.heirsPlan()); err != nil {
		t.Fatal(err)
	}
	e.at(730 * day)
	for _, h := range []sdk.AccAddress{son, daughter} {
		if _, err := e.k.ClaimInheritance(e.ctx, h, owner); err != nil {
			t.Fatal(err)
		}
	}
	e.at(760 * day)
	if err := e.k.ExecuteDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	if !fund.addr.Equals(son) {
		t.Fatalf("development fund is now %s, want the first heir", fund.addr)
	}
}

func TestRecoveringTheDevelopmentFundMovesItToTheNewAccount(t *testing.T) {
	e := setup(t)
	fund := &fakeFund{addr: owner}
	e.k.SetDevFund(fund)
	if err := e.k.SetPlan(e.ctx, owner, e.guardiansPlan()); err != nil {
		t.Fatal(err)
	}
	for _, g := range []sdk.AccAddress{wife, brother} {
		if _, _, err := e.k.ApproveRecovery(e.ctx, g, owner, newPhone); err != nil {
			t.Fatal(err)
		}
	}
	e.at(30 * day)
	if err := e.k.ExecuteDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	if !fund.addr.Equals(newPhone) {
		t.Fatalf("development fund is now %s, want the new account", fund.addr)
	}
}

func TestInheritingAnotherAccountLeavesTheFundAlone(t *testing.T) {
	e := setup(t)
	fund := &fakeFund{addr: friend}
	e.k.SetDevFund(fund)
	if err := e.k.SetPlan(e.ctx, owner, e.heirsPlan()); err != nil {
		t.Fatal(err)
	}
	e.at(730 * day)
	for _, h := range []sdk.AccAddress{son, daughter} {
		if _, err := e.k.ClaimInheritance(e.ctx, h, owner); err != nil {
			t.Fatal(err)
		}
	}
	e.at(760 * day)
	if err := e.k.ExecuteDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	if !fund.addr.Equals(friend) {
		t.Fatal("the fund moved when another account was inherited")
	}
}

type fakeStaking struct {
	dels        map[string][]stakingtypes.Delegation
	undelegated map[string]bool
}

func (s *fakeStaking) GetDelegatorDelegations(_ context.Context, d sdk.AccAddress, _ uint16) ([]stakingtypes.Delegation, error) {
	return s.dels[string(d)], nil
}

func (s *fakeStaking) Undelegate(_ context.Context, d sdk.AccAddress, v sdk.ValAddress, _ sdkmath.LegacyDec) (time.Time, sdkmath.Int, error) {
	s.undelegated[string(d)+"/"+v.String()] = true
	return time.Time{}, sdkmath.ZeroInt(), nil
}

// The owner was a LockKeeper with 200 in guarantee: inheritance gives the
// guarantee back, and when it returns to the account (after the network's
// waiting time) it goes on to the heirs in the same shares - as does
// anything else that reaches the account later.
func TestInheritedLockKeeperGuaranteeGoesToTheHeirs(t *testing.T) {
	e := setup(t)
	val := sdk.ValAddress("pc_lockkeeper_______")
	st := &fakeStaking{
		dels:        map[string][]stakingtypes.Delegation{string(owner): {{ValidatorAddress: val.String(), Shares: sdkmath.LegacyNewDec(200)}}},
		undelegated: map[string]bool{},
	}
	e.k.SetStaking(st)
	if err := e.k.SetPlan(e.ctx, owner, e.heirsPlan()); err != nil {
		t.Fatal(err)
	}
	e.at(730 * day)
	for _, h := range []sdk.AccAddress{son, daughter} {
		if _, err := e.k.ClaimInheritance(e.ctx, h, owner); err != nil {
			t.Fatal(err)
		}
	}
	e.at(760 * day)
	if err := e.k.ExecuteDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	if !st.undelegated[string(owner)+"/"+val.String()] {
		t.Fatal("the guarantee was not given back")
	}
	e.balance(t, son, 600)
	// 21 days later the guarantee is back in the account.
	e.bank.balances[string(owner)] += 200
	if err := e.k.ForwardDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	e.balance(t, owner, 0)
	e.balance(t, son, 720)
	e.balance(t, daughter, 480)
}

// After a recovery, coins that come back to the lost account later (a lock
// or an offer that returns) go to the owner's new account.
func TestRecoveredAccountForwardsWhatComesBackLater(t *testing.T) {
	e := setup(t)
	if err := e.k.SetPlan(e.ctx, owner, e.guardiansPlan()); err != nil {
		t.Fatal(err)
	}
	for _, g := range []sdk.AccAddress{wife, brother} {
		if _, _, err := e.k.ApproveRecovery(e.ctx, g, owner, newPhone); err != nil {
			t.Fatal(err)
		}
	}
	e.at(30 * day)
	if err := e.k.ExecuteDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	e.balance(t, newPhone, 1_000)
	e.bank.balances[string(owner)] += 50
	if err := e.k.ForwardDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	e.balance(t, owner, 0)
	e.balance(t, newPhone, 1_050)
}

// Many forwarded accounts are looked at a few per block, in turns: every
// one is reached and the round starts again.
func TestForwardsAreCheckedInTurns(t *testing.T) {
	e := setup(t)
	to := types.Plan{Heirs: []*types.Heir{{Address: e.s(son), Percent: 100}}}
	const n = 2*maxForwardChecks + 10
	for i := 0; i < n; i++ {
		a := sdk.AccAddress([]byte{byte(i), 'x', 'x', 'x', 'x', 'x', 'x', 'x', 'x', 'x', 'x', 'x', 'x', 'x', 'x', 'x', 'x', 'x', 'x', 'x'})
		e.bank.balances[string(a)] = 1
		if err := e.k.Forwards.Set(e.ctx, a, to); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		if err := e.k.ForwardDue(e.ctx); err != nil {
			t.Fatal(err)
		}
	}
	e.balance(t, son, n)
	if _, err := e.k.ForwardCursor.Get(e.ctx); err == nil {
		t.Fatal("the round did not start again")
	}
}

func TestInheritedAccountIsClosed(t *testing.T) {
	e := setup(t)
	if closed, _ := e.k.IsClosed(e.ctx, owner); closed {
		t.Fatal("closed before anything happened")
	}
	if err := e.k.SetPlan(e.ctx, owner, e.heirsPlan()); err != nil {
		t.Fatal(err)
	}
	e.at(730 * day)
	for _, h := range []sdk.AccAddress{son, daughter} {
		if _, err := e.k.ClaimInheritance(e.ctx, h, owner); err != nil {
			t.Fatal(err)
		}
	}
	e.at(760 * day)
	if err := e.k.ExecuteDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	if closed, _ := e.k.IsClosed(e.ctx, owner); !closed {
		t.Fatal("the inherited account can still sign")
	}
	if closed, _ := e.k.IsClosed(e.ctx, son); closed {
		t.Fatal("an heir's account was closed")
	}
}

// inherit runs owner's inheritance: every heir confirms after the waiting
// time, and 30 days later the coins move.
func (e *env) inherit(t *testing.T, account sdk.AccAddress, heirs ...sdk.AccAddress) {
	t.Helper()
	e.at(e.ctx.BlockTime().Sub(start) + 730*day)
	for _, h := range heirs {
		if _, err := e.k.ClaimInheritance(e.ctx, h, account); err != nil {
			t.Fatal(err)
		}
	}
	e.at(e.ctx.BlockTime().Sub(start) + 30*day)
	if err := e.k.ExecuteDue(e.ctx); err != nil {
		t.Fatal(err)
	}
}

// Two people who are each other's heir: once one is inherited, the other's
// coins never go round in circles between the two accounts.
func TestHeirsOfEachOtherNeverSendCoinsInCircles(t *testing.T) {
	e := setup(t)
	e.bank.balances[string(wife)] = 500
	both := func(heir sdk.AccAddress, other sdk.AccAddress, otherPct uint32) *types.Plan {
		p := &types.Plan{Heirs: []*types.Heir{{Address: e.s(heir), Percent: 100 - otherPct}}, InactivityDays: 730}
		if otherPct > 0 {
			p.Heirs = append(p.Heirs, &types.Heir{Address: e.s(other), Percent: otherPct})
		}
		return p
	}
	// The owner leaves everything to his wife; she leaves half to him and half to the son.
	if err := e.k.SetPlan(e.ctx, owner, both(wife, nil, 0)); err != nil {
		t.Fatal(err)
	}
	if err := e.k.SetPlan(e.ctx, wife, both(son, owner, 50)); err != nil {
		t.Fatal(err)
	}
	e.inherit(t, owner, wife)
	e.balance(t, wife, 1_500)
	// The owner's account is closed: it cannot be anybody's heir any more.
	if err := e.k.SetPlan(e.ctx, brother, both(owner, nil, 0)); !errors.Is(err, types.ErrClosed) {
		t.Fatalf("a closed account as heir: %v", err)
	}
	// The wife's inheritance: the son alone confirms (the owner cannot sign),
	// and the owner's half would come straight back to her: it goes to the son.
	e.inherit(t, wife, son)
	e.balance(t, wife, 0)
	e.balance(t, owner, 0)
	e.balance(t, son, 1_500)
	// Coins reaching either closed account later end with the son too.
	e.bank.balances[string(owner)] += 10
	e.bank.balances[string(wife)] += 20
	for i := 0; i < 3; i++ {
		if err := e.k.ForwardDue(e.ctx); err != nil {
			t.Fatal(err)
		}
	}
	e.balance(t, owner, 0)
	e.balance(t, wife, 0)
	e.balance(t, son, 1_530)
}

// Only heir already inherited, whose coins come back: nobody can receive, so
// the claim fails and the coins stay put (they do not go round in circles).
func TestNoLivingHeirLeavesTheCoinsWhereTheyAre(t *testing.T) {
	e := setup(t)
	e.bank.balances[string(wife)] = 500
	if err := e.k.SetPlan(e.ctx, owner, &types.Plan{Heirs: []*types.Heir{{Address: e.s(wife), Percent: 100}}, InactivityDays: 730}); err != nil {
		t.Fatal(err)
	}
	if err := e.k.SetPlan(e.ctx, wife, &types.Plan{Heirs: []*types.Heir{{Address: e.s(owner), Percent: 100}}, InactivityDays: 730}); err != nil {
		t.Fatal(err)
	}
	e.inherit(t, owner, wife)
	e.balance(t, wife, 1_500)
	// The wife's only heir can sign nothing (app/ante_hybrid.go), so nobody
	// can even ask; and if a claim ran, the coins would stay with the wife.
	if _, err := e.k.pay(e.ctx, wife, &types.Plan{Heirs: []*types.Heir{{Address: e.s(owner), Percent: 100}}}, sdkmath.NewInt(1_500)); !errors.Is(err, types.ErrNoLivingHeir) {
		t.Fatalf("paying to a circle: %v", err)
	}
	e.balance(t, wife, 1_500)
}

// Trusted people cannot recover an account into one already closed.
func TestRecoveryIntoAClosedAccountIsRefused(t *testing.T) {
	e := setup(t)
	if err := e.k.SetPlan(e.ctx, owner, e.guardiansPlan()); err != nil {
		t.Fatal(err)
	}
	for _, g := range []sdk.AccAddress{wife, brother} {
		if _, _, err := e.k.ApproveRecovery(e.ctx, g, owner, newPhone); err != nil {
			t.Fatal(err)
		}
	}
	e.at(30 * day)
	if err := e.k.ExecuteDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	// The new account's trusted people try to move it back into the old one.
	if err := e.k.SetPlan(e.ctx, newPhone, e.guardiansPlan()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.k.ApproveRecovery(e.ctx, wife, newPhone, owner); !errors.Is(err, types.ErrClosed) {
		t.Fatalf("recovery into a closed account: %v", err)
	}
	e.balance(t, newPhone, 1_000)
}

// A long chain of closed accounts, each passing everything on to 10 more,
// costs a block only a bounded amount of work.
func TestChainsOfClosedAccountsAreBounded(t *testing.T) {
	e := setup(t)
	acct := func(layer, i int) sdk.AccAddress {
		return sdk.AccAddress([]byte{'c', byte(layer), byte(i), 'x', 'x', 'x', 'x', 'x', 'x', 'x', 'x', 'x', 'x', 'x', 'x', 'x', 'x', 'x', 'x', 'x'})
	}
	for layer := 0; layer < 8; layer++ {
		for i := 0; i < 10; i++ {
			var heirs []*types.Heir
			for j := 0; j < 10; j++ {
				heirs = append(heirs, &types.Heir{Address: e.s(acct(layer+1, j)), Percent: 10})
			}
			if err := e.k.Forwards.Set(e.ctx, acct(layer, i), types.Plan{Heirs: heirs}); err != nil {
				t.Fatal(err)
			}
		}
	}
	dests, err := e.k.destinations(e.ctx, owner, &types.Plan{Heirs: []*types.Heir{{Address: e.s(acct(0, 0)), Percent: 100}}})
	if err != nil && !errors.Is(err, types.ErrNoLivingHeir) {
		t.Fatal(err)
	}
	if len(dests) > maxDestinations {
		t.Fatalf("%d destinations", len(dests))
	}
}
