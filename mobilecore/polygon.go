package mobilecore

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/go-bip39"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"golang.org/x/crypto/sha3"
)

// polygonPath is the standard path of the first Ethereum-style account
// (BIP-44, coin 60). Polygon uses the same addresses as Ethereum, so the 24
// words give the same USDC address in any other wallet too.
const polygonPath = "m/44'/60'/0'/0/0"

// polygonKey derives the secp256k1 key of the wallet's Polygon account.
func polygonKey(words string) (*secp256k1.PrivateKey, error) {
	words = strings.Join(strings.Fields(words), " ")
	if !bip39.IsMnemonicValid(words) {
		return nil, fmt.Errorf("invalid recovery words")
	}
	seed := bip39.NewSeed(words, "")
	master, chain := hd.ComputeMastersFromSeed(seed)
	raw, err := hd.DerivePrivateKeyForPath(master, chain, polygonPath)
	if err != nil {
		return nil, err
	}
	return secp256k1.PrivKeyFromBytes(raw), nil
}

// PolygonAddress is the wallet's address on Polygon (where its USDC and POL
// live), with the usual mixed-case checksum (EIP-55).
func PolygonAddress(words string) (string, error) {
	key, err := polygonKey(words)
	if err != nil {
		return "", err
	}
	pub := key.PubKey().SerializeUncompressed()
	h := sha3.NewLegacyKeccak256()
	h.Write(pub[1:])
	return checksumAddress(h.Sum(nil)[12:]), nil
}

// checksumAddress writes a 20-byte address as 0x… with EIP-55 capitals.
func checksumAddress(addr []byte) string {
	lower := hex.EncodeToString(addr)
	h := sha3.NewLegacyKeccak256()
	h.Write([]byte(lower))
	sum := h.Sum(nil)
	out := []byte(lower)
	for i, c := range out {
		nibble := sum[i/2]
		if i%2 == 0 {
			nibble >>= 4
		}
		if c >= 'a' && nibble&0xf >= 8 {
			out[i] = c - 32
		}
	}
	return "0x" + string(out)
}
