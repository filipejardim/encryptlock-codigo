package app

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/spf13/cast"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/baseapp"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"encryptlock/lockkeeper/polygonwatch"
	markettypes "encryptlock/lockkeeper/x/market/types"
)

// T4 — the LockKeepers confirm USDC payments on Polygon.
//
// Each LockKeeper watches Polygon (polygonwatch). In every vote for a block
// (CometBFT vote extension) it lists the open trades it saw paid: the USDC
// reached the seller with the trade's secret. The next block's proposer adds
// up those votes; a trade seen paid by more than 2/3 of the signing power is
// written at the top of the block ("pay trade X with secret S"), together with
// the signed votes, and every LockKeeper checks the sum before accepting the
// block. The trade is then settled before anything else in the block: the
// buyer gets the LOCK. Nobody has to send anything and the seller can be off.

// oraclePrefix marks the votes summary placed first in a block (it is not a
// transaction; decoding it as one fails harmlessly).
var oraclePrefix = []byte("encryptlock/polygon-paid/v1:")

// maxPaidPerVote limits what one vote can list.
const maxPaidPerVote = 200

// paidTrade is one trade seen paid on Polygon.
type paidTrade struct {
	Trade  uint64 `json:"t"`
	Secret string `json:"s"` // hex, 32 bytes
}

type paidVote struct {
	Paid []paidTrade `json:"paid"`
}

type paidSummary struct {
	Settle []paidTrade `json:"settle"`
	// Commit: the signed votes of the previous block (abci.ExtendedCommitInfo).
	Commit []byte `json:"commit"`
}

// defaultPolygonRPCs: independent public Polygon servers per network. A
// payment counts when two of them show it; one down or lying is not enough
// to stop or fake payments (polygonwatch).
var defaultPolygonRPCs = map[uint64][]string{
	// The first two read the payments (both take 2000-block queries); the
	// others confirm them. Servers that stop answering are left out.
	137: {"https://polygon-bor-rpc.publicnode.com", "https://polygon.gateway.tenderly.co",
		"https://rpc-mainnet.matic.quiknode.pro", "https://polygon.drpc.org", "https://polygon-rpc.com",
		"https://1rpc.io/matic"},
	80002: {"https://polygon-amoy-bor-rpc.publicnode.com", "https://rpc-amoy.polygon.technology",
		"https://polygon-amoy.drpc.org"},
}

// polygonWatch starts watching Polygon by itself, the first time it is
// needed, with what the network's first block says (market genesis: the
// Polygon network, the vault and USDC). app.toml [polygon] rpc = "a,b" may
// name other servers (for instance a mini-Polygon on this computer).
type polygonWatch struct {
	mu     *sync.Mutex
	w      *polygonwatch.Watcher
	rpcs   []string
	logger log.Logger
}

func newPolygonWatch(appOpts servertypes.AppOptions, logger log.Logger) polygonWatch {
	var rpcs []string
	for _, r := range strings.Split(cast.ToString(appOpts.Get("polygon.rpc")), ",") {
		if r = strings.TrimSpace(r); r != "" {
			rpcs = append(rpcs, r)
		}
	}
	return polygonWatch{mu: &sync.Mutex{}, rpcs: rpcs, logger: logger}
}

// watcher returns the running watcher, starting it if the network has a
// Polygon side (nil: nothing to watch, this LockKeeper votes nothing).
func (app *LockKeeperApp) watcher(ctx sdk.Context) *polygonwatch.Watcher {
	p := &app.polygon
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.w != nil {
		return p.w
	}
	c, ok, err := app.MarketKeeper.PolygonConfig(ctx)
	if err != nil || !ok {
		return nil
	}
	rpcs := p.rpcs
	if len(rpcs) == 0 {
		rpcs = defaultPolygonRPCs[c.ChainID]
	}
	if len(rpcs) == 0 {
		return nil
	}
	p.w = polygonwatch.New(polygonwatch.Config{RPCs: rpcs, ChainID: c.ChainID, Vault: c.Vault, USDC: c.USDC, StartBlock: c.StartBlock})
	go p.w.Run(context.Background(), 5*time.Second, func(err error) { p.logger.Debug("polygon watch", "err", err) })
	p.logger.Info("watching Polygon for USDC payments", "network", c.ChainID, "servers", len(rpcs))
	return p.w
}

