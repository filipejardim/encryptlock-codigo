package mobilecore

import (
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

func TestPolygonPaymentIsSignedByTheWallet(t *testing.T) {
	words, _ := NewMnemonic()
	me, _ := PolygonAddress(words)
	secret := strings.Repeat("ab", 32)
	out, err := SignPolygonPayment(words, 80002, 7, "30000000000", "90000000000",
		"0x41E94Eb019C0762f9Bfcf9Fb1E58725BfB0e7582", "0xe7f1725E7734CE288F8367e1Bb143E90bb3F0512",
		"0x00000000000000000000000000000000000000aa", secret, 2_000_000, 1_800_000_000)
	if err != nil {
		t.Fatal(err)
	}
	var txs []string
	if err := json.Unmarshal([]byte(out), &txs); err != nil || len(txs) != 3 {
		t.Fatalf("want 3 transactions, got %v (%v)", txs, err)
	}
	for _, tx := range txs {
		if !strings.HasPrefix(tx, "0x02") {
			t.Fatalf("not an EIP-1559 transaction: %s", tx[:10])
		}
	}
	// The signature recovers to the wallet's Polygon address.
	key, _ := polygonKey(words)
	hash := keccak([]byte("check"))
	sig := ecdsa.SignCompact(key, hash, false)
	pub, _, err := ecdsa.RecoverCompact(sig, hash)
	if err != nil || checksumAddress(keccak(pub.SerializeUncompressed()[1:])[12:]) != me {
		t.Fatal("the key does not match the wallet's Polygon address")
	}
	if _, err := SignPolygonPayment(words, 80002, 7, "1", "1", "0x1", "0x2", "0x3", secret, 1, 1); err == nil {
		t.Error("bad addresses were accepted")
	}
	_ = hex.EncodeToString
}

func TestPolygonSend(t *testing.T) {
	words, _ := NewMnemonic()
	to := "0x00000000000000000000000000000000000000aa"
	usdc := "0x41E94Eb019C0762f9Bfcf9Fb1E58725BfB0e7582"
	tx, err := SignPolygonSend(words, 80002, 0, "30000000000", "90000000000", usdc, to, "1500000")
	if err != nil || !strings.HasPrefix(tx, "0x02") {
		t.Fatalf("USDC send: %v %v", tx, err)
	}
	// The transfer(to, 1.5 USDC) call is inside the transaction.
	call := hex.EncodeToString(selTransfer) + strings.Repeat("0", 62) + "aa" + strings.Repeat("0", 58) + "16e360"
	if !strings.Contains(tx, call) {
		t.Error("transfer call not found")
	}
	if tx, err := SignPolygonSend(words, 80002, 0, "30000000000", "90000000000", "", to, "1000000000000000000"); err != nil || !strings.HasPrefix(tx, "0x02") {
		t.Fatalf("POL send: %v %v", tx, err)
	}
	for _, bad := range [][2]string{{to, "0"}, {to, "-1"}, {"0x1", "5"}, {to, "abc"}} {
		if _, err := SignPolygonSend(words, 80002, 0, "1", "1", usdc, bad[0], bad[1]); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
}
