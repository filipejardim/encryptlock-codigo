package app

import (
	"context"
	"testing"

	protov2 "google.golang.org/protobuf/proto"

	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/crypto/keys/mldsa65"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"encryptlock/lockkeeper/hybrid"
)

// signedTx is a minimal SigVerifiableTx with one signer.
type signedTx struct{ signer sdk.AccAddress }

func (t signedTx) GetMsgs() []sdk.Msg                              { return nil }
func (t signedTx) GetMsgsV2() ([]protov2.Message, error)           { return nil, nil }
func (t signedTx) GetSigners() ([][]byte, error)                   { return [][]byte{t.signer}, nil }
func (t signedTx) GetPubKeys() ([]cryptotypes.PubKey, error)       { return nil, nil }
func (t signedTx) GetSignaturesV2() ([]signing.SignatureV2, error) { return nil, nil }

type accounts map[string]sdk.AccountI

func (a accounts) GetAccount(_ context.Context, addr sdk.AccAddress) sdk.AccountI {
	return a[string(addr)]
}

func runHybrid(t *testing.T, pk cryptotypes.PubKey, simulate bool) error {
	t.Helper()
	addr := sdk.AccAddress(pk.Address())
	acc := authtypes.NewBaseAccount(addr, pk, 1, 0)
	decorator := HybridSignerDecorator{ak: accounts{string(addr): acc}}
	next := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil }
	_, err := decorator.AnteHandle(sdk.Context{}, signedTx{signer: addr}, simulate, next)
	return err
}

func TestHybridAccountsAreAccepted(t *testing.T) {
	pq, err := mldsa65.GenPrivKey()
	if err != nil {
		t.Fatal(err)
	}
	pk := hybrid.NewPubKey(ed25519.GenPrivKey().PubKey().(*ed25519.PubKey), pq.PubKey().(*mldsa65.PubKey))
	if err := runHybrid(t, pk, false); err != nil {
		t.Fatalf("hybrid account rejected: %v", err)
	}
}

func TestNonHybridAccountsAreRejected(t *testing.T) {
	pq, err := mldsa65.GenPrivKey()
	if err != nil {
		t.Fatal(err)
	}
	for name, pk := range map[string]cryptotypes.PubKey{
		"secp256k1":      secp256k1.GenPrivKey().PubKey(),
		"only Ed25519":   ed25519.GenPrivKey().PubKey(),
		"only ML-DSA-65": pq.PubKey(),
	} {
		if err := runHybrid(t, pk, false); err == nil {
			t.Errorf("%s account was accepted", name)
		}
	}
}
