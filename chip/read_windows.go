//go:build windows

package chip

import (
	"encoding/binary"
	"errors"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

var (
	ncrypt      = syscall.NewLazyDLL("ncrypt.dll")
	openStorage = ncrypt.NewProc("NCryptOpenStorageProvider")
	getProperty = ncrypt.NewProc("NCryptGetProperty")
	freeObject  = ncrypt.NewProc("NCryptFreeObject")
)

// Read asks Windows for this computer's security chip (no administrator
// needed): the endorsement key and, if the chip keeps it, its certificate;
// otherwise the certificate comes from the manufacturer's server.
func Read() (Chip, error) {
	var h uintptr
	name, _ := syscall.UTF16PtrFromString("Microsoft Platform Crypto Provider")
	if r, _, _ := openStorage.Call(uintptr(unsafe.Pointer(&h)), uintptr(unsafe.Pointer(name)), 0); r != 0 {
		return Chip{}, errors.New("this computer has no security chip (TPM 2.0), or it is switched off")
	}
	defer freeObject.Call(h)
	blob, err := property(h, "PCP_RSA_EKPUB")
	if err != nil || len(blob) < 24 {
		return Chip{}, errors.New("cannot read the security chip's key")
	}
	// BCRYPT_RSAKEY_BLOB: magic, bits, exponent size, modulus size, 2 x 0.
	expLen := binary.LittleEndian.Uint32(blob[8:])
	modLen := binary.LittleEndian.Uint32(blob[12:])
	if int(24+expLen+modLen) > len(blob) {
		return Chip{}, errors.New("cannot read the security chip's key")
	}
	c := Chip{Modulus: blob[24+expLen : 24+expLen+modLen]}
	// The certificate kept in the chip, then the copies Windows keeps in the
	// registry. Only the plain certificate goes to the checker (without the
	// header or filler bytes some chips keep around it).
	var kept [][]byte
	for _, p := range []string{"PCP_RSA_EKNVCERT", "PCP_EKCERT"} {
		if cert, err := property(h, p); err == nil {
			kept = append(kept, cert)
		}
	}
	kept = append(kept, registryEKCerts()...)
	for _, raw := range kept {
		if der := certFor(raw, c.Modulus); der != nil {
			c.Cert = der
			return c, nil
		}
	}
	if c.Cert, err = Fetch(c.Modulus); err != nil {
		return Chip{}, err
	}
	return c, nil
}

func property(h uintptr, prop string) ([]byte, error) {
	p, _ := syscall.UTF16PtrFromString(prop)
	var n uint32
	if r, _, _ := getProperty.Call(h, uintptr(unsafe.Pointer(p)), 0, 0, uintptr(unsafe.Pointer(&n)), 0); r != 0 || n == 0 {
		return nil, errors.New(prop)
	}
	buf := make([]byte, n)
	if r, _, _ := getProperty.Call(h, uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&buf[0])), uintptr(n), uintptr(unsafe.Pointer(&n)), 0); r != 0 {
		return nil, errors.New(prop)
	}
	return buf[:n], nil
}

// registryEKCerts returns the chip certificates Windows keeps in the
// registry (it saves them there when it looks after the chip).
func registryEKCerts() [][]byte {
	const path = `SYSTEM\CurrentControlSet\Services\TPM\WMI\Endorsement\EKCertStore\Certificates`
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil
	}
	defer k.Close()
	names, _ := k.ReadSubKeyNames(-1)
	var out [][]byte
	for _, name := range names {
		sub, err := registry.OpenKey(k, name, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		if blob, _, err := sub.GetBinaryValue("Blob"); err == nil {
			if cert := serializedCert(blob); cert != nil {
				out = append(out, cert)
			}
		}
		sub.Close()
	}
	return out
}
