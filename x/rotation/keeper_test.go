package rotation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authcodec "github.com/cosmos/cosmos-sdk/x/auth/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"encryptlock/lockkeeper/x/rotation/types"
)

const denom = "lockunit"

type fakeStaking struct {
	vals map[string]stakingtypes.Validator
}

func (s fakeStaking) GetValidator(_ context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error) {
	v, ok := s.vals[string(addr)]
	if !ok {
		return v, stakingtypes.ErrNoValidatorFound
	}
	return v, nil
}
func (fakeStaking) PowerReduction(context.Context) sdkmath.Int { return sdkmath.NewInt(1_000_000) }

type fakeBank struct{ balances map[string]int64 }

func (b *fakeBank) GetBalance(_ context.Context, addr sdk.AccAddress, d string) sdk.Coin {
	return sdk.NewInt64Coin(d, b.balances[string(addr)])
}
func (b *fakeBank) SendCoinsFromModuleToModule(_ context.Context, from, to string, amt sdk.Coins) error {
	n := amt.AmountOf(denom).Int64()
	f, t := string(authtypes.NewModuleAddress(from)), string(authtypes.NewModuleAddress(to))
	if b.balances[f] < n {
		return errors.New("insufficient funds")
	}
	b.balances[f] -= n
	b.balances[t] += n
	return nil
}

// fakeSlashing takes a LockKeeper back, like the real Unjail after the wait.
type fakeSlashing struct {
	stake   fakeStaking
	tooSoon *bool
}

func (s fakeSlashing) Unjail(_ context.Context, addr sdk.ValAddress) error {
	if *s.tooSoon {
		return errors.New("too soon")
	}
	v := s.stake.vals[string(addr)]
	v.Jailed = false
	s.stake.vals[string(addr)] = v
	return nil
}

type fakeDistr struct{ paid map[string]int64 }

func (d fakeDistr) AllocateTokensToValidator(_ context.Context, val stakingtypes.ValidatorI, tokens sdk.DecCoins) error {
	d.paid[val.GetOperator()] += tokens.AmountOf(denom).TruncateInt64()
	return nil
}

var start = time.Date(2026, time.October, 6, 12, 0, 30, 0, time.UTC)

type env struct {
	k       Keeper
	ctx     sdk.Context
	stake   fakeStaking
	bank    *fakeBank
	distr   fakeDistr
	tooSoon *bool
	accs    []sdk.AccAddress
}

// setup creates n LockKeepers; LockKeeper i has power powers[i].
func setup(t *testing.T, powers ...int64) *env {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.ModuleName)
	ctx := testutil.DefaultContext(key, storetypes.NewTransientStoreKey("transient_test")).WithBlockTime(start)
	reg := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(reg)
	e := &env{
		ctx:     ctx,
		stake:   fakeStaking{vals: map[string]stakingtypes.Validator{}},
		bank:    &fakeBank{balances: map[string]int64{}},
		distr:   fakeDistr{paid: map[string]int64{}},
		tooSoon: new(bool),
	}
	for i, p := range powers {
		acc := sdk.AccAddress(fmt.Sprintf("keeper-%02d___________", i)[:20])
		val, err := stakingtypes.NewValidator(sdk.ValAddress(acc).String(), ed25519.GenPrivKeyFromSecret(acc).PubKey(), stakingtypes.Description{})
		if err != nil {
			t.Fatal(err)
		}
		val.Tokens = sdkmath.NewInt(p * 1_000_000) // p steps of 0,01 LOCK
		val.Status = stakingtypes.Bonded
		e.stake.vals[string(sdk.ValAddress(acc))] = val
		e.accs = append(e.accs, acc)
	}
	e.k = NewKeeper(runtime.NewKVStoreService(key), cdc, e.stake, e.bank, e.distr, fakeSlashing{stake: e.stake, tooSoon: e.tooSoon}, authcodec.NewBech32Codec("cosmos"), denom, authtypes.FeeCollectorName)
	return e
}

func (e *env) at(d time.Duration) { e.ctx = e.ctx.WithBlockTime(start.Add(d)) }

