//go:build windows

package chip

import (
	"io"

	"github.com/google/go-attestation/attest"
	"github.com/google/go-tpm/legacy/tpm2"
)

// Answerer is this computer's side of the secret test: an attestation key
// made inside its chip.
//
// It talks to the chip directly (Windows TPM Base Services). Tested on the
// founder's AMD chip (8 Oct 2026): through Windows' key provider (PCP) the
// chip answers "value out of range", and directly Windows blocks the answer
// for normal users (0x80280400), so this runs with administrator rights -
// Windows asks "Allow?" once, when the computer becomes a LockKeeper.
type Answerer struct {
	tpm *attest.TPM
	ak  *attest.AK
}

type channel struct{ io.ReadWriteCloser }

func (channel) MeasurementLog() ([]byte, error) { return nil, nil }

// NewAnswerer makes a new attestation key in this computer's chip; its
// parameters go to the checker with the chip's certificate.
func NewAnswerer() (*Answerer, attest.AttestationParameters, error) {
	rwc, err := tpm2.OpenTPM()
	if err != nil {
		return nil, attest.AttestationParameters{}, err
	}
	tpm, err := attest.OpenTPM(&attest.OpenConfig{CommandChannel: channel{rwc}})
	if err != nil {
		rwc.Close()
		return nil, attest.AttestationParameters{}, err
	}
	ak, err := tpm.NewAK(nil)
	if err != nil {
		tpm.Close()
		return nil, attest.AttestationParameters{}, err
	}
	return &Answerer{tpm: tpm, ak: ak}, ak.AttestationParameters(), nil
}

// Answer opens the checker's question with the chip.
func (a *Answerer) Answer(q *Question) ([]byte, error) {
	return a.ak.ActivateCredential(a.tpm, *q)
}

// Close forgets the attestation key.
func (a *Answerer) Close() {
	_ = a.ak.Close(a.tpm)
	_ = a.tpm.Close()
}
