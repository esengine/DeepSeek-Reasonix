package extension

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"time"
)

// EffectClass classifies how an effect may be undone during scope disposal.
type EffectClass uint8

const (
	// Reversible effects can be fully undone by Dispose.
	Reversible EffectClass = iota
	// Cancelable effects stop further work; Dispose waits for completion.
	Cancelable
	// Compensatable effects may run Compensate when Dispose cannot reverse them.
	Compensatable
	// Irreversible effects record a receipt only; never claim rollback success.
	Irreversible
)

// Effect is one live resource owned by an EffectScope generation.
type Effect struct {
	ID         string
	Owner      string
	Component  string
	Class      EffectClass
	Dispose    func(context.Context) error
	Compensate func(context.Context) error
}

// EffectReceipt records irreversible or compensatable work for recovery.
type EffectReceipt struct {
	ID                 string      `json:"id"`
	Owner              string      `json:"owner"`
	Generation         uint64      `json:"generation"`
	Component          string      `json:"component,omitempty"`
	Class              EffectClass `json:"class"`
	StartedAt          time.Time   `json:"startedAt"`
	CompletedAt        time.Time   `json:"completedAt,omitempty"`
	CompensationStatus string      `json:"compensationStatus,omitempty"`
	Error              string      `json:"error,omitempty"`
}

// EffectScope tracks live resources for one runtime generation.
type EffectScope interface {
	Track(Effect) error
	TrackCloser(id string, c io.Closer) error
	Dispose(context.Context) error
	Generation() uint64
	Receipts() []EffectReceipt
	Closed() bool
}

// LiveScope is the default EffectScope implementation: reverse-order,
// once-only dispose with generation identity and receipt aggregation.
type LiveScope struct {
	mu         sync.Mutex
	generation uint64
	effects    []trackedEffect
	receipts   []EffectReceipt
	closed     bool
	seen       map[string]struct{}
}

type trackedEffect struct {
	effect   Effect
	disposed bool
	started  time.Time
}

// NewEffectScope returns an empty scope bound to generation.
func NewEffectScope(generation uint64) *LiveScope {
	return &LiveScope{
		generation: generation,
		seen:       make(map[string]struct{}),
	}
}

// Generation returns the bound runtime generation.
func (s *LiveScope) Generation() uint64 { return s.generation }

// Closed reports whether Dispose has completed.
func (s *LiveScope) Closed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// Receipts returns a copy of recorded effect receipts.
func (s *LiveScope) Receipts() []EffectReceipt {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]EffectReceipt, len(s.receipts))
	copy(out, s.receipts)
	return out
}

// Track registers an effect. If the scope is already closed the effect is
// disposed immediately so activation races cannot leak resources.
func (s *LiveScope) Track(e Effect) error {
	if e.ID == "" {
		return fmt.Errorf("extension: effect id is required")
	}
	if e.Dispose == nil && e.Class != Irreversible {
		return fmt.Errorf("extension: effect %q requires Dispose", e.ID)
	}
	s.mu.Lock()
	if _, dup := s.seen[e.ID]; dup {
		s.mu.Unlock()
		return fmt.Errorf("extension: duplicate effect id %q", e.ID)
	}
	if s.closed {
		s.mu.Unlock()
		return disposeNow(context.Background(), e)
	}
	s.seen[e.ID] = struct{}{}
	s.effects = append(s.effects, trackedEffect{effect: e, started: time.Now().UTC()})
	if e.Class == Irreversible || e.Class == Compensatable {
		s.receipts = append(s.receipts, EffectReceipt{
			ID:         e.ID,
			Owner:      e.Owner,
			Generation: s.generation,
			Component:  e.Component,
			Class:      e.Class,
			StartedAt:  time.Now().UTC(),
		})
	}
	s.mu.Unlock()
	return nil
}

// TrackCloser registers an io.Closer as a reversible effect.
func (s *LiveScope) TrackCloser(id string, c io.Closer) error {
	if c == nil {
		return nil
	}
	if id == "" {
		id = fmt.Sprintf("closer-%p", c)
	}
	return s.Track(Effect{
		ID:    id,
		Class: Reversible,
		Dispose: func(context.Context) error {
			return c.Close()
		},
	})
}

// Dispose releases every tracked effect in reverse registration order. It is
// idempotent. Individual dispose errors are joined and do not skip remaining
// effects. Cancelable dispose functions receive the caller's context so they
// can wait for background work.
func (s *LiveScope) Dispose(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	effects := s.effects
	s.effects = nil
	s.mu.Unlock()

	var errs []error
	for _, te := range slices.Backward(effects) {
		if te.disposed {
			continue
		}
		if err := disposeTracked(ctx, s, te); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func disposeTracked(ctx context.Context, s *LiveScope, te trackedEffect) error {
	e := te.effect
	var disposeErr error
	if e.Dispose != nil {
		disposeErr = e.Dispose(ctx)
	}
	compStatus := ""
	if e.Class == Compensatable && e.Compensate != nil {
		if cerr := e.Compensate(ctx); cerr != nil {
			compStatus = "failed"
			disposeErr = errors.Join(disposeErr, fmt.Errorf("compensate %s: %w", e.ID, cerr))
		} else {
			compStatus = "applied"
		}
	}
	if e.Class == Irreversible {
		// Cancellation/dispose never means the external action was undone.
		compStatus = "not_applicable"
	}
	s.mu.Lock()
	for i := range s.receipts {
		if s.receipts[i].ID == e.ID && s.receipts[i].CompletedAt.IsZero() {
			s.receipts[i].CompletedAt = time.Now().UTC()
			s.receipts[i].CompensationStatus = compStatus
			if disposeErr != nil {
				s.receipts[i].Error = disposeErr.Error()
			}
			break
		}
	}
	s.mu.Unlock()
	if disposeErr != nil {
		return fmt.Errorf("dispose %s: %w", e.ID, disposeErr)
	}
	return nil
}

func disposeNow(ctx context.Context, e Effect) error {
	var errs []error
	if e.Dispose != nil {
		if err := e.Dispose(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if e.Class == Compensatable && e.Compensate != nil {
		if err := e.Compensate(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

var _ EffectScope = (*LiveScope)(nil)
