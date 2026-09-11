// Package browserops records every agent write to a hosted browser before the
// write happens and settles it afterwards. A process death between the two
// leaves the operation "unknown", which is never retried automatically: the
// page may or may not have accepted the submission, and only a fresh snapshot
// can tell. The ledger is a versioned JSON file that the previous desktop
// shell never reads, so it can grow without touching core session formats.
package browserops

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

const ledgerVersion = 1

// State is the outcome recorded for one operation.
type State string

const (
	StateReserved    State = "reserved"
	StateExecuted    State = "executed"
	StateNotExecuted State = "not_executed"
	StateUnknown     State = "unknown"
)

var operationIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

// Operation is one reserved browser write and its settlement.
type Operation struct {
	ID            string    `json:"id"`
	SessionID     string    `json:"sessionId"`
	Generation    string    `json:"generation"`
	TabID         string    `json:"tabId"`
	Epoch         uint64    `json:"epoch"`
	DocumentToken string    `json:"documentToken"`
	Action        string    `json:"action"`
	Digest        string    `json:"digest"`
	State         State     `json:"state"`
	ReservedAt    time.Time `json:"reservedAt"`
	SettledAt     time.Time `json:"settledAt,omitempty"`
	Reason        string    `json:"reason,omitempty"`
}

type ledgerFile struct {
	Version    int                   `json:"version"`
	Operations map[string]*Operation `json:"operations"`
}

// Ledger is the durable operation log for one desktop data home.
type Ledger struct {
	path string
	mu   sync.Mutex
	file ledgerFile
	now  func() time.Time
}

var (
	ErrDuplicateOperation = errors.New("browser operation id already recorded")
	ErrInvalidOperationID = errors.New("browser operation id must match [A-Za-z0-9_-]{1,100}")
	ErrUnknownOperation   = errors.New("browser operation not reserved")
	ErrAlreadySettled     = errors.New("browser operation already settled")
)

// Open loads or creates the ledger. Operations left reserved by a previous
// process are marked unknown before anything else can run.
func Open(path string) (*Ledger, error) {
	l := &Ledger{path: path, now: func() time.Time { return time.Now().UTC() }}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		l.file = ledgerFile{Version: ledgerVersion, Operations: map[string]*Operation{}}
		return l, nil
	case err != nil:
		return nil, err
	}
	if err := json.Unmarshal(data, &l.file); err != nil {
		return nil, fmt.Errorf("browser ledger %s: %w", path, err)
	}
	if l.file.Version != ledgerVersion {
		return nil, fmt.Errorf("browser ledger %s: unsupported version %d", path, l.file.Version)
	}
	if l.file.Operations == nil {
		l.file.Operations = map[string]*Operation{}
	}
	recovered := false
	for _, op := range l.file.Operations {
		if op.State == StateReserved {
			op.State, op.SettledAt, op.Reason = StateUnknown, l.now(), "process exited before settlement"
			recovered = true
		}
	}
	if recovered {
		if err := l.persistLocked(); err != nil {
			return nil, err
		}
	}
	return l, nil
}

// Reserve durably records the operation before any side effect. A repeated
// ID fails even when the earlier attempt is unknown: the caller must read the
// page again and mint a new ID instead of replaying.
func (l *Ledger) Reserve(op Operation) error {
	if !operationIDRe.MatchString(op.ID) {
		return ErrInvalidOperationID
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, exists := l.file.Operations[op.ID]; exists {
		return ErrDuplicateOperation
	}
	op.State = StateReserved
	op.ReservedAt = l.now()
	op.SettledAt = time.Time{}
	stored := op
	l.file.Operations[op.ID] = &stored
	if err := l.persistLocked(); err != nil {
		delete(l.file.Operations, op.ID)
		return err
	}
	return nil
}

// Settle records the outcome of a reserved operation exactly once.
func (l *Ledger) Settle(id string, state State, reason string) error {
	if state == StateReserved {
		return fmt.Errorf("browser operation %s: cannot settle to reserved", id)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	op, ok := l.file.Operations[id]
	if !ok {
		return ErrUnknownOperation
	}
	if op.State != StateReserved {
		return ErrAlreadySettled
	}
	previous := *op
	op.State, op.SettledAt, op.Reason = state, l.now(), reason
	if err := l.persistLocked(); err != nil {
		*op = previous
		return err
	}
	return nil
}

// Lookup returns a copy of the recorded operation.
func (l *Ledger) Lookup(id string) (Operation, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	op, ok := l.file.Operations[id]
	if !ok {
		return Operation{}, false
	}
	return *op, true
}

// Unsettled lists operations whose outcome is unknown, oldest first, so the
// UI can show the user what may have reached a website.
func (l *Ledger) Unsettled() []Operation {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []Operation
	for _, op := range l.file.Operations {
		if op.State == StateUnknown {
			out = append(out, *op)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ReservedAt.Before(out[j].ReservedAt) })
	return out
}

func (l *Ledger) persistLocked() error {
	data, err := json.MarshalIndent(&l.file, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return err
	}
	tmp := l.path + "." + hex.EncodeToString(suffix) + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, l.path)
}
