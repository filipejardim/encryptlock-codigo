package polygonwatch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	vault  = "0x1111111111111111111111111111111111111111"
	usdc   = "0x2222222222222222222222222222222222222222"
	seller = "0x3333333333333333333333333333333333333333"
	thief  = "0x4444444444444444444444444444444444444444"
)

func pad(addr string) string { return "0x" + strings.Repeat("0", 24) + strings.TrimPrefix(addr, "0x") }

func amount(n int64) string { return fmt.Sprintf("0x%064x", n) }

func secret(b byte) (pre, hash [32]byte) {
	pre[0] = b
	return pre, sha256.Sum256(pre[:])
}

func claimLog(pre, hash [32]byte) rpcLog {
	return rpcLog{Address: vault, Topics: []string{claimTopic, "0x" + hex.EncodeToString(hash[:])}, Data: "0x" + hex.EncodeToString(pre[:])}
}

func transferLog(from, to string, n int64) rpcLog {
	return rpcLog{Address: usdc, Topics: []string{transferTopic, pad(from), pad(to)}, Data: amount(n)}
}

// fakePolygon answers like a Polygon node with these transactions (one
// Claim log each, in block 5).
func fakePolygon(t *testing.T, txs map[string][]rpcLog) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
			Params []json.RawMessage
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var result any
		switch req.Method {
		case "eth_getBlockByNumber":
			result = map[string]string{"number": "0x10"}
		case "eth_getLogs":
			var logs []rpcLog
			for hash, ls := range txs {
				for _, l := range ls {
					if strings.EqualFold(l.Address, vault) {
						l.TxHash = hash
						logs = append(logs, l)
						break
					}
				}
			}
			result = logs
		case "eth_getTransactionReceipt":
			var hash string
			_ = json.Unmarshal(req.Params[0], &hash)
			result = map[string]any{"status": "0x1", "logs": txs[hash]}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
	}))
}

func TestOnlyRealPaymentsToTheSellerCount(t *testing.T) {
	good, goodHash := secret(1)
	trick, trickHash := secret(2)
	batchA, batchAHash := secret(3)
	batchB, batchBHash := secret(4)
	server := fakePolygon(t, map[string][]rpcLog{
		// Paid: the secret and 10 USDC from the vault to the seller.
		"0xa": {claimLog(good, goodHash), transferLog(vault, seller, 10_000_000)},
		// Trick: the secret is revealed, but the USDC went to someone else.
		"0xb": {claimLog(trick, trickHash), transferLog(vault, thief, 10_000_000)},
		// Batch: two secrets, two payments — ignored.
		"0xc": {claimLog(batchA, batchAHash), claimLog(batchB, batchBHash), transferLog(vault, seller, 10_000_000), transferLog(vault, thief, 1)},
	})
	defer server.Close()

	w := New(Config{RPCs: []string{server.URL}, Vault: strings.ToUpper(vault[:2]) + vault[2:], USDC: usdc})
	if err := w.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	ten := big.NewInt(10_000_000)
	if pre, ok := w.PaidTo(goodHash, strings.ToUpper(seller), ten); !ok || pre != good {
		t.Error("the real payment to the seller was not seen")
	}
	if _, ok := w.PaidTo(goodHash, seller, big.NewInt(10_000_001)); ok {
		t.Error("a payment below the price was accepted")
	}
	if _, ok := w.PaidTo(trickHash, seller, ten); ok {
		t.Error("a secret revealed without paying the seller was accepted")
	}
	if _, ok := w.PaidTo(batchAHash, seller, ten); ok {
		t.Error("a batch claim was accepted")
	}
}

// One lying server cannot make up a payment: the second must show it too.
func TestSecondServerMustConfirm(t *testing.T) {
	good, goodHash := secret(1)
	fake, fakeHash := secret(5)
	paid := map[string][]rpcLog{"0xa": {claimLog(good, goodHash), transferLog(vault, seller, 10_000_000)}}
	liar := fakePolygon(t, map[string][]rpcLog{
		"0xa": paid["0xa"],
		"0xf": {claimLog(fake, fakeHash), transferLog(vault, seller, 10_000_000)},
	})
	defer liar.Close()
	honest := fakePolygon(t, paid)
	defer honest.Close()

	w := New(Config{RPCs: []string{liar.URL, honest.URL}, Vault: vault, USDC: usdc})
	if err := w.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	ten := big.NewInt(10_000_000)
	if _, ok := w.PaidTo(goodHash, seller, ten); !ok {
		t.Error("a payment both servers show was not seen")
	}
	if _, ok := w.PaidTo(fakeHash, seller, ten); ok {
		t.Error("a payment only one server shows was accepted")
	}
}

// One server down does not stop payments; one server hiding them neither.
func TestServerDownOrHidingIsNotEnough(t *testing.T) {
	good, goodHash := secret(1)
	paid := map[string][]rpcLog{"0xa": {claimLog(good, goodHash), transferLog(vault, seller, 10_000_000)}}
	hider := fakePolygon(t, map[string][]rpcLog{}) // shows nothing
	defer hider.Close()
	honest := fakePolygon(t, paid)
	defer honest.Close()
	honest2 := fakePolygon(t, paid)
	defer honest2.Close()

	w := New(Config{RPCs: []string{"http://127.0.0.1:1", hider.URL, honest.URL, honest2.URL}, Vault: vault, USDC: usdc})
	if err := w.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := w.PaidTo(goodHash, seller, big.NewInt(10_000_000)); !ok {
		t.Error("the payment was missed with one server down and one hiding it")
	}
}
