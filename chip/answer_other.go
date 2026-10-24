//go:build !windows

package chip

import (
	"errors"

	"github.com/google/go-attestation/attest"
)

// Answerer: LockKeepers on a computer are Windows only for now.
type Answerer struct{}

func NewAnswerer() (*Answerer, attest.AttestationParameters, error) {
	return nil, attest.AttestationParameters{}, errors.New("the secret test needs Windows")
}

func (a *Answerer) Answer(*Question) ([]byte, error) { return nil, errors.New("the secret test needs Windows") }

func (a *Answerer) Close() {}
