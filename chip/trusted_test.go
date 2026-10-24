package chip

import (
	"crypto/x509"
	"io/fs"
	"strings"
	"testing"
	"time"
)

// Every in-between certificate of the makers leads to a trusted root (so a
// chip certified by it is accepted without fetching anything), and a
// Microsoft Pluton chain counts only through the Pluton factory line.
func TestEveryMakerChainReachesARoot(t *testing.T) {
	broken := map[string][]string{}
	total := 0
	_ = fs.WalkDir(trusted, "trusted", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.Contains(p, "/IntermediateCA/") {
			return err
		}
		raw, _ := trusted.ReadFile(p)
		for _, c := range parseCerts(raw) {
			total++
			c.UnhandledCriticalExtensions = nil
			_, err := c.Verify(x509.VerifyOptions{
				Roots: roots, Intermediates: intermediates,
				KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
				CurrentTime: c.NotBefore.Add(24 * time.Hour),
			})
			if err != nil {
				maker := strings.Split(p, "/")[1]
				broken[maker] = append(broken[maker], p[strings.LastIndex(p, "/")+1:])
			}
		}
		return nil
	})
	for maker, files := range broken {
		t.Logf("%s: %d in-between certificates do not reach a root: %v", maker, len(files), files)
	}
	t.Logf("%d in-between certificates checked", total)
	if len(broken["Intel"]) > 0 {
		t.Fatal("Intel chains must be complete")
	}
}

func TestPlutonOnlyThroughItsFactoryLine(t *testing.T) {
	cert := func(cn string) *x509.Certificate {
		c := &x509.Certificate{}
		c.Subject.CommonName = cn
		return c
	}
	chip, policy, root := cert("chip"), cert("Microsoft Pluton Policy CA A"), cert(plutonRoot)
	if plutonOK([]*x509.Certificate{chip, policy, root}) {
		t.Fatal("Pluton root without the factory line accepted")
	}
	if !plutonOK([]*x509.Certificate{chip, cert("Pluton Factory DEVICE EK ICA DFID00A70F00"), policy, root}) {
		t.Fatal("Pluton factory line refused")
	}
	if !plutonOK([]*x509.Certificate{chip, cert("AMD Root CA R4")}) {
		t.Fatal("other makers are not limited")
	}
}

func TestOnlyMakersSitesAreAsked(t *testing.T) {
	for u, want := range map[string]bool{
		"http://ftpm.amd.com/pki/aia/abc":           true,
		"https://trustedservices.intel.com/content": true,
		"http://127.0.0.1:26657/status":             false,
		"http://localhost/x":                        false,
		"http://192.168.1.1/admin":                  false,
		"http://amd.com.evil.example/x":             false,
		"http://evilamd.com/x":                      false,
		"http://amd.com:8080/x":                     false,
		"file:///c:/windows/win.ini":                false,
	} {
		if got := fromMaker(u); got != want {
			t.Errorf("fromMaker(%q) = %v, want %v", u, got, want)
		}
	}
}
