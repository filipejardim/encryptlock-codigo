package mobilecore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"strings"

	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"golang.org/x/crypto/sha3"
)

// Paying a trade on Polygon from the wallet ("Buy"): three transactions
// signed with the wallet's Polygon key, sent in this order (consecutive
// nonces, so Polygon runs them in order):
//  1. approve: the vault may take the USDC;
//  2. lock: the USDC go into the vault for the seller, with the hash of the
//     buyer's secret and a deadline (after it, the buyer can take them back);
//  3. claim: the buyer shows the secret and the vault pays the seller. The
//     LockKeepers see this payment and hand the LOCK to the buyer.

// Gas limits, with room to spare (unused gas is not paid).
const (
	gasApprove = 80_000
	gasLock    = 160_000
	gasClaim   = 140_000
	// Sending POL to an address, and USDC with transfer().
	gasPol      = 21_000
	gasTransfer = 100_000
)

// Function selectors (first 4 bytes of keccak256 of the signature).
var (
	selApprove  = selector("approve(address,uint256)")
	selLock     = selector("lock(bytes32,uint256,address,address,uint256)")
	selClaim    = selector("claim(bytes32,uint256,address,address,address,uint256)")
	selTransfer = selector("transfer(address,uint256)")
)

func keccak(b ...[]byte) []byte {
	h := sha3.NewLegacyKeccak256()
	for _, x := range b {
		h.Write(x)
	}
	return h.Sum(nil)
}

func selector(sig string) []byte { return keccak([]byte(sig))[:4] }

func word(b []byte) []byte {
	w := make([]byte, 32)
	copy(w[32-len(b):], b)
	return w
}

func addrBytes(s string) ([]byte, error) {
	b, err := hex.DecodeString(strings.TrimPrefix(strings.ToLower(s), "0x"))
	if err != nil || len(b) != 20 {
		return nil, errors.New("invalid Polygon address " + s)
	}
	return b, nil
}

func bytes32(s string) ([]byte, error) {
	b, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil || len(b) != 32 {
		return nil, errors.New("invalid 32-byte value")
	}
	return b, nil
}

// --- RLP (the encoding of Ethereum-style transactions) ---

func rlpBytes(b []byte) []byte {
	if len(b) == 1 && b[0] < 0x80 {
		return b
	}
	return append(rlpLength(len(b), 0x80), b...)
}

func rlpList(items ...[]byte) []byte {
	var body []byte
	for _, it := range items {
		body = append(body, it...)
	}
	return append(rlpLength(len(body), 0xc0), body...)
}

func rlpLength(n int, offset byte) []byte {
	if n < 56 {
		return []byte{offset + byte(n)}
	}
	l := new(big.Int).SetInt64(int64(n)).Bytes()
	return append([]byte{offset + 55 + byte(len(l))}, l...)
}

func rlpInt(n *big.Int) []byte { return rlpBytes(n.Bytes()) }

// signTx1559 signs an EIP-1559 transaction and returns it as 0x… hex.
func signTx1559(words string, chainID, nonce, tip, feeCap *big.Int, gas uint64, to []byte, value *big.Int, data []byte) (string, error) {
	key, err := polygonKey(words)
	if err != nil {
		return "", err
	}
	fields := [][]byte{
		rlpInt(chainID), rlpInt(nonce), rlpInt(tip), rlpInt(feeCap),
		rlpInt(new(big.Int).SetUint64(gas)), rlpBytes(to), rlpInt(value), rlpBytes(data), rlpList(),
	}
	hash := keccak([]byte{0x02}, rlpList(fields...))
	sig := ecdsa.SignCompact(key, hash, false) // [27+recovery id, r, s]
	signed := append(fields, rlpInt(big.NewInt(int64(sig[0]-27))), rlpInt(new(big.Int).SetBytes(sig[1:33])), rlpInt(new(big.Int).SetBytes(sig[33:65])))
	return "0x" + hex.EncodeToString(append([]byte{0x02}, rlpList(signed...)...)), nil
}

func bigArg(s, name string) (*big.Int, error) {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok || n.Sign() < 0 {
		return nil, errors.New("invalid " + name)
	}
	return n, nil
}

