package wallet

import (
	"errors"
	"strings"
	"testing"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"

	"encryptlock/lockkeeper/hybrid"
)

func newKeyring(t *testing.T) keyring.Keyring {
	t.Helper()
	registry := codectypes.NewInterfaceRegistry()
	cryptocodec.RegisterInterfaces(registry)
	return keyring.NewInMemory(codec.NewProtoCodec(registry), hybrid.KeyringOption())
}

func multiKey(t *testing.T, rec *keyring.Record) *multisig.LegacyAminoPubKey {
	t.Helper()
	pk, err := rec.GetPubKey()
	if err != nil {
		t.Fatal(err)
	}
	return pk.(*multisig.LegacyAminoPubKey)
}

func verify(pk *multisig.LegacyAminoPubKey, data *signing.MultiSignatureData, msg []byte) error {
	return pk.VerifyMultisignature(func(signing.SignMode) ([]byte, error) { return msg, nil }, data)
}

func TestCreateGives24WordsAndAHybridAccount(t *testing.T) {
	kr := newKeyring(t)
	rec, words, err := Create(kr, "ana")
	if err != nil {
		t.Fatal(err)
	}
	if n := len(strings.Fields(words)); n != 24 {
		t.Fatalf("got %d recovery words, want 24", n)
	}
	if !hybrid.IsHybrid(multiKey(t, rec)) {
		t.Fatal("new wallet is not hybrid")
	}
}

func TestRecoverGivesTheSameWallet(t *testing.T) {
	rec, words, err := Create(newKeyring(t), "ana")
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := Recover(newKeyring(t), "ana-no-telemovel-novo", words)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := rec.GetAddress()
	b, _ := recovered.GetAddress()
	if !a.Equals(b) {
		t.Fatalf("recovered address %s differs from original %s", b, a)
	}
	if _, err := Recover(newKeyring(t), "x", "only three words"); !errors.Is(err, ErrWords) {
		t.Fatalf("short phrase: err = %v, want ErrWords", err)
	}
}

func TestSignHybridWallet(t *testing.T) {
	kr := newKeyring(t)
	rec, _, err := Create(kr, "ana")
	if err != nil {
		t.Fatal(err)
	}
	pk, msg := multiKey(t, rec), []byte("transfer")
	data, err := SignMultisig(kr, pk, msg)
	if err != nil {
		t.Fatal(err)
	}
	if err := verify(pk, data, msg); err != nil {
		t.Fatalf("hybrid signature rejected: %v", err)
	}
	if LeafSignatures(pk) != 2 {
		t.Errorf("hybrid wallet needs %d signatures, want 2", LeafSignatures(pk))
	}
}

// The development fund: 2 of 3 hybrid keys. Any 2 members can sign; 1 cannot.
func TestSignTeamTwoOfThree(t *testing.T) {
	kr := newKeyring(t)
	var members []cryptotypes.PubKey
	for _, name := range []string{"fundo-1", "fundo-2", "fundo-3"} {
		rec, _, err := Create(kr, name)
		if err != nil {
			t.Fatal(err)
		}
		members = append(members, multiKey(t, rec))
	}
	team := multisig.NewLegacyAminoPubKey(2, members)
	if !hybrid.IsTeam(team) {
		t.Fatal("team key not recognized")
	}
	msg := []byte("pay the developers")

	// Member 1's keys are elsewhere (e.g. in a safe): 2 and 3 sign.
	for _, k := range []string{"fundo-1-ed", "fundo-1-pq", "fundo-1"} {
		if err := kr.Delete(k); err != nil {
			t.Fatal(err)
		}
	}
	data, err := SignMultisig(kr, team, msg)
	if err != nil {
		t.Fatal(err)
	}
	if err := verify(team, data, msg); err != nil {
		t.Fatalf("2-of-3 signature rejected: %v", err)
	}

	// Only member 3 left: not enough.
	for _, k := range []string{"fundo-2-ed", "fundo-2-pq", "fundo-2"} {
		if err := kr.Delete(k); err != nil {
			t.Fatal(err)
		}
	}
	var notEnough NotEnoughKeysError
	if _, err := SignMultisig(kr, team, msg); !errors.As(err, &notEnough) {
		t.Fatalf("one member signed alone: err = %v", err)
	}
	if LeafSignatures(team) != 4 {
		t.Errorf("team needs %d signatures, want 4", LeafSignatures(team))
	}
}

func TestLOCKAmounts(t *testing.T) {
	cases := map[string]uint64{"2": 200_000_000, "0.5": 50_000_000, "0,5": 50_000_000, "0.00000001": 1, "1000": 100_000_000_000}
	for in, want := range cases {
		got, err := ParseLOCK(in)
		if err != nil || got != want {
			t.Errorf("ParseLOCK(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"0", "-1", "abc", "0.000000001", ""} {
		if _, err := ParseLOCK(bad); err == nil {
			t.Errorf("ParseLOCK(%q) accepted", bad)
		}
	}
	formats := map[uint64]string{200_000_000: "2", 150_000: "0.0015", 1: "0.00000001", 0: "0"}
	for in, want := range formats {
		if got := FormatLOCK(in); got != want {
			t.Errorf("FormatLOCK(%d) = %q, want %q", in, got, want)
		}
	}
}
