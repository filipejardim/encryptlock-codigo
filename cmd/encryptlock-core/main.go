// Command encryptlock-core is the wallet core for the computer app: the same
// Go code as the phone (package mobilecore) and the network. The app starts
// it and talks to it through stdin/stdout, one JSON request per line:
//
//	{"id":1,"method":"address","args":{"words":"…"}}
//	{"id":1,"result":"lock1…"}            or  {"id":1,"error":"…"}
//
// It keeps nothing: no file, no network. The words only pass through memory
// for the one call that needs them.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	core "encryptlock/lockkeeper/mobilecore"
)

type request struct {
	ID     int64          `json:"id"`
	Method string         `json:"method"`
	Args   map[string]any `json:"args"`
}

type response struct {
	ID     int64  `json:"id"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// On the computer the record is only password-encrypted (Argon2id) here;
// Windows then encrypts it again for this user (DPAPI, flutter_secure_storage).
const pcPrefix = "pc1:"

type args map[string]any

func (a args) s(k string) string {
	v, _ := a[k].(string)
	return v
}

func (a args) i(k string) int64 {
	switch v := a[k].(type) {
	case float64:
		return int64(v)
	case string:
		var n int64
		fmt.Sscan(v, &n)
		return n
	}
	return 0
}

func call(method string, a args) (any, error) {
	w, c, acc, seq := a.s("words"), a.s("chainId"), a.s("accountNumber"), a.i("sequence")
	switch method {
	case "newMnemonic":
		return core.NewMnemonic()
	case "validateMnemonic":
		return core.ValidateMnemonic(a.s("words")), nil
	case "polygonAddress":
		return core.PolygonAddress(a.s("words"))
	case "address":
		return core.Address(w)
	case "validatorAddress":
		return core.ValidatorAddress(a.s("address"))
	case "isAddress":
		return core.IsAddress(a.s("address")), nil
	case "parseLock":
		return core.ParseLock(a.s("text"))
	case "transferFee":
		return core.TransferFee(a.i("units")), nil
	case "sealSecret":
		return core.SealWords(a.s("text"), a.s("password"))
	case "openSecret":
		return core.OpenWords(a.s("record"), a.s("password"))
	case "sealWords":
		r, err := core.SealWords(w, a.s("password"))
		return pcPrefix + r, err
	case "openWords":
		return core.OpenWords(strings.TrimPrefix(a.s("record"), pcPrefix), a.s("password"))
	case "signRegister":
		return core.SignRegister(w, c, acc, seq)
	case "signSend":
		return core.SignSend(w, c, acc, seq, a.s("to"), a.i("units"))
	case "signAccept":
		return core.SignAccept(w, c, acc, seq, a.i("id"))
	case "signCancel":
		return core.SignCancel(w, c, acc, seq, a.i("id"))
	case "signFreeze":
		return core.SignFreeze(w, c, acc, seq)
	case "signUnfreeze":
		return core.SignUnfreeze(w, c, acc, seq)
	case "signFreezeFor":
		return core.SignFreezeFor(w, c, acc, seq, a.s("account"))
	case "signSetDevice":
		return core.SignSetDevice(w, c, acc, seq, a.s("device"))
	case "signSetLimits":
		return core.SignSetLimits(w, c, acc, seq, a.i("dailyLimit"), a.i("approvalAbove"))
	case "signApprove":
		return core.SignApprove(w, c, acc, seq, a.i("id"))
	case "signSetPlan":
		return core.SignSetPlan(w, c, acc, seq, a.s("plan"))
	case "signAlive":
		return core.SignAlive(w, c, acc, seq)
	case "signCancelClaims":
		return core.SignCancelClaims(w, c, acc, seq)
	case "signClaimInheritance":
		return core.SignClaimInheritance(w, c, acc, seq, a.s("account"))
	case "signApproveOffer":
		return core.SignApproveOffer(w, c, acc, seq, a.i("id"))
	case "signApproveRecovery":
		return core.SignApproveRecovery(w, c, acc, seq, a.s("account"), a.s("newAddress"))
	case "signSetVault":
		return core.SignSetVault(w, c, acc, seq, a.i("delayDays"))
	case "signCollectRewards":
		return core.SignCollectRewards(w, c, acc, seq)
	case "newSwapSecret":
		return core.NewSwapSecret()
	case "swapHash":
		return core.SwapHash(a.s("secret"))
	case "signPlaceOffer":
		return core.SignPlaceOffer(w, c, acc, seq, a.i("side"), a.i("lock"), a.i("usdc"), a.s("polygon"), a.s("hash"), a.i("expiresAt"))
	case "signCancelOffer":
		return core.SignCancelOffer(w, c, acc, seq, a.i("id"))
	case "signTakeOffer":
		return core.SignTakeOffer(w, c, acc, seq, a.i("id"), a.s("polygon"), a.s("hash"))
	case "signLock":
		return core.SignLock(w, c, acc, seq, a.i("units"), a.i("until"))
	case "signBecomeLockKeeper":
		return core.SignBecomeLockKeeper(w, c, acc, seq, a.s("consPubKey"), a.i("units"), a.s("moniker"), a.s("helper"))
	case "signPolygonSend":
		return core.SignPolygonSend(w, a.i("polygonChainId"), a.i("nonce"), a.s("tipWei"), a.s("feeCapWei"), a.s("token"), a.s("to"), a.s("amount"))
	case "signPolygonPayment":
		return core.SignPolygonPayment(w, a.i("polygonChainId"), a.i("nonce"), a.s("tipWei"), a.s("feeCapWei"), a.s("usdc"), a.s("vault"), a.s("seller"), a.s("secret"), a.i("amount"), a.i("timelock"))
	case "signPolygonRefund":
		return core.SignPolygonRefund(w, a.i("polygonChainId"), a.i("nonce"), a.s("tipWei"), a.s("feeCapWei"), a.s("usdc"), a.s("vault"), a.s("seller"), a.s("hash"), a.i("amount"), a.i("timelock"))
	case "signHeartbeat":
		return core.SignHeartbeat(w, c, acc, seq)
	}
	return nil, errors.New("unknown method " + method)
}

func main() {
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1<<20), 1<<20)
	out := json.NewEncoder(os.Stdout)
	for in.Scan() {
		var req request
		if err := json.Unmarshal(in.Bytes(), &req); err != nil {
			out.Encode(response{Error: err.Error()})
			continue
		}
		res, err := call(req.Method, req.Args)
		r := response{ID: req.ID, Result: res}
		if err != nil {
			r.Result, r.Error = nil, err.Error()
		}
		out.Encode(r)
	}
}
