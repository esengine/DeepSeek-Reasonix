package mutation

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"reasonix/internal/base/filelock"
)

type leaseCompletion struct {
	mu    sync.Mutex
	waits []<-chan struct{}
}

func HoldUntil(ctx context.Context, done <-chan struct{}) error {
	l, _ := ctx.Value(leaseContextKey{}).(*Lease)
	return l.HoldUntil(done)
}

// HoldUntil retains physical ownership after End until execution is settled.
func (l *Lease) HoldUntil(done <-chan struct{}) error {
	if l == nil || l.coordinator == nil || done == nil {
		return nil
	}
	l.completion.mu.Lock()
	defer l.completion.mu.Unlock()
	if l.closed.Load() {
		return ErrInvalidIntent
	}
	l.completion.waits = append(l.completion.waits, done)
	return nil
}

func (l *Lease) endAfterCompletion(outcome Outcome) {
	l.completion.mu.Lock()
	waits := l.completion.waits
	l.completion.waits = nil
	l.completion.mu.Unlock()
	for i, done := range waits {
		select {
		case <-done:
		default:
			go func() {
				for _, done := range waits[i:] {
					<-done
				}
				l.releaseResources(outcome)
			}()
			return
		}
	}
	l.releaseResources(outcome)
}

func (l *Lease) releaseResources(outcome Outcome) {
	if l.coordinator == nil {
		return
	}
	c := l.coordinator
	if l.releaseCompat != nil {
		l.releaseCompat()
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if unlock, err := filelock.Acquire(ctx, c.guardPath()); err == nil {
		_ = os.Remove(c.recordPath(l.ID))
		if l.releaseLive != nil {
			l.releaseLive()
			l.releaseLive = nil
		}
		_ = os.Remove(c.livePath(l.ID))
		_ = os.Remove(c.preparationPath(l.ID))
		unlock()
	}
	if l.releaseLive != nil {
		l.releaseLive()
	}
	if !l.acquiredAt.IsZero() {
		if err := c.notice(l.record, "released", nil, outcome); err != nil {
			slog.Warn("mutation release notification failed after resource release", "transaction", l.ID, "err", err)
		}
	}
}
