package app

import (
	abci "github.com/cometbft/cometbft/abci/types"
)

// Light LockKeeper (22 Oct 2026): a LockKeeper keeps only its last whole
// blocks (keepBlocks, set by the LockKeeper program: cmd/lockkeeper/cmd/light.go).
// The Cosmos SDK asks CometBFT to keep every block of the evidence window
// (100.000 blocks, with every signer's quantum-safe signature several times
// in each: 4,7 GB on the founder's computer with 3 signers, ~150 GB with
// 100). CometBFT itself already keeps what proves a cheat over that window
// (the header of each block) when it deletes a block's body, so only the
// body, not the proof, goes. The rest of the clean-up (old signatures,
// unused index entries) is in cmd/lockkeeper/cmd/diet.go.

// Commit asks CometBFT to delete the blocks older than the last keepBlocks.
func (app *LockKeeperApp) Commit() (*abci.ResponseCommit, error) {
	res, err := app.BaseApp.Commit()
	if err != nil || res == nil || app.keepBlocks <= 0 {
		return res, err
	}
	res.RetainHeight = retainHeight(res.RetainHeight, app.LastBlockHeight(), app.keepBlocks)
	return res, nil
}

// retainHeight is the first block to keep: the Cosmos SDK's answer, raised
// to keep only the last keep blocks. The SDK says 0 (delete nothing) while
// the chain is younger than its evidence window: that is raised too.
func retainHeight(sdk, height, keep int64) int64 {
	if height-keep <= sdk {
		return sdk
	}
	return height - keep
}
