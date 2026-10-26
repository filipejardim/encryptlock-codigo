package mobilecore

import (
	"encoding/base64"
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keys/mldsa65"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	rotationtypes "encryptlock/lockkeeper/x/rotation/types"
)

func TestBecomeLockKeeperAndHeartbeat(t *testing.T) {
	words, _ := NewMnemonic()
	from, _ := Address(words)
	helperWords, _ := NewMnemonic()
	helper, _ := Address(helperWords)
	cons, err := mldsa65.GenPrivKey()
	if err != nil {
		t.Fatal(err)
	}
	consKey := base64.StdEncoding.EncodeToString(cons.PubKey().Bytes())
	cfg, _ := txConfig()

	b64, err := SignBecomeLockKeeper(words, "encryptlock-local", "7", 3, consKey, 250_000_000, "o meu PC", helper)
	if err != nil {
		t.Fatal(err)
	}
	bz, _ := base64.StdEncoding.DecodeString(b64)
	tx, err := cfg.TxDecoder()(bz)
	if err != nil {
		t.Fatal(err)
	}
	msgs := tx.GetMsgs()
	create, ok1 := msgs[0].(*stakingtypes.MsgCreateValidator)
	set, ok2 := msgs[1].(*rotationtypes.MsgSetHelper)
	if len(msgs) != 2 || !ok1 || !ok2 {
		t.Fatalf("messages %T", msgs)
	}
	if create.Value.Amount.Int64() != 250_000_000 || create.MinSelfDelegation.Int64() != 250_000_000 || create.Description.Moniker != "o meu PC" {
		t.Fatalf("create = %+v", create)
	}
	if set.Keeper != from || set.Helper != helper {
		t.Fatalf("helper = %+v", set)
	}
	if _, err := SignBecomeLockKeeper(words, "c", "7", 3, "", 1, "x", helper); err == nil {
		t.Error("an empty block-signing key was accepted")
	}

	b64, err = SignHeartbeat(helperWords, "encryptlock-local", "9", 0)
	if err != nil {
		t.Fatal(err)
	}
	bz, _ = base64.StdEncoding.DecodeString(b64)
	tx, _ = cfg.TxDecoder()(bz)
	if hb, ok := tx.GetMsgs()[0].(*rotationtypes.MsgHeartbeat); !ok || hb.Keeper != helper {
		t.Fatalf("heartbeat = %+v", tx.GetMsgs())
	}
}

func TestSignMoveLockKeeper(t *testing.T) {
	words, _ := NewMnemonic()
	helperWords, _ := NewMnemonic()
	helper, _ := Address(helperWords)
	key := base64.StdEncoding.EncodeToString(make([]byte, 1952))
	if _, err := SignMoveLockKeeper(words, "encryptlock-test", "1", 0, key, helper); err != nil {
		t.Fatalf("move with a new key: %v", err)
	}
	if _, err := SignMoveLockKeeper(words, "encryptlock-test", "1", 0, "", helper); err != nil {
		t.Fatalf("move keeping the key: %v", err)
	}
	if _, err := SignMoveLockKeeper(words, "encryptlock-test", "1", 0, "", "not-an-address"); err == nil {
		t.Fatal("bad helper accepted")
	}
}
