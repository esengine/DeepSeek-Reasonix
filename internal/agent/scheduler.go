package agent

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"

	"reasonix/internal/agentgraph"
)

// SubagentSlotStatus is the queue lifecycle shown for background task/fleet
// items that share the session scheduler.
type SubagentSlotStatus string

const (
	SubagentSlotQueued  SubagentSlotStatus = "queued"
	SubagentSlotRunning SubagentSlotStatus = "running"
	SubagentSlotDone    SubagentSlotStatus = "done"
	SubagentSlotFailed  SubagentSlotStatus = "failed"
)

// AcquireRequest describes a sub-agent slot request against the session pool.
type AcquireRequest struct {
	// Writer is true for writer-capable runs (task without read_only, profile
	// that is not read-only, fleet items that can write).
	Writer bool
	// WritePaths is the claim held while the slot is active. Empty for
	// read-only work. Whole-workspace claims count as writers and serialize
	// against every other writer.
	WritePaths WritePathSet
	// Nested fails immediately when no capacity is free instead of queueing.
	// Nested sub-agents must not block waiting for a parent-held slot.
	Nested bool
	// Priority is how much work waits on this run in its caller's graph. It
	// orders the queue, never the ceiling, and a caller holding no graph
	// leaves it zero.
	Priority int
	// OnQueued fires once with what held this request back. Nothing else can
	// answer it: by the time the caller runs, the constraint is gone. It may
	// refuse, and a wait that could not be recorded is not shown as one.
	OnQueued func(agentgraph.WaitCause) error
	// Label is optional diagnostics text.
	Label string
}

// SubagentScheduler is a session-scoped concurrency controller shared by task,
// fleet, parallel_tasks, profile skills, and nested sub-agents.
type SubagentScheduler struct {
	mu sync.Mutex

	maxTotal   int
	maxWriters int

	activeTotal   int
	activeWriters int
	activeClaims  []WritePathSet
	// parentClaims are write paths held by the parent agent during a write-tool
	// Execute. They block overlapping subagent claims without consuming a
	// subagent concurrency slot (parent is not a subagent).
	parentClaims []WritePathSet

	// waiters hold non-nested acquires, drained heaviest first (arrival order
	// within equal priority).
	waiters []*schedulerWaiter
}

type schedulerWaiter struct {
	req    AcquireRequest
	ready  chan struct{}
	failed error
}

// NewSubagentScheduler builds a scheduler with the given limits (normalized).
func NewSubagentScheduler(maxTotal, maxWriters int) *SubagentScheduler {
	maxTotal, maxWriters = NormalizeConcurrencyLimits(maxTotal, maxWriters)
	return &SubagentScheduler{maxTotal: maxTotal, maxWriters: maxWriters}
}

// Limits returns the effective total/writer caps.
func (s *SubagentScheduler) Limits() (total, writers int) {
	if s == nil {
		return DefaultMaxSubagentConcurrency, DefaultMaxParallelWriters
	}
	return s.maxTotal, s.maxWriters
}

