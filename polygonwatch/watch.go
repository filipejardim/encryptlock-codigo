// Package polygonwatch lets a LockKeeper see USDC payments on Polygon
// (T4: the LOCK of a trade are handed over only once the USDC reached the
// seller).
//
// It reads the public Polygon network (JSON-RPC), only blocks Polygon calls
// final. A payment counts only when ONE Polygon transaction both reveals the
// secret (the vault's Claim event) AND moves the USDC out of the vault to an
// address (the USDC Transfer event). Seeing the secret alone is not enough:
// someone could reveal it in another vault payment of their own and leave
// the seller unpaid.
//
// With two Polygon servers (the default), every payment must be confirmed by
// both, final on both: one lying or broken server cannot make up a payment.
package polygonwatch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// claimTopic is keccak256("Claim(bytes32,bytes32)") (the vault's event).
	claimTopic = "0x5664142af3dcfc3dc3de45a43f75c746bd1d8c11170a5037fdf98bdb35775137"
	// transferTopic is keccak256("Transfer(address,address,uint256)") (ERC-20).
	transferTopic = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
	// maxRange: how many Polygon blocks one query covers at most; a server
	// that refuses that many gets smaller queries, down to minRange.
	maxRange = 2000
	minRange = 25
)

// Config says where to look. Addresses are 0x… (any case).
type Config struct {
	// RPCs: Polygon JSON-RPC addresses. With two or more, a payment counts
	// only when the second one confirms it too.
	RPCs       []string
	ChainID    uint64 // the Polygon network (137; 80002 Amoy); 0 = not checked
	Vault      string // the HTLC vault contract
	USDC       string // the USDC token contract
	StartBlock uint64 // the vault's first block (nothing to see before)
}

// Payment is USDC paid out of the vault to To in a transaction that revealed
// Preimage (whose sha256 is Hash).
type Payment struct {
	Hash     [32]byte
	Preimage [32]byte
	To       string // 0x… lowercase
	Amount   *big.Int
}

// Watcher keeps the payments seen so far.
type Watcher struct {
	cfg    Config
	client *http.Client

	mu       sync.Mutex
	next     uint64
	span     uint64 // blocks per query now (maxRange, smaller after a refusal)
	payments map[[32]byte][]Payment
	network  map[string]bool // server → on the right network
}

// New creates a watcher; call Run (or Poll) to fill it.
func New(cfg Config) *Watcher {
	cfg.Vault, cfg.USDC = strings.ToLower(cfg.Vault), strings.ToLower(cfg.USDC)
	return &Watcher{cfg: cfg, client: &http.Client{Timeout: 20 * time.Second}, next: cfg.StartBlock, span: maxRange, payments: map[[32]byte][]Payment{}, network: map[string]bool{}}
}

