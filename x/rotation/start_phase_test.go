package rotation

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"encryptlock/lockkeeper/protocol"
)

func (e *env) group(who []int) []stakingtypes.Validator {
	var g []stakingtypes.Validator
	for _, i := range who {
		g = append(g, e.stake.vals[string(sdk.ValAddress(e.accs[i]))])
	}
	return g
}

func (e *env) powers(t *testing.T, who []int) ([]int64, bool) {
	t.Helper()
	p, start, err := e.k.SigningPowers(e.ctx, e.group(who), 1)
	if err != nil {
		t.Fatal(err)
	}
	return p, start
}

// Founder's decision of 20 Oct: the computer that started the network holds
// more than 2/3 while there are fewer than 20 stable LockKeepers on.
func TestStartPhaseFounderHoldsTwoThirds(t *testing.T) {
	e := setup(t, 100, 1, 1, 1)
	if err := e.k.ChipFree.Set(e.ctx, e.accs[0]); err != nil {
		t.Fatal(err)
	}

	// Alone: power 1, the same as before the start phase existed.
	if p, _ := e.powers(t, []int{0}); p[0] != 1 {
		t.Fatalf("alone: %v", p)
	}

	// With 3 others (new, not stable): 7 against 3, more than 2/3.
	p, start := e.powers(t, []int{0, 1, 2, 3})
	if !start || p[0] != 7 || p[1] != 1 || p[2] != 1 || p[3] != 1 {
		t.Fatalf("start phase: %v %v", p, start)
	}
	if p[0]*3 <= (p[0]+3)*2 {
		t.Fatalf("not more than 2/3: %v", p)
	}

	// Any of the others switched off: the founder still has more than 2/3.
	for off := 1; off <= 3; off++ {
		var signed, total int64
		for i, v := range p {
			total += v
			if i != off {
				signed += v
			}
		}
		if signed*3 <= total*2 {
			t.Fatalf("blocks stop with %d off", off)
		}
	}

	// No computer that started the network in the group: all equal.
	if p, start := e.powers(t, []int{1, 2, 3}); start || p[0] != 1 || p[1] != 1 || p[2] != 1 {
		t.Fatalf("without the founder: %v %v", p, start)
	}
}

// From 20 stable LockKeepers (100 h on in the last 7 days) every computer
// counts the same again; 19 are not enough, nor 20 that are not stable.
func TestStartPhaseEndsWithTwentyStable(t *testing.T) {
	powers := make([]int64, 22)
	for i := range powers {
		powers[i] = 1
	}
	e := setup(t, powers...)
	if err := e.k.ChipFree.Set(e.ctx, e.accs[0]); err != nil {
		t.Fatal(err)
	}
	all := span(0, 22)

	// 21 others on for 50 hours: on, but not stable yet.
	e.beatEvery5(t, span(1, 22), 0, 50*60)
	if p, start := e.powers(t, all); !start || p[0] != 43 {
		t.Fatalf("not stable yet: %v %v", p[0], start)
	}

	// 20 of them stay on until 100 hours; with only 19 of those in the
	// group it is still the start phase.
	e.beatEvery5(t, span(1, 21), 50*60+5, int(protocol.StableWeek.Minutes())+10)
	if _, start := e.powers(t, span(0, 20)); !start {
		t.Fatal("19 stable must keep the start phase")
	}

	// All 20 in the group: everyone equal.
	p, start := e.powers(t, all)
	if start {
		t.Fatal("20 stable must end the start phase")
	}
	for i, v := range p {
		if v != 1 {
			t.Fatalf("LockKeeper %d: power %d", i, v)
		}
	}

	// A week later without them: the hours are gone, the start phase is back.
	e.at(protocol.StableWeek + 8*24*time.Hour)
	if _, start := e.powers(t, all); !start {
		t.Fatal("start phase must come back when the stable ones are gone")
	}
}
