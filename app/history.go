package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/gorilla/mux"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// A wallet's last accepted transfers, for the app's history (22 Oct 2026).
// The REST search (/cosmos/tx/v1beta1/txs) opens the block of every result
// to give its time, and a LockKeeper keeps only its last blocks: as soon as
// one of a wallet's last transfers was older, the whole search failed and
// the app showed an empty history. This reads the transaction index (never
// deleted) and opens no block: the time comes from the block while it is
// kept, and for an older one it is placed between the notes of block times
// (blocktimes.go), marked "estimated".

// PathHistory is the history of one wallet, newest first.
const PathHistory = "/encryptlock/history/v1/{address}"

const (
	historyDefault = 10
	historyMax     = 50
)

// historySource is the part of the LockKeeper's CometBFT client it uses.
type historySource interface {
	Status(context.Context) (*coretypes.ResultStatus, error)
	TxSearch(ctx context.Context, query string, prove bool, page, perPage *int, orderBy string) (*coretypes.ResultTxSearch, error)
	BlockchainInfo(ctx context.Context, minHeight, maxHeight int64) (*coretypes.ResultBlockchainInfo, error)
}

// HistoryTransfer is one accepted transfer, as the app reads it.
type HistoryTransfer struct {
	ID        string    `json:"id"`
	Incoming  bool      `json:"incoming"`
	Other     string    `json:"other"`
	Amount    string    `json:"amount"`
	Time      time.Time `json:"time"`
	Estimated bool      `json:"estimated,omitempty"`
}

func historyHandler(src func() historySource, times *blockTimes) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fail := func(code int, err error) {
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		}
		address := mux.Vars(r)["address"]
		if _, err := sdk.AccAddressFromBech32(address); err != nil {
			fail(http.StatusBadRequest, err)
			return
		}
		limit := historyDefault
		if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 {
			limit = min(n, historyMax)
		}
		s := src()
		if s == nil {
			fail(http.StatusServiceUnavailable, fmt.Errorf("the LockKeeper is starting"))
			return
		}
		list, err := history(r.Context(), s, times.notes(), address, limit)
		if err != nil {
			fail(http.StatusInternalServerError, err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"transfers": list})
	}
}

func history(ctx context.Context, s historySource, notes []blockTime, address string, limit int) ([]HistoryTransfer, error) {
	status, err := s.Status(ctx)
	if err != nil {
		return nil, err
	}
	sync := status.SyncInfo
	perBlock := float64(0)
	if sync.LatestBlockHeight > sync.EarliestBlockHeight {
		perBlock = sync.LatestBlockTime.Sub(sync.EarliestBlockTime).Seconds() / float64(sync.LatestBlockHeight-sync.EarliestBlockHeight)
	}
	notes = append(append([]blockTime(nil), notes...), blockTime{sync.EarliestBlockHeight, sync.EarliestBlockTime})
	at := func(height int64) (time.Time, bool) {
		if height >= sync.EarliestBlockHeight {
			if info, err := s.BlockchainInfo(ctx, height, height); err == nil && len(info.BlockMetas) == 1 {
				return info.BlockMetas[0].Header.Time, false
			}
		}
		if t, ok := estimateTime(notes, height); ok {
			return t, true
		}
		return sync.EarliestBlockTime.Add(time.Duration(float64(height-sync.EarliestBlockHeight) * perBlock * float64(time.Second))), true
	}

	var out []HistoryTransfer
	page := 1
	for _, role := range []string{"recipient", "sender"} {
		res, err := s.TxSearch(ctx, fmt.Sprintf("transfer_accepted.%s='%s'", role, address), false, &page, &limit, "desc")
		if err != nil {
			return nil, err
		}
		for _, tx := range res.Txs {
			for _, ev := range tx.TxResult.Events {
				if ev.Type != "transfer_accepted" {
					continue
				}
				a := map[string]string{}
				for _, x := range ev.Attributes {
					a[x.Key] = x.Value
				}
				if a[role] != address {
					continue
				}
				incoming := role == "recipient"
				other := a["recipient"]
				if incoming {
					other = a["sender"]
				}
				t, estimated := at(tx.Height)
				out = append(out, HistoryTransfer{ID: a["id"], Incoming: incoming, Other: other, Amount: a["amount"], Time: t, Estimated: estimated})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