// Run polls every interval until ctx ends. Errors only delay the next round.
func (w *Watcher) Run(ctx context.Context, interval time.Duration, logErr func(error)) {
	for {
		if err := w.Poll(ctx); err != nil && logErr != nil {
			logErr(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

// PaidTo returns the secret of a payment of at least min USDC to `to` for
// this hash, if one was seen.
func (w *Watcher) PaidTo(hash [32]byte, to string, min *big.Int) ([32]byte, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	to = strings.ToLower(to)
	for _, p := range w.payments[hash] {
		if p.To == to && p.Amount.Cmp(min) >= 0 {
			return p.Preimage, true
		}
	}
	return [32]byte{}, false
}

// Poll reads the final blocks not read yet. Servers that do not answer are
// left out this round; with two or more servers, at least two must answer.
func (w *Watcher) Poll(ctx context.Context) error {
	live, last := w.liveServers(ctx)
	if len(live) == 0 || (len(w.cfg.RPCs) > 1 && len(live) < 2) {
		return errors.New("not enough Polygon servers answer")
	}
	w.mu.Lock()
	from := w.next
	w.mu.Unlock()
	for from <= last {
		to := min(from+w.span-1, last)
		if err := w.scan(ctx, live, from, to); err != nil {
			// Some free servers refuse long queries: ask again for fewer
			// blocks (otherwise the watch would stop on the same query).
			if w.span > minRange && to > from {
				w.span = max(w.span/2, minRange)
				continue
			}
			return err
		}
		w.mu.Lock()
		w.next = to + 1
		w.mu.Unlock()
		from = to + 1
	}
	return nil
}

// liveServers returns the servers that answer on the right network and the
// lowest final block among them (only blocks final everywhere count).
func (w *Watcher) liveServers(ctx context.Context) ([]string, uint64) {
	var live []string
	var last uint64
	for _, rpc := range w.cfg.RPCs {
		if !w.rightNetwork(ctx, rpc) {
			continue
		}
		var final struct {
			Number string `json:"number"`
		}
		if err := w.callAt(ctx, rpc, "eth_getBlockByNumber", []any{"finalized", false}, &final); err != nil {
			continue
		}
		n, err := parseQuantity(final.Number)
		if err != nil {
			continue
		}
		if len(live) == 0 || n.Uint64() < last {
			last = n.Uint64()
		}
		live = append(live, rpc)
	}
	return live, last
}

type rpcLog struct {
	Address string   `json:"address"`
	Topics  []string `json:"topics"`
	Data    string   `json:"data"`
	TxHash  string   `json:"transactionHash"`
	Removed bool     `json:"removed"`
}

// scan reads the vault's Claim logs from two servers (one hiding payments is
// not enough) and keeps each payment another server confirms.
func (w *Watcher) scan(ctx context.Context, live []string, from, to uint64) error {
	filter := map[string]any{
		"address":   w.cfg.Vault,
		"topics":    []any{claimTopic},
		"fromBlock": fmt.Sprintf("0x%x", from),
		"toBlock":   fmt.Sprintf("0x%x", to),
	}
	type found struct{ tx, server string }
	var txs []found
	seen := map[string]bool{}
	for _, rpc := range live[:min(2, len(live))] {
		var logs []rpcLog
		if err := w.callAt(ctx, rpc, "eth_getLogs", []any{filter}, &logs); err != nil {
			return err
		}
		for _, l := range logs {
			if !l.Removed && !seen[l.TxHash] {
				seen[l.TxHash] = true
				txs = append(txs, found{l.TxHash, rpc})
			}
		}
	}
	for _, f := range txs {
		p, ok, err := w.receipt(ctx, f.server, f.tx)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		// Another server must show the same payment (alone, only with a
		// single configured server, such as a mini-Polygon for tests).
		agree := len(live) == 1
		answered := false
		for _, rpc := range live {
			if rpc == f.server || agree {
				continue
			}
			q, ok, err := w.receipt(ctx, rpc, f.tx)
			if err != nil {
				continue
			}
			answered = true
			agree = ok && q.Hash == p.Hash && q.Preimage == p.Preimage && q.To == p.To && q.Amount.Cmp(p.Amount) == 0
		}
		if !agree && !answered {
			return errors.New("no second Polygon server to confirm a payment")
		}
		if agree {
			w.mu.Lock()
			w.payments[p.Hash] = append(w.payments[p.Hash], p)
			w.mu.Unlock()
		}
	}
	return nil
}

// receipt reads a transaction from one server and the payment in it.
func (w *Watcher) receipt(ctx context.Context, rpc, tx string) (Payment, bool, error) {
	var r struct {
		Status string   `json:"status"`
		Logs   []rpcLog `json:"logs"`
	}
	if err := w.callAt(ctx, rpc, "eth_getTransactionReceipt", []any{tx}, &r); err != nil {
		return Payment{}, false, err
	}
	if r.Status != "0x1" {
		return Payment{}, false, nil
	}
	p, ok := w.payment(r.Logs)
	return p, ok, nil
}

// payment pairs, inside one transaction, the revealed secret with the USDC
// that left the vault.
func (w *Watcher) payment(logs []rpcLog) (Payment, bool) {
	type out struct {
		to     string
		amount *big.Int
	}
	var secrets [][2][32]byte
	var outs []out
	for _, l := range logs {
		addr := strings.ToLower(l.Address)
		switch {
		case addr == w.cfg.Vault && len(l.Topics) == 2 && strings.EqualFold(l.Topics[0], claimTopic):
			hash, err1 := word(l.Topics[1])
			pre, err2 := word(l.Data)
			if err1 == nil && err2 == nil && sha256.Sum256(pre[:]) == hash {
				secrets = append(secrets, [2][32]byte{hash, pre})
			}
		case addr == w.cfg.USDC && len(l.Topics) == 3 && strings.EqualFold(l.Topics[0], transferTopic):
			from, err1 := word(l.Topics[1])
			toW, err2 := word(l.Topics[2])
			amount, err3 := parseQuantity(l.Data)
			if err1 != nil || err2 != nil || err3 != nil || !bytes.Equal(from[12:], mustAddr(w.cfg.Vault)) {
				continue
			}
			outs = append(outs, out{to: "0x" + hex.EncodeToString(toW[12:]), amount: amount})
		}
	}
	// Only a plain claim counts: one secret and one payment out of the vault
	// in the transaction. A batch claim is ignored: one payment could
	// otherwise be counted for two trades of the same seller.
	if len(secrets) != 1 || len(outs) != 1 {
		return Payment{}, false
	}
	s, o := secrets[0], outs[0]
	return Payment{Hash: s[0], Preimage: s[1], To: o.to, Amount: o.amount}, true
}

// rightNetwork checks, once per server, that it is on the configured Polygon
// network (a server on another network is never used).
func (w *Watcher) rightNetwork(ctx context.Context, rpc string) bool {
	if w.cfg.ChainID == 0 {
		return true
	}
	w.mu.Lock()
	ok, known := w.network[rpc]
	w.mu.Unlock()
	if known {
		return ok
	}
	var id string
	if err := w.callAt(ctx, rpc, "eth_chainId", []any{}, &id); err != nil {
		return false // try again next round
	}
	n, err := parseQuantity(id)
	ok = err == nil && n.Uint64() == w.cfg.ChainID
	w.mu.Lock()
	w.network[rpc] = ok
	w.mu.Unlock()
	return ok
}

func mustAddr(a string) []byte {
	b, _ := hex.DecodeString(strings.TrimPrefix(a, "0x"))
	return b
}

func word(s string) ([32]byte, error) {
	var w [32]byte
	b, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil || len(b) < 32 {
		return w, errors.New("not a 32-byte word")
	}
	copy(w[:], b[:32])
	return w, nil
}

func parseQuantity(s string) (*big.Int, error) {
	n, ok := new(big.Int).SetString(strings.TrimPrefix(s, "0x"), 16)
	if !ok {
		return nil, fmt.Errorf("bad number %q", s)
	}
	return n, nil
}

// call asks the first server.
func (w *Watcher) call(ctx context.Context, method string, params []any, out any) error {
	if len(w.cfg.RPCs) == 0 {
		return errors.New("no Polygon server")
	}
	return w.callAt(ctx, w.cfg.RPCs[0], method, params, out)
}

func (w *Watcher) callAt(ctx context.Context, rpc, method string, params []any, out any) error {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rpc, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("polygon %s: HTTP %d", method, res.StatusCode)
	}
	var r struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 32<<20)).Decode(&r); err != nil {
		return err
	}
	if r.Error != nil {
		return fmt.Errorf("polygon %s: %s", method, r.Error.Message)
	}
	return json.Unmarshal(r.Result, out)
}
