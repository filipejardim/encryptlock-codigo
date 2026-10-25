package cmd

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	dbm "github.com/cometbft/cometbft-db"
	cmtcfg "github.com/cometbft/cometbft/config"
	sm "github.com/cometbft/cometbft/state"
	"github.com/cometbft/cometbft/store"
)

// trustBack: how many blocks back a new LockKeeper's trusted block is, before
// the network's last two copies of the state (every 120 blocks) and after the
// oldest whole block kept (480, light.go). The app uses the same.
const trustBack = 300

// prepareStateSync runs before a LockKeeper that joins from the hourly copy of
// the network's state (state sync) starts:
//   - a join that stopped half way (the state restored, no blocks: versions up
//     to 1.0.3 stopped right there) leaves folders CometBFT refuses for good
//     ("state and store height mismatch"). They are emptied - the signing
//     state file is kept - and the join starts again;
//   - before joining, the door's current address and a fresh trusted block
//     are taken from the network: the ones written when the computer was
//     prepared may be gone (the door moves when the founder's computer
//     restarts, old blocks are deleted after about a day).
//
// The founder's computer (no state sync, no door-p2p.txt) and any LockKeeper
// that already joined are never touched.
func prepareStateSync(cfg *cmtcfg.Config) {
	// A computer that joined from another home in the first days replayed
	// the blocks from the first one, with state sync off: it too comes back
	// from the copy of the state when it missed too much.
	fromDoor := fileExists(filepath.Join(cfg.RootDir, doorP2PTxt))
	if !cfg.StateSync.Enable && !fromDoor {
		return
	}
	stateHeight, storeHeight, err := heights(cfg)
	if err != nil {
		return
	}
	if stateHeight > 0 && storeHeight == 0 {
		if emptyData(cfg) != nil {
			return
		}
		stateHeight = 0
	}
	if stateHeight > 0 {
		// Switched off for longer than the network keeps whole blocks (about
		// 40 minutes), or stuck that long while on (rejoin.go): nobody can
		// send the blocks it missed and it would wait forever. It joins again
		// from the copy of the state, like a new LockKeeper (minutes, a few
		// MB); the signing state file stays. The trusted block is taken
		// before anything is deleted: without internet nothing changes.
		rpc := doorRPCFromSite()
		if rpc == "" {
			return
		}
		if _, earliest, err := doorHeights(rpc); err != nil || !missedTooMuch(storeHeight, earliest) {
			return
		}
		h, hash, err := trustedBlock(rpc)
		if err != nil || emptyData(cfg) != nil {
			return
		}
		useTrusted(cfg, rpc, h, hash)
		return
	}
	// Nothing kept yet (a new LockKeeper, or a rejoin that stopped half way):
	// from the copy of the state too.
	if rpc := doorRPCFromSite(); rpc != "" {
		if h, hash, err := trustedBlock(rpc); err == nil {
			useTrusted(cfg, rpc, h, hash)
		}
	}
}

// useTrusted makes this start join from the copy of the state, checked
// against block h of the door.
func useTrusted(cfg *cmtcfg.Config, rpc string, h int64, hash string) {
	cfg.StateSync.Enable = true
	cfg.StateSync.RPCServers = []string{rpc, rpc}
	cfg.StateSync.TrustHeight = h
	cfg.StateSync.TrustHash = hash
}

// heights returns the last block of the saved state and of the block store.
func heights(cfg *cmtcfg.Config) (state, blocks int64, err error) {
	backend := dbm.BackendType(cfg.DBBackend)
	sdb, err := dbm.NewDB("state", backend, cfg.DBDir())
	if err != nil {
		return 0, 0, err
	}
	s, err := sm.NewStore(sdb, sm.StoreOptions{}).Load()
	sdb.Close()
	if err != nil {
		return 0, 0, err
	}
	bdb, err := dbm.NewDB("blockstore", backend, cfg.DBDir())
	if err != nil {
		return 0, 0, err
	}
	b := store.LoadBlockStoreState(bdb)
	bdb.Close()
	return s.LastBlockHeight, b.Height, nil
}

// emptyData deletes the network's data of this computer, except the file
// that keeps it from ever signing two different blocks.
func emptyData(cfg *cmtcfg.Config) error {
	dir := cfg.DBDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	keep := filepath.Base(cfg.PrivValidatorStateFile())
	for _, e := range entries {
		if e.Name() == keep {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// doorRPCFromSite returns the network door's RPC address the website
// publishes ("" when it cannot be read).
func doorRPCFromSite() string {
	c := &http.Client{Timeout: 15 * time.Second}
	res, err := c.Get(doorSite)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	var j struct {
		RPC string `json:"rpc"`
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&j) != nil {
		return ""
	}
	if !strings.HasPrefix(j.RPC, "https://") || !doorHost.MatchString(strings.TrimPrefix(j.RPC, "https://")) {
		return ""
	}
	return j.RPC
}

// trustedBlock returns a block trustBack blocks before the newest (or just
// after the oldest the network still keeps) and its hash.
func trustedBlock(rpc string) (int64, string, error) {
	latest, earliest, err := doorHeights(rpc)
	if err != nil {
		return 0, "", err
	}
	h := latest - trustBack
	if h <= earliest {
		h = earliest + 1
	}
	var block struct {
		Result struct {
			BlockID struct {
				Hash string `json:"hash"`
			} `json:"block_id"`
		} `json:"result"`
	}
	if err := rpcGet(rpc, fmt.Sprintf("/block?height=%d", h), &block); err != nil {
		return 0, "", err
	}
	if b, err := hex.DecodeString(block.Result.BlockID.Hash); err != nil || len(b) != 32 {
		return 0, "", fmt.Errorf("bad block hash")
	}
	return h, block.Result.BlockID.Hash, nil
}

// missedTooMuch: the blocks after this computer's last one are no longer
// kept by the network (or soon will not be: a margin of 60 blocks, 5 minutes).
func missedTooMuch(lastBlock, networkEarliest int64) bool {
	return lastBlock+1 < networkEarliest+60
}

// doorHeights returns the newest block and the oldest the network still keeps.
func doorHeights(rpc string) (latest, earliest int64, err error) {
	var status struct {
		Result struct {
			SyncInfo struct {
				Latest   string `json:"latest_block_height"`
				Earliest string `json:"earliest_block_height"`
			} `json:"sync_info"`
		} `json:"result"`
	}
	if err := rpcGet(rpc, "/status", &status); err != nil {
		return 0, 0, err
	}
	latest, err1 := strconv.ParseInt(status.Result.SyncInfo.Latest, 10, 64)
	earliest, err2 := strconv.ParseInt(status.Result.SyncInfo.Earliest, 10, 64)
	if err1 != nil || err2 != nil || latest <= 0 || earliest <= 0 {
		return 0, 0, fmt.Errorf("bad status")
	}
	return latest, earliest, nil
}

func rpcGet(rpc, path string, out any) error {
	c := &http.Client{Timeout: 30 * time.Second}
	res, err := c.Get(rpc + path)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", path, res.Status)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(out)
}
