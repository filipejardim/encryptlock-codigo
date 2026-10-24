package chip

import (
	"crypto/rsa"
	"crypto/subtle"
	"errors"
	"math/big"

	"github.com/google/go-attestation/attest"
)

// The secret test (step C2). The chip's certificate is public: anyone could
// copy another computer's. So the computer also makes a new key inside its
// chip (the attestation key, AK) and the checker asks a question locked to
// the chip's endorsement key AND that new key together (TPM2_MakeCredential).
// Only the real chip that holds both can open it and give back the secret
// (TPM2_ActivateCredential). A copied certificate or a virtual chip cannot.
//
// The cryptography is Google's go-attestation (Apache 2.0), built for this.

// Question is the locked question; Want is the secret the right answer must
// match (kept by the checker, never sent).
type Question = attest.EncryptedCredential

// Ask makes the secret question for the chip c and the attestation key the
// computer made in it. It refuses keys that are not proper chip-made
// attestation keys.
func Ask(c Chip, ak attest.AttestationParameters) (want []byte, q *Question, err error) {
	ek := &rsa.PublicKey{N: new(big.Int).SetBytes(c.Modulus), E: 65537}
	p := attest.ActivationParameters{EK: ek, AK: ak}
	return p.Generate()
}

// ErrWrongAnswer: the computer could not open the question - its chip is not
// the one in the certificate.
var ErrWrongAnswer = errors.New("the security chip did not pass the secret test")

// Right checks the computer's answer.
func Right(want, answer []byte) error {
	if len(want) == 0 || subtle.ConstantTimeCompare(want, answer) != 1 {
		return ErrWrongAnswer
	}
	return nil
}
