package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unsafe"

	dbm "github.com/cometbft/cometbft-db"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/evidence"
	"github.com/cometbft/cometbft/rpc/client/local"
	rpccore "github.com/cometbft/cometbft/rpc/core"
	sm "github.com/cometbft/cometbft/state"
	"github.com/cometbft/cometbft/state/txindex/kv"
	"github.com/cometbft/cometbft/store"
	"github.com/cosmos/gogoproto/proto"
	"github.com/google/orderedcode"
	"golang.org/x/sync/errgroup"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server"

	"encryptlock/lockkeeper/app"
)

// Light LockKeeper, part 2 (22 Oct 2026): what CometBFT leaves on the disk
// and nothing reads is deleted while the LockKeeper runs (every minute, a
// little at a time):
//   - the signatures of blocks older than the kept ones. CometBFT keeps the
//     header and the signatures of every block of the evidence window
//     (100.000 blocks) when it deletes their bodies; the header is what proves
//     a cheat (two votes for the same block) and stays until the window ends;
//   - the "I am on" transactions (MsgHeartbeat, every LockKeeper every 5
//     minutes) in the transaction index: the network counts them when they
//     arrive, nothing searches them afterwards;
//   - the index of every block's automatic records (new coins, rewards: 900
//     MB in 4 days on the founder's computer). Only the searches the app and
//     the scripts make are indexed now (indexEvents, light.go);
//   - the old files of the consensus log (cs.wal, 1 GB): only the newest are
//     read, after a crash.
//
// CometBFT does not let a program reach its databases, so they are taken from
// the LockKeeper's own RPC client (reflection). If a new CometBFT changes
// that, nothing is deleted and the LockKeeper runs as before (and the test
// TestDietReachesTheStores fails).

const (
	dietEvery = time.Minute
	// dietBatch: heights (or index entries) cleaned per round at most.
	dietBatch = 5000
	// keepHeartbeats: blocks during which the "I am on" transactions stay
	// in the index (a program that sent one can still see it went in).
	keepHeartbeats = 120
	// walKeepFiles: old files of the consensus log kept besides the newest.
	walKeepFiles = 2
	dietFile     = "light-diet.json"
	heartbeatMsg = "/encryptlock.rotation.v1.MsgHeartbeat"
)

// dietCursor is where the clean-up got to, kept between starts.
type dietCursor struct {
	Times      int64 `json:"times"`      // next height whose time may be noted
	Signatures int64 `json:"signatures"` // next height whose signatures may go
	Headers    int64 `json:"headers"`    // next height whose header may go
	BlockIndex bool  `json:"block_index"`
}

// dietStores are CometBFT's databases and state.
type dietStores struct {
	blocks  *store.BlockStore
	blockDB dbm.DB
	txDB    dbm.DB
	state   sm.Store
	genesis blockTimeNote
}

// blockTimeNote: a block and when it was made.
type blockTimeNote struct {
	height int64
	at     time.Time
}

// dietPostSetup runs the clean-up next to the LockKeeper (start command).
func dietPostSetup(svrCtx *server.Context, clientCtx client.Context, ctx context.Context, g *errgroup.Group) error {
	logger := svrCtx.Logger.With("module", "light-diet")
	s, err := dietStoresOf(clientCtx.Client)
	if err != nil {
		logger.Error("disk clean-up off", "err", err)
		return nil
	}
	cfg := svrCtx.Config
	cursorPath := filepath.Join(cfg.DBDir(), dietFile)
	timesPath := filepath.Join(cfg.DBDir(), app.BlockTimesFile)
	walDir := filepath.Dir(cfg.Consensus.WalFile())
	g.Go(func() error {
		t := time.NewTicker(dietEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-t.C:
				dietRound(s, cursorPath, timesPath, walDir, logger)
			}
		}
	})
	return nil
}

