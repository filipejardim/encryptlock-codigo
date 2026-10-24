// Package wallet is the logic behind `lockkeeper wallet`: hybrid wallets from
// 24 recovery words, LOCK amounts, and signing with every key an account
// needs (Ed25519 + ML-DSA-65, also inside team accounts such as the
// development fund).
package wallet

import (
	"fmt"
	"strings"

	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/crypto/keys/mldsa65"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"encryptlock/lockkeeper/hybrid"
)

// CoinType is the BIP-44 coin type of the key paths: EncryptLock's own
// number (free in the SLIP-44 list on 12 Oct 2026; registration pending).
const CoinType = 4041

// Key paths: the Ed25519 key uses account 0 and the ML-DSA-65 key account 1,
// so the two keys never share secret material.
var (
	Ed25519Path = hd.CreateHDPath(CoinType, 0, 0).String()
	MLDSAPath   = hd.CreateHDPath(CoinType, 1, 0).String()
)

// Key names in the keyring: <name> is the hybrid account, <name>-ed and
// <name>-pq its two halves.
func edName(name string) string { return name + "-ed" }
func pqName(name string) string { return name + "-pq" }

// Create makes a new hybrid wallet and returns its 24 recovery words.
func Create(kr keyring.Keyring, name string) (*keyring.Record, string, error) {
	_, mnemonic, err := kr.NewMnemonic(edName(name), keyring.English, Ed25519Path, "", hybrid.Ed25519)
	if err != nil {
		return nil, "", err
	}
	rec, err := finish(kr, name, mnemonic)
	if err != nil {
		return nil, "", err
	}
	return rec, mnemonic, nil
}

// Recover rebuilds a hybrid wallet from its 24 recovery words.
func Recover(kr keyring.Keyring, name, mnemonic string) (*keyring.Record, error) {
	mnemonic = strings.Join(strings.Fields(mnemonic), " ")
	if len(strings.Fields(mnemonic)) != 24 {
		return nil, ErrWords
	}
	if _, err := kr.NewAccount(edName(name), mnemonic, "", Ed25519Path, hybrid.Ed25519); err != nil {
		return nil, err
	}
	return finish(kr, name, mnemonic)
}

// ErrWords means the recovery phrase does not have 24 words.
var ErrWords = fmt.Errorf("the recovery phrase must have 24 words")

func finish(kr keyring.Keyring, name, mnemonic string) (*keyring.Record, error) {
	pqRec, err := kr.NewAccount(pqName(name), mnemonic, "", MLDSAPath, hd.MlDsa65)
	if err != nil {
		return nil, err
	}
	edRec, err := kr.Key(edName(name))
	if err != nil {
		return nil, err
	}
	edPK, err := edRec.GetPubKey()
	if err != nil {
		return nil, err
	}
	pqPK, err := pqRec.GetPubKey()
	if err != nil {
		return nil, err
	}
	return kr.SaveMultisig(name, hybrid.NewPubKey(edPK.(*ed25519.PubKey), pqPK.(*mldsa65.PubKey)))
}

// Address returns the address of a wallet name, or parses an address.
func Address(kr keyring.Keyring, nameOrAddress string) (sdk.AccAddress, error) {
	if addr, err := sdk.AccAddressFromBech32(nameOrAddress); err == nil {
		return addr, nil
	}
	rec, err := kr.Key(nameOrAddress)
	if err != nil {
		return nil, err
	}
	return rec.GetAddress()
}