func (e *env) beat(t *testing.T, i int) {
	t.Helper()
	if err := e.k.Heartbeat(e.ctx, e.accs[i]); err != nil {
		t.Fatalf("heartbeat of %d: %v", i, err)
	}
}

func (e *env) weight(i int) int64 {
	w, err := e.k.Weights.Get(e.ctx, sdk.ValAddress(e.accs[i]))
	if errors.Is(err, collections.ErrNotFound) {
		return 0
	}
	return w
}

func TestHeartbeatCountsTimeOn(t *testing.T) {
	e := setup(t, 10)
	e.beat(t, 0)
	if e.weight(0) != 0 {
		t.Fatal("the first heartbeat must not count any time")
	}
	e.at(5 * time.Minute)
	e.beat(t, 0)
	if got := e.weight(0); got != 10*300 {
		t.Fatalf("weight = %d, want 10 × 300 s", got)
	}
	// Too soon.
	e.at(7 * time.Minute)
	if err := e.k.Heartbeat(e.ctx, e.accs[0]); !errors.Is(err, types.ErrTooSoon) {
		t.Fatalf("heartbeat 2 minutes later: %v, want ErrTooSoon", err)
	}
	// Off for an hour: that hour does not count.
	e.at(65 * time.Minute)
	e.beat(t, 0)
	if got := e.weight(0); got != 10*300 {
		t.Fatalf("weight after being off = %d, want unchanged %d", got, 10*300)
	}
	// Not a LockKeeper.
	if err := e.k.Heartbeat(e.ctx, sdk.AccAddress("nobody______________")); !errors.Is(err, types.ErrNotLockKeeper) {
		t.Fatalf("heartbeat of a non-LockKeeper: %v", err)
	}
}

func TestEveryoneOnIsPaidByTimeAndGuarantee(t *testing.T) {
	e := setup(t, 10, 30, 10)
	if err := e.k.BeginBlock(e.ctx); err != nil { // starts the pay hour
		t.Fatal(err)
	}
	for _, i := range []int{0, 1, 2} {
		e.beat(t, i)
	}
	e.at(5 * time.Minute)
	e.beat(t, 0)
	e.beat(t, 1)
	e.at(10 * time.Minute)
	e.beat(t, 0)
	e.beat(t, 1)
	e.beat(t, 2)
	// 0: 10 × 600 s, 1: 30 × 600 s, 2: 10 × 600 s (one heartbeat skipped but
	// still within 10 minutes) → 1 : 3 : 1.
	e.bank.balances[string(authtypes.NewModuleAddress(authtypes.FeeCollectorName))] = 5_000
	e.at(60 * time.Minute)
	if err := e.k.BeginBlock(e.ctx); err != nil {
		t.Fatal(err)
	}
	want := []int64{1_000, 3_000, 1_000}
	for i, w := range want {
		op := e.stake.vals[string(sdk.ValAddress(e.accs[i]))].GetOperator()
		if got := e.distr.paid[op]; got != w {
			t.Errorf("LockKeeper %d paid %d, want %d", i, got, w)
		}
	}
	if got := e.bank.balances[string(authtypes.NewModuleAddress(distrtypes.ModuleName))]; got != 5_000 {
		t.Errorf("distribution received %d, want 5000", got)
	}
	if e.weight(1) != 0 {
		t.Error("time on must start from zero after the payout")
	}
}

func (e *env) selectNow(t *testing.T) map[int]bool {
	t.Helper()
	vals := make([]stakingtypes.Validator, len(e.accs))
	for i, a := range e.accs {
		vals[i] = e.stake.vals[string(sdk.ValAddress(a))]
	}
	out, err := e.k.Select(e.ctx, vals, func(v stakingtypes.Validator) int64 { return v.Tokens.Int64() })
	if err != nil {
		t.Fatal(err)
	}
	seated := map[int]bool{}
	for _, v := range out {
		for i, a := range e.accs {
			if v.GetOperator() == sdk.ValAddress(a).String() {
				seated[i] = true
			}
		}
	}
	return seated
}

