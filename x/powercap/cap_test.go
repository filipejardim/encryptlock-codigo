package powercap

import (
	"math/rand"
	"slices"
	"testing"
)

func total(powers []int64) (sum int64) {
	for _, p := range powers {
		sum += p
	}
	return sum
}

func TestCapIsOffBelow20LockKeepers(t *testing.T) {
	cases := [][]int64{
		{1000},            // founder alone at launch
		{900, 50, 50},     // founder with 90%
		make([]int64, 19), // 19 LockKeepers
	}
	cases[2][0] = 1_000_000
	for i := 1; i < 19; i++ {
		cases[2][i] = 1
	}
	for _, powers := range cases {
		if got := CapPowers(powers); !slices.Equal(got, powers) {
			t.Errorf("CapPowers(%v) = %v, want unchanged (fewer than 20 LockKeepers)", powers, got)
		}
	}
}

func TestEqualLockKeepersAreUnchanged(t *testing.T) {
	powers := make([]int64, 20)
	for i := range powers {
		powers[i] = 100
	}
	if got := CapPowers(powers); !slices.Equal(got, powers) {
		t.Fatalf("20 equal LockKeepers (5%% each) were changed: %v", got)
	}
}

func TestBigLockKeeperIsCappedAt5Percent(t *testing.T) {
	// One LockKeeper with half of all coins in guarantee, 20 small ones.
	powers := []int64{2000}
	for range 20 {
		powers = append(powers, 100)
	}
	got := CapPowers(powers)
	if 20*got[0] > total(got) {
		t.Fatalf("big LockKeeper has %d of %d (more than 5%%)", got[0], total(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i] != 100 {
			t.Fatalf("small LockKeeper %d changed from 100 to %d", i, got[i])
		}
	}
}

func TestCapPropertiesOnRandomNetworks(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for round := 0; round < 2000; round++ {
		n := 20 + rng.Intn(150)
		powers := make([]int64, n)
		for i := range powers {
			// mix of tiny and huge LockKeepers
			powers[i] = 1 + rng.Int63n(1+rng.Int63n(10_000_000))
		}
		got := CapPowers(powers)
		sum := total(got)

		var limit int64 = -1
		for i, p := range got {
			if p <= 0 {
				t.Fatalf("round %d: LockKeeper %d lost all power", round, i)
			}
			if 20*p > sum {
				t.Fatalf("round %d: LockKeeper %d has %d of %d (more than 5%%)", round, i, p, sum)
			}
			if p > powers[i] {
				t.Fatalf("round %d: LockKeeper %d gained power", round, i)
			}
			if p < powers[i] { // capped: all capped ones share the same limit
				if limit != -1 && p != limit {
					t.Fatalf("round %d: different caps %d and %d", round, limit, p)
				}
				limit = p
			}
		}
		for i, p := range got {
			if limit != -1 && p == powers[i] && p > limit {
				t.Fatalf("round %d: uncapped LockKeeper %d (%d) above the cap %d", round, i, p, limit)
			}
		}
	}
}
