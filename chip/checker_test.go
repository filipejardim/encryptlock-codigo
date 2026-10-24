package chip

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"encryptlock/lockkeeper/x/rotation"
)

func TestCheckerSignsOnlyRightAnswersOnce(t *testing.T) {
	file, pub, err := NewCheckerKey()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), CheckerKeyFile)
	if err := os.WriteFile(path, []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadChecker(path, "encryptlock-test")
	if err != nil || c == nil {
		t.Fatalf("load: %v", err)
	}
	if none, err := LoadChecker(filepath.Join(t.TempDir(), "none"), "x"); none != nil || err != nil {
		t.Fatal("no key file: not a checker")
	}
	account := sdk.AccAddress("helper______________")
	c.sessions["s1"] = session{want: []byte("secret"), code: "abc", maker: "AMD", account: account, at: time.Now()}
	c.sessions["s2"] = session{want: []byte("secret"), code: "abc", maker: "AMD", account: account, at: time.Now()}

	if _, err := c.Answer(AnswerRequest{Session: "s1", Secret: []byte("wrong")}); err == nil {
		t.Fatal("wrong answer accepted")
	}
	if _, err := c.Answer(AnswerRequest{Session: "s1", Secret: []byte("secret")}); err == nil {
		t.Fatal("second try on the same question accepted")
	}
	tk, err := c.Answer(AnswerRequest{Session: "s2", Secret: []byte("secret")})
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(pub, rotation.TicketMessage("encryptlock-test", "abc", account), tk.Ticket) || tk.Maker != "AMD" {
		t.Fatalf("ticket does not verify: %+v", tk)
	}
}
