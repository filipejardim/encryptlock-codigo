// Package powercap enforces the golden rule "no LockKeeper has more than 5%
// of the voting power" (white paper, section 4). The rule switches on by
// itself once there are at least 20 active LockKeepers (with fewer it is
// mathematically impossible for everyone to be at or below 5%).
//
// Capping the voting power sent to CometBFT also caps rewards: the
// distribution module pays in proportion to that voting power, so what a
// LockKeeper would earn above 5% is shared among the others.
package powercap

import "sort"

const (
	// MinValidators is how many active LockKeepers switch the cap on.
	MinValidators = 20
	// capDivisor: each LockKeeper may have at most 1/20 = 5% of the total.
	capDivisor = 20
)

// CapPowers returns the voting powers after applying the 5% cap. With fewer
// than MinValidators entries the powers are returned unchanged.
//
// It finds the largest cap C such that every capped LockKeeper gets C,
// everyone else keeps their power, and C is at most 5% of the new total.
// Integer arithmetic only, so every LockKeeper computes the same result.
func CapPowers(powers []int64) []int64 {
	out := append([]int64(nil), powers...)
	if len(powers) < MinValidators {
		return out
	}

	sorted := append([]int64(nil), powers...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] > sorted[j] })

	// suffix[k] = sum of the powers that remain uncapped when the k largest
	// are capped.
	suffix := make([]int64, len(sorted)+1)
	for i := len(sorted) - 1; i >= 0; i-- {
		suffix[i] = suffix[i+1] + sorted[i]
	}

	// Cap the k largest at C = suffix[k] / (20 - k). Valid when the (k+1)-th
	// largest fits under C. k = 0 means nobody is above 5%.
	for k := 0; k < capDivisor; k++ {
		limit := suffix[k] / int64(capDivisor-k)
		if sorted[k] <= limit {
			if k == 0 {
				return out
			}
			for i, p := range out {
				if p > limit {
					out[i] = limit
				}
			}
			return out
		}
	}
	// Unreachable: with 20+ positive powers, k = 19 always fits.
	panic("powercap: no valid cap found")
}