func TestEveryoneSignsWhileThereAreSeats(t *testing.T) {
	e := setup(t, 10, 10, 10)
	if got := e.selectNow(t); len(got) != 3 {
		t.Fatalf("before any heartbeat everyone signs (network starting), got %v", got)
	}
	e.beat(t, 0)
	e.beat(t, 1)
	if got := e.selectNow(t); len(got) != 2 || got[2] {
		t.Fatalf("only the 2 on may sign, got %v", got)
	}
}

// beatEvery5 makes LockKeepers who say "I am on" every 5 minutes from
// minute from to minute to (inclusive).
func (e *env) beatEvery5(t *testing.T, who []int, from, to int) {
	t.Helper()
	for m := from; m <= to; m += 5 {
		e.at(time.Duration(m) * time.Minute)
		for _, i := range who {
			e.beat(t, i)
		}
	}
}

func span(from, to int) []int {
	var s []int
	for i := from; i < to; i++ {
		s = append(s, i)
	}
	return s
}

func TestDrawnGroupIsStableRotatesSlowlyAndReplacesWhoSwitchesOff(t *testing.T) {
	powers := make([]int64, 30)
	for i := range powers {
		powers[i] = 10
	}
	powers[29] = 100_000 // a big guarantee buys no extra chance to sign
	e := setup(t, powers...)
	e.k.Seats, e.k.MaxSeats = 8, 8
	// 0-11 and 29 on since minute 0; 12-28 only switched on at minute 60.
	stable := append(span(0, 12), 29)
	e.beatEvery5(t, stable, 0, 55)
	e.beatEvery5(t, span(0, 30), 60, 65)
	first := e.selectNow(t)
	if len(first) != 8 {
		t.Fatalf("group = %v, want 8 seats", first)
	}
	for i := range first {
		if i >= 12 && i < 29 {
			t.Fatalf("group = %v: %d is on for less than an hour", first, i)
		}
	}

	// A member switches off; the others keep beating. Past the 10-minute
	// window it is replaced at once by another stable one.
	var off int
	for i := range first {
		off = i
		break
	}
	var rest []int
	for i := range 30 {
		if i != off {
			rest = append(rest, i)
		}
	}
	e.beatEvery5(t, rest, 70, 80)
	second := e.selectNow(t)
	if second[off] || len(second) != 8 {
		t.Fatalf("group = %v: %d is off and must be replaced", second, off)
	}
	for i := range second {
		if i >= 12 && i < 29 {
			t.Fatalf("group = %v: replacement %d is not stable", second, i)
		}
	}

	// Every 6 minutes 1 seat (1% of 8, at least one) changes hands, the
	// longest-seated first, while stable ones wait outside.
	e.beatEvery5(t, rest, 85, 85)
	third := e.selectNow(t)
	changed := 0
	for i := range second {
		if !third[i] {
			changed++
		}
	}
	if len(third) != 8 || changed != 1 {
		t.Fatalf("rotation changed %d seats (%v → %v), want 1", changed, second, third)
	}

	// Two hours on: everyone is stable and gets a turn over time.
	everSeated := map[int]bool{}
	for m := 90; m <= 600; m += 5 {
		e.beatEvery5(t, rest, m, m)
		for i := range e.selectNow(t) {
			everSeated[i] = true
		}
	}
	if len(everSeated) < 25 {
		t.Fatalf("only %d of 29 LockKeepers on got a seat in 8 hours", len(everSeated))
	}
}

func TestNoRotationWithoutStableOnesOutside(t *testing.T) {
	powers := make([]int64, 12)
	for i := range powers {
		powers[i] = 10
	}
	e := setup(t, powers...)
	e.k.Seats, e.k.MaxSeats = 8, 8
	e.beatEvery5(t, span(0, 8), 0, 60)
	e.beatEvery5(t, span(0, 12), 65, 65) // 8-11 just switched on
	first := e.selectNow(t)
	e.beatEvery5(t, span(0, 12), 70, 70)
	second := e.selectNow(t)
	for i := range first {
		if !second[i] {
			t.Fatalf("group changed %v → %v with nobody stable outside", first, second)
		}
	}
}

