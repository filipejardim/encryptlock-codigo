package chip

import (
	"strings"
	"testing"
)

// A chip counts only while a LockKeeper works with it: an account that never
// switched on, or is off, does not hold it (founder, 26 Oct 2026).
func TestChipIsFreeUnlessALockKeeperWorksWithIt(t *testing.T) {
	ch := Chip{Modulus: []byte{0, 1, 2, 3, 4}}
	type use struct {
		account string
		working bool
	}
	network := map[string]use{}
	c := &Checker{Owner: func(code string) (string, bool, error) {
		u := network[code]
		return u.account, u.working, nil
	}}

	// A new chip: its first code.
	code, err := c.freeCode(ch, "lock1aaa")
	if err != nil || code != ch.Code() {
		t.Fatalf("new chip: %q %v", code, err)
	}
	network[code] = use{"lock1aaa", false}

	// Asked again by the same account: the same code.
	if again, _ := c.freeCode(ch, "lock1aaa"); again != code {
		t.Fatal("same account must get the same code")
	}

	// The first account never switched on: another account gets a new code.
	second, err := c.freeCode(ch, "lock1bbb")
	if err != nil || second == code {
		t.Fatalf("chip held by an account that never worked: %q %v", second, err)
	}
	network[second] = use{"lock1bbb", true}

	// Now a LockKeeper works with it: a third account is refused, with only
	// "lock1" and one letter of the other account.
	_, err = c.freeCode(ch, "lock1ccc")
	if err == nil || !strings.Contains(err.Error(), "(lock1b…)") || strings.Contains(err.Error(), "lock1bbb") {
		t.Fatalf("chip in use: %v", err)
	}

	// It switches off: the chip is free again.
	network[second] = use{"lock1bbb", false}
	third, err := c.freeCode(ch, "lock1ccc")
	if err != nil || third == code || third == second {
		t.Fatalf("chip free again: %q %v", third, err)
	}
}