// Acquire reserves a concurrency slot (and optional write claim). Nested
// requests fail immediately when capacity is exhausted. Non-nested requests
// queue until capacity is free or ctx is cancelled.
//
// The returned release function must be called exactly once when the sub-agent
// finishes. release is safe to call even if Acquire returns an error (no-op).
func (s *SubagentScheduler) Acquire(ctx context.Context, req AcquireRequest) (release func(), err error) {
	noop := func() {}
	if s == nil {
		return noop, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	ok, cause := s.canStartLocked(req)
	if ok {
		s.activateLocked(req)
		s.mu.Unlock()
		return s.makeRelease(req), nil
	}
	if req.Nested {
		detail := s.waitDetailLocked(cause)
		s.mu.Unlock()
		return noop, fmt.Errorf("subagent concurrency limit reached (%s); nested subagents fail fast to avoid parent/child slot deadlock", detail)
	}

	w := &schedulerWaiter{req: req, ready: make(chan struct{})}
	s.waiters = append(s.waiters, w)
	s.mu.Unlock()
	// Reported after the unlock: the cause was read under it, and the callback
	// reaches a sink this must not be holding the scheduler's mutex for.
	if req.OnQueued != nil {
		if err := req.OnQueued(cause); err != nil {
			return noop, s.abandonWaiter(w, req, err)
		}
	}

	select {
	case <-w.ready:
		if w.failed != nil {
			return noop, w.failed
		}
		return s.makeRelease(req), nil
	case <-ctx.Done():
		s.mu.Lock()
		s.removeWaiterLocked(w)
		s.mu.Unlock()
		// If we were activated between cancel and remove, release.
		select {
		case <-w.ready:
			if w.failed == nil {
				s.makeRelease(req)()
			}
		default:
		}
		return noop, ctx.Err()
	}
}

// abandonWaiter withdraws a request whose wait could not be recorded, taking
// the care the cancellation path takes: capacity may have been granted since
// the refusal, and a slot nobody releases never comes back.
func (s *SubagentScheduler) abandonWaiter(w *schedulerWaiter, req AcquireRequest, cause error) error {
	s.mu.Lock()
	s.removeWaiterLocked(w)
	s.mu.Unlock()
	select {
	case <-w.ready:
		if w.failed == nil {
			s.makeRelease(req)()
		}
	default:
	}
	return cause
}

// TryClaimWritePaths checks whether paths conflict with active claims without
// taking a concurrency slot. Used for diagnostics; prefer ReserveParentWrite
// for parent agent writes so the check is not TOCTOU with subagent Acquire.
func (s *SubagentScheduler) TryClaimWritePaths(paths WritePathSet) error {
	if s == nil || paths.Empty() {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conflictLocked(paths)
}

// ReserveParentWrite holds paths against overlapping subagent claims for the
// duration of a parent write-tool Execute. It does not consume subagent
// concurrency slots. On conflict it fails immediately (parent cannot queue
// behind background jobs mid-tool-call). release must be called once when the
// write finishes so queued subagents can proceed.
func (s *SubagentScheduler) ReserveParentWrite(paths WritePathSet) (release func(), err error) {
	noop := func() {}
	if s == nil || paths.Empty() {
		return noop, nil
	}
	s.mu.Lock()
	if err := s.conflictLocked(paths); err != nil {
		s.mu.Unlock()
		return noop, err
	}
	s.parentClaims = append(s.parentClaims, paths)
	s.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			s.parentClaims = removeClaim(s.parentClaims, paths)
			s.pumpWaitersLocked()
			s.mu.Unlock()
		})
	}, nil
}

// ActiveWriterClaims returns a snapshot of subagent + parent write claims.
func (s *SubagentScheduler) ActiveWriterClaims() []WritePathSet {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]WritePathSet, 0, len(s.activeClaims)+len(s.parentClaims))
	out = append(out, s.activeClaims...)
	out = append(out, s.parentClaims...)
	return out
}

func (s *SubagentScheduler) conflictLocked(paths WritePathSet) error {
	if paths.Empty() {
		return nil
	}
	for _, active := range s.activeClaims {
		if active.Overlaps(paths) {
			return fmt.Errorf("write path is claimed by a running background subagent; wait for it to finish before writing the same path")
		}
	}
	for _, active := range s.parentClaims {
		if active.Overlaps(paths) {
			return fmt.Errorf("write path is claimed by another parent write in progress")
		}
	}
	return nil
}

func (s *SubagentScheduler) makeRelease(req AcquireRequest) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			s.deactivateLocked(req)
			s.pumpWaitersLocked()
			s.mu.Unlock()
		})
	}
}

