package mobilecore

import (
	"encoding/base64"
	"errors"

	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/crypto/keys/mldsa65"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	rotationtypes "encryptlock/lockkeeper/x/rotation/types"
)

// baseDenom is the smallest unit of LOCK (the network's bond denomination).
const baseDenom = "lockunit"

// SignBecomeLockKeeper makes this computer a LockKeeper ("Make this PC a
// LockKeeper"), in one transaction signed by the wallet:
//   - puts units of LOCK in guarantee (they stay the owner's), also as the
//     minimum self-guarantee (the network requires at least 1 dollar);
//   - uses the computer's block-signing key: consPubKey is the base64 ML-DSA-65
//     public key from the LockKeeper's priv_validator_key.json;
//   - names helper (an account whose words stay on the computer) to send
//     "I am on" every 5 minutes without asking for the password.
func SignBecomeLockKeeper(words, chainID, accountNumber string, sequence int64, consPubKey string, units int64, moniker, helper string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(consPubKey)
	if err != nil || len(raw) == 0 {
		return "", errors.New("invalid block-signing key")
	}
	if units <= 0 {
		return "", errors.New("amount must be more than 0")
	}
	if !IsAddress(helper) {
		return "", errors.New("invalid helper address")
	}
	var buildErr error
	tx, err := signMsgs(words, chainID, accountNumber, sequence, func(from string) []sdk.Msg {
		acc, _ := sdk.AccAddressFromBech32(from)
		amount := sdkmath.NewInt(units)
		create, err := stakingtypes.NewMsgCreateValidator(
			sdk.ValAddress(acc).String(),
			&mldsa65.PubKey{Key: raw},
			sdk.NewCoin(baseDenom, amount),
			stakingtypes.Description{Moniker: moniker},
			stakingtypes.NewCommissionRates(sdkmath.LegacyNewDecWithPrec(1, 1), sdkmath.LegacyNewDecWithPrec(2, 1), sdkmath.LegacyNewDecWithPrec(1, 2)),
			amount,
		)
		buildErr = err
		return []sdk.Msg{create, &rotationtypes.MsgSetHelper{Keeper: from, Helper: helper}}
	})
	if buildErr != nil {
		return "", buildErr
	}
	return tx, err
}

// SignMoveLockKeeper moves a wallet that is already a LockKeeper to this
// computer (its guarantee stays where it is, nothing is paid but the fee):
//   - names this computer's helper, so its "I am on" counts (the old
//     computer's helper stops counting);
//   - signs blocks with this computer's key consPubKey ("" keeps the key the
//     LockKeeper has: the same computer again). The network allows one key
//     change per 21 days and burns 0.01 LOCK for it.
//
// A guarantee put in from a computer whose chip does not work is never lost:
// the wallet takes it to a computer that works (founder, 26 Oct 2026).
func SignMoveLockKeeper(words, chainID, accountNumber string, sequence int64, consPubKey, helper string) (string, error) {
	var raw []byte
	if consPubKey != "" {
		var err error
		if raw, err = base64.StdEncoding.DecodeString(consPubKey); err != nil || len(raw) == 0 {
			return "", errors.New("invalid block-signing key")
		}
	}
	if !IsAddress(helper) {
		return "", errors.New("invalid helper address")
	}
	var buildErr error
	tx, err := signMsgs(words, chainID, accountNumber, sequence, func(from string) []sdk.Msg {
		msgs := []sdk.Msg{&rotationtypes.MsgSetHelper{Keeper: from, Helper: helper}}
		if raw != nil {
			acc, _ := sdk.AccAddressFromBech32(from)
			rotate, err := stakingtypes.NewMsgRotateConsPubKey(sdk.ValAddress(acc).String(), &mldsa65.PubKey{Key: raw})
			buildErr = err
			msgs = append([]sdk.Msg{rotate}, msgs...)
		}
		return msgs
	})
	if buildErr != nil {
		return "", buildErr
	}
	return tx, err
}

// SignHeartbeat is "I am on", signed by the helper account of a LockKeeper
// (its words stay on the computer). Free.
func SignHeartbeat(helperWords, chainID, accountNumber string, sequence int64) (string, error) {
	return signMsg(helperWords, chainID, accountNumber, sequence, func(from string) sdk.Msg {
		return &rotationtypes.MsgHeartbeat{Keeper: from}
	})
}

// SignAnnounceKey publishes a computer's block-signing key (consPubKey, base64
// ML-DSA-65 from priv_validator_key.json), signed by its helper account, so a
// phone that scans the computer's QR code can make it a LockKeeper. Free.
func SignAnnounceKey(helperWords, chainID, accountNumber string, sequence int64, consPubKey string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(consPubKey)
	if err != nil || len(raw) == 0 {
		return "", errors.New("invalid block-signing key")
	}
	return signMsg(helperWords, chainID, accountNumber, sequence, func(from string) sdk.Msg {
		return &rotationtypes.MsgAnnounceKey{Helper: from, PubKey: raw}
	})
}

// SignProveChip sends the network's chip checker ticket (base64), which says
// this computer is a real one with the chip code, signed by the computer's
// helper account (its words stay on the computer). Free.
func SignProveChip(helperWords, chainID, accountNumber string, sequence int64, code, ticket string) (string, error) {
	sig, err := base64.StdEncoding.DecodeString(ticket)
	if err != nil || len(sig) == 0 {
		return "", errors.New("invalid chip ticket")
	}
	return signMsg(helperWords, chainID, accountNumber, sequence, func(from string) sdk.Msg {
		return &rotationtypes.MsgProveChip{Account: from, Code: code, Ticket: sig}
	})
}
