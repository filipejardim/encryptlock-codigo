// Package emission implements EncryptLock's coin emission (white paper, section 4):
// 40.000.000 LOCK maximum supply; at genesis 1.000 LOCK to start the first
// LockKeepers and 400.000 LOCK (1%) to the creator, publicly; the rest
// released over 360 months of 30 days, halving every 60 months, paid 100% to
// LockKeepers: everyone switched on, by time on × guarantee (x/rotation,
// founder's decision of 6 Oct). 800.000 LOCK (2%) are kept apart as the
// LockKeepers' everlasting reserve: after the 30 years what is left of it
// halves every 30 years, released bit by bit (99,9% within 330 years, the
// last lockunit about 1.440 years after genesis; founder's decision of
// 12 Oct 2026). The supply never goes above 40.000.000 LOCK.
package emission

import (
	"math/big"

	"encryptlock/lockkeeper/protocol"
)

const (
	// StartAllocation starts the first LockKeepers: 1.000 LOCK.
	StartAllocation = 1_000 * protocol.BaseUnitsPerCoin

	// FounderAllocation is the creator's reward for building the blockchain:
	// 1% of the maximum supply, 400.000 LOCK, public from the first block
	// (founder's decision of 7 Oct 2026).
	FounderAllocation = protocol.MaxSupplyBaseUnits / 100

	// GenesisAllocation is the only supply that exists at genesis: 401.000 LOCK.
	GenesisAllocation = StartAllocation + FounderAllocation

	// ReserveAllocation is the LockKeepers' everlasting reserve: 2% of the
	// maximum supply, 800.000 LOCK, released only after the 30 years.
	ReserveAllocation = protocol.MaxSupplyBaseUnits / 50

	// TotalEmission is what the 30-year schedule releases after genesis:
	// 38.799.000 LOCK.
	TotalEmission = protocol.MaxSupplyBaseUnits - GenesisAllocation - ReserveAllocation

	// AllEmission is everything ever released after genesis: the 30 years
	// and the reserve.
	AllEmission = TotalEmission + ReserveAllocation

	// ReservePeriodSeconds: every 30 years (360 emission months) what is
	// left of the reserve halves.
	ReservePeriodSeconds = protocol.IssuanceMonths * MonthSeconds

	// MonthSeconds is one emission month: 30 days of network (block) time.
	MonthSeconds int64 = 30 * 24 * 60 * 60
)

// ComputeSchedule derives the 360 monthly amounts (in lockunit) from the white
// paper formula: month n = a × r^(n−1), r = 0,5^(1/60), with a chosen so the
// months add up to exactly TotalEmission. Each month is rounded down and the
// last month absorbs the rounding remainder.
//
// It uses math/big (software arithmetic, identical on every computer); it is
// only used to generate and verify MonthlyEmission, never inside the chain.
func ComputeSchedule() [protocol.IssuanceMonths]uint64 {
	const prec = 512
	newFloat := func() *big.Float { return new(big.Float).SetPrec(prec) }

	// r = 0,5^(1/60): solve x^60 = 0,5 with Newton's method.
	half := newFloat().SetFloat64(0.5)
	r := newFloat().SetFloat64(0.99)
	for range 200 {
		pow59 := newFloat().SetInt64(1)
		for range protocol.HalvingPeriodMonths - 1 {
			pow59.Mul(pow59, r)
		}
		pow60 := newFloat().Mul(pow59, r)
		step := newFloat().Sub(pow60, half)
		step.Quo(step, newFloat().Mul(newFloat().SetInt64(protocol.HalvingPeriodMonths), pow59))
		r.Sub(r, step)
	}

	// sum of r^k for k = 0..359, and the factors r^k themselves.
	factors := make([]*big.Float, protocol.IssuanceMonths)
	sum := newFloat()
	factor := newFloat().SetInt64(1)
	for n := range protocol.IssuanceMonths {
		factors[n] = newFloat().Set(factor)
		sum.Add(sum, factor)
		factor.Mul(factor, r)
	}

	// a = TotalEmission / sum
	a := newFloat().Quo(newFloat().SetUint64(TotalEmission), sum)

	var schedule [protocol.IssuanceMonths]uint64
	var total uint64
	for n := range protocol.IssuanceMonths {
		amount, _ := newFloat().Mul(a, factors[n]).Uint64() // truncates toward zero
		schedule[n] = amount
		total += amount
	}
	schedule[protocol.IssuanceMonths-1] += TotalEmission - total
	return schedule
}

// CumulativeTarget returns how many lockunit must have been emitted in total
// once elapsedSeconds of network time have passed since genesis. Within a
// month, the month's amount is released in proportion to the time elapsed.
// Pure integer arithmetic: the same on every LockKeeper.
func CumulativeTarget(elapsedSeconds int64) uint64 {
	if elapsedSeconds <= 0 {
		return 0
	}
	fullMonths := elapsedSeconds / MonthSeconds
	if fullMonths >= protocol.IssuanceMonths {
		return TotalEmission + reserveReleased(elapsedSeconds-protocol.IssuanceMonths*MonthSeconds)
	}

	var target uint64
	for n := range fullMonths {
		target += MonthlyEmission[n]
	}

	intoMonth := elapsedSeconds % MonthSeconds
	partial := new(big.Int).SetUint64(MonthlyEmission[fullMonths])
	partial.Mul(partial, big.NewInt(intoMonth))
	partial.Quo(partial, big.NewInt(MonthSeconds))
	return target + partial.Uint64()
}

// ReserveLeft is what is left of the reserve after the given number of
// 30-year periods past the first 30 years: 800.000 LOCK halved each time
// (rounded down to whole lockunit; nothing is left after 47 periods).
func ReserveLeft(periods int64) uint64 {
	if periods >= 64 {
		return 0
	}
	return ReserveAllocation >> periods
}

// reserveReleased is how much of the reserve is out once afterSeconds have
// passed since the end of the 30 years: in each 30-year period, the half
// that leaves is released in proportion to the time elapsed. Pure integer
// arithmetic.
func reserveReleased(afterSeconds int64) uint64 {
	k := afterSeconds / ReservePeriodSeconds
	if k >= 64 {
		return ReserveAllocation
	}
	left, next := ReserveLeft(k), ReserveLeft(k+1)
	partial := new(big.Int).SetUint64(left - next)
	partial.Mul(partial, big.NewInt(afterSeconds%ReservePeriodSeconds))
	partial.Quo(partial, big.NewInt(ReservePeriodSeconds))
	return ReserveAllocation - left + partial.Uint64()
}
