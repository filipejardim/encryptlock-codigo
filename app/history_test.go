package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/gorilla/mux"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// fakeIndex: blocks 1000..2000 are kept (one every 2 seconds); the index
// still has older transactions, as a real LockKeeper after a day.
type fakeIndex struct {
	start time.Time
	txs   map[string][]*coretypes.ResultTx
}

func (f fakeIndex) Status(context.Context) (*coretypes.ResultStatus, error) {
	return &coretypes.ResultStatus{SyncInfo: coretypes.SyncInfo{
		EarliestBlockHeight: 1000, EarliestBlockTime: f.start,
		LatestBlockHeight: 2000, LatestBlockTime: f.start.Add(2000 * time.Second),
	}}, nil
}

func (f fakeIndex) TxSearch(_ context.Context, query string, _ bool, _, perPage *int, _ string) (*coretypes.ResultTxSearch, error) {
	txs := f.txs[query]
	if len(txs) > *perPage {
		txs = txs[:*perPage]
	}
	return &coretypes.ResultTxSearch{Txs: txs, TotalCount: len(txs)}, nil
}

func (f fakeIndex) BlockchainInfo(_ context.Context, low, _ int64) (*coretypes.ResultBlockchainInfo, error) {
	if low < 1000 {
		return nil, context.DeadlineExceeded // deleted block
	}
	at := f.start.Add(time.Duration(low-1000) * 2 * time.Second)
	return &coretypes.ResultBlockchainInfo{BlockMetas: []*cmttypes.BlockMeta{{Header: cmttypes.Header{Height: low, Time: at}}}}, nil
}

func accepted(height int64, id, sender, recipient, amount string) *coretypes.ResultTx {
	attr := func(k, v string) abci.EventAttribute { return abci.EventAttribute{Key: k, Value: v} }
	return &coretypes.ResultTx{Height: height, TxResult: abci.ExecTxResult{Events: []abci.Event{
		{Type: "message", Attributes: []abci.EventAttribute{attr("sender", recipient)}},
		{Type: "transfer_accepted", Attributes: []abci.EventAttribute{attr("id", id), attr("sender", sender), attr("recipient", recipient), attr("amount", amount)}},
	}}}
}

func TestHistoryKeepsTransfersOlderThanTheKeptBlocks(t *testing.T) {
	me := sdk.AccAddress(strings.Repeat("m", 20)).String()
	other := sdk.AccAddress(strings.Repeat("o", 20)).String()
	start := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	f := fakeIndex{start: start, txs: map[string][]*coretypes.ResultTx{
		"transfer_accepted.recipient='" + me + "'": {accepted(1500, "7", other, me, "300")},
		"transfer_accepted.sender='" + me + "'":    {accepted(400, "3", me, other, "50")}, // block deleted
	}}
	r := mux.NewRouter()
	r.Handle(PathHistory, historyHandler(func() historySource { return f }, &blockTimes{path: filepath.Join(t.TempDir(), BlockTimesFile)}))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/encryptlock/history/v1/"+me, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var got struct{ Transfers []HistoryTransfer }
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Transfers) != 2 {
		t.Fatalf("got %d transfers, want 2: %s", len(got.Transfers), w.Body)
	}
	recent, old := got.Transfers[0], got.Transfers[1]
	if recent.ID != "7" || !recent.Incoming || recent.Other != other || recent.Amount != "300" || recent.Estimated ||
		!recent.Time.Equal(start.Add(1000*time.Second)) {
		t.Errorf("recent = %+v", recent)
	}
	// 600 blocks before the earliest kept one, at the kept day's 2 s average.
	if old.ID != "3" || old.Incoming || old.Other != other || !old.Estimated || !old.Time.Equal(start.Add(-1200*time.Second)) {
		t.Errorf("old = %+v", old)
	}

	bad := httptest.NewRecorder()
	r.ServeHTTP(bad, httptest.NewRequest(http.MethodGet, "/encryptlock/history/v1/x'%20OR%201=1", nil))
	if bad.Code != http.StatusBadRequest {
		t.Errorf("a wrong address answered %d", bad.Code)
	}
}