func dietRound(s dietStores, cursorPath, timesPath, walDir string, logger log.Logger) {
	c := loadDietCursor(cursorPath)
	before := c
	base, height := s.blocks.Base(), s.blocks.Height()
	if base <= 1 || height <= 0 {
		return
	}
	st, err := s.state.Load()
	if err != nil {
		logger.Error("disk clean-up: state", "err", err)
		return
	}
	// First the times (the history places old transfers with them), then
	// the signatures and headers.
	if c.Times, err = noteBlockTimes(timesPath, c.Times, base, s.genesis, func(h int64) (time.Time, bool) {
		if meta := s.blocks.LoadBlockMeta(h); meta != nil {
			return meta.Header.Time, true
		}
		return time.Time{}, false
	}); err != nil {
		logger.Error("disk clean-up: block times", "err", err)
		return
	}
	if c.Signatures, err = dropSignatures(s.blockDB, c.Signatures, base, dietBatch); err != nil {
		logger.Error("disk clean-up: signatures", "err", err)
	}
	isOld := func(h int64) (bool, bool) {
		meta := s.blocks.LoadBlockMeta(h)
		if meta == nil {
			return false, true
		}
		return evidence.IsEvidenceExpired(st.LastBlockHeight, st.LastBlockTime, h, meta.Header.Time, st.ConsensusParams.Evidence), false
	}
	if c.Headers, err = dropHeaders(s.blockDB, c.Headers, base, dietBatch, isOld); err != nil {
		logger.Error("disk clean-up: headers", "err", err)
	}
	if _, err = dropHeartbeats(s.txDB, height-keepHeartbeats, dietBatch); err != nil {
		logger.Error("disk clean-up: heartbeats", "err", err)
	}
	if !c.BlockIndex {
		done, err := dropBlockIndex(s.txDB, base, dietBatch*20)
		if err != nil {
			logger.Error("disk clean-up: block index", "err", err)
		}
		if done {
			c.BlockIndex = true
			compact(s.txDB)
		}
	} else if _, err := dropBlockIndex(s.txDB, base, dietBatch); err != nil {
		logger.Error("disk clean-up: block index", "err", err)
	}
	if err := dropOldWAL(walDir, walKeepFiles); err != nil {
		logger.Error("disk clean-up: consensus log", "err", err)
	}
	if c != before {
		if before.Signatures < base && c.Signatures >= base {
			compact(s.blockDB) // the first clean-up of a computer: give the space back
		}
		saveDietCursor(cursorPath, c)
	}
}

// dietStoresOf takes CometBFT's databases from the LockKeeper's RPC client.
func dietStoresOf(c client.CometRPC) (dietStores, error) {
	l, ok := c.(*local.Local)
	if !ok || l == nil {
		return dietStores{}, fmt.Errorf("not the LockKeeper's own client: %T", c)
	}
	env, ok := unexported(reflect.ValueOf(l).Elem(), "env").(*rpccore.Environment)
	if !ok || env == nil {
		return dietStores{}, errors.New("no RPC environment")
	}
	bs, ok := env.BlockStore.(*store.BlockStore)
	if !ok || bs == nil {
		return dietStores{}, fmt.Errorf("block store %T", env.BlockStore)
	}
	ti, ok := env.TxIndexer.(*kv.TxIndex)
	if !ok || ti == nil {
		return dietStores{}, fmt.Errorf("transaction index %T", env.TxIndexer)
	}
	blockDB, _ := unexported(reflect.ValueOf(bs).Elem(), "db").(dbm.DB)
	txDB, _ := unexported(reflect.ValueOf(ti).Elem(), "store").(dbm.DB)
	if blockDB == nil || txDB == nil || env.StateStore == nil {
		return dietStores{}, errors.New("databases not found")
	}
	s := dietStores{blocks: bs, blockDB: blockDB, txDB: txDB, state: env.StateStore}
	if env.GenDoc != nil {
		s.genesis = blockTimeNote{max(env.GenDoc.InitialHeight, 1), env.GenDoc.GenesisTime}
	}
	return s, nil
}

// unexported reads an unexported field of a struct (nil if there is none).
func unexported(v reflect.Value, name string) any {
	f := v.FieldByName(name)
	if !f.IsValid() {
		return nil
	}
	return reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem().Interface()
}

// dropSignatures deletes the signatures (commit and extended commit) of the
// heights from..below-1, at most max of them; returns the next height.
// Below the block store's base nothing reads them (catch-up of a neighbour,
// block sync and the RPC all stop at the base).
func dropSignatures(db dbm.DB, from, below int64, max int) (int64, error) {
	if from < 1 {
		from = 1
	}
	b := db.NewBatch()
	defer b.Close()
	h := from
	for ; h < below && h < from+int64(max); h++ {
		for _, k := range []string{"C:%d", "EC:%d", "SC:%d"} {
			if err := b.Delete([]byte(fmt.Sprintf(k, h))); err != nil {
				return from, err
			}
		}
	}
	if h == from {
		return from, nil
	}
	if err := b.Write(); err != nil {
		return from, err
	}
	return h, nil
}

