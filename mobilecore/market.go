package mobilecore

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	markettypes "encryptlock/lockkeeper/x/market/types"
)

// NewSwapSecret makes the secret of a trade (32 random bytes, hex). Whoever
// shows it on Polygon pays the seller: it can only send the USDC to the
// seller, so it is not a key to anyone's money.
func NewSwapSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// SwapHash is the sha256 of a secret (hex in, hex out), the trade's lock on
// both networks.
func SwapHash(secretHex string) (string, error) {
	b, err := hex.DecodeString(secretHex)
	if err != nil || len(b) != 32 {
		return "", errors.New("invalid secret")
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func hashBytes(hashHex string) ([]byte, error) {
	if hashHex == "" {
		return nil, nil
	}
	b, err := hex.DecodeString(hashHex)
	if err != nil || len(b) != 32 {
		return nil, errors.New("invalid hash")
	}
	return b, nil
}

// SignPlaceOffer publishes an offer: side 1 = sell LOCK, 2 = buy LOCK; lock in
// lockunit, usdc in USDC units (6 decimals); polygon is the maker's 0x
// address; hashHex only for buy offers; expiresAt Unix seconds (0 = until
// canceled).
func SignPlaceOffer(words, chainID, accountNumber string, sequence, side, lock, usdc int64, polygon, hashHex string, expiresAt int64) (string, error) {
	if lock <= 0 || usdc <= 0 {
		return "", errors.New("amounts must be more than 0")
	}
	hash, err := hashBytes(hashHex)
	if err != nil {
		return "", err
	}
	return signMsg(words, chainID, accountNumber, sequence, func(from string) sdk.Msg {
		return &markettypes.MsgPlaceOffer{Maker: from, Side: markettypes.Side(side), Lock: uint64(lock), Usdc: uint64(usdc),
			Polygon: polygon, Hash: hash, ExpiresAt: expiresAt}
	})
}

// SignCancelOffer takes one's own open offer down.
func SignCancelOffer(words, chainID, accountNumber string, sequence, id int64) (string, error) {
	return signMsg(words, chainID, accountNumber, sequence, func(from string) sdk.Msg {
		return &markettypes.MsgCancelOffer{Maker: from, Id: uint64(id)}
	})
}

// SignTakeOffer accepts an offer; hashHex when taking a sell offer (buying).
// SignApproveOffer: the second device lets the protected account's sell
// offer be taken.
func SignApproveOffer(words, chainID, accountNumber string, sequence, id int64) (string, error) {
	return signMsg(words, chainID, accountNumber, sequence, func(from string) sdk.Msg {
		return &markettypes.MsgApproveOffer{Device: from, Id: uint64(id)}
	})
}

func SignTakeOffer(words, chainID, accountNumber string, sequence, id int64, polygon, hashHex string) (string, error) {
	hash, err := hashBytes(hashHex)
	if err != nil {
		return "", err
	}
	return signMsg(words, chainID, accountNumber, sequence, func(from string) sdk.Msg {
		return &markettypes.MsgTakeOffer{Taker: from, Id: uint64(id), Polygon: polygon, Hash: hash}
	})
}
