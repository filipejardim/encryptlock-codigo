package app

import (
	"encoding/json"
	"errors"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
)

// ExportAppStateAndValidators exports the current state as a genesis file.
// Zero-height export (restarting the chain from scratch with reset rewards)
// is not supported: EncryptLock never restarts its history.
func (app *LockKeeperApp) ExportAppStateAndValidators(forZeroHeight bool, _ []string, modulesToExport []string) (servertypes.ExportedApp, error) {
	if forZeroHeight {
		return servertypes.ExportedApp{}, errors.New("zero-height export is not supported by LockKeeper")
	}

	ctx := app.NewContextLegacy(true, cmtproto.Header{Height: app.LastBlockHeight()})

	genState, err := app.ModuleManager.ExportGenesisForModules(ctx, app.appCodec, modulesToExport)
	if err != nil {
		return servertypes.ExportedApp{}, err
	}

	appState, err := json.MarshalIndent(genState, "", "  ")
	if err != nil {
		return servertypes.ExportedApp{}, err
	}

	validators, err := staking.WriteValidators(ctx, app.StakingKeeper)
	return servertypes.ExportedApp{
		AppState:        appState,
		Validators:      validators,
		Height:          app.LastBlockHeight() + 1,
		ConsensusParams: app.GetConsensusParams(ctx),
	}, err
}
