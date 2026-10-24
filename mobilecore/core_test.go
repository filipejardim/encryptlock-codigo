package mobilecore

import (
	"encoding/base64"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"

	"encryptlock/lockkeeper/x/transfers"
)

func TestMnemonicAndAddress(t *testing.T) {
	words, err := NewMnemonic()
	if err != nil {
		t.Fatal(err)
	}
	if !ValidateMnemonic(words) {
		t.Fatal("new recovery words are not valid")
	}
	if ValidateMnemonic("abandon abandon") {
		t.Fatal("2 words accepted")
	}
	a, err := Address(words)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Address("  " + words + "\n")
	if a != b || a[:5] != "lock1" {
		t.Fatalf("addresses %q and %q", a, b)
	}
}

func TestSealAndOpenWords(t *testing.T) {
	words := "tide quarter nerve olympic awake ladder"
	rec, err := SealWords(words, "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	got, err := OpenWords(rec, "correct horse")
	if err != nil || got != words {
		t.Fatalf("right password: %q %v", got, err)
	}
	got, err = OpenWords(rec, "wrong horse")
	if err != nil || got != "" {
		t.Fatalf("wrong password opened: %q %v", got, err)
	}
	other, _ := SealWords(words, "correct horse")
	if other == rec {
		t.Fatal("same record twice: salt/nonce not random")
	}
	if _, err := SealWords(words, ""); err == nil {
		t.Fatal("empty password accepted")
	}
	if _, err := OpenWords(`{"v":2,"kdf":"argon2id","t":3,"m":99999999,"p":4}`, "x"); err == nil {
		t.Fatal("absurd memory accepted")
	}
}

func TestValidatorAddress(t *testing.T) {
	v, err := ValidatorAddress("lock1pxgrxd7tsdalzml5h5nzpzq038uhsuuu2d7gn7")
	if err != nil || v != "lockvaloper1pxgrxd7tsdalzml5h5nzpzq038uhsuuunu7a95" {
		t.Fatalf("%q %v", v, err)
	}
	if _, err := ValidatorAddress("nonsense"); err == nil {
		t.Fatal("bad address accepted")
	}
}

func TestSignTransactions(t *testing.T) {
	words, _ := NewMnemonic()
	from, _ := Address(words)
	other, _ := NewMnemonic()
	to, _ := Address(other)
	cfg, err := txConfig()
	if err != nil {
		t.Fatal(err)
	}
	check := func(b64 string, err error, wantSeq uint64) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		bz, _ := base64.StdEncoding.DecodeString(b64)
		tx, err := cfg.TxDecoder()(bz)
		if err != nil {
			t.Fatal(err)
		}
		sigTx := tx.(authsigning.SigVerifiableTx)
		sigs, _ := sigTx.GetSignaturesV2()
		if len(sigs) != 1 || sigs[0].Sequence != wantSeq {
			t.Fatalf("signatures %+v", sigs)
		}
		multi, ok := sigs[0].Data.(*signing.MultiSignatureData)
		if !ok || len(multi.Signatures) != 2 {
			t.Fatal("not signed by both keys (Ed25519 + ML-DSA-65)")
		}
		signers, _ := sigTx.GetSigners()
		if sdk.AccAddress(signers[0]).String() != from {
			t.Fatalf("signer %x is not %s", signers[0], from)
		}
	}
	tx, err := SignSend(words, "encryptlock-local", "17202940963748667228", 3, to, 150000000)
	check(tx, err, 3)
	tx, err = SignAccept(words, "encryptlock-local", "7", 4, 12)
	check(tx, err, 4)
	tx, err = SignCancel(words, "encryptlock-local", "7", 5, 12)
	check(tx, err, 5)
	tx, err = SignFreeze(words, "encryptlock-local", "7", 6)
	check(tx, err, 6)
	tx, err = SignUnfreeze(words, "encryptlock-local", "7", 7)
	check(tx, err, 7)
	tx, err = SignFreezeFor(words, "encryptlock-local", "7", 8, to)
	check(tx, err, 8)
	tx, err = SignSetDevice(words, "encryptlock-local", "7", 9, to)
	check(tx, err, 9)
	tx, err = SignSetDevice(words, "encryptlock-local", "7", 10, "")
	check(tx, err, 10)
	tx, err = SignSetLimits(words, "encryptlock-local", "7", 11, 500_000_000, 100_000_000)
	check(tx, err, 11)
	tx, err = SignApprove(words, "encryptlock-local", "7", 12, 3)
	check(tx, err, 12)
	if _, err := SignSetLimits(words, "c", "1", 1, -1, 0); err == nil {
		t.Fatal("negative limit accepted")
	}
	plan := `{"heirs":[{"address":"` + to + `","percent":100}],"inactivity_days":365,"guardians":[],"guardian_threshold":0,"heir_threshold":0}`
	tx, err = SignSetPlan(words, "encryptlock-local", "7", 13, plan)
	check(tx, err, 13)
	tx, err = SignAlive(words, "encryptlock-local", "7", 14)
	check(tx, err, 14)
	tx, err = SignCancelClaims(words, "encryptlock-local", "7", 15)
	check(tx, err, 15)
	tx, err = SignClaimInheritance(words, "encryptlock-local", "7", 16, to)
	check(tx, err, 16)
	tx, err = SignApproveRecovery(words, "encryptlock-local", "7", 17, to, from)
	check(tx, err, 17)
	tx, err = SignSetVault(words, "encryptlock-local", "7", 18, 30)
	check(tx, err, 18)
	tx, err = SignLock(words, "encryptlock-local", "7", 19, 100000000, 1900000000)
	check(tx, err, 19)
	if _, err := SignSetPlan(words, "c", "1", 1, "not json"); err == nil {
		t.Fatal("bad plan accepted")
	}
	tx, err = SignCollectRewards(words, "encryptlock-local", "7", 20)
	check(tx, err, 20)
	tx, err = SignRegister(words, "encryptlock-local", "0", 0)
	check(tx, err, 0)
	if _, err := SignSend(words, "c", "1", 1, "nonsense", 1); err == nil {
		t.Fatal("bad address accepted")
	}
	if _, err := SignSend(words, "c", "1", 1, to, 0); err == nil {
		t.Fatal("0 accepted")
	}
	if !IsAddress(to) || IsAddress("lock1abc") {
		t.Fatal("IsAddress")
	}
}

