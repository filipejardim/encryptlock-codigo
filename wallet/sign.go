package wallet

import (
	"context"
	"errors"
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	multisigtypes "github.com/cosmos/cosmos-sdk/crypto/types/multisig"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
)

// signMode: amino JSON, the mode every key of a Cosmos multisig can sign with.
const signMode = signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON

// NotEnoughKeysError: this computer does not hold enough of the account's keys.
type NotEnoughKeysError struct{ Have, Need int }

func (e NotEnoughKeysError) Error() string {
	return fmt.Sprintf("not enough keys to sign: %d of %d", e.Have, e.Need)
}

// SignTx signs txBuilder for an account whose key is a multisig (a hybrid
// key, or a team of hybrid keys), using the keys found in kr.
func SignTx(ctx context.Context, txConfig client.TxConfig, kr keyring.Keyring, chainID string, accNum, seq uint64, pk *multisig.LegacyAminoPubKey, txBuilder client.TxBuilder) error {
	// The signer info (key + sequence) must be in the transaction before the
	// sign bytes are computed.
	placeholder := signing.SignatureV2{PubKey: pk, Data: multisigtypes.NewMultisig(len(pk.GetPubKeys())), Sequence: seq}
	if err := txBuilder.SetSignatures(placeholder); err != nil {
		return err
	}

	signerData := authsigning.SignerData{
		Address:       sdk.AccAddress(pk.Address()).String(),
		ChainID:       chainID,
		AccountNumber: accNum,
		Sequence:      seq,
		PubKey:        pk,
	}
	bytesToSign, err := authsigning.GetSignBytesAdapter(ctx, txConfig.SignModeHandler(), signMode, signerData, txBuilder.GetTx())
	if err != nil {
		return err
	}

	data, err := SignMultisig(kr, pk, bytesToSign)
	if err != nil {
		return err
	}
	return txBuilder.SetSignatures(signing.SignatureV2{PubKey: pk, Data: data, Sequence: seq})
}

// SignMultisig signs msg with as many of pk's keys as its threshold needs,
// going inside nested multisigs (a team made of hybrid keys).
func SignMultisig(kr keyring.Keyring, pk *multisig.LegacyAminoPubKey, msg []byte) (*signing.MultiSignatureData, error) {
	keys := pk.GetPubKeys()
	data := multisigtypes.NewMultisig(len(keys))
	signed := 0
	for i, sub := range keys {
		if signed == int(pk.Threshold) {
			break
		}
		if nested, ok := sub.(*multisig.LegacyAminoPubKey); ok {
			nestedData, err := SignMultisig(kr, nested, msg)
			var notEnough NotEnoughKeysError
			if errors.As(err, &notEnough) {
				continue // that member's keys are not on this computer
			}
			if err != nil {
				return nil, err
			}
			multisigtypes.AddSignature(data, nestedData, i)
			signed++
			continue
		}

		rec, err := kr.KeyByAddress(sdk.AccAddress(sub.Address()))
		if err != nil || rec.GetType() != keyring.TypeLocal {
			continue // key not held here
		}
		sig, _, err := kr.SignByAddress(sdk.AccAddress(sub.Address()), msg, signMode)
		if err != nil {
			return nil, err
		}
		multisigtypes.AddSignature(data, &signing.SingleSignatureData{SignMode: signMode, Signature: sig}, i)
		signed++
	}
	if signed < int(pk.Threshold) {
		return nil, NotEnoughKeysError{Have: signed, Need: int(pk.Threshold)}
	}
	return data, nil
}

// LeafSignatures counts the individual signatures pk needs (for gas).
func LeafSignatures(pk *multisig.LegacyAminoPubKey) int {
	n := 0
	for i, sub := range pk.GetPubKeys() {
		if i == int(pk.Threshold) {
			break
		}
		if nested, ok := sub.(*multisig.LegacyAminoPubKey); ok {
			n += LeafSignatures(nested)
		} else {
			n++
		}
	}
	return n
}
