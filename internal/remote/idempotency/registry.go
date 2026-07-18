// Package idempotency provides the in-memory Remote V1 mutation admission
// registry. It deliberately does not execute mutations or persist state. A
// Host/Session sequencer calls Begin inside its atomic admission region, then
// resolves the returned Claim only after semantic state has been committed.
package idempotency

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"reasonix/internal/remote/protocol"
)

var (
	// ErrClaimClosed means another terminal action or a Host epoch reset won
	// the race for a first-admission claim.
	ErrClaimClosed = errors.New("idempotency: claim is already closed")
	// ErrAdmissionAbandoned wakes duplicates when the first request failed
	// before business admission. The record is removed, so a later request is
	// evaluated afresh and this error is never cached.
	ErrAdmissionAbandoned = errors.New("idempotency: admission was abandoned")
	// ErrHostEpochChanged wakes waiters when a registry is explicitly rotated
	// to a new daemon epoch.
	ErrHostEpochChanged = errors.New("idempotency: Host epoch changed")
)

type TargetKind string

const (
	TargetHost      TargetKind = "host"
	TargetWorkspace TargetKind = "workspace"
	TargetSession   TargetKind = "session"
)

// Target is the stable mutation identity used for conflicts and per-Session
// accounting. RuntimeEpoch is intentionally absent: an accepted request must
// remain replayable after a runtime rebuild within the same Host epoch. The
// caller's expectedHostEpoch/expectedRuntimeEpoch remain in typed params and
// therefore in the fingerprint.
type Target struct {
	Kind        TargetKind
	WorkspaceID protocol.WorkspaceID
	SessionID   protocol.SessionID
}

func HostTarget() Target { return Target{Kind: TargetHost} }

func WorkspaceTarget(workspaceID protocol.WorkspaceID) Target {
	return Target{Kind: TargetWorkspace, WorkspaceID: workspaceID}
}

func SessionTarget(target protocol.RuntimeTarget) Target {
	return Target{Kind: TargetSession, WorkspaceID: target.WorkspaceID, SessionID: target.SessionID}
}

func (t Target) Validate() error {
	switch t.Kind {
	case TargetHost:
		if t.WorkspaceID != "" || t.SessionID != "" {
			return errors.New("idempotency: Host target cannot contain workspaceId or sessionId")
		}
	case TargetWorkspace:
		if strings.TrimSpace(string(t.WorkspaceID)) == "" || t.SessionID != "" {
			return errors.New("idempotency: workspace target requires workspaceId and forbids sessionId")
		}
	case TargetSession:
		if strings.TrimSpace(string(t.WorkspaceID)) == "" || strings.TrimSpace(string(t.SessionID)) == "" {
			return errors.New("idempotency: Session target requires workspaceId and sessionId")
		}
	default:
		return fmt.Errorf("idempotency: unknown target kind %q", t.Kind)
	}
	return nil
}

func (t Target) canonicalValue() map[string]any {
	value := map[string]any{"kind": string(t.Kind)}
	if t.WorkspaceID != "" {
		value["workspaceId"] = string(t.WorkspaceID)
	}
	if t.SessionID != "" {
		value["sessionId"] = string(t.SessionID)
	}
	return value
}

func (t Target) sessionKey() (sessionKey, bool) {
	if t.Kind != TargetSession {
		return sessionKey{}, false
	}
	return sessionKey{workspaceID: t.WorkspaceID, sessionID: t.SessionID}, true
}

func (t Target) runtimeTarget() *protocol.RuntimeTarget {
	if t.Kind != TargetSession {
		return nil
	}
	target := protocol.RuntimeTarget{WorkspaceID: t.WorkspaceID, SessionID: t.SessionID}
	return &target
}

// Request contains one fully decoded mutation DTO. Params must still contain
// its top-level requestId so Begin can verify that the registry key and wire
// value agree before omitting it from the fingerprint.
type Request struct {
	RequestID protocol.RequestID
	Method    string
	Target    Target
	Params    any
}

type Status uint8

const (
	StatusNew Status = iota + 1
	StatusPending
	StatusCompleted
)

