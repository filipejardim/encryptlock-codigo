package chip

import (
	"crypto/rsa"
	"crypto/x509"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"testing"
)

// A maker's root that is not in trusted/ is taken from the maker's own site
// over https (25 Oct 2026: a real AMD chip was refused). Real network: an
// AMD in-between certificate is checked with AMD's root left out of the
// list, so the checker must fetch it from ftpm.amd.com.
func TestRootFromMakersSite(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the internet")
	}
	raw, err := trusted.ReadFile("trusted/AMD/IntermediateCA/AMD-fTPM-RSA-RNFamily.crt")
	if err != nil {
		t.Fatal(err)
	}
	ica := parseCerts(raw)[0]
	saved := roots
	defer func() { roots = saved }()
	roots = x509.NewCertPool()
	_ = fs.WalkDir(trusted, "trusted", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.Contains(p, "/RootCA/") || strings.Contains(p, "AMDTPM") {
			return err
		}
		b, _ := trusted.ReadFile(p)
		for _, c := range parseCerts(b) {
			roots.AddCert(c)
		}
		return nil
	})
	maker, err := Check(Chip{Modulus: ica.PublicKey.(*rsa.PublicKey).N.Bytes(), Cert: ica.Raw})
	if err != nil || maker != "AMD" {
		t.Fatalf("maker %q, err %v", maker, err)
	}
}

// A self-signed root from a site that is not a chip maker's (Microsoft, a
// public certificate authority) is never trusted.
func TestRootMakersOnlyChipMakers(t *testing.T) {
	for host, want := range map[string]string{
		"https://ftpm.amd.com/pki/aia/x":     "AMD",
		"https://ekop.intel.com/x":           "Intel",
		"https://pki.infineon.com/x":         "Infineon",
		"https://www.microsoft.com/pkiops/x": "",
		"https://secure.globalsign.com/x":    "",
		"https://amd.com.evil.example/x":     "",
	} {
		u, _ := url.Parse(host)
		if got := rootMaker(u); got != want {
			t.Errorf("%s: %q, want %q", host, got, want)
		}
	}
}

// AMD's "PRG-ZP" family (Ryzen 2000) names its root in other bytes than the
// root's own certificate: the issuer is found by its key and signature
// (25 Oct 2026, a real chip of this family was refused).
func TestIssuerNameInOtherBytes(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the internet")
	}
	raw, err := os.ReadFile("testdata/AMD-fTPM-ICA-PRG-ZP.cer")
	if err != nil {
		t.Fatal(err)
	}
	ica := parseCerts(raw)[0]
	maker, err := Check(Chip{Modulus: ica.PublicKey.(*rsa.PublicKey).N.Bytes(), Cert: ica.Raw})
	if err != nil || maker != "AMD" {
		t.Fatalf("maker %q, err %v", maker, err)
	}
}
