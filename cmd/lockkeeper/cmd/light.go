package cmd

import (
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
)

// Light LockKeeper (founder's decision, 7 Oct 2026): a LockKeeper must never
// fill people's disk or slow their computer. Every LockKeeper - test or real,
// on a PC or a server - runs with these settings, whatever its app.toml says:
//   - only the last ~40 minutes of whole blocks are kept: older ones are
//     deleted, and so are their signatures and what nothing reads (22 Oct
//     2026: 5,7 GB on the founder's computer, ~150 GB with 100 signers; see
//     app/retain.go and diet.go);
//   - a block about every 5 seconds, not every 1,4 (22 Oct 2026): a quarter
//     of the internet and disk. Earnings and every time limit count clock
//     time, not blocks, so they do not change; a transfer is confirmed in
//     about 5 seconds;
//   - every 10 minutes it keeps a copy of the network's state, so a new
//     LockKeeper can start from that copy instead of replaying deleted blocks;
//   - the results of each block are not stored (nothing reads them), and
//     the transaction index keeps only what the app and the scripts search;
//   - a memory limit and low priority, so games and other programs come first;
//   - the computer does not fall asleep by itself while it runs (21 Oct 2026:
//     a friend's PC slept and stopped earning); the screen still turns off,
//     and the owner can still put it to sleep or switch it off.
const (
	keepRecentStates = 720   // states kept: about 1 hour
	keepRecentBlocks = 480   // whole blocks kept: about 40 minutes
	snapshotEvery    = 120   // a copy of the state every 10 minutes, the last 2 kept
	iavlCacheSize    = 50000 // the default (781.250) used several GB of memory
	memoryLimit      = 1 << 30
	// blockPause: the wait after each block (CometBFT's timeout_commit, a
	// setting of each computer, not a rule of the network).
	blockPause = 4500 * time.Millisecond
)

// indexEvents: the only records searched in the transaction index (the
// app's history, the users window, scripts/amigos-testes.ps1). The hash and
// height of every transaction are always kept.
var indexEvents = []string{
	"message.action",
	"transfer_accepted.sender", "transfer_accepted.recipient",
	"transfer_expired.id", "transfer_expired.sender", "transfer_expired.recipient",
}

func applyLightSettings(cmd *cobra.Command) {
	ctx := server.GetServerContextFromCmd(cmd)
	ctx.Config.Storage.DiscardABCIResponses = true
	ctx.Config.Consensus.TimeoutCommit = blockPause
	// Make blocks while catching up. Without this a LockKeeper that starts
	// with no neighbour waits for one forever (CometBFT block sync needs a
	// peer to call itself caught up): after a restart of the founder's
	// computer, alone at launch, the network would never move again.
	// Except on the first start of a new LockKeeper from the hourly copy of
	// the state (state sync): there CometBFT stops with "extended commit
	// not found" (adaptive sync jumps to consensus before block sync has
	// the signed votes of the copy's block). From the next start on it is on.
	if cmd.Name() == "start" {
		prepareStateSync(ctx.Config)
	}
	firstStateSync := ctx.Config.StateSync.Enable && !fileExists(filepath.Join(ctx.Config.RootDir, "data", "application.db"))
	ctx.Config.BlockSync.AdaptiveSync = !firstStateSync
	v := ctx.Viper
	v.Set(server.FlagPruning, "custom")
	v.Set(server.FlagPruningKeepRecent, keepRecentStates)
	v.Set(server.FlagPruningInterval, 10)
	v.Set(server.FlagMinRetainBlocks, keepRecentBlocks)
	v.Set(server.FlagStateSyncSnapshotInterval, snapshotEvery)
	v.Set(server.FlagStateSyncSnapshotKeepRecent, 2)
	v.Set(server.FlagIAVLCacheSize, iavlCacheSize)
	v.Set(server.FlagIndexEvents, indexEvents)
	if cmd.Name() == "start" || cmd.Name() == "pc-heartbeat" {
		debug.SetMemoryLimit(memoryLimit)
		lowerPriority()
	}
	if cmd.Name() == "start" {
		keepAwake()
	}
}
