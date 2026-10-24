package rotation

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sort"

	"cosmossdk.io/collections"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"encryptlock/lockkeeper/protocol"
)

// candidate is a LockKeeper that may sign: in guarantee, not out, and on.
type candidate struct {
	val    stakingtypes.Validator
	cons   []byte
	stable bool // on without a break for protocol.StableAfter
}

// Select returns the LockKeepers that sign the blocks now, out of the
// validators staking keeps in guarantee (founder's decision, 9 Oct):
//
//   - only LockKeepers that are on can get a seat (until the first heartbeat
//     of the network, everyone can: that is how it starts);
//   - with no more of them than seats, all sign;
//   - otherwise every seat is drawn by lot, everyone with the same chance
//     (1 chip = 1 vote), only among those on without a break for an hour
//     while there are enough of them;
//   - the group grows with the network (Seats(n));
//   - every 6 minutes 1% of the seats (at least one) change hands, the
//     longest-seated first; anyone who switches off or is taken out for
//     missing blocks is replaced at once.
//
// Earning does not depend on being in the group (see pay).
// The draw uses the hash of the block, so every LockKeeper computes the same.
func (k Keeper) Select(ctx context.Context, validators []stakingtypes.Validator, powerOf func(stakingtypes.Validator) int64) ([]stakingtypes.Validator, error) {
	t := now(ctx)
	period := t / int64(protocol.RotateEvery.Seconds())

	all := make([]candidate, 0, len(validators))
	anyOnline := false
	for _, v := range validators {
		cons, err := v.GetConsAddr()
		if err != nil {
			return nil, err
		}
		valAddr, err := sdk.ValAddressFromBech32(v.GetOperator())
		if err != nil {
			return nil, err
		}
		online, _, err := k.IsOnline(ctx, valAddr)
		if err != nil {
			return nil, err
		}
		stable := false
		if online {
			since, err := k.OnSince.Get(ctx, valAddr)
			if err != nil && !errors.Is(err, collections.ErrNotFound) {
				return nil, err
			}
			stable = err == nil && t-since >= int64(protocol.StableAfter.Seconds())
		}
		anyOnline = anyOnline || online
		if !online || powerOf(v) <= 0 {
			cons = nil // marks "cannot sign now"
		}
		all = append(all, candidate{val: v, cons: cons, stable: stable})
	}
	eligible := make([]candidate, 0, len(all))
	for _, c := range all {
		if c.cons != nil {
			eligible = append(eligible, c)
		}
	}
	if !anyOnline {
		// Nobody has said "I am on" (the network is starting, or it stood
		// still for more than 10 minutes): every real computer signs. Only
		// those with a proven chip (or that started the network): otherwise
		// LockKeepers without a chip, which can never say "I am on", would
		// take over the blocks right after a stop.
		eligible = eligible[:0]
		for i, v := range validators {
			valAddr, err := sdk.ValAddressFromBech32(v.GetOperator())
			if err != nil {
				return nil, err
			}
			ok, err := k.ChipOK(ctx, valAddr)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			cons, _ := v.GetConsAddr()
			all[i].cons = cons
			eligible = append(eligible, all[i])
		}
		if len(eligible) == 0 {
			// Never an empty group (the blocks would stop for good): the
			// last group that signed carries on.
			for i, v := range validators {
				cons, _ := v.GetConsAddr()
				if has, err := k.Committee.Has(ctx, cons); err != nil {
					return nil, err
				} else if has {
					all[i].cons = cons
					eligible = append(eligible, all[i])
				}
			}
		}
	}
	byCons := make(map[string]candidate, len(eligible))
	for _, c := range eligible {
		byCons[string(c.cons)] = c
	}

	// The current group, without whoever is off or out.
	seated := map[string]int64{}
	err := k.Committee.Walk(ctx, nil, func(cons []byte, since int64) (bool, error) {
		if _, ok := byCons[string(cons)]; ok {
			seated[string(cons)] = since
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	seats := k.SeatsFor(len(eligible))
	if len(eligible) <= seats {
		for _, c := range eligible {
			if _, ok := seated[string(c.cons)]; !ok {
				seated[string(c.cons)] = period
			}
		}
	} else {
		k.reseat(ctx, eligible, seated, seats, period)
	}

	if err := k.Committee.Clear(ctx, nil); err != nil {
		return nil, err
	}
	out := make([]stakingtypes.Validator, 0, len(seated))
	for _, c := range eligible {
		if since, ok := seated[string(c.cons)]; ok {
			if err := k.Committee.Set(ctx, c.cons, since); err != nil {
				return nil, err
			}
			out = append(out, c.val)
		}
	}
	if err := k.RotatePeriod.Set(ctx, period); err != nil {
		return nil, err
	}
	return out, nil
}

// SeatsFor is the size of the signing group with on LockKeepers on: it grows
// with the network, from Seats up to MaxSeats.
func (k Keeper) SeatsFor(on int) int {
	return max(k.Seats, min(k.MaxSeats, on/protocol.OnlinePerSeat))
}

// reseat updates the group in place when there are more LockKeepers on than
// seats: gradual rotation, then drawn replacements.
func (k Keeper) reseat(ctx context.Context, eligible []candidate, seated map[string]int64, seats int, period int64) {
	stable := map[string]bool{}
	stableOutside := 0
	for _, c := range eligible {
		if c.stable {
			stable[string(c.cons)] = true
			if _, ok := seated[string(c.cons)]; !ok {
				stableOutside++
			}
		}
	}

	// Members, longest-seated first.
	members := func() []string {
		var ms []string
		for cons := range seated {
			ms = append(ms, cons)
		}
		sort.Slice(ms, func(i, j int) bool {
			if seated[ms[i]] != seated[ms[j]] {
				return seated[ms[i]] < seated[ms[j]]
			}
			return ms[i] < ms[j]
		})
		return ms
	}

	// Rotation: only when stable LockKeepers wait outside to take the seats.
	left := map[string]bool{}
	last, err := k.RotatePeriod.Get(ctx)
	if err == nil && last != period {
		rotate := min((seats+protocol.RotateDivisor-1)/protocol.RotateDivisor, stableOutside)
		for _, cons := range members()[:min(rotate, len(seated))] {
			delete(seated, cons)
			left[cons] = true
		}
	}
	// The group got smaller.
	for ms := members(); len(seated) > seats && len(ms) > 0; ms = ms[1:] {
		delete(seated, ms[0])
	}

	// Fill the free seats by lot, everyone with the same chance: first the
	// stable ones, then (so the blocks never stop) anyone on; those who just
	// left only if nobody else is available.
	seed := sha256.Sum256(append(sdk.UnwrapSDKContext(ctx).HeaderHash(), binary.BigEndian.AppendUint64(nil, uint64(period))...))
	pools := []func(c candidate) bool{
		func(c candidate) bool { return stable[string(c.cons)] && !left[string(c.cons)] },
		func(c candidate) bool { return !left[string(c.cons)] },
		func(c candidate) bool { return true },
	}
	for round := uint64(0); len(seated) < seats; round++ {
		var pool []candidate
		for _, in := range pools {
			for _, c := range eligible {
				if _, ok := seated[string(c.cons)]; !ok && in(c) {
					pool = append(pool, c)
				}
			}
			if len(pool) > 0 {
				break
			}
		}
		if len(pool) == 0 {
			return
		}
		h := sha256.Sum256(binary.BigEndian.AppendUint64(seed[:], round))
		seated[string(pool[binary.BigEndian.Uint64(h[:8])%uint64(len(pool))].cons)] = period
	}
}
