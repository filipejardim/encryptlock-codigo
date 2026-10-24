package mobilecore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters for the wallet password (RFC 9106, second recommended
// option): 64 MiB of memory makes guessing with graphics cards expensive.
const (
	argonTime    = 3
	argonMemory  = 64 * 1024 // KiB
	argonThreads = 4
	argonKeyLen  = 32
)

// sealed is how the encrypted 24 words are kept on the phone. On Android the
// whole record is encrypted once more with a key that never leaves the
// phone's security chip (Android Keystore).
type sealed struct {
	V      int    `json:"v"`
	KDF    string `json:"kdf"`
	Time   uint32 `json:"t"`
	Memory uint32 `json:"m"`
	Thread uint8  `json:"p"`
	Salt   []byte `json:"salt"`
	Nonce  []byte `json:"nonce"`
	Cipher []byte `json:"cipher"`
}

func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func gcm(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// SealWords encrypts the 24 words with the password: Argon2id + AES-256-GCM.
func SealWords(words, password string) (string, error) {
	if password == "" {
		return "", errors.New("empty password")
	}
	s := sealed{V: 2, KDF: "argon2id", Time: argonTime, Memory: argonMemory, Thread: argonThreads,
		Salt: make([]byte, 16), Nonce: make([]byte, 12)}
	if _, err := rand.Read(s.Salt); err != nil {
		return "", err
	}
	if _, err := rand.Read(s.Nonce); err != nil {
		return "", err
	}
	pw := []byte(password)
	key := argon2.IDKey(pw, s.Salt, s.Time, s.Memory, s.Thread, argonKeyLen)
	wipe(pw)
	defer wipe(key)
	aead, err := gcm(key)
	if err != nil {
		return "", err
	}
	plain := []byte(words)
	s.Cipher = aead.Seal(nil, s.Nonce, plain, nil)
	wipe(plain)
	out, err := json.Marshal(s)
	return string(out), err
}

// OpenWords decrypts what SealWords made. A wrong password returns "" and no
// error; a damaged record returns an error.
func OpenWords(record, password string) (string, error) {
	var s sealed
	if err := json.Unmarshal([]byte(record), &s); err != nil {
		return "", err
	}
	if s.V != 2 || s.KDF != "argon2id" {
		return "", fmt.Errorf("unknown wallet format %d %q", s.V, s.KDF)
	}
	// Refuse absurd parameters (a damaged record must not freeze the phone).
	if s.Time == 0 || s.Time > 10 || s.Memory < 8*1024 || s.Memory > 256*1024 || s.Thread == 0 || len(s.Nonce) != 12 {
		return "", errors.New("damaged wallet record")
	}
	pw := []byte(password)
	key := argon2.IDKey(pw, s.Salt, s.Time, s.Memory, s.Thread, argonKeyLen)
	wipe(pw)
	defer wipe(key)
	aead, err := gcm(key)
	if err != nil {
		return "", err
	}
	plain, err := aead.Open(nil, s.Nonce, s.Cipher, nil)
	if err != nil {
		return "", nil
	}
	return string(plain), nil
}
