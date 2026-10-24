package chip

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/go-attestation/attest"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"encryptlock/lockkeeper/x/rotation"
)

// The network's chip checker (step C3). A computer that wants to become a
// LockKeeper shows its chip's certificate and a new attestation key made in
// its chip; the checker answers with the secret question. If the computer
// opens it, the checker signs the ticket "chip code -> account" that the
// network accepts (x/rotation, ProveChip). The checker never sees anything
// personal: only the chip's public key and the account address.
//
// It runs inside a LockKeeper of the network (first the founder's computer),
// at the same address as the network's REST API, when the LockKeeper's
// config folder holds chip-checker.key. Its public key is in the network's
// first block.

// CheckerKeyFile is the checker's key, in the LockKeeper's config folder.
const CheckerKeyFile = "chip-checker.key"

const (
	PathQuestion = "/encryptlock/chip/v1/question"
	PathAnswer   = "/encryptlock/chip/v1/answer"
	sessionLife  = 5 * time.Minute
	maxSessions  = 1000
	// perOrigin: open questions from one internet address at most.
	perOrigin = 3
)

// NewCheckerKey makes a checker key and returns the private key file's
// contents and the public key (for the first block).
func NewCheckerKey() (file string, pub []byte, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", nil, err
	}
	return base64.StdEncoding.EncodeToString(priv.Seed()), pub, nil
}

// QuestionRequest is what the computer sends first.
type QuestionRequest struct {
	Modulus []byte                       `json:"modulus"`
	Cert    []byte                       `json:"cert"`
	AK      attest.AttestationParameters `json:"ak"`
	Account string                       `json:"account"`
}

// QuestionResponse is the secret question locked to the chip.
type QuestionResponse struct {
	Session  string   `json:"session"`
	Question Question `json:"question"`
}

// AnswerRequest is the computer's answer.
type AnswerRequest struct {
	Session string `json:"session"`
	Secret  []byte `json:"secret"`
}

// Ticket is what the computer gets for a right answer.
type Ticket struct {
	Code   string `json:"code"`
	Maker  string `json:"maker"`
	Ticket []byte `json:"ticket"`
}

// Checker does the secret test and signs tickets.
type Checker struct {
	key     ed25519.PrivateKey
	chainID string

	mu       sync.Mutex
	sessions map[string]session
}

type session struct {
	want    []byte
	code    string
	maker   string
	account sdk.AccAddress
	at      time.Time
	origin  string
}

// LoadChecker reads the checker's key; nil (no error) when the file does not
// exist: this LockKeeper is not a checker.
func LoadChecker(path, chainID string) (*Checker, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("chip checker: bad key file")
	}
	return &Checker{key: ed25519.NewKeyFromSeed(seed), chainID: chainID, sessions: map[string]session{}}, nil
}

// Ask checks the chip's certificate and makes the secret question. origin
// is the asker's internet address (at most perOrigin open questions each).
func (c *Checker) Ask(req QuestionRequest, origin string) (*QuestionResponse, error) {
	account, err := sdk.AccAddressFromBech32(req.Account)
	if err != nil {
		return nil, errors.New("not an account address")
	}
	if err := c.room(origin); err != nil {
		return nil, err
	}
	ch := Chip{Modulus: req.Modulus, Cert: req.Cert}
	maker, err := Check(ch)
	if err != nil {
		return nil, err
	}
	want, q, err := Ask(ch, req.AK)
	if err != nil {
		return nil, err
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, s := range c.sessions {
		if time.Since(s.at) > sessionLife {
			delete(c.sessions, k)
		}
	}
	if len(c.sessions) >= maxSessions {
		return nil, errors.New("too many computers at once, try again in a few minutes")
	}
	c.sessions[hex.EncodeToString(id)] = session{want: want, code: ch.Code(), maker: maker, account: account, at: time.Now(), origin: origin}
	return &QuestionResponse{Session: hex.EncodeToString(id), Question: *q}, nil
}

// room forgets old questions and refuses a new one when the checker or this
// origin already has too many open.
func (c *Checker) room(origin string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	mine := 0
	for k, s := range c.sessions {
		if time.Since(s.at) > sessionLife {
			delete(c.sessions, k)
		} else if s.origin == origin {
			mine++
		}
	}
	if len(c.sessions) >= maxSessions || mine >= perOrigin {
		return errors.New("too many computers at once, try again in a few minutes")
	}
	return nil
}

// Answer checks the computer's answer and signs its ticket. Each question
// gets one try.
func (c *Checker) Answer(req AnswerRequest) (*Ticket, error) {
	c.mu.Lock()
	s, ok := c.sessions[req.Session]
	delete(c.sessions, req.Session)
	c.mu.Unlock()
	if !ok || time.Since(s.at) > sessionLife {
		return nil, errors.New("question expired, start again")
	}
	if err := Right(s.want, req.Secret); err != nil {
		return nil, err
	}
	sig := ed25519.Sign(c.key, rotation.TicketMessage(c.chainID, s.code, s.account))
	return &Ticket{Code: s.code, Maker: s.maker, Ticket: sig}, nil
}

// Handler serves the two steps over HTTP (POST, JSON).
func (c *Checker) Handler() http.Handler {
	mux := http.NewServeMux()
	serve := func(path string, do func(dec *json.Decoder, origin string) (any, error)) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "POST only", http.StatusMethodNotAllowed)
				return
			}
			res, err := do(json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)), origin(r))
			w.Header().Set("Content-Type", "application/json")
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			_ = json.NewEncoder(w).Encode(res)
		})
	}
	serve(PathQuestion, func(dec *json.Decoder, origin string) (any, error) {
		var req QuestionRequest
		if err := dec.Decode(&req); err != nil {
			return nil, err
		}
		return c.Ask(req, origin)
	})
	serve(PathAnswer, func(dec *json.Decoder, _ string) (any, error) {
		var req AnswerRequest
		if err := dec.Decode(&req); err != nil {
			return nil, err
		}
		return c.Answer(req)
	})
	return mux
}

// origin is who asks: the address Cloudflare's door reports, or the direct
// connection's.
func origin(r *http.Request) string {
	if ip := r.Header.Get("Cf-Connecting-Ip"); ip != "" {
		return ip
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// ProveWith does the computer's side against a checker at base (the
// network's REST address): question, secret test in this computer's chip,
// answer. It returns the ticket for account.
func ProveWith(base, account string) (*Ticket, error) {
	ch, err := Read()
	if err != nil {
		return nil, err
	}
	a, ak, err := NewAnswerer()
	if err != nil {
		return nil, err
	}
	defer a.Close()
	var q QuestionResponse
	if err := post(base+PathQuestion, QuestionRequest{Modulus: ch.Modulus, Cert: ch.Cert, AK: ak, Account: account}, &q); err != nil {
		return nil, err
	}
	secret, err := a.Answer(&q.Question)
	if err != nil {
		return nil, err
	}
	var t Ticket
	if err := post(base+PathAnswer, AnswerRequest{Session: q.Session, Secret: secret}, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func post(url string, in, out any) error {
	bz, err := json.Marshal(in)
	if err != nil {
		return err
	}
	res, err := (&http.Client{Timeout: time.Minute}).Post(url, "application/json", bytes.NewReader(bz))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(res.Body).Decode(&e)
		if e.Error == "" {
			e.Error = res.Status
		}
		return errors.New("chip checker: " + e.Error)
	}
	return json.NewDecoder(res.Body).Decode(out)
}
