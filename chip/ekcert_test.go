package chip

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"math/big"
	"testing"
	"time"
)

func testEKCert(t *testing.T) ([]byte, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "ek"}, NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return der, key
}

// Nuvoton chips keep the certificate with FF filler after it; some chips put
// the TCG header in front. Both must still give the plain certificate.
func TestEKCertWithFillerAndHeader(t *testing.T) {
	der, key := testEKCert(t)
	padded := append(append([]byte{}, der...), bytes.Repeat([]byte{0xFF}, 300)...)
	if _, err := x509.ParseCertificate(padded); err == nil {
		t.Fatal("Go was expected to refuse the padded certificate")
	}
	header := append([]byte{0x10, 0x01, 0x00, 0, 0}, padded...)
	binary.BigEndian.PutUint16(header[3:5], uint16(len(der)))
	for name, raw := range map[string][]byte{"plain": der, "filler": padded, "header": header} {
		if got := EKCertDER(raw); !bytes.Equal(got, der) {
			t.Fatalf("%s: certificate not found", name)
		}
		if got := certFor(raw, key.PublicKey.N.Bytes()); !bytes.Equal(got, der) {
			t.Fatalf("%s: not matched to its key", name)
		}
		if len(parseCerts(raw)) != 1 {
			t.Fatalf("%s: parseCerts", name)
		}
	}
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	if certFor(padded, other.PublicKey.N.Bytes()) != nil {
		t.Fatal("a certificate for another key was accepted")
	}
	if EKCertDER(bytes.Repeat([]byte{0xFF}, 100)) != nil {
		t.Fatal("filler alone is not a certificate")
	}
}

// Windows keeps the certificates in the registry as a list of properties;
// the certificate is property 32.
func TestSerializedCertFromRegistry(t *testing.T) {
	der, _ := testEKCert(t)
	prop := func(id uint32, data []byte) []byte {
		b := make([]byte, 12)
		binary.LittleEndian.PutUint32(b, id)
		binary.LittleEndian.PutUint32(b[4:], 1)
		binary.LittleEndian.PutUint32(b[8:], uint32(len(data)))
		return append(b, data...)
	}
	blob := append(prop(3, []byte("thumbprint-bytes-20b")), prop(32, der)...)
	if !bytes.Equal(serializedCert(blob), der) {
		t.Fatal("certificate not found in the registry blob")
	}
	if serializedCert(blob[:20]) != nil {
		t.Fatal("a cut blob must give nothing")
	}
}