// setOracleHandlers wires the votes into the block life cycle.
func (app *LockKeeperApp) setOracleHandlers(bApp *baseapp.BaseApp) {
	dph := baseapp.NewDefaultProposalHandler(bApp.Mempool(), bApp)
	prepare, process := dph.PrepareProposalHandler(), dph.ProcessProposalHandler()

	app.SetExtendVoteHandler(func(ctx sdk.Context, _ *abci.RequestExtendVote) (*abci.ResponseExtendVote, error) {
		return &abci.ResponseExtendVote{VoteExtension: app.paidVote(ctx)}, nil
	})

	app.SetVerifyVoteExtensionHandler(func(_ sdk.Context, req *abci.RequestVerifyVoteExtension) (*abci.ResponseVerifyVoteExtension, error) {
		if _, err := decodeVote(req.VoteExtension); err != nil {
			return &abci.ResponseVerifyVoteExtension{Status: abci.ResponseVerifyVoteExtension_REJECT}, nil
		}
		return &abci.ResponseVerifyVoteExtension{Status: abci.ResponseVerifyVoteExtension_ACCEPT}, nil
	})

	app.SetPrepareProposal(func(ctx sdk.Context, req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
		res, err := prepare(ctx, req)
		if err != nil || !voteExtensionsOn(ctx) {
			return res, err
		}
		if err := baseapp.ValidateVoteExtensions(ctx, app.StakingKeeper, 0, "", req.LocalLastCommit); err != nil {
			app.Logger().Error("votes on Polygon payments not usable", "err", err)
			return res, nil
		}
		settle := tallyPaid(req.LocalLastCommit)
		if len(settle) == 0 {
			return res, nil
		}
		commit, err := req.LocalLastCommit.Marshal()
		if err != nil {
			return res, nil
		}
		bz, err := json.Marshal(paidSummary{Settle: settle, Commit: commit})
		if err != nil {
			return res, nil
		}
		res.Txs = append([][]byte{append(append([]byte{}, oraclePrefix...), bz...)}, res.Txs...)
		return res, nil
	})

	app.SetProcessProposal(func(ctx sdk.Context, req *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
		reject := &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_REJECT}
		for i, tx := range req.Txs {
			if i > 0 && bytes.HasPrefix(tx, oraclePrefix) {
				return reject, nil
			}
		}
		if len(req.Txs) > 0 && bytes.HasPrefix(req.Txs[0], oraclePrefix) {
			sum, err := decodeSummary(req.Txs[0])
			if err != nil || !voteExtensionsOn(ctx) {
				return reject, nil
			}
			var commit abci.ExtendedCommitInfo
			if err := commit.Unmarshal(sum.Commit); err != nil {
				return reject, nil
			}
			if err := baseapp.ValidateVoteExtensions(ctx, app.StakingKeeper, 0, "", commit); err != nil {
				return reject, nil
			}
			if !samePaid(tallyPaid(commit), sum.Settle) {
				return reject, nil
			}
			rest := *req
			rest.Txs = req.Txs[1:]
			return process(ctx, &rest)
		}
		return process(ctx, req)
	})
}

// voteExtensionsOn: votes carry Polygon payments from the enable height on.
func voteExtensionsOn(ctx sdk.Context) bool {
	cp := ctx.ConsensusParams()
	return cp.Abci != nil && cp.Abci.VoteExtensionsEnableHeight != 0 && ctx.HeaderInfo().Height > cp.Abci.VoteExtensionsEnableHeight
}

// paidVote lists the open trades this LockKeeper saw paid on Polygon.
func (app *LockKeeperApp) paidVote(ctx sdk.Context) []byte {
	w := app.watcher(ctx)
	if w == nil {
		return nil
	}
	var v paidVote
	// Open trades, and unpaid ones still waiting for a late payment
	// (x/market late.go).
	check := func(_ uint64, tr markettypes.Trade) (bool, error) {
		if len(tr.Hash) != 32 {
			return false, nil
		}
		var hash [32]byte
		copy(hash[:], tr.Hash)
		if secret, ok := w.PaidTo(hash, tr.SellerPolygon, new(big.Int).SetUint64(tr.Usdc)); ok {
			v.Paid = append(v.Paid, paidTrade{Trade: tr.Id, Secret: hex.EncodeToString(secret[:])})
		}
		return len(v.Paid) >= maxPaidPerVote, nil
	}
	_ = app.MarketKeeper.Trades.Walk(ctx, nil, check)
	if len(v.Paid) < maxPaidPerVote {
		_ = app.MarketKeeper.Late.Walk(ctx, nil, check)
	}
	if len(v.Paid) == 0 {
		return nil
	}
	bz, _ := json.Marshal(v)
	return bz
}

func decodeVote(bz []byte) (paidVote, error) {
	var v paidVote
	if len(bz) == 0 {
		return v, nil
	}
	if err := json.Unmarshal(bz, &v); err != nil {
		return v, err
	}
	if len(v.Paid) > maxPaidPerVote {
		return v, errors.New("too many")
	}
	for _, p := range v.Paid {
		if s, err := hex.DecodeString(p.Secret); err != nil || len(s) != 32 {
			return v, errors.New("bad secret")
		}
	}
	return v, nil
}

func decodeSummary(tx []byte) (paidSummary, error) {
	var s paidSummary
	err := json.Unmarshal(bytes.TrimPrefix(tx, oraclePrefix), &s)
	return s, err
}

// tallyPaid returns the trades that more than 2/3 of the voting power saw
// paid with the same secret, ordered by trade.
func tallyPaid(commit abci.ExtendedCommitInfo) []paidTrade {
	var total int64
	power := map[paidTrade]int64{}
	for _, vote := range commit.Votes {
		total += vote.Validator.Power
		if vote.BlockIdFlag != cmtproto.BlockIDFlagCommit {
			continue
		}
		v, err := decodeVote(vote.VoteExtension)
		if err != nil {
			continue
		}
		seen := map[paidTrade]bool{}
		for _, p := range v.Paid {
			if !seen[p] {
				seen[p] = true
				power[p] += vote.Validator.Power
			}
		}
	}
	var out []paidTrade
	for p, vp := range power {
		if vp*3 > total*2 {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Trade != out[j].Trade {
			return out[i].Trade < out[j].Trade
		}
		return out[i].Secret < out[j].Secret
	})
	return out
}

func samePaid(a, b []paidTrade) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// settlePaid pays out the trades confirmed at the top of the block. A trade
// that cannot be settled (already settled, reservation over) is skipped.
func (app *LockKeeperApp) settlePaid(ctx sdk.Context, txs [][]byte) {
	if len(txs) == 0 || !bytes.HasPrefix(txs[0], oraclePrefix) {
		return
	}
	sum, err := decodeSummary(txs[0])
	if err != nil {
		return
	}
	for _, p := range sum.Settle {
		secret, err := hex.DecodeString(p.Secret)
		if err != nil {
			continue
		}
		cache, write := ctx.CacheContext()
		if err := app.MarketKeeper.Settle(cache, p.Trade, secret); err != nil {
			app.Logger().Info("Polygon payment not settled", "trade", p.Trade, "err", err)
			continue
		}
		write()
	}
}
