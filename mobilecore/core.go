// Package mobilecore is what the EncryptLock Android app calls (through
// gomobile): the same Go wallet code the network and the command-line wallet
// use, so the app never reimplements cryptography.
//
// Build the Android library with scripts/mobilecore.sh.
package mobilecore

import (
	"strings"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/go-bip39"

	"encryptlock/lockkeeper/hybrid"
	"encryptlock/lockkeeper/protocol"
	"encryptlock/lockkeeper/wallet"
)

func init() {
	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount(protocol.Bech32Prefix, protocol.Bech32Prefix+"pub")
	cfg.SetBech32PrefixForValidator(protocol.Bech32Prefix+"valoper", protocol.Bech32Prefix+"valoperpub")
}

// newKeyring is a keyring that lives only in memory for one call: the app
// keeps the 24 words encrypted on the phone, never a keyring file.
func newKeyring() keyring.Keyring {
	registry := codectypes.NewInterfaceRegistry()
	cryptocodec.RegisterInterfaces(registry)
	return keyring.NewInMemory(codec.NewProtoCodec(registry), hybrid.KeyringOption())
}

// NewMnemonic returns 24 new recovery words.
func NewMnemonic() (string, error) {
	_, words, err := wallet.Create(newKeyring(), "wallet")
	return words, err
}

// ValidateMnemonic reports whether words are 24 valid recovery words.
func ValidateMnemonic(words string) bool {
	words = strings.Join(strings.Fields(strings.ToLower(words)), " ")
	return len(strings.Fields(words)) == 24 && bip39.IsMnemonicValid(words)
}

// Address returns the lock1... address of the wallet of these 24 words.
func Address(words string) (string, error) {
	rec, err := wallet.Recover(newKeyring(), "wallet", strings.ToLower(words))
	if err != nil {
		return "", err
	}
	addr, err := rec.GetAddress()
	if err != nil {
		return "", err
	}
	return addr.String(), nil
}

// ValidatorAddress turns a lock1... address into the lockvaloper1... address
// it would have as a LockKeeper (to show its LockKeeper commission).
func ValidatorAddress(address string) (string, error) {
	acc, err := sdk.AccAddressFromBech32(address)
	if err != nil {
		return "", err
	}
	return sdk.ValAddress(acc).String(), nil
}
