package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
	"unsafe"

	dbm "github.com/cometbft/cometbft-db"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/libs/pubsub/query"
	"github.com/cometbft/cometbft/rpc/client/local"
	rpccore "github.com/cometbft/cometbft/rpc/core"
	sm "github.com/cometbft/cometbft/state"
	blockidx "github.com/cometbft/cometbft/state/indexer/block/kv"
	"github.com/cometbft/cometbft/state/txindex/kv"
	"github.com/cometbft/cometbft/store"
	"github.com/cometbft/cometbft/types"
)

// The clean-up reaches CometBFT's databases through the LockKeeper's RPC
// client: if a new CometBFT renames those hidden fields, this fails.
func TestDietReachesTheStores(t *testing.T) {
	blockDB, txDB := dbm.NewMemDB(), dbm.NewMemDB()
	env := &rpccore.Environment{
		BlockStore: store.NewBlockStore(blockDB),
		TxIndexer:  kv.NewTxIndex(txDB),
		StateStore: sm.NewStore(dbm.NewMemDB(), sm.StoreOptions{}),
	}
	l := &local.Local{}
	f := reflect.ValueOf(l).Elem().FieldByName("env")
	reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem().Set(reflect.ValueOf(env))

	s, err := dietStoresOf(l)
	if err != nil {
		t.Fatal(err)
	}
	if s.blockDB != blockDB || s.txDB != txDB || s.blocks != env.BlockStore || s.state != env.StateStore {
		t.Fatal("not the LockKeeper's databases")
	}
	if _, err := dietStoresOf(nil); err == nil {
		t.Error("no client: should refuse")
	}
}

func TestDropSignaturesBelowTheBaseOnly(t *testing.T) {
	db := dbm.NewMemDB()
	for h := 1; h <= 10; h++ {
		for _, k := range []string{"C:%d", "EC:%d", "SC:%d", "H:%d"} {
			_ = db.Set([]byte(fmt.Sprintf(k, h)), []byte("x"))
		}
	}
	next, err := dropSignatures(db, 0, 8, 4)
	if err != nil || next != 5 {
		t.Fatalf("first round: next %d, %v", next, err)
	}
	if next, err = dropSignatures(db, next, 8, 4); err != nil || next != 8 {
		t.Fatalf("second round: next %d, %v", next, err)
	}
	for h := 1; h <= 10; h++ {
		for _, k := range []string{"C:%d", "EC:%d", "SC:%d"} {
			has, _ := db.Has([]byte(fmt.Sprintf(k, h)))
			if has != (h >= 8) {
				t.Errorf("%s: kept %v", fmt.Sprintf(k, h), has)
			}
		}
		if has, _ := db.Has([]byte(fmt.Sprintf("H:%d", h))); !has {
			t.Errorf("header %d deleted: it proves cheats", h)
		}
	}
}

func TestDropHeadersOnlyOutOfTheEvidenceWindow(t *testing.T) {
	db := dbm.NewMemDB()
	for h := 3; h <= 10; h++ { // 1 and 2 already gone
		_ = db.Set([]byte(fmt.Sprintf("H:%d", h)), []byte("x"))
	}
	isOld := func(h int64) (bool, bool) {
		has, _ := db.Has([]byte(fmt.Sprintf("H:%d", int(h))))
		return h < 6, !has
	}
	next, err := dropHeaders(db, 1, 9, 100, isOld)
	if err != nil || next != 6 {
		t.Fatalf("next %d, %v", next, err)
	}
	for h := 3; h <= 10; h++ {
		has, _ := db.Has([]byte(fmt.Sprintf("H:%d", h)))
		if has != (h >= 6) {
			t.Errorf("header %d kept %v", h, has)
		}
	}
}

func txResult(height int64, index uint32, tx string, events ...abci.Event) *abci.TxResult {
	return &abci.TxResult{Height: height, Index: index, Tx: []byte(tx), Result: abci.ExecTxResult{Events: events}}
}

func event(typ string, kv ...string) abci.Event {
	e := abci.Event{Type: typ}
	for i := 0; i < len(kv); i += 2 {
		e.Attributes = append(e.Attributes, abci.EventAttribute{Key: kv[i], Value: kv[i+1], Index: true})
	}
	return e
}

