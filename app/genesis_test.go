package app

import (
	"testing"
	"time"

	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log/v2"

	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
)

func TestDefaultGenesisRules(t *testing.T) {
	lockApp := New(log.NewNopLogger(), dbm.NewMemDB(), true, simtestutil.NewAppOptionsWithFlagHome(t.TempDir()))
	genesis := lockApp.DefaultGenesis()
	cdc := lockApp.AppCodec()

	var distr distrtypes.GenesisState
	cdc.MustUnmarshalJSON(genesis[distrtypes.ModuleName], &distr)
	if !distr.Params.CommunityTax.IsZero() {
		t.Errorf("community tax = %s, want 0 (100%% of the emission to LockKeepers)", distr.Params.CommunityTax)
	}

	var slashing slashingtypes.GenesisState
	cdc.MustUnmarshalJSON(genesis[slashingtypes.ModuleName], &slashing)
	if !slashing.Params.SlashFractionDowntime.IsZero() {
		t.Errorf("downtime slash = %s, want 0 (a LockKeeper that is off only gets jailed)", slashing.Params.SlashFractionDowntime)
	}
	if slashing.Params.DowntimeJailDuration > time.Minute || slashing.Params.SignedBlocksWindow > 120 {
		t.Errorf("a member that stops signing must leave after ~1 minute and may come back after 1 minute, got window %d, wait %s", slashing.Params.SignedBlocksWindow, slashing.Params.DowntimeJailDuration)
	}
	if slashing.Params.SlashFractionDoubleSign.IsZero() {
		t.Error("cheating (double signing) must still burn coins")
	}
}
