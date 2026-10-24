package app

import (
	"encoding/json"
	"fmt"
	"time"

	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	distr "github.com/cosmos/cosmos-sdk/x/distribution"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/cosmos/cosmos-sdk/x/slashing"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// distrGenesis is the distribution module with EncryptLock's genesis rule:
// community tax is 0%, so 100% of the emission goes to the LockKeepers
// (white paper, section 4). Cosmos defaults to 2%.
type distrGenesis struct {
	distr.AppModule
}

func (distrGenesis) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	gs := distrtypes.DefaultGenesisState()
	gs.Params.CommunityTax = sdkmath.LegacyZeroDec()
	return cdc.MustMarshalJSON(gs)
}

func (d distrGenesis) ValidateGenesis(cdc codec.JSONCodec, txCfg client.TxEncodingConfig, bz json.RawMessage) error {
	if err := d.AppModule.ValidateGenesis(cdc, txCfg, bz); err != nil {
		return err
	}
	var gs distrtypes.GenesisState
	if err := cdc.UnmarshalJSON(bz, &gs); err != nil {
		return err
	}
	if !gs.Params.CommunityTax.IsZero() {
		return fmt.Errorf("community tax must be 0 (100%% of the emission goes to LockKeepers), got %s", gs.Params.CommunityTax)
	}
	return nil
}

// slashingGenesis is the slashing module with EncryptLock's genesis rules:
//   - a LockKeeper that is off never loses coins nor earnings (founder's
//     decision, 5 Oct): it is only taken out of the validator set, earns
//     nothing while out, and its program brings it back when it is on again;
//   - the window is 120 blocks (~2 minutes at 1 block/s) and half of them
//     must be signed, so a member of the signing group that stops signing
//     leaves it after ~1 minute and a stable LockKeeper takes its seat at
//     once (founder's decision, 9 Oct). Leaving fast keeps the switched-off
//     ones from holding back the blocks (each block needs more than 2/3 of
//     the signing group);
//   - it may ask to come back 1 minute after leaving;
//   - cheating (signing two different blocks at the same height) still burns
//     5% of the coins in guarantee (white paper, section 5).
type slashingGenesis struct {
	slashing.AppModule
}

func (slashingGenesis) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	gs := slashingtypes.DefaultGenesisState()
	gs.Params.SlashFractionDowntime = sdkmath.LegacyZeroDec()
	gs.Params.SignedBlocksWindow = 120
	gs.Params.MinSignedPerWindow = sdkmath.LegacyNewDecWithPrec(5, 1)
	gs.Params.DowntimeJailDuration = time.Minute
	return cdc.MustMarshalJSON(gs)
}

// stakingGenesis is the staking module with EncryptLock's genesis rule: up to
// MaxLockKeepers LockKeepers can be in guarantee at once (Cosmos: 100). They
// do not all sign each block: a drawn, rotating group of 100 to 300 does
// (x/rotation); everyone switched on earns.
type stakingGenesis struct {
	staking.AppModule
}

// MaxLockKeepers is how many LockKeepers can be in guarantee at once.
const MaxLockKeepers = 100_000

func (stakingGenesis) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	gs := stakingtypes.DefaultGenesisState()
	gs.Params.MaxValidators = MaxLockKeepers
	return cdc.MustMarshalJSON(gs)
}