func TestGroupGrowsWithTheNetwork(t *testing.T) {
	k := Keeper{Seats: 100, MaxSeats: 300}
	for on, want := range map[int]int{5: 100, 1_000: 100, 2_000: 200, 3_000: 300, 50_000: 300} {
		if got := k.SeatsFor(on); got != want {
			t.Errorf("SeatsFor(%d) = %d, want %d", on, got, want)
		}
	}
}

func TestComingBackAfterBeingOutIsNotStable(t *testing.T) {
	e := setup(t, 10)
	e.beatEvery5(t, []int{0}, 0, 70)
	since, _ := e.k.OnSince.Get(e.ctx, sdk.ValAddress(e.accs[0]))
	if since != start.Unix() {
		t.Fatalf("on since %d, want the first heartbeat %d", since, start.Unix())
	}
	v := e.stake.vals[string(sdk.ValAddress(e.accs[0]))]
	v.Jailed = true // taken out for missing blocks
	e.stake.vals[string(sdk.ValAddress(e.accs[0]))] = v
	e.beatEvery5(t, []int{0}, 75, 75)
	if since, _ := e.k.OnSince.Get(e.ctx, sdk.ValAddress(e.accs[0])); since != start.Add(75*time.Minute).Unix() {
		t.Fatalf("on since %d after coming back, want restarted", since)
	}
}

func TestHelperSendsHeartbeatsAndBringsBack(t *testing.T) {
	e := setup(t, 10, 20)
	helper := sdk.AccAddress("helper______________")
	if err := e.k.SetHelper(e.ctx, e.accs[0], helper); err != nil {
		t.Fatal(err)
	}
	// A LockKeeper cannot be a helper; a helper serves one LockKeeper only.
	if err := e.k.SetHelper(e.ctx, e.accs[1], e.accs[0]); !errors.Is(err, types.ErrHelperTaken) {
		t.Fatalf("a LockKeeper as helper: %v", err)
	}
	if err := e.k.SetHelper(e.ctx, e.accs[1], helper); !errors.Is(err, types.ErrHelperTaken) {
		t.Fatalf("the same helper twice: %v", err)
	}
	if err := e.k.Heartbeat(e.ctx, helper); err != nil {
		t.Fatal(err)
	}
	e.at(5 * time.Minute)
	if err := e.k.Heartbeat(e.ctx, helper); err != nil {
		t.Fatal(err)
	}
	if got := e.weight(0); got != 10*300 {
		t.Fatalf("time on counted for the LockKeeper = %d, want %d", got, 10*300)
	}

	// Out for not signing: the heartbeat brings it back once allowed.
	v := e.stake.vals[string(sdk.ValAddress(e.accs[0]))]
	v.Jailed = true
	e.stake.vals[string(sdk.ValAddress(e.accs[0]))] = v
	*e.tooSoon = true
	e.at(10 * time.Minute)
	if err := e.k.Heartbeat(e.ctx, helper); !errors.Is(err, types.ErrNotLockKeeper) {
		t.Fatalf("heartbeat while it cannot come back yet: %v", err)
	}
	*e.tooSoon = false
	if err := e.k.Heartbeat(e.ctx, helper); err != nil {
		t.Fatalf("heartbeat once it can come back: %v", err)
	}
	if e.stake.vals[string(sdk.ValAddress(e.accs[0]))].Jailed {
		t.Fatal("the LockKeeper must be back")
	}

	// Removing the helper: its heartbeats no longer count.
	if err := e.k.SetHelper(e.ctx, e.accs[0], nil); err != nil {
		t.Fatal(err)
	}
	e.at(20 * time.Minute)
	if err := e.k.Heartbeat(e.ctx, helper); !errors.Is(err, types.ErrNotLockKeeper) {
		t.Fatalf("heartbeat from a removed helper: %v", err)
	}
}

