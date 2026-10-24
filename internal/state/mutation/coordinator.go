package mutation

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/base/filelock"
	"reasonix/internal/base/fileutil"
	"reasonix/internal/state/workspacelease"
)

const DefaultWaitLimit = 30 * time.Second
const retryInterval = 20 * time.Millisecond

type Coordinator struct {
	root               string
	dir                string
	compatDir          string
	notify             func(Notice)
	admit              func(context.Context, Intent) error
	workspaceResources []string
	compatClaim        func(string, string, string, []string) (func(), *workspacelease.ConflictError, error)
}

type Option func(*Coordinator)

func WithContextAdmissionCheck(check func(context.Context, Intent) error) Option {
	return func(c *Coordinator) { c.admit = check }
}

// WithWorkspaceResources includes host-known shared metadata in opaque writes.
// Named file transactions keep their narrower resource sets.
func WithWorkspaceResources(paths ...string) Option {
	resources := slices.Clone(paths)
	return func(c *Coordinator) { c.workspaceResources = slices.Clone(resources) }
}

func New(root, stateDir string, notify func(Notice), opts ...Option) (*Coordinator, error) {
	canonical, err := fileutil.CanonicalWritePath(root)
	if err != nil {
		return nil, err
	}
	if stateDir == "" {
		return nil, ErrInvalidIntent
	}
	dir := filepath.Join(stateDir, "mutations-v1")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	c := &Coordinator{root: canonical, dir: dir, compatDir: stateDir, notify: notify, compatClaim: workspacelease.TryClaimPaths}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

type Lease struct {
	ID            string
	once          sync.Once
	coordinator   *Coordinator
	record        record
	releaseLive   func()
	releaseCompat func()
	acquiredAt    time.Time
	closed        atomic.Bool
	completion    leaseCompletion
}

// TryAcquire preserves queue order but never waits for a conflicting resource.
// Registry operations remain cancellable short critical sections.
func (c *Coordinator) TryAcquire(ctx context.Context, intent Intent) (*Lease, error) {
	intent.tryOnly = true
	return c.Acquire(ctx, intent)
}

func (c *Coordinator) Acquire(ctx context.Context, intent Intent) (acquired *Lease, resultErr error) {
	var owned *Lease
	waiting := false
	defer func() {
		if p := recover(); p != nil {
			acquired, resultErr = nil, fmt.Errorf("%w: admission panicked: %v", ErrCallback, p)
		}
		resultErr = c.abandonAcquisition(owned, acquired, waiting, resultErr)
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	capacities, err := normalizedCapacities(intent.Capacities)
	if err != nil {
		return nil, err
	}
	intent.Capacities = capacities
	paths, err := c.resources(intent)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 && len(capacities) == 0 {
		return &Lease{}, nil
	}
	if parent, _ := ctx.Value(leaseContextKey{}).(*Lease); parent != nil && parent.coordinator == c {
		if parent.closed.Load() || !covers(parent.record.Paths, paths) || !coversCapacities(parent.record.Capacities, capacities) {
			return nil, ErrInvalidIntent
		}
		if err := c.validate(ctx, intent, paths); err != nil {
			return nil, err
		}
		return &Lease{}, nil
	}
	ctx, cancel := acquisitionContext(ctx, intent.Effect)
	defer cancel()
	l := &Lease{ID: rand.Text(), coordinator: c, record: record{
		Version: 1, SessionID: intent.SessionID, ExecutionID: intent.ExecutionID,
		CallID: intent.CallID, Paths: paths, StartedAt: time.Now(),
		Effect: intent.Effect, Capacities: capacities,
		Workspace: c.root,
	}}
	if len(capacities) != 0 {
		l.record.Version = 2
	}
	l.record.ID = l.ID
	defer func() {
		if errors.Is(resultErr, context.DeadlineExceeded) {
			resultErr = &WaitError{TransactionID: l.ID, Paths: paths, Cause: resultErr, Effect: intent.Effect, Capacities: capacities}
		}
	}()
	l.releaseLive, err = filelock.TryAcquire(c.livePath(l.ID))
	if err != nil {
		return nil, err
	}
	owned = l
	if err := c.enqueue(ctx, &l.record); err != nil {
		return nil, err
	}
	probe := versionProbe{}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := probe.check(ctx, intent.Expected, paths); err != nil {
			return nil, err
		}
		held, err := c.tryGrant(ctx, l)
		if err != nil {
			return nil, err
		}
		if held == nil {
			return c.finishAcquisition(ctx, l, intent, paths, waiting)
		}
		if intent.tryOnly {
			return nil, ErrBusy
		}
		if !waiting {
			waiting = true
			l.record.waitKind = held.kind
			if err := c.notice(l.record, "waiting", &held.record, NotExecuted); err != nil {
				return nil, err
			}
		}
		timer := time.NewTimer(retryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (c *Coordinator) finishAcquisition(ctx context.Context, lease *Lease, intent Intent, paths []string, waiting bool) (*Lease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := c.validate(ctx, intent, paths); err != nil {
		return nil, err
	}
	lease.acquiredAt = time.Now()
	if waiting {
		if err := c.notice(lease.record, "acquired", nil, NotExecuted); err != nil {
			return nil, err
		}
	}
	return lease, nil
}

func (c *Coordinator) abandonAcquisition(owned, acquired *Lease, waiting bool, err error) error {
	if owned == nil || acquired != nil {
		return err
	}
	owned.End(NotExecuted)
	if waiting {
		return errors.Join(err, c.waitEnded(owned.record, err))
	}
	return err
}

func (l *Lease) End(outcome Outcome) {
	if l == nil {
		return
	}
	l.once.Do(func() {
		l.closed.Store(true)
		l.endAfterCompletion(outcome)
	})
}

type leaseContextKey struct{}

func HoldingLease(ctx context.Context) bool {
	lease, _ := ctx.Value(leaseContextKey{}).(*Lease)
	return lease != nil && lease.coordinator != nil && len(lease.record.Paths) != 0 && !lease.closed.Load()
}

func CheckHeldPaths(ctx context.Context, paths []string) error {
	lease, _ := ctx.Value(leaseContextKey{}).(*Lease)
	return lease.CheckPaths(paths)
}

func (l *Lease) CheckPaths(paths []string) error {
	if l == nil || l.coordinator == nil || l.closed.Load() {
		return ErrInvalidIntent
	}
	requested, err := l.coordinator.resources(Intent{Effect: NamedPaths, Paths: paths})
	if err != nil {
		return err
	}
	if !covers(l.record.Paths, requested) {
		return ErrInvalidIntent
	}
	return nil
}

func (l *Lease) Context(ctx context.Context) context.Context {
	if l == nil || l.coordinator == nil {
		return ctx
	}
	return context.WithValue(ctx, leaseContextKey{}, l)
}

func WithoutLease(ctx context.Context) context.Context {
	return context.WithValue(ctx, leaseContextKey{}, (*Lease)(nil))
}

func (c *Coordinator) notice(r record, state string, holder *record, outcome Outcome) error {
	return c.emitNotice(r, state, "", holder, outcome)
}

func (c *Coordinator) emitNotice(r record, state, reason string, holder *record, outcome Outcome) (resultErr error) {
	defer func() {
		if p := recover(); p != nil {
			resultErr = fmt.Errorf("%w: notice %s panicked: %v", ErrCallback, state, p)
		}
	}()
	if c.notify == nil {
		return nil
	}
	n := Notice{TransactionID: r.ID, SessionID: r.SessionID, ExecutionID: r.ExecutionID,
		Effect: r.Effect, BlockedOn: r.waitKind, Capacities: slices.Clone(r.Capacities),
		CallID: r.CallID, Paths: slices.Clone(r.Paths), State: state, StartedAt: r.StartedAt,
		Elapsed: time.Since(r.StartedAt), Outcome: outcome, Reason: reason}
	if holder != nil {
		n.HolderSessionID, n.HolderExecutionID = holder.SessionID, holder.ExecutionID
		n.HolderTransactionID, n.HolderPaths = holder.ID, slices.Clone(holder.Paths)
	}
	c.notify(n)
	return nil
}

func (c *Coordinator) waitEnded(r record, err error) error {
	reason := "rejected"
	switch {
	case errors.Is(err, context.Canceled):
		reason = "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		reason = "timeout"
	case errors.Is(err, ErrVersionConflict):
		reason = "conflict"
	}
	return c.emitNotice(r, "abandoned", reason, nil, NotExecuted)
}

func (c *Coordinator) recordPath(id string) string { return filepath.Join(c.dir, id+".json") }
func (c *Coordinator) livePath(id string) string   { return filepath.Join(c.dir, id+".live") }
func (c *Coordinator) guardPath() string           { return filepath.Join(c.dir, "registry.lock") }
func (c *Coordinator) preparationPath(id string) string {
	return filepath.Join(c.dir, id+".preparing")
}
