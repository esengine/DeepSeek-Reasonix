package teamroster

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"regexp"
	"time"

	"reasonix/internal/base/fileutil"
)

// ExchangeFile is the append-only record of messages between agents.
const ExchangeFile = "exchanges.jsonl"

// Once the record passes compactAt lines it is folded down to every open
// exchange and the keepClosed most recent settled ones; an older request id
// is then treated as new.
var (
	keepClosed = 600
	compactAt  = 2000
)

var (
	exchangeID = regexp.MustCompile(`^x-[0-9a-f]{16}$`)
	requestID  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	ceiling    = regexp.MustCompile(`^[A-Za-z_]{0,16}$`)
)

// ReplyTo is where the receiver's answer goes.
type ReplyTo string

const (
	ReplyToSender ReplyTo = "me"
	ReplyToUser   ReplyTo = "user"
)

// ExchangeState is where one message between agents stands.
type ExchangeState string

const (
	ExchangeQueued      ExchangeState = "queued"
	ExchangeDelivered   ExchangeState = "delivered"
	ExchangeWaiting     ExchangeState = "waiting_children"
	ExchangeReplied     ExchangeState = "replied"
	ExchangeFailed      ExchangeState = "failed"
	ExchangeStopped     ExchangeState = "stopped"
	ExchangeInterrupted ExchangeState = "interrupted"
)

// Settled reports whether the exchange has ended and takes no further change.
func (s ExchangeState) Settled() bool {
	switch s {
	case ExchangeReplied, ExchangeFailed, ExchangeStopped, ExchangeInterrupted:
		return true
	}
	return false
}

func (s ExchangeState) valid() bool {
	switch s {
	case ExchangeQueued, ExchangeDelivered, ExchangeWaiting:
		return true
	}
	return s.Settled()
}

// Exchange is one message an agent, or the user, sent another agent. Parent
// is the exchange the sender was serving when it sent this one, which is what
// the handoff tree is built from. Returned is set once the sender has read the
// answer, which can be after the exchange settled. The message itself lives in
// the receiver's session; the book keeps its digest to tell retries apart.
type Exchange struct {
	ID        string        `json:"id"`
	From      string        `json:"from"`
	To        string        `json:"to"`
	RequestID string        `json:"requestId,omitempty"`
	Digest    string        `json:"digest"`
	ReplyTo   ReplyTo       `json:"replyTo"`
	Parent    string        `json:"parent,omitempty"`
	Group     string        `json:"group,omitempty"`
	Ceiling   string        `json:"ceiling,omitempty"`
	State     ExchangeState `json:"state"`
	Returned  bool          `json:"returned,omitempty"`
	OpenedAt  time.Time     `json:"openedAt"`
	At        time.Time     `json:"at"`
}

func (x *Exchange) check() error {
	switch {
	case x.From != ByUser && !agentID.MatchString(x.From):
		return fieldErr("exchange.from", ErrInvalid)
	case !agentID.MatchString(x.To):
		return fieldErr("exchange.to", ErrInvalid)
	case x.RequestID != "" && !requestID.MatchString(x.RequestID):
		return fieldErr("request_id", ErrInvalid)
	case x.ReplyTo != ReplyToSender && x.ReplyTo != ReplyToUser:
		return fieldErr("reply_to", ErrInvalid)
	case x.Parent != "" && !exchangeID.MatchString(x.Parent):
		return fieldErr("exchange.parent", ErrInvalid)
	case x.Group != "" && !groupID.MatchString(x.Group):
		return fieldErr("group.id", ErrInvalid)
	case !ceiling.MatchString(x.Ceiling):
		return fieldErr("ceiling", ErrInvalid)
	}
	return nil
}

// Exchanges folds the record into the latest state of each exchange, in the
// order they were opened.
func (s Store) Exchanges() ([]Exchange, error) {
	if s.Dir == "" {
		return nil, errNoDir
	}
	out, _, err := foldExchanges(filepath.Join(s.Dir, ExchangeFile))
	return out, err
}