func TestDropHeartbeatsKeepsEverythingElse(t *testing.T) {
	db := dbm.NewMemDB()
	idx := kv.NewTxIndex(db)
	beat := func(h int64, i uint32, tx string) *abci.TxResult {
		return txResult(h, i, tx, event("tx", "acc_seq", tx), event("message", "action", heartbeatMsg, "sender", "lock1keeper"))
	}
	old1, old12, recent := beat(100, 1, "beat-a"), beat(100, 12, "beat-b"), beat(990, 0, "beat-c")
	send := txResult(100, 0, "send", event("message", "action", "/encryptlock.transfers.v1.MsgAccept", "sender", "lock1bob"),
		event("transfer_accepted", "sender", "lock1alice", "recipient", "lock1bob"))
	mixed := txResult(101, 0, "mixed", event("message", "action", heartbeatMsg), event("message", "action", "/encryptlock.registry.v1.MsgRegister"))
	for _, r := range []*abci.TxResult{old1, old12, recent, send, mixed} {
		if err := idx.Index(r); err != nil {
			t.Fatal(err)
		}
	}
	keysOf := func() map[string]bool {
		out := map[string]bool{}
		it, _ := db.Iterator(nil, nil)
		defer it.Close()
		for ; it.Valid(); it.Next() {
			out[string(it.Key())] = true
		}
		return out
	}
	before := keysOf()

	n, err := dropHeartbeats(db, 900, 100)
	if err != nil || n != 2 {
		t.Fatalf("dropped %d, %v; want the 2 old heartbeats", n, err)
	}
	for _, r := range []*abci.TxResult{old1, old12} {
		if got, _ := idx.Get(types.Tx(r.Tx).Hash()); got != nil {
			t.Errorf("old heartbeat %s still there", r.Tx)
		}
	}
	for _, r := range []*abci.TxResult{recent, send, mixed} {
		if got, _ := idx.Get(types.Tx(r.Tx).Hash()); got == nil {
			t.Errorf("%s was deleted", r.Tx)
		}
	}
	search := func(q string) int {
		res, err := idx.Search(context.Background(), query.MustCompile(q))
		if err != nil {
			t.Fatal(err)
		}
		return len(res)
	}
	if got := search("transfer_accepted.recipient='lock1bob'"); got != 1 {
		t.Errorf("history search finds %d, want 1", got)
	}
	if got := search("message.action='" + heartbeatMsg + "'"); got != 2 { // the recent one and the mixed one
		t.Errorf("heartbeat search finds %d, want 2", got)
	}
	if got := search("tx.height=100"); got != 1 {
		t.Errorf("height 100 finds %d, want only the transfer", got)
	}
	// Every key that went belonged to the two old heartbeats.
	after := keysOf()
	var gone []string
	for k := range before {
		if !after[k] {
			gone = append(gone, k)
		}
	}
	sort.Strings(gone)
	if len(gone) != 2*5 { // per heartbeat: record, height, acc_seq, action, sender
		t.Errorf("deleted %d keys, want 10: %q", len(gone), gone)
	}
}

func TestDropBlockIndexKeepsTheSearchedRecords(t *testing.T) {
	db := dbm.NewMemDB()
	bi := blockidx.New(dbm.NewPrefixDB(db, []byte("block_events")))
	for h := int64(1); h <= 6; h++ {
		ev := []abci.Event{event("coin_received", "receiver", "lock1x", "amount", "5lockunit"), event("rewards", "validator", "v")}
		if h == 2 {
			ev = append(ev, event("transfer_expired", "id", "7", "sender", "lock1alice"))
		}
		if err := bi.Index(types.EventDataNewBlockEvents{Height: h, Events: ev}); err != nil {
			t.Fatal(err)
		}
	}
	done, err := dropBlockIndex(db, 5, 3) // a few per round
	for i := 0; err == nil && !done && i < 100; i++ {
		done, err = dropBlockIndex(db, 5, 3)
	}
	if err != nil || !done {
		t.Fatalf("done %v, %v", done, err)
	}
	search := func(q string) []int64 {
		res, err := bi.Search(context.Background(), query.MustCompile(q))
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if got := search("transfer_expired.id='7' AND transfer_expired.sender='lock1alice'"); len(got) != 1 || got[0] != 2 {
		t.Errorf("expired transfer search = %v, want [2]", got)
	}
	if got := search("coin_received.receiver='lock1x'"); len(got) != 0 {
		t.Errorf("unsearched records kept: %v", got)
	}
	for h := int64(1); h <= 6; h++ {
		has, _ := bi.Has(h)
		if has != (h >= 5 || h == 2) { // 2 has the expired transfer: its height entry stays
			t.Errorf("height %d indexed %v", h, has)
		}
	}
}

func TestDropOldWALKeepsTheNewest(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"wal", "wal.998", "wal.999", "wal.1000", "wal.1001", "other"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := dropOldWAL(dir, 2); err != nil {
		t.Fatal(err)
	}
	var left []string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		left = append(left, e.Name())
	}
	sort.Strings(left)
	if want := []string{"other", "wal", "wal.1000", "wal.1001"}; !reflect.DeepEqual(left, want) {
		t.Errorf("left %v, want %v", left, want)
	}
}

func TestRejoinOnlyWhenTheMissedBlocksAreGone(t *testing.T) {
	const top = 100_000
	earliest := int64(top - keepRecentBlocks)
	for _, c := range []struct {
		last int64
		want bool
	}{
		{top - 5, false},        // restarted after a few seconds
		{top - 300, false},      // off 25 minutes: the blocks are still there
		{earliest + 100, false}, // still more than 5 minutes of margin
		{earliest + 30, true},   // about to be deleted
		{earliest - 1000, true}, // off for the night
		{top, false},            // the network was stopped, nothing missed
	} {
		if got := missedTooMuch(c.last, earliest); got != c.want {
			t.Errorf("last block %d (network keeps from %d): rejoin %v, want %v", c.last, earliest, got, c.want)
		}
	}
}

func TestNoteBlockTimesBeforeTheyGo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "times.csv")
	start := time.Date(2026, 10, 18, 17, 50, 55, 0, time.UTC)
	timeOf := func(h int64) (time.Time, bool) {
		if h < 200 { // headers already gone
			return time.Time{}, false
		}
		return start.Add(time.Duration(h) * time.Second), true
	}
	next, err := noteBlockTimes(path, 0, 450, blockTimeNote{1, start}, timeOf)
	if err != nil || next != 450 {
		t.Fatalf("next %d, %v", next, err)
	}
	if next, err = noteBlockTimes(path, next, 650, blockTimeNote{1, start}, timeOf); err != nil || next != 650 {
		t.Fatalf("second round: next %d, %v", next, err)
	}
	b, _ := os.ReadFile(path)
	want := fmt.Sprintf("1,%d\n200,%d\n300,%d\n400,%d\n500,%d\n600,%d\n", start.Unix(),
		start.Unix()+200, start.Unix()+300, start.Unix()+400, start.Unix()+500, start.Unix()+600)
	if string(b) != want {
		t.Errorf("notes:\n%s\nwant:\n%s", b, want)
	}
}
