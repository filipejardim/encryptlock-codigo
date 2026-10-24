package mobilecore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/cosmos-sdk/x/tx/signing"
	"github.com/cosmos/gogoproto/proto"

	"encryptlock/lockkeeper/protocol"
	"encryptlock/lockkeeper/wallet"
	markettypes "encryptlock/lockkeeper/x/market/types"
	recoverytypes "encryptlock/lockkeeper/x/recovery/types"
	registrytypes "encryptlock/lockkeeper/x/registry/types"
	rotationtypes "encryptlock/lockkeeper/x/rotation/types"
	transferstypes "encryptlock/lockkeeper/x/transfers/types"
	vaulttypes "encryptlock/lockkeeper/x/vault/types"
)

// txConfig encodes and signs transactions exactly like the LockKeeper does.
func txConfig() (client.TxConfig, error) {
	cfg := sdk.GetConfig()
	registry, err := codectypes.NewInterfaceRegistryWithOptions(codectypes.InterfaceRegistryOptions{
		ProtoFiles: proto.HybridResolver,
		SigningOptions: signing.Options{
			AddressCodec:          addresscodec.Bech32Codec{Bech32Prefix: cfg.GetBech32AccountAddrPrefix()},
			ValidatorAddressCodec: addresscodec.Bech32Codec{Bech32Prefix: cfg.GetBech32ValidatorAddrPrefix()},
		},
	})
	if err != nil {
		return nil, err
	}
	cryptocodec.RegisterInterfaces(registry)
	transferstypes.RegisterInterfaces(registry)
	registrytypes.RegisterInterfaces(registry)
	recoverytypes.RegisterInterfaces(registry)
	vaulttypes.RegisterInterfaces(registry)
	markettypes.RegisterInterfaces(registry)
	distrtypes.RegisterInterfaces(registry)
	stakingtypes.RegisterInterfaces(registry)
	rotationtypes.RegisterInterfaces(registry)
	return authtx.NewTxConfigWithOptions(codec.NewProtoCodec(registry), authtx.ConfigOptions{})
}

// signMsg signs one message with both keys of the wallet of these words and
// returns the transaction, ready to broadcast, in base64.
//
// accountNumber is text: account numbers use the whole uint64 range, more
// than Java/Dart integers hold.
func signMsg(words, chainID, accountNumber string, sequence int64, msg func(from string) sdk.Msg) (string, error) {
	return signMsgs(words, chainID, accountNumber, sequence, func(from string) []sdk.Msg { return []sdk.Msg{msg(from)} })
}

// signMsgs signs several messages in one transaction.
func signMsgs(words, chainID, accountNumber string, sequence int64, msgs func(from string) []sdk.Msg) (string, error) {
	accNum, err := strconv.ParseUint(accountNumber, 10, 64)
	if err != nil {
		return "", err
	}
	if sequence < 0 {
		return "", errors.New("negative sequence")
	}
	kr := newKeyring()
	rec, err := wallet.Recover(kr, "wallet", strings.ToLower(strings.Join(strings.Fields(words), " ")))
	if err != nil {
		return "", err
	}
	pub, err := rec.GetPubKey()
	if err != nil {
		return "", err
	}
	pk, ok := pub.(*multisig.LegacyAminoPubKey)
	if !ok {
		return "", errors.New("not a hybrid wallet")
	}
	cfg, err := txConfig()
	if err != nil {
		return "", err
	}
	b := cfg.NewTxBuilder()
	if err := b.SetMsgs(msgs(sdk.AccAddress(pk.Address()).String())...); err != nil {
		return "", err
	}
	// Same gas as the command-line wallet: the post-quantum signatures are
	// large. Gas costs nothing (fees are EncryptLock's own rules).
	b.SetGasLimit(uint64(300_000 + 250_000*wallet.LeafSignatures(pk)))
	if err := wallet.SignTx(context.Background(), cfg, kr, chainID, accNum, uint64(sequence), pk, b); err != nil {
		return "", err
	}
	bz, err := cfg.TxEncoder()(b.GetTx())
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(bz), nil
}

// SignRegister registers a wallet (free; needed before it can receive).
// A brand-new account is not known by the network yet: number 0, sequence 0;
// an account that already received coins uses its real number and sequence.
func SignRegister(words, chainID, accountNumber string, sequence int64) (string, error) {
	return signMsg(words, chainID, accountNumber, sequence, func(from string) sdk.Msg {
		return &registrytypes.MsgRegister{Address: from}
	})
}

