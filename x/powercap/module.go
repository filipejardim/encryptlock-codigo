package powercap

import (
	"bytes"
	"context"
	"sort"

	abci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"
	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// ModuleName is the store key of the cap's own records.
const ModuleName = "powercap"

// EqualPower is the voting power of each member of the signing group.
const EqualPower = 1

// StakingKeeper is the part of the staking keeper the cap reads.
type StakingKeeper interface {
	GetLastValidators(ctx context.Context) ([]stakingtypes.Validator, error)
	PowerReduction(ctx context.Context) sdkmath.Int
}

// Selector picks, out of the validators in guarantee, the ones that sign the
// blocks now (the rotating drawn group, x/rotation).
type Selector interface {
	Select(ctx context.Context, validators []stakingtypes.Validator, powerOf func(stakingtypes.Validator) int64) ([]stakingtypes.Validator, error)
}

// Weigher, when the selector is one, gives the group's powers to sign
// instead of EqualPower for all (startPhase: the 5% cap is then left out).
type Weigher interface {
	SigningPowers(ctx context.Context, group []stakingtypes.Validator, equal int64) (powers []int64, startPhase bool, err error)
}

// CappedStaking is the Cosmos staking module with the 5% cap applied to the
// voting power it reports to CometBFT. Staking itself still tracks the real
// coins in guarantee; only the power used for consensus (and rewards) is
// capped.
type CappedStaking struct {
	staking.AppModule
	keeper StakingKeeper
	// innerEndBlock is staking's EndBlock (replaceable in tests).
	innerEndBlock func(context.Context) ([]abci.ValidatorUpdate, error)
	// selector, when set, picks the signing group before the cap applies.
	selector Selector

	// sent holds, per consensus address, the last validator update sent to
	// CometBFT: what CometBFT currently believes.
	sent collections.Map[[]byte, []byte]
	// initialized becomes true after the first block.
	initialized collections.Item[bool]
}

// NewCappedStaking wraps the staking module.
func NewCappedStaking(inner staking.AppModule, keeper StakingKeeper, storeService store.KVStoreService) CappedStaking {
	sb := collections.NewSchemaBuilder(storeService)
	m := CappedStaking{
		AppModule:     inner,
		keeper:        keeper,
		innerEndBlock: inner.EndBlock,
		sent:          collections.NewMap(sb, collections.NewPrefix(0), "sent", collections.BytesKey, collections.BytesValue),
		initialized:   collections.NewItem(sb, collections.NewPrefix(1), "initialized", collections.BoolValue),
	}
	if _, err := sb.Build(); err != nil {
		panic(err)
	}
	return m
}

// WithSelector makes only the group the selector picks sign the blocks.
func (m CappedStaking) WithSelector(s Selector) CappedStaking {
	m.selector = s
	return m
}

// EndBlock runs staking's end block, then reports the capped voting powers to
// CometBFT as the difference from what was last sent.
func (m CappedStaking) EndBlock(ctx context.Context) ([]abci.ValidatorUpdate, error) {
	initialized, err := m.initialized.Has(ctx)
	if err != nil {
		return nil, err
	}
	if !initialized {
		// First block: CometBFT holds exactly the genesis validator set as
		// staking computed it (no cap at genesis), so record it.
		current, err := m.desired(ctx, false)
		if err != nil {
			return nil, err
		}
		for addr, upd := range current {
			if err := m.saveSent(ctx, []byte(addr), upd); err != nil {
				return nil, err
			}
		}
		if err := m.initialized.Set(ctx, true); err != nil {
			return nil, err
		}
	}

	if _, err := m.innerEndBlock(ctx); err != nil {
		return nil, err
	}

	want, err := m.desired(ctx, true)
	if err != nil {
		return nil, err
	}

	var updates []abci.ValidatorUpdate
	var removed [][]byte

	// Changed or new validators.
	addrs := make([]string, 0, len(want))
	for addr := range want {
		addrs = append(addrs, addr)
	}
	sort.Strings(addrs)
	for _, addr := range addrs {
		upd := want[addr]
		prevBz, err := m.sent.Get(ctx, []byte(addr))
		if err == nil {
			var prev abci.ValidatorUpdate
			if err := prev.Unmarshal(prevBz); err != nil {
				return nil, err
			}
			if prev.Power == upd.Power && prev.PubKey.Equal(upd.PubKey) {
				continue
			}
		}
		updates = append(updates, upd)
		if err := m.saveSent(ctx, []byte(addr), upd); err != nil {
			return nil, err
		}
	}

	// Validators that left the set (jailed, unbonded, rotated key): power 0.
	err = m.sent.Walk(ctx, nil, func(addr, bz []byte) (bool, error) {
		if _, ok := want[string(addr)]; ok {
			return false, nil
		}
		var prev abci.ValidatorUpdate
		if err := prev.Unmarshal(bz); err != nil {
			return true, err
		}
		prev.Power = 0
		updates = append(updates, prev)
		removed = append(removed, bytes.Clone(addr))
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	for _, addr := range removed {
		if err := m.sent.Remove(ctx, addr); err != nil {
			return nil, err
		}
	}

	return updates, nil
}

// desired returns the validator updates CometBFT should hold now, keyed by
// consensus address, with the cap applied when capped is true.
func (m CappedStaking) desired(ctx context.Context, capped bool) (map[string]abci.ValidatorUpdate, error) {
	validators, err := m.keeper.GetLastValidators(ctx)
	if err != nil {
		return nil, err
	}
	reduction := m.keeper.PowerReduction(ctx)
	if capped && m.selector != nil {
		if validators, err = m.selector.Select(ctx, validators, func(v stakingtypes.Validator) int64 { return v.ConsensusPower(reduction) }); err != nil {
			return nil, err
		}
	}

	powers := make([]int64, len(validators))
	for i, v := range validators {
		powers[i] = v.ConsensusPower(reduction)
		if capped && m.selector != nil {
			// In the signing group every computer counts the same (1 chip =
			// 1 vote, founder's decision of 9 Oct): the guarantee counts
			// for the earnings, not for the power to sign.
			powers[i] = EqualPower
		}
	}
	startPhase := false
	if w, ok := m.selector.(Weigher); ok && capped {
		// Start phase: the computers that started the network hold more
		// than 2/3 (x/rotation SigningPowers).
		if powers, startPhase, err = w.SigningPowers(ctx, validators, EqualPower); err != nil {
			return nil, err
		}
	}
	if capped && !startPhase {
		powers = CapPowers(powers)
	}

	out := make(map[string]abci.ValidatorUpdate, len(validators))
	for i, v := range validators {
		addr, err := v.GetConsAddr()
		if err != nil {
			return nil, err
		}
		upd := v.ABCIValidatorUpdate(reduction)
		upd.Power = powers[i]
		out[string(addr)] = upd
	}
	return out, nil
}

func (m CappedStaking) saveSent(ctx context.Context, addr []byte, upd abci.ValidatorUpdate) error {
	bz, err := upd.Marshal()
	if err != nil {
		return err
	}
	return m.sent.Set(ctx, addr, bz)
}
