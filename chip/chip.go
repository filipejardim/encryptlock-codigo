// Package chip proves that a LockKeeper runs on a real computer: one with the
// security chip that comes from the factory (TPM 2.0). Founder's decision
// (8 Oct 2026): one computer = one LockKeeper; cloud servers and virtual
// machines are not accepted.
//
// Every TPM has an endorsement key made in the factory and a certificate for
// it signed by the chip's manufacturer. This package reads the key, gets the
// certificate (from the chip, or from the manufacturer's server for AMD and
// Intel firmware chips) and checks it against the manufacturers' certificates
// in trusted/ (Microsoft's TrustedTpm.cab list, hardware makers only: virtual
// chips of cloud servers and virtual machines are not signed by them).
//
// The certificate alone is public: the secret test that the key really is
// inside this chip comes on top of this (step C2).
package chip

import (
	"bytes"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"embed"
	"encoding/asn1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Chip is what a computer's security chip shows about itself.
type Chip struct {
	// Modulus of the RSA endorsement key (big-endian).
	Modulus []byte
	// Cert is the manufacturer's certificate of that key (DER).
	Cert []byte
}

// Code is the chip's fingerprint kept by the network: the same chip always
// gives the same code, and it says nothing about the owner.
func (c Chip) Code() string {
	h := sha256.Sum256(bytes.TrimLeft(c.Modulus, "\x00"))
	return hex.EncodeToString(h[:])
}

//go:embed trusted
var trusted embed.FS

var (
	roots         = x509.NewCertPool()
	intermediates = x509.NewCertPool()
	vendorOf      = map[string]string{} // root subject -> manufacturer folder
)

func init() {
	_ = fs.WalkDir(trusted, "trusted", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		parts := strings.Split(p, "/") // trusted/<maker>/<RootCA|IntermediateCA>/<file>
		if len(parts) < 4 {
			return nil
		}
		raw, _ := trusted.ReadFile(p)
		for _, cert := range parseCerts(raw) {
			if parts[2] == "RootCA" {
				roots.AddCert(cert)
				vendorOf[string(cert.RawSubject)] = parts[1]
			} else {
				intermediates.AddCert(cert)
			}
		}
		return nil
	})
}

// EKCertDER returns the certificate inside what a chip keeps in its memory:
// the TCG header (10 01 00 + length) some chips put in front, and the filler
// bytes after it (Nuvoton chips pad the rest of the space with FF), are left
// out. Go refuses a certificate with anything around it. Nil: none inside.
func EKCertDER(raw []byte) []byte {
	if len(raw) > 5 && bytes.Equal(raw[:3], []byte{0x10, 0x01, 0x00}) {
		if n := int(binary.BigEndian.Uint16(raw[3:5])); len(raw) >= 5+n {
			raw = raw[5 : 5+n]
		}
	}
	var v asn1.RawValue
	if _, err := asn1.UnmarshalWithParams(raw, &v, "lax"); err != nil {
		return nil
	}
	if _, err := x509.ParseCertificate(v.FullBytes); err != nil {
		return nil
	}
	return v.FullBytes
}

// certFor returns the plain certificate in raw if it is for the RSA key with
// this modulus (nil otherwise).
func certFor(raw, modulus []byte) []byte {
	der := raw
	if _, err := x509.ParseCertificate(der); err != nil {
		if der = EKCertDER(raw); der == nil {
			return nil
		}
	}
	c, _ := x509.ParseCertificate(der)
	pub, ok := c.PublicKey.(*rsa.PublicKey)
	if !ok || !bytes.Equal(pub.N.Bytes(), bytes.TrimLeft(modulus, "\x00")) {
		return nil
	}
	return der
}

// serializedCert returns the certificate inside a certificate Windows saved
// in the registry (a list of properties: id, 1, length, data; the
// certificate is property 32).
func serializedCert(blob []byte) []byte {
	for len(blob) >= 12 {
		id := binary.LittleEndian.Uint32(blob)
		n := int(binary.LittleEndian.Uint32(blob[8:]))
		if n < 0 || 12+n > len(blob) {
			return nil
		}
		if id == 32 {
			return blob[12 : 12+n]
		}
		blob = blob[12+n:]
	}
	return nil
}