func (s Status) String() string {
	switch s {
	case StatusNew:
		return "new"
	case StatusPending:
		return "pending"
	case StatusCompleted:
		return "completed"
	default:
		return "unknown"
	}
}

// Options exposes the clock and capacity values for deterministic tests. Zero
// values use the frozen Remote V1 constants; these values are not user
// configuration.
type Options struct {
	Now               func() time.Time
	Retention         time.Duration
	PerSessionEntries int
	PerHostEntries    int
}

type Registry struct {
	mu                sync.Mutex
	hostEpoch         protocol.HostEpoch
	now               func() time.Time
	retention         time.Duration
	perSessionEntries int
	perHostEntries    int
	entries           map[protocol.RequestID]*entry
	sessionCounts     map[sessionKey]int
	completedLRU      list.List // oldest access at the front; contains *entry
}

type sessionKey struct {
	workspaceID protocol.WorkspaceID
	sessionID   protocol.SessionID
}

type entry struct {
	requestID   protocol.RequestID
	method      string
	target      Target
	fingerprint Fingerprint
	done        chan struct{}
	outcome     *Outcome
	terminalErr error
	completedAt time.Time
	lruElement  *list.Element
}

// New constructs a registry for one daemon Host epoch. Changing Host epoch
// requires ResetHostEpoch, which intentionally drops all in-memory records.
func New(hostEpoch protocol.HostEpoch, options Options) (*Registry, error) {
	if strings.TrimSpace(string(hostEpoch)) == "" {
		return nil, errors.New("idempotency: hostEpoch is required")
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	retention := options.Retention
	if retention <= 0 {
		retention = protocol.IdempotencyRetention
	}
	perSessionEntries := options.PerSessionEntries
	if perSessionEntries <= 0 {
		perSessionEntries = protocol.IdempotencySessionEntries
	}
	perHostEntries := options.PerHostEntries
	if perHostEntries <= 0 {
		perHostEntries = protocol.IdempotencyHostEntries
	}
	return &Registry{
		hostEpoch:         hostEpoch,
		now:               now,
		retention:         retention,
		perSessionEntries: perSessionEntries,
		perHostEntries:    perHostEntries,
		entries:           make(map[protocol.RequestID]*entry),
		sessionCounts:     make(map[sessionKey]int),
	}, nil
}

// Begin performs only registry admission. StatusNew returns a Claim; the
// caller owns business admission and semantic-state commit. StatusPending and
// StatusCompleted return an Attempt whose Wait method yields the first
// admission outcome. A conflicting reuse returns REQUEST_ID_CONFLICT.
func (r *Registry) Begin(request Request) (Attempt, error) {
	fingerprint, err := FingerprintFor(request.Method, request.Target, request.RequestID, request.Params)
	if err != nil {
		return Attempt{}, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	r.pruneExpiredLocked(now)
	if attempt, found, err := r.lookupLocked(request, fingerprint); found || err != nil {
		return attempt, err
	}

	r.makeRoomLocked(request.Target)
	created := &entry{
		requestID: request.RequestID, method: request.Method, target: request.Target,
		fingerprint: fingerprint, done: make(chan struct{}),
	}
	r.entries[request.RequestID] = created
	if key, ok := request.Target.sessionKey(); ok {
		r.sessionCounts[key]++
	}
	return Attempt{
		status: StatusNew, registry: r, entry: created,
		claim: &Claim{registry: r, entry: created},
	}, nil
}

// Lookup performs the mandatory pre-epoch registry query without registering a
// new request. A transport uses it before resolving a target/runtime so an
// accepted request replay (or conflicting requestId reuse) wins over stale
// epoch and changed catalog state. On a miss, the owning Host/Session sequencer
// must call Begin again inside its atomic admission action; that second lookup
// closes the race between this read and sequencer admission.
func (r *Registry) Lookup(request Request) (Attempt, bool, error) {
	fingerprint, err := FingerprintFor(request.Method, request.Target, request.RequestID, request.Params)
	if err != nil {
		return Attempt{}, false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneExpiredLocked(r.now())
	return r.lookupLocked(request, fingerprint)
}

func (r *Registry) lookupLocked(request Request, fingerprint Fingerprint) (Attempt, bool, error) {
	existing := r.entries[request.RequestID]
	if existing == nil {
		return Attempt{}, false, nil
	}
	if existing.method != request.Method || existing.target != request.Target || existing.fingerprint != fingerprint {
		return Attempt{}, true, protocol.MustRemoteError(protocol.ErrRequestIDConflict, protocol.ErrorOptions{
			Target: request.Target.runtimeTarget(),
		})
	}
	if existing.outcome != nil {
		r.touchCompletedLocked(existing)
		return Attempt{status: StatusCompleted, registry: r, entry: existing}, true, nil
	}
	return Attempt{status: StatusPending, registry: r, entry: existing}, true, nil
}

// ResetHostEpoch models daemon epoch replacement for in-process owners and
// tests. A real daemon restart naturally constructs a fresh Registry. Records
// intentionally survive Session runtime epoch replacement and are only reset
// with the Host epoch or normal expiry/eviction.
func (r *Registry) ResetHostEpoch(next protocol.HostEpoch) error {
	if strings.TrimSpace(string(next)) == "" {
		return errors.New("idempotency: next hostEpoch is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.hostEpoch == next {
		return nil
	}
	err := fmt.Errorf("%w: %s -> %s", ErrHostEpochChanged, r.hostEpoch, next)
	for _, current := range r.entries {
		if current.outcome == nil {
			current.terminalErr = err
			close(current.done)
		}
	}
	r.hostEpoch = next
	r.entries = make(map[protocol.RequestID]*entry)
	r.sessionCounts = make(map[sessionKey]int)
	r.completedLRU.Init()
	return nil
}

type Stats struct {
	HostEpoch protocol.HostEpoch
	Entries   int
	Pending   int
	Completed int
}

func (r *Registry) Stats() Stats {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneExpiredLocked(r.now())
	stats := Stats{HostEpoch: r.hostEpoch, Entries: len(r.entries), Completed: r.completedLRU.Len()}
	stats.Pending = stats.Entries - stats.Completed
	return stats
}

// Attempt is a stable view of one Begin decision. Wait never owns, executes or
// cancels the underlying mutation. Context cancellation only stops this
// caller's wait.
type Attempt struct {
	status   Status
	registry *Registry
	entry    *entry
	claim    *Claim
}

func (a Attempt) Status() Status { return a.status }

func (a Attempt) Claim() (*Claim, bool) {
	return a.claim, a.claim != nil
}

func (a Attempt) Fingerprint() Fingerprint {
	if a.entry == nil {
		return Fingerprint{}
	}
	return a.entry.fingerprint
}

func (a Attempt) Wait(ctx context.Context) (Outcome, error) {
	if a.entry == nil || a.registry == nil {
		return Outcome{}, errors.New("idempotency: empty attempt")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-a.entry.done:
	case <-ctx.Done():
		return Outcome{}, ctx.Err()
	}

	a.registry.mu.Lock()
	defer a.registry.mu.Unlock()
	if a.entry.terminalErr != nil {
		return Outcome{}, cloneTerminalError(a.entry.terminalErr)
	}
	if a.entry.outcome == nil {
		return Outcome{}, ErrClaimClosed
	}
	if a.registry.entries[a.entry.requestID] == a.entry {
		a.registry.touchCompletedLocked(a.entry)
	}
	return a.entry.outcome.clone(), nil
}

// Claim belongs only to the StatusNew caller. Complete/Reject must happen
// after business admission and the sequencer's semantic-state commit. Abort is
// for pre-admission failures (for example stale epochs) and never caches them.
type Claim struct {
	registry *Registry
	entry    *entry
}

func (c *Claim) Complete(result any) error {
	outcome, err := PrepareSuccess(result)
	if err != nil {
		return err
	}
	return c.finish(outcome)
}

func (c *Claim) Reject(err error) error {
	outcome, outcomeErr := PrepareRejection(err)
	if outcomeErr != nil {
		return outcomeErr
	}
	return c.finish(outcome)
}

// Resolve publishes a previously prepared admission outcome. Session actors
// can PrepareSuccess/PrepareRejection, commit their snapshot semantics, and
// then call Resolve in the same serialized turn without executing business
// work inside the registry.
func (c *Claim) Resolve(outcome Outcome) error {
	if !outcome.valid() {
		return errors.New("idempotency: invalid admission outcome")
	}
	return c.finish(outcome.clone())
}

func (c *Claim) Abort(cause error) error {
	if c == nil || c.registry == nil || c.entry == nil {
		return ErrClaimClosed
	}
	if cause == nil {
		cause = ErrAdmissionAbandoned
	}
	r := c.registry
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries[c.entry.requestID] != c.entry || c.entry.outcome != nil || c.entry.terminalErr != nil {
		return ErrClaimClosed
	}
	r.removeEntryLocked(c.entry)
	c.entry.terminalErr = cause
	close(c.entry.done)
	return nil
}

func (c *Claim) finish(outcome Outcome) error {
	if c == nil || c.registry == nil || c.entry == nil {
		return ErrClaimClosed
	}
	r := c.registry
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries[c.entry.requestID] != c.entry || c.entry.outcome != nil || c.entry.terminalErr != nil {
		return ErrClaimClosed
	}
	now := r.now()
	c.entry.outcome = &outcome
	c.entry.completedAt = now
	c.entry.lruElement = r.completedLRU.PushBack(c.entry)
	close(c.entry.done)
	r.pruneExpiredLocked(now)
	r.enforceLimitsLocked(c.entry.target)
	return nil
}

func cloneTerminalError(err error) error {
	var remote *protocol.RemoteError
	if errors.As(err, &remote) && remote != nil {
		return cloneRemoteError(remote)
	}
	return err
}

func (r *Registry) makeRoomLocked(target Target) {
	if key, ok := target.sessionKey(); ok {
		for r.sessionCounts[key] >= r.perSessionEntries {
			if !r.evictOldestCompletedLocked(&key) {
				break // pending entries are never evicted
			}
		}
	}
	for len(r.entries) >= r.perHostEntries {
		if !r.evictOldestCompletedLocked(nil) {
			break // pending entries are never evicted
		}
	}
}

func (r *Registry) enforceLimitsLocked(target Target) {
	if key, ok := target.sessionKey(); ok {
		for r.sessionCounts[key] > r.perSessionEntries {
			if !r.evictOldestCompletedLocked(&key) {
				break
			}
		}
	}
	for len(r.entries) > r.perHostEntries {
		if !r.evictOldestCompletedLocked(nil) {
			break
		}
	}
}

func (r *Registry) pruneExpiredLocked(now time.Time) {
	for element := r.completedLRU.Front(); element != nil; {
		next := element.Next()
		current := element.Value.(*entry)
		if !now.Before(current.completedAt.Add(r.retention)) {
			r.removeEntryLocked(current)
		}
		element = next
	}
}

func (r *Registry) touchCompletedLocked(current *entry) {
	if current.lruElement != nil {
		r.completedLRU.MoveToBack(current.lruElement)
	}
}

func (r *Registry) evictOldestCompletedLocked(onlySession *sessionKey) bool {
	for element := r.completedLRU.Front(); element != nil; element = element.Next() {
		current := element.Value.(*entry)
		if onlySession != nil {
			key, ok := current.target.sessionKey()
			if !ok || key != *onlySession {
				continue
			}
		}
		r.removeEntryLocked(current)
		return true
	}
	return false
}

func (r *Registry) removeEntryLocked(current *entry) {
	if r.entries[current.requestID] != current {
		return
	}
	delete(r.entries, current.requestID)
	if key, ok := current.target.sessionKey(); ok {
		r.sessionCounts[key]--
		if r.sessionCounts[key] == 0 {
			delete(r.sessionCounts, key)
		}
	}
	if current.lruElement != nil {
		r.completedLRU.Remove(current.lruElement)
		current.lruElement = nil
	}
}
