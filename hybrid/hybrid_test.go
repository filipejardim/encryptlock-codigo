package hybrid

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/crypto/keys/mldsa65"
	"github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	multisigtypes "github.com/cosmos/cosmos-sdk/crypto/types/multisig"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
)

func newKeys(t *testing.T) (*ed25519.PrivKey, *mldsa65.PrivKey) {
	t.Helper()
	pq, err := mldsa65.GenPrivKey()
	if err != nil {
		t.Fatal(err)
	}
	return ed25519.GenPrivKey(), &pq
}

func TestOnlyEd25519PlusMLDSA2of2IsHybrid(t *testing.T) {
	ed, pq := newKeys(t)
	edPK, pqPK := ed.PubKey().(*ed25519.PubKey), pq.PubKey().(*mldsa65.PubKey)
	ed2, _ := newKeys(t)

	cases := []struct {
		name string
		pk   cryptotypes.PubKey
		want bool
	}{
		{"hybrid", NewPubKey(edPK, pqPK), true},
		{"hybrid, keys in other order", multisig.NewLegacyAminoPubKey(2, []cryptotypes.PubKey{pqPK, edPK}), true},
		{"only Ed25519", edPK, false},
		{"only ML-DSA-65", pqPK, false},
		{"secp256k1 (Cosmos default)", secp256k1.GenPrivKey().PubKey(), false},
		{"1 of 2: one signature would be enough", multisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{edPK, pqPK}), false},
		{"two Ed25519, no post-quantum", multisig.NewLegacyAminoPubKey(2, []cryptotypes.PubKey{edPK, ed2.PubKey()}), false},
		{"2 of 3", multisig.NewLegacyAminoPubKey(2, []cryptotypes.PubKey{edPK, pqPK, ed2.PubKey()}), false},
	}
	for _, c := range cases {
		if got := IsHybrid(c.pk); got != c.want {
			t.Errorf("%s: IsHybrid = %v, want %v", c.name, got, c.want)
		}
	}
}

func sign(t *testing.T, msg []byte, signers ...cryptotypes.PrivKey) (*multisig.LegacyAminoPubKey, *signing.MultiSignatureData, []cryptotypes.PubKey) {
	t.Helper()
	ed, pq := newKeys(t)
	keys := []cryptotypes.PubKey{ed.PubKey(), pq.PubKey()}
	pk := NewPubKey(ed.PubKey().(*ed25519.PubKey), pq.PubKey().(*mldsa65.PubKey)).(*multisig.LegacyAminoPubKey)
	data := multisigtypes.NewMultisig(len(keys))
	for _, which := range signers {
		var priv cryptotypes.PrivKey = ed
		if _, isPQ := which.(*mldsa65.PrivKey); isPQ {
			priv = pq
		}
		sig, err := priv.Sign(msg)
		if err != nil {
			t.Fatal(err)
		}
		single := &signing.SingleSignatureData{SignMode: signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON, Signature: sig}
		if err := multisigtypes.AddSignatureFromPubKey(data, single, priv.PubKey(), keys); err != nil {
			t.Fatal(err)
		}
	}
	return pk, data, keys
}

func verify(pk *multisig.LegacyAminoPubKey, data *signing.MultiSignatureData, msg []byte) error {
	return pk.VerifyMultisignature(func(signing.SignMode) ([]byte, error) { return msg, nil }, data)
}

func TestBothSignaturesAreRequired(t *testing.T) {
	msg := []byte("transfer 10 LOCK")
	edMarker, pqMarker := &ed25519.PrivKey{}, &mldsa65.PrivKey{}

	pk, data, _ := sign(t, msg, edMarker, pqMarker)
	if err := verify(pk, data, msg); err != nil {
		t.Fatalf("both signatures: rejected: %v", err)
	}

	pk, data, _ = sign(t, msg, edMarker)
	if err := verify(pk, data, msg); err == nil {
		t.Fatal("only the Ed25519 signature: accepted")
	}

	pk, data, _ = sign(t, msg, pqMarker)
	if err := verify(pk, data, msg); err == nil {
		t.Fatal("only the ML-DSA-65 signature: accepted")
	}
}

func TestWrongMessageIsRejected(t *testing.T) {
	pk, data, _ := sign(t, []byte("transfer 10 LOCK"), &ed25519.PrivKey{}, &mldsa65.PrivKey{})
	if err := verify(pk, data, []byte("transfer 99 LOCK")); err == nil {
		t.Fatal("signatures for a different message were accepted")
	}
}

func TestKeyringEd25519IsDeterministic(t *testing.T) {
	derive := Ed25519.Derive()
	seed, err := derive("abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about", "", "m/44'/118'/0'/0/0")
	if err != nil {
		t.Fatal(err)
	}
	a, b := Ed25519.Generate()(seed), Ed25519.Generate()(seed)
	if !a.PubKey().Equals(b.PubKey()) {
		t.Fatal("same recovery phrase gave different Ed25519 keys")
	}
}

func newHybrid(t *testing.T) cryptotypes.PubKey {
	t.Helper()
	ed, pq := newKeys(t)
	return NewPubKey(ed.PubKey().(*ed25519.PubKey), pq.PubKey().(*mldsa65.PubKey))
}

func TestTeamKeysForTheDevelopmentFund(t *testing.T) {
	h1, h2, h3 := newHybrid(t), newHybrid(t), newHybrid(t)
	ed, _ := newKeys(t)

	cases := []struct {
		name string
		pk   cryptotypes.PubKey
		want bool
	}{
		{"2 of 3 hybrid keys", multisig.NewLegacyAminoPubKey(2, []cryptotypes.PubKey{h1, h2, h3}), true},
		{"3 of 3 hybrid keys", multisig.NewLegacyAminoPubKey(3, []cryptotypes.PubKey{h1, h2, h3}), true},
		{"1 of 3: a single stolen key would be enough", multisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{h1, h2, h3}), false},
		{"2 of 3 with a plain Ed25519 key", multisig.NewLegacyAminoPubKey(2, []cryptotypes.PubKey{h1, h2, ed.PubKey()}), false},
	}
	for _, c := range cases {
		if got := IsTeam(c.pk); got != c.want {
			t.Errorf("%s: IsTeam = %v, want %v", c.name, got, c.want)
		}
		if got := IsAllowed(c.pk); got != c.want {
			t.Errorf("%s: IsAllowed = %v, want %v", c.name, got, c.want)
		}
	}
	if !IsAllowed(h1) {
		t.Error("a plain hybrid key is not allowed")
	}
}