// parseCerts reads DER or PEM certificates.
func parseCerts(raw []byte) []*x509.Certificate {
	if c, err := x509.ParseCertificate(raw); err == nil {
		return []*x509.Certificate{c}
	}
	if der := EKCertDER(raw); der != nil {
		c, _ := x509.ParseCertificate(der)
		return []*x509.Certificate{c}
	}
	// Some makers' PEM files end their lines with a bare carriage return.
	raw = bytes.ReplaceAll(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")), []byte("\r"), []byte("\n"))
	var out []*x509.Certificate
	for {
		var b *pem.Block
		b, raw = pem.Decode(raw)
		if b == nil {
			return out
		}
		if c, err := x509.ParseCertificate(b.Bytes); err == nil {
			out = append(out, c)
		}
	}
}

var exponent = []byte{0x01, 0x00, 0x01}

// CertURLs are where the manufacturers of firmware chips (inside the
// processor) publish the certificate of an endorsement key, computed as the
// Trusted Computing Group's TPM tools do (AMD tested on the founder's PC).
func CertURLs(modulus []byte) []string {
	amd := sha256.Sum256(append([]byte{0x00, 0x00, 0x22, 0x22, 0x00, 0x01, 0x00, 0x01}, modulus...))
	intel := sha256.Sum256(append(append([]byte{}, modulus...), exponent...))
	return []string{
		"https://ftpm.amd.com/pki/aia/" + hex.EncodeToString(amd[:]),
		"https://ekop.intel.com/ekcertservice/" + url.QueryEscape(base64.URLEncoding.EncodeToString(intel[:])),
	}
}

// Fetch gets the manufacturer's certificate of an endorsement key that the
// chip does not keep itself.
func Fetch(modulus []byte) ([]byte, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	for _, u := range CertURLs(modulus) {
		res, err := client.Get(u)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(res.Body, 64<<10))
		res.Body.Close()
		if err != nil || res.StatusCode != http.StatusOK {
			continue
		}
		// Intel answers with JSON holding the certificate in base64url.
		const key = `"certificate":"`
		if i := bytes.Index(body, []byte(key)); i >= 0 {
			rest := body[i+len(key):]
			if j := bytes.IndexByte(rest, '"'); j > 0 {
				if der, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(string(rest[:j]), "=")); err == nil {
					body = der
				}
			}
		}
		if len(parseCerts(body)) > 0 {
			return body, nil
		}
	}
	return nil, errors.New("the chip's manufacturer has no certificate for it")
}

// makerSites: the only places in-between certificates are fetched from. The
// address comes inside the certificate the computer sends, so without this
// a forged certificate could make the checker ask anything, even computers
// inside the founder's own network.
var makerSites = []string{"amd.com", "intel.com", "infineon.com", "st.com", "nuvoton.com", "microsoft.com",
	"qualcomm.com", "microchip.com", "atmel.com", "globalsign.com", "nationz.com.cn", "nsing.com.sg"}

func fromMaker(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Port() != "" || u.User != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, site := range makerSites {
		if host == site || strings.HasSuffix(host, "."+site) {
			return true
		}
	}
	return false
}

// rootMakers: the chip makers whose own servers may also hand over the
// top certificate (the root) of a chain, when it is not in trusted/ (25 Oct
// 2026: a real AMD chip was refused, its processor family's root is published
// only on ftpm.amd.com, not in Microsoft's list). Only the makers' own sites,
// only over https: never Microsoft (it also signs virtual chips) nor a public
// certificate authority such as GlobalSign (it signs anything for anyone).
var rootMakers = map[string]string{"amd.com": "AMD", "intel.com": "Intel", "infineon.com": "Infineon",
	"st.com": "STMicro", "nuvoton.com": "Nuvoton", "qualcomm.com": "QC", "microchip.com": "Atmel",
	"atmel.com": "Atmel", "nationz.com.cn": "NationZ", "nsing.com.sg": "NationZ"}

// rootMaker returns the maker whose site u is ("" if none of rootMakers).
func rootMaker(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	for site, maker := range rootMakers {
		if host == site || strings.HasSuffix(host, "."+site) {
			return maker
		}
	}
	return ""
}

// fetched is an in-between certificate taken from a maker's site; maker is
// set when it came over https from one of rootMakers.
type fetched struct {
	cert  *x509.Certificate
	maker string
}

// fetchIssuer gets the certificate at u, trying https first (the makers'
// addresses inside certificates are mostly plain http).
func fetchIssuer(u string) *fetched {
	if !fromMaker(u) {
		return nil
	}
	tries := []string{u}
	if strings.HasPrefix(u, "http://") {
		tries = []string{"https://" + strings.TrimPrefix(u, "http://"), u}
	}
	for _, try := range tries {
		if c := getCert(try); c != nil {
			f := &fetched{cert: c}
			if p, err := url.Parse(try); err == nil && p.Scheme == "https" {
				f.maker = rootMaker(p)
			}
			return f
		}
	}
	return nil
}

