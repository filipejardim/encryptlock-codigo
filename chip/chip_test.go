package chip

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"testing"
	"time"
)

// selfMade is a certificate anyone can make for a key, as a fake chip or a
// virtual machine would show.
func selfMade(t *testing.T) (Chip, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "Advanced Micro Devices"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return Chip{Modulus: key.N.Bytes(), Cert: der}, key
}

func TestSelfMadeCertificateIsRefused(t *testing.T) {
	c, _ := selfMade(t)
	if _, err := Check(c); !errors.Is(err, ErrNotReal) {
		t.Fatalf("a self-made certificate must be refused, got %v", err)
	}
}

func TestCertificateOfAnotherKeyIsRefused(t *testing.T) {
	c, _ := selfMade(t)
	other, _ := selfMade(t)
	c.Modulus = other.Modulus
	if _, err := Check(c); err == nil || errors.Is(err, ErrNotReal) {
		t.Fatalf("a certificate for another key must be refused as such, got %v", err)
	}
}

func TestManufacturersAreLoaded(t *testing.T) {
	makers := map[string]bool{}
	for _, m := range vendorOf {
		makers[m] = true
	}
	for _, m := range []string{"AMD", "Intel", "Infineon", "STMicro", "Nuvoton"} {
		if !makers[m] {
			t.Errorf("no root certificate of %s", m)
		}
	}
}

func TestSameChipSameCode(t *testing.T) {
	c, _ := selfMade(t)
	padded := Chip{Modulus: append([]byte{0}, c.Modulus...)}
	if c.Code() != padded.Code() || len(c.Code()) != 64 {
		t.Fatal("the code must depend only on the chip's key")
	}
}