// dropHeaders deletes the headers of the heights from..below-1 once out of
// the evidence window (isOld: expired, or already gone); stops at the first
// one still needed. Returns the next height.
func dropHeaders(db dbm.DB, from, below int64, max int, isOld func(int64) (old, gone bool)) (int64, error) {
	if from < 1 {
		from = 1
	}
	b := db.NewBatch()
	defer b.Close()
	h := from
	for ; h < below && h < from+int64(max); h++ {
		old, gone := isOld(h)
		if gone {
			continue
		}
		if !old {
			break
		}
		if err := b.Delete([]byte(fmt.Sprintf("H:%d", h))); err != nil {
			return from, err
		}
	}
	if h == from {
		return from, nil
	}
	if err := b.Write(); err != nil {
		return from, err
	}
	return h, nil
}

// dropHeartbeats deletes from the transaction index the "I am on"
// transactions below the given height (at most max); returns how many.
func dropHeartbeats(db dbm.DB, below int64, max int) (int, error) {
	prefix := []byte("message.action/" + heartbeatMsg + "/")
	it, err := db.Iterator(prefix, prefixEnd(prefix))
	if err != nil {
		return 0, err
	}
	var hashes [][]byte
	for ; it.Valid() && len(hashes) < max; it.Next() {
		h, err := strconv.ParseInt(strings.SplitN(string(it.Key()[len(prefix):]), "/", 2)[0], 10, 64)
		if err != nil || h >= below {
			continue
		}
		hashes = append(hashes, bytes.Clone(it.Value()))
	}
	if err := it.Close(); err != nil {
		return 0, err
	}
	n := 0
	for _, hash := range hashes {
		ok, err := dropHeartbeat(db, hash)
		if err != nil {
			return n, err
		}
		if ok {
			n++
		}
	}
	return n, nil
}

// dropHeartbeat deletes one transaction from the index if it only says "I am
// on": its record, its height entry and every searchable entry it made.
func dropHeartbeat(db dbm.DB, hash []byte) (bool, error) {
	raw, err := db.Get(hash)
	if err != nil || raw == nil {
		return false, err
	}
	var r abci.TxResult
	if err := proto.Unmarshal(raw, &r); err != nil {
		return false, err
	}
	actions := 0
	for _, e := range r.Result.Events {
		for _, a := range e.Attributes {
			if e.Type == "message" && a.Key == "action" {
				if a.Value != heartbeatMsg {
					return false, nil // another message in the same transaction: kept
				}
				actions++
			}
		}
	}
	if actions == 0 {
		return false, nil
	}
	b := db.NewBatch()
	defer b.Close()
	at := fmt.Sprintf("/%d/%d", r.Height, r.Index)
	keys := [][]byte{hash}
	add := func(prefix string) error {
		found, err := entries(db, []byte(prefix))
		keys = append(keys, found...)
		return err
	}
	if err := add("tx.height/" + strconv.FormatInt(r.Height, 10) + at); err != nil {
		return false, err
	}
	for _, e := range r.Result.Events {
		for _, a := range e.Attributes {
			if a.Index && e.Type != "" && a.Key != "" {
				if err := add(e.Type + "." + a.Key + "/" + a.Value + at); err != nil {
					return false, err
				}
			}
		}
	}
	for _, k := range keys {
		if err := b.Delete(k); err != nil {
			return false, err
		}
	}
	return true, b.Write()
}

// entries are the index keys that are exactly prefix, or prefix followed by
// the event number ("$es$…"): never the entry of transaction 12 for 1.
func entries(db dbm.DB, prefix []byte) ([][]byte, error) {
	it, err := db.Iterator(prefix, prefixEnd(prefix))
	if err != nil {
		return nil, err
	}
	defer it.Close()
	var out [][]byte
	for ; it.Valid(); it.Next() {
		rest := it.Key()[len(prefix):]
		if len(rest) == 0 || bytes.HasPrefix(rest, []byte("$es$")) {
			out = append(out, bytes.Clone(it.Key()))
		}
	}
	return out, nil
}