func getCert(u string) *x509.Certificate {
	client := &http.Client{
		Timeout: 15 * time.Second,
		// Redirects must stay on the makers' sites too (and on https when
		// the first address was https).
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 3 || !fromMaker(req.URL.String()) || (via[0].URL.Scheme == "https" && req.URL.Scheme != "https") {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	res, err := client.Get(u)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if err != nil || res.StatusCode != http.StatusOK {
		return nil
	}
	if certs := parseCerts(body); len(certs) > 0 {
		return certs[0]
	}
	return nil
}

// ErrNotReal: the certificate is not from a manufacturer of real chips (a
// virtual chip of a cloud server or virtual machine, or a forged one).
var ErrNotReal = errors.New("not a security chip from a real computer's manufacturer")

// Check confirms the certificate belongs to this endorsement key and was
// signed by a manufacturer of real chips. It returns the manufacturer.
func Check(c Chip) (string, error) {
	certs := parseCerts(c.Cert)
	if len(certs) == 0 {
		return "", errors.New("unreadable chip certificate")
	}
	cert := certs[0]
	pub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok || !bytes.Equal(pub.N.Bytes(), bytes.TrimLeft(c.Modulus, "\x00")) {
		return "", errors.New("the certificate is not for this chip's key")
	}
	// EK certificates carry TPM-specific critical extensions (who made the
	// chip); they are informative here.
	cert.UnhandledCriticalExtensions = nil
	opts := x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}
	vendor := vendorOf
	chains, err := verifyAnyTime(cert, opts)
	top := cert
	if err != nil {
		// Not every manufacturer's in-between certificate is in the list:
		// fetch them from the address in the certificate. That is safe: the
		// chain must still end at a manufacturer's root, in trusted/ or
		// handed over by the maker's own site over https (rootMakers).
		opts.Intermediates = intermediates.Clone()
		opts.Roots = roots.Clone()
		vendor = map[string]string{}
		for k, v := range vendorOf {
			vendor[k] = v
		}
		for i := 0; i < 4 && len(top.IssuingCertificateURL) > 0; i++ {
			up := fetchIssuer(top.IssuingCertificateURL[0])
			if up == nil {
				break
			}
			up.cert.UnhandledCriticalExtensions = nil
			if selfSigned(up.cert) {
				if up.maker != "" {
					opts.Roots.AddCert(up.cert)
					vendor[string(up.cert.RawSubject)] = up.maker
				}
				top = up.cert
				break
			}
			opts.Intermediates.AddCert(up.cert)
			top = up.cert
		}
		chains, err = verifyAnyTime(cert, opts)
	}
	if err != nil {
		return "", fmt.Errorf("%w: %v (last certificate reached: %q, issued by %q, key %X, from %v)", ErrNotReal, err,
			top.Subject.CommonName, top.Issuer.CommonName, top.AuthorityKeyId, top.IssuingCertificateURL)
	}
	for _, chain := range chains {
		if plutonOK(chain) {
			return vendor[string(chain[len(chain)-1].RawSubject)], nil
		}
	}
	return "", fmt.Errorf("%w: Microsoft root outside the Pluton factory line", ErrNotReal)
}

// selfSigned: a root certificate (it signs itself).
func selfSigned(c *x509.Certificate) bool {
	return bytes.Equal(c.RawSubject, c.RawIssuer) && c.CheckSignatureFrom(c) == nil
}

// plutonRoot is Microsoft's root for Pluton, the security chip inside newer
// processors (AMD, Intel, Qualcomm). Microsoft also signs virtual chips, so a
// chain to this root counts only through Pluton's factory certificates
// ("Pluton Factory ... EK ICA", one per processor family). Microsoft's other
// TPM roots are not trusted at all (trusted/ORIGEM.md).
const plutonRoot = "Microsoft Pluton Root CA 2021"

func plutonOK(chain []*x509.Certificate) bool {
	if chain[len(chain)-1].Subject.CommonName != plutonRoot {
		return true
	}
	for _, c := range chain[1 : len(chain)-1] {
		if strings.HasPrefix(c.Subject.CommonName, "Pluton Factory ") && strings.Contains(c.Subject.CommonName, " EK ICA ") {
			return true
		}
	}
	return false
}

// verifyAnyTime checks the chain at moments while the chip's certificate was
// valid: chips outlive their certificates' dates, and a maker may have issued
// its own certificate a few hours after the chip's (same day).
func verifyAnyTime(cert *x509.Certificate, opts x509.VerifyOptions) ([][]*x509.Certificate, error) {
	var err error
	for _, after := range []time.Duration{time.Minute, 24 * time.Hour, 30 * 24 * time.Hour, 365 * 24 * time.Hour} {
		at := cert.NotBefore.Add(after)
		if at.After(cert.NotAfter) {
			break
		}
		opts.CurrentTime = at
		var chains [][]*x509.Certificate
		if chains, err = cert.Verify(opts); err == nil {
			return chains, nil
		}
	}
	return nil, err
}