// SignSend sends units lockunit to a lock1... address (it waits 1 hour for
// the recipient to accept).
func SignSend(words, chainID, accountNumber string, sequence int64, to string, units int64) (string, error) {
	if units <= 0 {
		return "", errors.New("amount must be more than 0")
	}
	if _, err := sdk.AccAddressFromBech32(to); err != nil {
		return "", err
	}
	return signMsg(words, chainID, accountNumber, sequence, func(from string) sdk.Msg {
		return &transferstypes.MsgSend{Sender: from, Recipient: to, Amount: uint64(units)}
	})
}

// SignAccept accepts pending transfer id sent to this wallet.
func SignAccept(words, chainID, accountNumber string, sequence, id int64) (string, error) {
	return signMsg(words, chainID, accountNumber, sequence, func(from string) sdk.Msg {
		return &transferstypes.MsgAccept{Recipient: from, Id: uint64(id)}
	})
}

// SignCancel cancels pending transfer id sent by this wallet (money back).
func SignCancel(words, chainID, accountNumber string, sequence, id int64) (string, error) {
	return signMsg(words, chainID, accountNumber, sequence, func(from string) sdk.Msg {
		return &transferstypes.MsgCancel{Sender: from, Id: uint64(id)}
	})
}

// IsAddress reports whether s is a valid lock1... address.
func IsAddress(s string) bool {
	_, err := sdk.AccAddressFromBech32(strings.TrimSpace(s))
	return err == nil
}

// TransferFee is the fee the sender pays for sending units lockunit: 0,1%,
// at least protocol.TransferFeeMin, at most protocol.TransferFeeMax (the
// same rule as x/transfers; a test checks they agree).
func TransferFee(units int64) int64 {
	fee := uint64(units) * protocol.TransferFeeBasisPoints / 10_000
	fee = max(fee, protocol.TransferFeeMin)
	fee = min(fee, protocol.TransferFeeMax)
	return int64(fee)
}

// ParseLock reads an amount typed by the user ("1,5" or "1.5", at most 8
// decimals) and returns lockunit.
func ParseLock(s string) (int64, error) {
	units, err := wallet.ParseLOCK(strings.TrimSpace(s))
	return int64(units), err
}

// SignFreeze is "It wasn't me": the account freezes at once (nothing can
// leave it) and its pending transfers are canceled.
func SignFreeze(words, chainID, accountNumber string, sequence int64) (string, error) {
	return signMsg(words, chainID, accountNumber, sequence, func(from string) sdk.Msg {
		return &transferstypes.MsgFreeze{Signer: from, Account: from}
	})
}

// SignUnfreeze asks to unfreeze the account. Asked only by the account
// itself it takes 7 days (white paper 7.2); with the second device, at once.
func SignUnfreeze(words, chainID, accountNumber string, sequence int64) (string, error) {
	return signMsg(words, chainID, accountNumber, sequence, func(from string) sdk.Msg {
		return &transferstypes.MsgUnfreeze{Signer: from, Account: from}
	})
}

// SignFreezeFor is "It wasn't me" pressed on the second device: it freezes
// the account it protects.
func SignFreezeFor(words, chainID, accountNumber string, sequence int64, account string) (string, error) {
	if _, err := sdk.AccAddressFromBech32(account); err != nil {
		return "", err
	}
	return signMsg(words, chainID, accountNumber, sequence, func(from string) sdk.Msg {
		return &transferstypes.MsgFreeze{Signer: from, Account: account}
	})
}

// SignSetDevice links (or, with device "", removes) the second device: a
// registered account that can only approve and freeze. Linking the first is
// immediate; changing or removing takes 7 days.
func SignSetDevice(words, chainID, accountNumber string, sequence int64, device string) (string, error) {
	if device != "" {
		if _, err := sdk.AccAddressFromBech32(device); err != nil {
			return "", err
		}
	}
	return signMsg(words, chainID, accountNumber, sequence, func(from string) sdk.Msg {
		return &transferstypes.MsgSetDevice{Owner: from, Device: device}
	})
}

// SignSetLimits sets the daily limit and the amount above which the second
// device must approve (lockunit; 0 = none). Lower is immediate, higher 48 h.
func SignSetLimits(words, chainID, accountNumber string, sequence, dailyLimit, approvalAbove int64) (string, error) {
	if dailyLimit < 0 || approvalAbove < 0 {
		return "", errors.New("negative limit")
	}
	return signMsg(words, chainID, accountNumber, sequence, func(from string) sdk.Msg {
		return &transferstypes.MsgSetLimits{Owner: from, DailyLimit: uint64(dailyLimit), ApprovalAbove: uint64(approvalAbove)}
	})
}