// canStartLocked answers whether the request may run, and when it may not, which
// constraint said so. The cause is a value rather than a sentence because two of
// them are answered by a setting and the third only by a path or an edge, and a
// reader cannot tell those apart from "queued".
func (s *SubagentScheduler) canStartLocked(req AcquireRequest) (bool, agentgraph.WaitCause) {
	if s.activeTotal >= s.maxTotal {
		return false, agentgraph.WaitSlots
	}
	if !req.Writer {
		return true, ""
	}
	if s.activeWriters >= s.maxWriters {
		return false, agentgraph.WaitWriters
	}
	for _, active := range s.activeClaims {
		if active.Overlaps(req.WritePaths) {
			return false, agentgraph.WaitClaim
		}
	}
	for _, active := range s.parentClaims {
		if active.Overlaps(req.WritePaths) {
			return false, agentgraph.WaitClaim
		}
	}
	return true, ""
}

// waitDetailLocked renders a cause with the counts that produced it, for the one
// caller that reports rather than records: a nested acquire that failed fast.
func (s *SubagentScheduler) waitDetailLocked(cause agentgraph.WaitCause) string {
	switch cause {
	case agentgraph.WaitSlots:
		return fmt.Sprintf("total concurrency %d/%d", s.activeTotal, s.maxTotal)
	case agentgraph.WaitWriters:
		return fmt.Sprintf("writer concurrency %d/%d", s.activeWriters, s.maxWriters)
	default:
		return "write path conflict with a claim already held"
	}
}

func (s *SubagentScheduler) activateLocked(req AcquireRequest) {
	s.activeTotal++
	if req.Writer {
		s.activeWriters++
		if !req.WritePaths.Empty() {
			s.activeClaims = append(s.activeClaims, req.WritePaths)
		}
	}
}

func (s *SubagentScheduler) deactivateLocked(req AcquireRequest) {
	if s.activeTotal > 0 {
		s.activeTotal--
	}
	if req.Writer {
		if s.activeWriters > 0 {
			s.activeWriters--
		}
		if !req.WritePaths.Empty() {
			s.activeClaims = removeClaim(s.activeClaims, req.WritePaths)
		}
	}
}

// pumpWaitersLocked hands freed capacity to the waiters that can use it,
// heaviest first. Arrival order alone puts the head of the longest chain
// behind leaves that unblock nothing, and that wait lands on the run's
// makespan one for one. Sorting in place is stable across pumps because a
// waiter's priority never changes, so equal weights keep arrival order.
func (s *SubagentScheduler) pumpWaitersLocked() {
	if len(s.waiters) == 0 {
		return
	}
	slices.SortStableFunc(s.waiters, func(a, b *schedulerWaiter) int {
		return cmp.Compare(b.req.Priority, a.req.Priority)
	})
	remaining := s.waiters[:0]
	for _, w := range s.waiters {
		if ok, _ := s.canStartLocked(w.req); ok {
			s.activateLocked(w.req)
			close(w.ready)
			continue
		}
		remaining = append(remaining, w)
	}
	s.waiters = remaining
}

func (s *SubagentScheduler) removeWaiterLocked(target *schedulerWaiter) {
	if len(s.waiters) == 0 {
		return
	}
	out := s.waiters[:0]
	for _, w := range s.waiters {
		if w == target {
			continue
		}
		out = append(out, w)
	}
	s.waiters = out
}

func removeClaim(claims []WritePathSet, target WritePathSet) []WritePathSet {
	for i, c := range claims {
		if writeClaimEqual(c, target) {
			return append(claims[:i], claims[i+1:]...)
		}
	}
	return claims
}

func writeClaimEqual(a, b WritePathSet) bool {
	if a.WholeWorkspace != b.WholeWorkspace || a.WorkspaceRoot != b.WorkspaceRoot {
		return false
	}
	if len(a.Paths) != len(b.Paths) {
		return false
	}
	for i := range a.Paths {
		if a.Paths[i] != b.Paths[i] {
			return false
		}
	}
	return true
}