// keepBlockIndex: the block records still searched (scripts/amigos-testes.ps1).
var keepBlockIndex = map[string]bool{
	"transfer_expired.id": true, "transfer_expired.sender": true, "transfer_expired.recipient": true,
}

// dropBlockIndex deletes the block index entries nothing searches, and the
// height entries of deleted blocks (at most max); reports whether it is done.
// A search only finds a block whose height entry is there: the heights of
// the kept records keep theirs.
func dropBlockIndex(db dbm.DB, base int64, max int) (bool, error) {
	prefix := []byte("block_events")
	searched := map[int64]bool{}
	for name := range keepBlockIndex {
		enc, err := orderedcode.Append(nil, name)
		if err != nil {
			return false, err
		}
		p := append(bytes.Clone(prefix), enc...)
		it, err := db.Iterator(p, prefixEnd(p))
		if err != nil {
			return false, err
		}
		for ; it.Valid(); it.Next() {
			var value string
			var h int64
			if _, err := orderedcode.Parse(string(it.Key()[len(p):]), &value, &h); err == nil {
				searched[h] = true
			}
		}
		if err := it.Close(); err != nil {
			return false, err
		}
	}
	it, err := db.Iterator(prefix, prefixEnd(prefix))
	if err != nil {
		return false, err
	}
	var drop [][]byte
	for ; it.Valid() && len(drop) < max; it.Next() {
		key := it.Key()[len(prefix):]
		var name string
		rest, err := orderedcode.Parse(string(key), &name)
		if err != nil {
			continue
		}
		if name == "block.height" {
			var h int64
			if _, err := orderedcode.Parse(rest, &h); err != nil || h >= base || searched[h] {
				continue
			}
		} else if keepBlockIndex[name] {
			continue
		}
		drop = append(drop, bytes.Clone(it.Key()))
	}
	if err := it.Close(); err != nil {
		return false, err
	}
	b := db.NewBatch()
	defer b.Close()
	for _, k := range drop {
		if err := b.Delete(k); err != nil {
			return false, err
		}
	}
	if len(drop) > 0 {
		if err := b.Write(); err != nil {
			return false, err
		}
	}
	return len(drop) < max, nil
}

// dropOldWAL deletes the old files of the consensus log (wal.000, wal.001 …),
// keeping the newest keep besides the one being written ("wal").
func dropOldWAL(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	type file struct {
		name  string
		index int
	}
	var old []file
	for _, e := range entries {
		if n, ok := strings.CutPrefix(e.Name(), "wal."); ok && !e.IsDir() {
			if i, err := strconv.Atoi(n); err == nil {
				old = append(old, file{e.Name(), i})
			}
		}
	}
	sort.Slice(old, func(i, j int) bool { return old[i].index < old[j].index })
	for i := 0; i < len(old)-keep; i++ {
		if err := os.Remove(filepath.Join(dir, old[i].name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func prefixEnd(p []byte) []byte {
	end := bytes.Clone(p)
	for i := len(end) - 1; i >= 0; i-- {
		if end[i] < 0xff {
			end[i]++
			return end[:i+1]
		}
	}
	return nil
}

func compact(db dbm.DB) {
	if c, ok := db.(interface{ Compact(start, end []byte) error }); ok {
		_ = c.Compact(nil, nil)
	}
}

func loadDietCursor(path string) dietCursor {
	var c dietCursor
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

func saveDietCursor(path string, c dietCursor) {
	if b, err := json.Marshal(c); err == nil {
		_ = os.WriteFile(path, b, 0o600)
	}
}

// noteBlockTimes writes down the time of one block in every
// app.BlockTimesEvery from height from to below-1 (the first time, also the
// network's start); returns the next height.
func noteBlockTimes(path string, from, below int64, genesis blockTimeNote, timeOf func(int64) (time.Time, bool)) (int64, error) {
	if from < 1 {
		if !genesis.at.IsZero() {
			if err := app.AppendBlockTime(path, genesis.height, genesis.at); err != nil {
				return from, err
			}
		}
		from = 1
	}
	h := from
	for ; h < below; h++ {
		if h%app.BlockTimesEvery != 0 {
			continue
		}
		if t, ok := timeOf(h); ok {
			if err := app.AppendBlockTime(path, h, t); err != nil {
				return h, err
			}
		}
	}
	return h, nil
}
