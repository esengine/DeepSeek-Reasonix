package observe

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrParked is the identity of a request that was set aside for a person. A
// caller tells it from a refusal by errors.Is, never by reading the message.
var ErrParked = errors.New("observe: parked for a person")

// Kind says what was waiting for a person.
type Kind string

const (
	KindApproval Kind = "approval"
	KindAsk      Kind = "ask"
	KindTicket   Kind = "ticket"
	KindEscape   Kind = "escape"
)

// Risk grades what granting the request would let the run do.
type Risk string

const (
	RiskLow    Risk = "low"
	RiskMedium Risk = "medium"
	RiskHigh   Risk = "high"
)

// MaxPending is the most records one run may park. The next distinct request
// is refused with ErrParkLimit and the run is ended.
const MaxPending = 32

// ErrParkLimit is the identity of a run that asked for a person more often than
// MaxPending allows.
var ErrParkLimit = errors.New("observe: the run parked more requests than it may")

// TTL is how long a Pending stays open before it expires unresolved.
const TTL = 7 * 24 * time.Hour

// Pending is one thing a person has to decide. It is data only: nothing
// executes it, and resolving it starts a new turn rather than replaying a call.
type Pending struct {
	ID     string `json:"id"`
	Kind   Kind   `json:"kind"`
	Source string `json:"source"`
	// Summary is written by the host. Detail is the model's own wording of an
	// ask and is Untrusted: a frontend labels it and never renders it as its own.
	Summary   string    `json:"summary"`
	Detail    string    `json:"detail,omitempty"`
	Untrusted bool      `json:"untrusted"`
	Risk      Risk      `json:"risk"`
	Digest    string    `json:"digest"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// PendingSink is where a run parks what needs a person. It reports the record
// it stored, whose ID is the one the model is told. An error means nothing was
// stored; the request is refused all the same.
type PendingSink interface {
	Park(Pending) (Pending, error)
}

// DigestOf identifies a request by what it asks, so the same request parked
// twice is one Pending.
func DigestOf(kind Kind, source, subject string) string {
	sum := sha256.Sum256([]byte(string(kind) + "\x00" + source + "\x00" + subject))
	return hex.EncodeToString(sum[:])
}

// Ledger is an in-memory PendingSink that numbers records within one run.
type Ledger struct {
	mu   sync.Mutex
	now  func() time.Time
	list []Pending
}

// NewLedger returns an empty ledger; a nil clock reads the wall clock.
func NewLedger(now func() time.Time) *Ledger {
	if now == nil {
		now = time.Now
	}
	return &Ledger{now: now}
}

// Park stores p, stamping its ID and times, or returns the record already
// holding the same kind and digest.
func (l *Ledger) Park(p Pending) (Pending, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, have := range l.list {
		if have.Kind == p.Kind && have.Digest == p.Digest {
			return have, nil
		}
	}
	if len(l.list) >= MaxPending {
		return Pending{}, ErrParkLimit
	}
	p.Summary, p.Detail = Sanitize(p.Summary), Sanitize(p.Detail)
	p.ID = "p" + strconv.Itoa(len(l.list)+1)
	p.CreatedAt = l.now().UTC()
	p.ExpiresAt = p.CreatedAt.Add(TTL)
	l.list = append(l.list, p)
	return p, nil
}

// List returns the parked records in the order they were parked.
func (l *Ledger) List() []Pending {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Pending(nil), l.list...)
}

// Sanitize removes what could misrepresent text on a card: control characters
// other than newline and tab, characters that reorder text, zero-width and
// Unicode tag characters that hide it, and the line and paragraph separators.
// Every sink stores what it returns.
func Sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			return -1
		case r == 0x061c || r == 0x200e || r == 0x200f || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069):
			return -1
		case r >= 0x200b && r <= 0x200d, r == 0x2060, r == 0xfeff, r == 0x2028, r == 0x2029, r >= 0xe0000 && r <= 0xe007f:
			return -1
		}
		return r
	}, s)
}
