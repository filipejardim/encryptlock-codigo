package emission

import (
	"testing"

	"encryptlock/lockkeeper/protocol"
)

func TestTableMatchesFormula(t *testing.T) {
	if ComputeSchedule() != MonthlyEmission {
		t.Fatal("schedule_table.go is out of date: run go run ./x/emission/gen_schedule.go")
	}
}

func TestSupplyAddsUpToExactly40Million(t *testing.T) {
	var sum uint64
	for _, amount := range MonthlyEmission {
		sum += amount
	}
	if got := GenesisAllocation + sum + ReserveAllocation; got != 40_000_000*protocol.BaseUnitsPerCoin {
		t.Fatalf("genesis + 360 months + reserve = %d lockunit, want exactly 40.000.000 LOCK", got)
	}
	if TotalEmission != 38_799_000*protocol.BaseUnitsPerCoin || ReserveAllocation != 800_000*protocol.BaseUnitsPerCoin {
		t.Fatalf("30 years = %d, reserve = %d; want 38.799.000 and 800.000 LOCK", TotalEmission, ReserveAllocation)
	}
}

func TestCurveHalvesEvery60Months(t *testing.T) {
	// month 1 ≈ 452.718 LOCK (white paper; 401.000 at genesis, 800.000 in reserve)
	if m1 := MonthlyEmission[0] / protocol.BaseUnitsPerCoin; m1 != 452_718 {
		t.Errorf("month 1 = %d LOCK, want 452.718", m1)
	}
	for n := 0; n+60 < protocol.IssuanceMonths; n++ {
		// month n+60 is half of month n, within rounding (a few lockunit).
		// The last month also absorbs the rounding of all 359 others.
		tolerance := int64(4)
		if n+60 == protocol.IssuanceMonths-1 {
			tolerance = 2 * protocol.IssuanceMonths
		}
		diff := int64(MonthlyEmission[n]) - 2*int64(MonthlyEmission[n+60])
		if diff < -tolerance || diff > tolerance {
			t.Fatalf("month %d (%d) is not half of month %d (%d)", n+61, MonthlyEmission[n+60], n+1, MonthlyEmission[n])
		}
	}
	for n := 1; n < protocol.IssuanceMonths; n++ {
		if MonthlyEmission[n] >= MonthlyEmission[n-1] {
			t.Fatalf("month %d is not smaller than month %d", n+1, n)
		}
	}
}

func TestCumulativeTarget(t *testing.T) {
	cases := []struct {
		name    string
		elapsed int64
		want    uint64
	}{
		{"before genesis", -100, 0},
		{"at genesis", 0, 0},
		{"half of month 1", MonthSeconds / 2, MonthlyEmission[0] / 2},
		{"end of month 1", MonthSeconds, MonthlyEmission[0]},
		{"end of month 2", 2 * MonthSeconds, MonthlyEmission[0] + MonthlyEmission[1]},
		{"end of 30 years", 360 * MonthSeconds, TotalEmission},
		{"60 years: half the reserve out", 720 * MonthSeconds, TotalEmission + ReserveAllocation/2},
		{"45 years: a quarter of the reserve out", 540 * MonthSeconds, TotalEmission + ReserveAllocation/4},
		{"90 years: three quarters out", 1080 * MonthSeconds, TotalEmission + ReserveAllocation/4*3},
		{"2.000 years: everything", 24_000 * MonthSeconds, AllEmission},
	}
	for _, c := range cases {
		if got := CumulativeTarget(c.elapsed); got != c.want {
			t.Errorf("%s: CumulativeTarget(%d) = %d, want %d", c.name, c.elapsed, got, c.want)
		}
	}
}

func TestCumulativeTargetNeverDecreasesAndNeverExceedsCap(t *testing.T) {
	var previous uint64
	for elapsed := int64(0); elapsed <= 361*MonthSeconds; elapsed += 86_400 / 2 {
		got := CumulativeTarget(elapsed)
		if got < previous {
			t.Fatalf("emission went down at %d s: %d < %d", elapsed, got, previous)
		}
		if GenesisAllocation+got > protocol.MaxSupplyBaseUnits {
			t.Fatalf("supply above 40.000.000 LOCK at %d s", elapsed)
		}
		previous = got
	}
}

func TestReserveLastsAbout1440Years(t *testing.T) {
	// 99,9% of the reserve is out after 330 years (10 halvings after year 30).
	if left := ReserveLeft(10); left*1000 > ReserveAllocation {
		t.Errorf("after 330 years %d lockunit left, more than 0,1%%", left)
	}
	// The last lockunit leaves in the period that ends 1.440 years after genesis.
	if ReserveLeft(46) == 0 || ReserveLeft(47) != 0 {
		t.Errorf("reserve left after 46 periods = %d, after 47 = %d; want >0 and 0", ReserveLeft(46), ReserveLeft(47))
	}
	end := int64(30+47*30) * 12 * MonthSeconds
	if got := CumulativeTarget(end); got != AllEmission {
		t.Errorf("at 1.440 years %d, want everything (%d)", got, AllEmission)
	}
	if got := CumulativeTarget(end - 30*12*MonthSeconds); got == AllEmission {
		t.Error("at 1.410 years everything is already out; the last lockunit should still be coming")
	}
}

func TestSupplyNeverAbove40MillionInAThousandYears(t *testing.T) {
	var previous uint64
	for elapsed := int64(0); elapsed <= 1_500*12*MonthSeconds; elapsed += 7 * 86_400 {
		got := CumulativeTarget(elapsed)
		if got < previous {
			t.Fatalf("emission went down at %d s: %d < %d", elapsed, got, previous)
		}
		if GenesisAllocation+got > protocol.MaxSupplyBaseUnits {
			t.Fatalf("supply above 40.000.000 LOCK at %d s", elapsed)
		}
		previous = got
	}
}
