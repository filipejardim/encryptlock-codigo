package cmd

import "testing"

// The door host from the website goes into a command: plain host names only.
func TestDoorHostOnlyPlainNames(t *testing.T) {
	for _, ok := range []string{"e-f.trycloudflare.com", "a.b", "x1.y2.z3"} {
		if !doorHost.MatchString(ok) {
			t.Errorf("%q refused", ok)
		}
	}
	for _, bad := range []string{"-x.com", "x-.com", "x.com --url 0.0.0.0:22", "x.com\"", "X.COM", "nodots", "", "x.com\n"} {
		if doorHost.MatchString(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}