// SignPolygonPayment signs the three transactions that pay a trade on
// Polygon and returns them as a JSON list of 0x… hex, in sending order.
//
// chainID, nonce: Polygon chain and the wallet's next nonce; tipWei and
// feeCapWei: EIP-1559 fees (decimal text); usdc, vault, seller: 0x
// addresses; secretHex: the buyer's 32-byte secret (its sha256 is the
// trade's hash); amount: USDC units (6 decimals); timelock: the vault
// deadline (Unix seconds), at most the trade's pay-by time.
func SignPolygonPayment(words string, chainID, nonce int64, tipWei, feeCapWei, usdc, vault, seller, secretHex string, amount, timelock int64) (string, error) {
	tip, err := bigArg(tipWei, "tip")
	if err != nil {
		return "", err
	}
	feeCap, err := bigArg(feeCapWei, "fee cap")
	if err != nil {
		return "", err
	}
	usdcB, err := addrBytes(usdc)
	if err != nil {
		return "", err
	}
	vaultB, err := addrBytes(vault)
	if err != nil {
		return "", err
	}
	sellerB, err := addrBytes(seller)
	if err != nil {
		return "", err
	}
	secret, err := bytes32(secretHex)
	if err != nil {
		return "", err
	}
	if amount <= 0 || timelock <= 0 {
		return "", errors.New("amount and deadline must be more than 0")
	}
	me, err := PolygonAddress(words)
	if err != nil {
		return "", err
	}
	meB, _ := addrBytes(me)
	hash := sha256Sum(secret)
	amt := word(big.NewInt(amount).Bytes())
	tl := word(big.NewInt(timelock).Bytes())

	approve := append(append(append([]byte{}, selApprove...), word(vaultB)...), amt...)
	lock := append(append([]byte{}, selLock...), hash...)
	lock = append(append(append(append(lock, amt...), word(usdcB)...), word(sellerB)...), tl...)
	claim := append(append([]byte{}, selClaim...), secret...)
	claim = append(append(append(append(append(claim, amt...), word(usdcB)...), word(sellerB)...), word(meB)...), tl...)

	cid := big.NewInt(chainID)
	var out []string
	for i, c := range []struct {
		gas  uint64
		to   []byte
		data []byte
	}{{gasApprove, usdcB, approve}, {gasLock, vaultB, lock}, {gasClaim, vaultB, claim}} {
		tx, err := signTx1559(words, cid, big.NewInt(nonce+int64(i)), tip, feeCap, c.gas, c.to, big.NewInt(0), c.data)
		if err != nil {
			return "", err
		}
		out = append(out, tx)
	}
	bz, _ := json.Marshal(out)
	return string(bz), nil
}

// SignPolygonSend signs one transaction that sends POL or USDC from the
// wallet to another 0x address ("Send" with the coin set to POL or USDC).
//
// token: the USDC contract (0x…), or "" to send POL; amount: decimal text in
// the coin's smallest units (USDC 6 decimals, POL 18). Returns 0x… hex.
func SignPolygonSend(words string, chainID, nonce int64, tipWei, feeCapWei, token, to, amount string) (string, error) {
	tip, err := bigArg(tipWei, "tip")
	if err != nil {
		return "", err
	}
	feeCap, err := bigArg(feeCapWei, "fee cap")
	if err != nil {
		return "", err
	}
	toB, err := addrBytes(to)
	if err != nil {
		return "", err
	}
	amt, err := bigArg(amount, "amount")
	if err != nil {
		return "", err
	}
	if amt.Sign() == 0 {
		return "", errors.New("amount must be more than 0")
	}
	cid, n := big.NewInt(chainID), big.NewInt(nonce)
	if token == "" {
		return signTx1559(words, cid, n, tip, feeCap, gasPol, toB, amt, nil)
	}
	tokenB, err := addrBytes(token)
	if err != nil {
		return "", err
	}
	data := append(append(append([]byte{}, selTransfer...), word(toB)...), word(amt.Bytes())...)
	return signTx1559(words, cid, n, tip, feeCap, gasTransfer, tokenB, big.NewInt(0), data)
}

func sha256Sum(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}

// selRefund: refund(bytes32,uint256,address,address,uint256).
var selRefund = selector("refund(bytes32,uint256,address,address,uint256)")

// SignPolygonRefund signs the transaction that takes back USDC the wallet
// locked in the vault for a trade but did not hand over (the app stopped
// before showing the secret, or the trade's reservation ended first). The
// vault allows it only after the deadline (timelock). hashHex: the trade's
// hash; the other arguments as in SignPolygonPayment. Returns 0x… hex.
func SignPolygonRefund(words string, chainID, nonce int64, tipWei, feeCapWei, usdc, vault, seller, hashHex string, amount, timelock int64) (string, error) {
	tip, err := bigArg(tipWei, "tip")
	if err != nil {
		return "", err
	}
	feeCap, err := bigArg(feeCapWei, "fee cap")
	if err != nil {
		return "", err
	}
	usdcB, err := addrBytes(usdc)
	if err != nil {
		return "", err
	}
	vaultB, err := addrBytes(vault)
	if err != nil {
		return "", err
	}
	sellerB, err := addrBytes(seller)
	if err != nil {
		return "", err
	}
	hash, err := bytes32(hashHex)
	if err != nil {
		return "", err
	}
	if amount <= 0 || timelock <= 0 {
		return "", errors.New("amount and deadline must be more than 0")
	}
	data := append(append([]byte{}, selRefund...), hash...)
	data = append(append(append(append(data, word(big.NewInt(amount).Bytes())...), word(usdcB)...), word(sellerB)...), word(big.NewInt(timelock).Bytes())...)
	return signTx1559(words, big.NewInt(chainID), big.NewInt(nonce), tip, feeCap, gasLock, vaultB, big.NewInt(0), data)
}