// SignApprove: the second device approves transfer id of the account it
// protects (new contact or above the limit).
func SignApprove(words, chainID, accountNumber string, sequence, id int64) (string, error) {
	return signMsg(words, chainID, accountNumber, sequence, func(from string) sdk.Msg {
		return &transferstypes.MsgApprove{Device: from, Id: uint64(id)}
	})
}

// planJSON is how the app passes an inheritance / recovery plan.
type planJSON struct {
	Heirs []struct {
		Address string `json:"address"`
		Percent uint32 `json:"percent"`
	} `json:"heirs"`
	InactivityDays    uint32   `json:"inactivity_days"`
	Guardians         []string `json:"guardians"`
	GuardianThreshold uint32   `json:"guardian_threshold"`
	HeirThreshold     uint32   `json:"heir_threshold"`
}

// SignSetPlan sets heirs (percent shares) and/or trusted people. The first
// plan applies at once; later changes wait 7 days (the network's rule).
func SignSetPlan(words, chainID, accountNumber string, sequence int64, plan string) (string, error) {
	var p planJSON
	if err := json.Unmarshal([]byte(plan), &p); err != nil {
		return "", err
	}
	out := &recoverytypes.Plan{
		InactivityDays:    p.InactivityDays,
		Guardians:         p.Guardians,
		GuardianThreshold: p.GuardianThreshold,
		HeirThreshold:     p.HeirThreshold,
	}
	for _, h := range p.Heirs {
		out.Heirs = append(out.Heirs, &recoverytypes.Heir{Address: h.Address, Percent: h.Percent})
	}
	return signMsg(words, chainID, accountNumber, sequence, func(from string) sdk.Msg {
		return &recoverytypes.MsgSetPlan{Owner: from, Plan: out}
	})
}

// SignAlive is "I'm here": a sign of life that cancels an inheritance request.
func SignAlive(words, chainID, accountNumber string, sequence int64) (string, error) {
	return signMsg(words, chainID, accountNumber, sequence, func(from string) sdk.Msg {
		return &recoverytypes.MsgAlive{Owner: from}
	})
}

// SignCancelClaims stops any inheritance or recovery running on this account.
func SignCancelClaims(words, chainID, accountNumber string, sequence int64) (string, error) {
	return signMsg(words, chainID, accountNumber, sequence, func(from string) sdk.Msg {
		return &recoverytypes.MsgCancelClaims{Owner: from}
	})
}

// SignClaimInheritance: an heir asks for (or confirms) the inheritance of account.
func SignClaimInheritance(words, chainID, accountNumber string, sequence int64, account string) (string, error) {
	return signMsg(words, chainID, accountNumber, sequence, func(from string) sdk.Msg {
		return &recoverytypes.MsgClaimInheritance{Heir: from, Account: account}
	})
}

// SignApproveRecovery: a trusted person approves moving account to newAddress.
func SignApproveRecovery(words, chainID, accountNumber string, sequence int64, account, newAddress string) (string, error) {
	return signMsg(words, chainID, accountNumber, sequence, func(from string) sdk.Msg {
		return &recoverytypes.MsgApproveRecovery{Guardian: from, Account: account, NewAddress: newAddress}
	})
}

// SignSetVault makes the account a Sovereign Vault: every sending waits
// delayDays (7–365; 0 = off) before the recipient can accept.
func SignSetVault(words, chainID, accountNumber string, sequence, delayDays int64) (string, error) {
	return signMsg(words, chainID, accountNumber, sequence, func(from string) sdk.Msg {
		return &transferstypes.MsgSetVault{Owner: from, DelayDays: uint32(delayDays)}
	})
}

// SignLock locks units lockunit until a date (Unix seconds); they come back by
// themselves then.
func SignLock(words, chainID, accountNumber string, sequence, units, until int64) (string, error) {
	if units <= 0 {
		return "", errors.New("amount must be more than 0")
	}
	return signMsg(words, chainID, accountNumber, sequence, func(from string) sdk.Msg {
		return &vaulttypes.MsgLock{Owner: from, Amount: uint64(units), Until: until}
	})
}

// SignCollectRewards moves a LockKeeper's rewards and commission into its
// balance, in one transaction (the app does it on opening and every hour).
func SignCollectRewards(words, chainID, accountNumber string, sequence int64) (string, error) {
	return signMsgs(words, chainID, accountNumber, sequence, func(from string) []sdk.Msg {
		acc, _ := sdk.AccAddressFromBech32(from)
		val := sdk.ValAddress(acc).String()
		return []sdk.Msg{
			&distrtypes.MsgWithdrawDelegatorReward{DelegatorAddress: from, ValidatorAddress: val},
			&distrtypes.MsgWithdrawValidatorCommission{ValidatorAddress: val},
		}
	})
}