func TestComputerAnnouncesItsKeyForTheQRLink(t *testing.T) {
	e := setup(t, 10)
	helper := sdk.AccAddress("pc-helper___________")
	if err := e.k.AnnounceKey(e.ctx, helper, []byte("short")); !errors.Is(err, types.ErrBadKey) {
		t.Fatalf("a wrong key: %v, want ErrBadKey", err)
	}
	key := make([]byte, 1952)
	key[0] = 7
	if err := e.k.AnnounceKey(e.ctx, helper, key); err != nil {
		t.Fatal(err)
	}
	got, keeper, err := e.k.HelperInfo(e.ctx, helper)
	if err != nil || len(got) != 1952 || got[0] != 7 || keeper != nil {
		t.Fatalf("info = %d bytes, keeper %v, err %v", len(got), keeper, err)
	}
	// The phone links it: the helper now works for LockKeeper 0.
	if err := e.k.SetHelper(e.ctx, e.accs[0], helper); err != nil {
		t.Fatal(err)
	}
	if _, keeper, _ = e.k.HelperInfo(e.ctx, helper); !keeper.Equals(e.accs[0]) {
		t.Fatalf("keeper = %v, want %v", keeper, e.accs[0])
	}
	// Forgotten after 24 hours, and deleted from the blockchain.
	e.at(25 * time.Hour)
	if got, _, _ = e.k.HelperInfo(e.ctx, helper); got != nil {
		t.Fatal("an announced key must expire after 24 hours")
	}
	if err := e.k.BeginBlock(e.ctx); err != nil {
		t.Fatal(err)
	}
	if has, _ := e.k.Announced.Has(e.ctx, helper); has {
		t.Fatal("an expired key must be deleted")
	}
}

func TestHoursLogKeeps30Days(t *testing.T) {
	var o types.Online
	day := int64(20_000)
	// 1 hour on each of the last 40 days.
	for d := day - 39; d <= day; d++ {
		o = rollDays(o, d)
		if o.Day == 0 {
			o.Day = d
		}
		o.Today += 3600
	}
	today, yesterday, week, month := HoursLog(o, day*daySeconds+10)
	if today != 3600 || yesterday != 3600 || week != 7*3600 || month != 30*3600 {
		t.Fatalf("log = %d/%d/%d/%d", today, yesterday, week, month)
	}
	// Three days later, with no heartbeat since: today and yesterday are 0.
	today, yesterday, week, month = HoursLog(o, (day+3)*daySeconds)
	if today != 0 || yesterday != 0 || week != 4*3600 || month != 27*3600 {
		t.Fatalf("log after 3 days off = %d/%d/%d/%d", today, yesterday, week, month)
	}
}

// A computer that froze for a minute (taken out for missing blocks) keeps
// the rest of its hour; the stretch in which it was out does not count.
func TestAFreezeCostsOnlyThatStretch(t *testing.T) {
	e := setup(t, 10, 10)
	if err := e.k.BeginBlock(e.ctx); err != nil {
		t.Fatal(err)
	}
	e.beatEvery5(t, []int{0, 1}, 0, 50)
	// 0 is taken out (missed blocks) and is still out when the hour is paid.
	v := e.stake.vals[string(sdk.ValAddress(e.accs[0]))]
	v.Jailed = true
	e.stake.vals[string(sdk.ValAddress(e.accs[0]))] = v
	e.at(55 * time.Minute)
	e.beat(t, 1)
	e.bank.balances[string(authtypes.NewModuleAddress(authtypes.FeeCollectorName))] = 10_500
	e.at(60 * time.Minute)
	if err := e.k.BeginBlock(e.ctx); err != nil {
		t.Fatal(err)
	}
	op0 := e.stake.vals[string(sdk.ValAddress(e.accs[0]))].GetOperator()
	op1 := e.stake.vals[string(sdk.ValAddress(e.accs[1]))].GetOperator()
	// 0: 50 minutes, 1: 55 minutes → 10 : 11.
	if e.distr.paid[op0] != 5_000 || e.distr.paid[op1] != 5_500 {
		t.Fatalf("paid %d and %d, want 5000 and 5500", e.distr.paid[op0], e.distr.paid[op1])
	}
	// Coming back: the stretch while out does not count.
	e.at(64 * time.Minute)
	e.beat(t, 0)
	if e.weight(0) != 0 {
		t.Fatalf("weight %d after coming back, want 0 for the stretch out", e.weight(0))
	}
}
