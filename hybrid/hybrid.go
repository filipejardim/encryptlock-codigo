// Package hybrid implements EncryptLock's hybrid account keys (white paper,
// section 5.1): every account is a 2-of-2 Cosmos multisig made of one Ed25519
// key and one ML-DSA-65 (post-quantum) key, so every transaction carries both
// signatures. No new cryptography: Ed25519, ML-DSA-65 and multisig are the
// Cosmos SDK's own implementations.
package hybrid

import (
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/crypto/keys/mldsa65"
	"github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
)

// Threshold: both keys must sign.
const Threshold = 2

// NewPubKey returns the hybrid account key for an Ed25519 and an ML-DSA-65 key.
func NewPubKey(ed *ed25519.PubKey, pq *mldsa65.PubKey) cryptotypes.PubKey {
	return multisig.NewLegacyAminoPubKey(Threshold, []cryptotypes.PubKey{ed, pq})
}

// IsHybrid reports whether pk is a valid EncryptLock account key: a 2-of-2
// multisig of exactly one Ed25519 and one ML-DSA-65 key (in either order).
func IsHybrid(pk cryptotypes.PubKey) bool {
	multi, ok := pk.(*multisig.LegacyAminoPubKey)
	if !ok || multi.Threshold != Threshold {
		return false
	}
	keys := multi.GetPubKeys()
	if len(keys) != 2 {
		return false
	}
	var hasEd, hasPQ bool
	for _, k := range keys {
		switch k.(type) {
		case *ed25519.PubKey:
			hasEd = true
		case *mldsa65.PubKey:
			hasPQ = true
		}
	}
	return hasEd && hasPQ
}

// ed25519Algo lets the Cosmos keyring create Ed25519 keys from a recovery
// phrase, the same way the SDK does for ML-DSA-65 (crypto/hd/mldsa65.go):
// standard BIP-32 derivation gives 32 secret bytes, which seed the key.
type ed25519Algo struct{}

func (ed25519Algo) Name() hd.PubKeyType { return hd.Ed25519Type }

func (ed25519Algo) Derive() hd.DeriveFn { return hd.Secp256k1.Derive() }

func (ed25519Algo) Generate() hd.GenerateFn {
	return func(bz []byte) cryptotypes.PrivKey {
		return ed25519.GenPrivKeyFromSecret(bz)
	}
}

// Ed25519 is the keyring algorithm for the Ed25519 half of a hybrid account.
var Ed25519 keyring.SignatureAlgo = ed25519Algo{}

// KeyringOption adds Ed25519 to the algorithms the keyring can create.
func KeyringOption() keyring.Option {
	return func(options *keyring.Options) {
		options.SupportedAlgos = append(options.SupportedAlgos, Ed25519)
	}
}

// MaxTeamKeys: a team account has at most this many keys (each one makes
// every LockKeeper check two more signatures, for free).
const MaxTeamKeys = 10

// IsTeam reports whether pk is a team account key: an M-of-N multisig
// (2 ≤ M ≤ N) where every key is itself hybrid. The development fund uses one
// (2 of 3), so it is post-quantum too and survives the loss of one key.
func IsTeam(pk cryptotypes.PubKey) bool {
	multi, ok := pk.(*multisig.LegacyAminoPubKey)
	if !ok {
		return false
	}
	keys := multi.GetPubKeys()
	if multi.Threshold < 2 || int(multi.Threshold) > len(keys) || len(keys) > MaxTeamKeys {
		return false
	}
	for _, k := range keys {
		if !IsHybrid(k) {
			return false
		}
	}
	return true
}

// IsAllowed reports whether pk can own an EncryptLock account: a hybrid key
// or a team of hybrid keys.
func IsAllowed(pk cryptotypes.PubKey) bool {
	return IsHybrid(pk) || IsTeam(pk)
}