func TestFeeAndAmounts(t *testing.T) {
	for _, units := range []int64{1, 2_000_000, 20_000_000, 100_000_000, 999_999_999_999} {
		if TransferFee(units) != int64(transfers.TransferFee(uint64(units))) {
			t.Fatalf("fee of %d differs from the network rule", units)
		}
	}
	if u, err := ParseLock(" 1,5 "); err != nil || u != 150_000_000 {
		t.Fatalf("1,5 -> %d %v", u, err)
	}
	if _, err := ParseLock("0,123456789"); err == nil {
		t.Fatal("9 decimals accepted")
	}
}

func TestPolygonAddress(t *testing.T) {
	// Well-known test vector: these words give this first Ethereum/Polygon
	// address in every standard wallet.
	words := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	got, err := PolygonAddress(words)
	if err != nil {
		t.Fatal(err)
	}
	if got != "0x9858EfFD232B4033E47d90003D41EC34EcaEda94" {
		t.Fatalf("got %s", got)
	}
	if _, err := PolygonAddress("not valid words"); err == nil {
		t.Fatal("invalid words accepted")
	}
}

func TestMarketSigning(t *testing.T) {
	words, _ := NewMnemonic()
	secret, err := NewSwapSecret()
	if err != nil {
		t.Fatal(err)
	}
	hash, err := SwapHash(secret)
	if err != nil || len(hash) != 64 {
		t.Fatalf("hash %q: %v", hash, err)
	}
	poly := "0x1111111111111111111111111111111111111111"
	if _, err := SignPlaceOffer(words, "encryptlock-local", "1", 0, 1, 100_000_000, 1_000_000, poly, "", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := SignPlaceOffer(words, "encryptlock-local", "1", 0, 2, 100_000_000, 1_000_000, poly, hash, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := SignTakeOffer(words, "encryptlock-local", "1", 0, 3, poly, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := SignCancelOffer(words, "encryptlock-local", "1", 0, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := SignTakeOffer(words, "encryptlock-local", "1", 0, 3, poly, "abc"); err == nil {
		t.Fatal("a bad hash was accepted")
	}
}