func foldExchanges(path string) ([]Exchange, int, error) {
	var order []string
	latest := map[string]Exchange{}
	lines := 0
	err := readJSONL(path, func(line []byte) error {
		var x Exchange
		if err := json.Unmarshal(line, &x); err != nil {
			return err
		}
		if !exchangeID.MatchString(x.ID) || !x.State.valid() {
			return ErrInvalid
		}
		if _, seen := latest[x.ID]; !seen {
			order = append(order, x.ID)
		}
		latest[x.ID] = x
		lines++
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	out := make([]Exchange, 0, len(order))
	for _, id := range order {
		out = append(out, latest[id])
	}
	return out, lines, nil
}

// OpenExchange records x as a new queued exchange carrying message and returns
// it under a fresh id. A request id the sender already used for the same
// message returns that exchange with existed true; for a different message it
// answers ErrRequestReused.
func (s Store) OpenExchange(ctx context.Context, x Exchange, message string) (_ Exchange, existed bool, _ error) {
	if err := x.check(); err != nil {
		return Exchange{}, false, err
	}
	sum := sha256.Sum256([]byte(message))
	digest := hex.EncodeToString(sum[:])
	var out Exchange
	err := s.withLock(ctx, s.Dir, ExchangeFile, func() error {
		path := filepath.Join(s.Dir, ExchangeFile)
		book, lines, err := foldExchanges(path)
		if err != nil {
			return err
		}
		if x.RequestID != "" {
			for _, old := range book {
				if old.From != x.From || old.RequestID != x.RequestID {
					continue
				}
				if old.Digest != digest {
					return ErrRequestReused
				}
				out, existed = old, true
				return nil
			}
		}
		x.ID, x.Digest, x.State = "x-"+randomHex(), digest, ExchangeQueued
		if x.OpenedAt.IsZero() {
			x.OpenedAt = time.Now().UTC()
		}
		x.At = x.OpenedAt
		if err := appendJSONL(path, x); err != nil {
			return err
		}
		out = x
		return compactExchanges(path, append(book, x), lines+1)
	})
	return out, existed, err
}

// SetExchangeState records that exchange id moved to state at at (now when
// zero). A settled exchange refuses any further change.
func (s Store) SetExchangeState(ctx context.Context, id string, state ExchangeState, at time.Time) (Exchange, error) {
	if !state.valid() {
		return Exchange{}, fieldErr("exchange.state", ErrInvalid)
	}
	var out Exchange
	err := s.withLock(ctx, s.Dir, ExchangeFile, func() error {
		path := filepath.Join(s.Dir, ExchangeFile)
		book, lines, err := foldExchanges(path)
		if err != nil {
			return err
		}
		for i := range book {
			if book[i].ID != id {
				continue
			}
			if book[i].State.Settled() {
				return ErrExchangeSettled
			}
			if at.IsZero() {
				at = time.Now().UTC()
			}
			book[i].State, book[i].At = state, at
			if err := appendJSONL(path, book[i]); err != nil {
				return err
			}
			out = book[i]
			return compactExchanges(path, book, lines+1)
		}
		return ErrUnknownExchange
	})
	return out, err
}

// MarkReturned records that the sender of exchange id has read its answer.
// It applies to a settled exchange too, and marking twice changes nothing.
func (s Store) MarkReturned(ctx context.Context, id string) error {
	return s.withLock(ctx, s.Dir, ExchangeFile, func() error {
		path := filepath.Join(s.Dir, ExchangeFile)
		book, lines, err := foldExchanges(path)
		if err != nil {
			return err
		}
		for i := range book {
			if book[i].ID != id {
				continue
			}
			if book[i].Returned {
				return nil
			}
			book[i].Returned = true
			if err := appendJSONL(path, book[i]); err != nil {
				return err
			}
			return compactExchanges(path, book, lines+1)
		}
		return ErrUnknownExchange
	})
}

func compactExchanges(path string, book []Exchange, lines int) error {
	if lines <= compactAt {
		return nil
	}
	settled := 0
	for _, x := range book {
		if x.State.Settled() {
			settled++
		}
	}
	var data []byte
	for _, x := range book {
		if x.State.Settled() {
			settled--
			if settled >= keepClosed {
				continue
			}
		}
		line, err := json.Marshal(x)
		if err != nil {
			return err
		}
		data = append(append(data, line...), '\n')
	}
	return fileutil.AtomicWriteFile(path, data, 0o600)
}
