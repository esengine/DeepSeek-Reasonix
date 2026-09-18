package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/transcript"
)

// SessionRef is the only execution identity used by the linear session
// service. Paths and UI selection are deliberately absent.
type SessionRef struct {
	HostID    string `json:"hostId"`
	SessionID string `json:"sessionId"`
}

func (r SessionRef) validate(hostID string) error {
	if r.HostID == "" || r.HostID != hostID {
		return fmt.Errorf("session: session host %q does not match service host %q", r.HostID, hostID)
	}
	return validateSessionID(r.SessionID)
}

var (
	ErrSessionNotRunning = errors.New("session runtime is not attached")
	ErrRuntimeBusy       = errors.New("session runtime already has an activity")
	ErrRuntimeBound      = errors.New("session runtime still has client bindings")
	ErrRuntimeRetiring   = errors.New("session runtime is retiring")
	ErrRecoveryRequired  = errors.New("session runtime requires recovery")
	ErrStaleActivity     = errors.New("session activity no longer owns commit authority")
	ErrStaleExecution    = errors.New("session execution generation no longer owns commit authority")
)

type RuntimePhase string

const (
	RuntimeIdle             RuntimePhase = "idle"
	RuntimeRunning          RuntimePhase = "running"
	RuntimeCancelling       RuntimePhase = "cancelling"
	RuntimeFinalizing       RuntimePhase = "finalizing"
	RuntimeRecoveryRequired RuntimePhase = "recovery_required"
	RuntimeClosed           RuntimePhase = "closed"
)

type RuntimeSnapshot struct {
	Ref              SessionRef   `json:"session"`
	Epoch            string       `json:"runtimeEpoch"`
	ActivityRevision uint64       `json:"activityRevision"`
	Phase            RuntimePhase `json:"phase"`
	Activity         string       `json:"activity,omitempty"`
	Session          Snapshot     `json:"sessionSnapshot"`
}

type CancelReceipt struct {
	Ref              SessionRef   `json:"session"`
	Accepted         bool         `json:"accepted"`
	RuntimeEpoch     string       `json:"runtimeEpoch,omitempty"`
	ActivityRevision uint64       `json:"activityRevision,omitempty"`
	Phase            RuntimePhase `json:"phase"`
}

// Runtime is the sole owner of a live Session and its write handle. Execution
// lifecycle lives in the bound turn-loop; persisted running events never
// create a Runtime after process restart.
type Runtime struct {
	transcript *transcript.Projection
	ref        SessionRef
	epoch      string
	session    *Session
	owner      *Service
	// instance stamps the publish grant so a delayed owner can prove it still
	// refers to the exact instance it published.
	instance string

	mu       sync.Mutex
	phase    RuntimePhase
	activity string
	revision atomic.Uint64
	// execution is the generation-scoped turn-loop. Cancel loads it without
	// taking mu so Stop never waits on a commit or persistence lock.
	execution atomic.Pointer[executionBinding]
	bindGen   atomic.Uint64
	canceling atomic.Bool
	closeDone chan struct{}
	closeErr  error
}

func newRuntime(ref SessionRef, session *Session) (*Runtime, error) {
	runtime, err := initializeRuntime(ref, session)
	// Logging can perform I/O; release the session lock before emitting.
	var diagnostic *TranscriptInitializationError
	if errors.As(err, &diagnostic) {
		slog.Error("session transcript initialization failed", "diagnostic", diagnostic)
	}
	return runtime, err
}

func initializeRuntime(ref SessionRef, session *Session) (*Runtime, error) {
	session.mu.Lock()
	defer session.mu.Unlock()
	runtime := &Runtime{ref: ref, epoch: randomID(), session: session, phase: RuntimeIdle}
	baseline := session.recentMessages
	if len(baseline) == 0 {
		baseline = session.projection.Messages
	}
	totalMessages := len(baseline)
	if len(baseline) > 96 {
		baseline = baseline[len(baseline)-96:]
	}
	projection, err := transcript.NewProjection(transcript.Identity{SessionID: ref.SessionID, RuntimeEpoch: runtime.epoch}, session.transcriptRows(baseline), session.next-1)
	if err != nil {
		return nil, &TranscriptInitializationError{sessionID: ref.SessionID, covered: session.next - 1,
			messageCount: len(baseline), totalMessages: totalMessages, cause: err}
	}
	runtime.transcript = projection
	durable := uint64(0)
	if session.binding != nil {
		durable, _, _ = session.binding.progress()
	}
	restored := transcript.Runtime{TurnID: session.projection.TurnID, Status: session.projection.TurnStatus, FinalMessageID: session.projection.CurrentTurnMessageID}
	if receipt, ok := session.projection.Submissions.byTurn[session.id+"\x00"+restored.TurnID]; ok {
		restored.SubmissionID = receipt.SubmissionID
	}
	restored.SamplingCount, restored.ToolCount = len(session.projection.CurrentAttempts), len(session.projection.CurrentCalls)
	if restored.TurnID != "" && !restored.Status.Terminal() {
		// A persisted open turn is recovery evidence, not a running model.
		restored.Status = event.TurnRecoveryRequired
	}
	if restored.TurnID == "" && len(session.projection.Turns) > 0 {
		last := session.projection.Turns[len(session.projection.Turns)-1]
		restored.TurnID, restored.FinalMessageID = last.TurnID, last.MessageID
		restored.DurationMs = last.DurationMs
		restored.SamplingCount, restored.ToolCount = last.SamplingCount, last.ToolCount
	}
	for _, message := range baseline {
		if message.ID == restored.FinalMessageID {
			restored.DurationMs = max(restored.DurationMs, message.WorkDurationMs)
		}
	}
	runtime.transcript.RestoreRuntime(restored, durable)
	session.transcript = runtime.transcript
	runtime.revision.Store(1)
	return runtime, nil
}

func (r *Runtime) Ref() SessionRef { return r.ref }

func (r *Runtime) Session() *Session { return r.session }

func (r *Runtime) Snapshot() RuntimeSnapshot {
	state := r.activitySnapshot()
	state.Session = r.session.Snapshot()
	return state
}

func (r *Runtime) StateSnapshot() RuntimeSnapshot {
	state := r.activitySnapshot()
	state.Session = r.session.StateSnapshot()
	return state
}

// ExecutionSnapshot returns the provider projection and lightweight turn
// boundaries without reconstructing the durable UI transcript.
func (r *Runtime) ExecutionSnapshot() RuntimeSnapshot {
	state := r.activitySnapshot()
	state.Session = r.session.ExecutionSnapshot()
	return state
}

func (r *Runtime) activitySnapshot() RuntimeSnapshot {
	r.mu.Lock()
	phase := r.phase
	if r.canceling.Load() && phase == RuntimeRunning {
		phase = RuntimeCancelling
	}
	state := RuntimeSnapshot{Ref: r.ref, Epoch: r.epoch, ActivityRevision: r.revision.Load(), Phase: phase, Activity: r.activity}
	r.mu.Unlock()
	return state
}

// Cancel forwards Stop to the bound turn-loop without taking the runtime
// mutex. An unbound runtime is already idle.
func (r *Runtime) Cancel() bool {
	for {
		exec := r.loadExecution()
		if exec == nil || exec.control == nil {
			return false
		}
		if !exec.control.Cancel() {
			// A host cutover may linearize while Cancel is inside the outgoing
			// loop. Retry only when ownership actually changed; a stable owner
			// rejecting Cancel remains a normal idle result.
			if r.loadExecution() != exec {
				continue
			}
			return false
		}
		break
	}
	r.canceling.Store(true)
	if r.mu.TryLock() {
		if r.phase == RuntimeRunning {
			r.phase = RuntimeCancelling
			r.activity = "cancelling"
			r.revision.Add(1)
		}
		r.mu.Unlock()
	}
	return true
}

func (r *Runtime) RequireRecovery(activity string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.phase == RuntimeClosed {
		return
	}
	r.phase = RuntimeRecoveryRequired
	r.activity = activity
	r.revision.Add(1)
}

func (r *Runtime) close(ctx context.Context) error {
	r.mu.Lock()
	if r.closeDone != nil {
		done := r.closeDone
		r.mu.Unlock()
		<-done
		return r.closeErr
	}
	if r.phase.busy() && r.loadExecution() != nil {
		r.mu.Unlock()
		return ErrRuntimeBusy
	}
	// Seal admission in the same critical section as the idle check. The
	// irreversible close has one uncancellable result for every caller.
	r.closeDone = make(chan struct{})
	r.phase = RuntimeClosed
	r.activity = ""
	r.revision.Add(1)
	r.mu.Unlock()
	r.transcript.CloseFollowers()
	r.closeErr = r.session.close(context.Background())
	close(r.closeDone)
	return r.closeErr
}

func osClosedError() error { return errors.New("session runtime is closed") }

// Service applies DSH's prepare/publish/exact-detach rule. Candidate handles
// are opened outside the registry lock; only the exact published Runtime can
// later unregister itself.
type Service struct {
	hostID      string
	persistence SessionPersistence

	mu         sync.Mutex
	active     map[SessionRef]*Runtime
	closed     map[SessionRef]error
	preparing  map[SessionRef]*prepareRuntime
	bindings   map[*Runtime]int
	retiring   map[*Runtime]chan struct{}
	retireIdle map[*Runtime]bool
	idleTimers map[*Runtime]*time.Timer
	idleWeight map[*Runtime]int64
	idleOrder  map[*Runtime]uint64
	idleUsed   int64
	idleClock  uint64
	idleBudget int64
	idleTTL    time.Duration
	query      *Query
	revision   atomic.Uint64
}

func (s *Service) Cancel(ref SessionRef) (RuntimeSnapshot, error) {
	runtime, ok := s.Runtime(ref)
	if !ok {
		return RuntimeSnapshot{}, ErrSessionNotRunning
	}
	runtime.Cancel()
	return runtime.StateSnapshot(), nil
}

// CancelSession is the public session-scoped Stop contract. A missing runtime
// is already idle and therefore succeeds idempotently; no caller-supplied turn
// id participates in routing or authorization.
func (s *Service) CancelSession(ref SessionRef) (CancelReceipt, error) {
	if err := ref.validate(s.hostID); err != nil {
		return CancelReceipt{}, err
	}
	runtime, ok := s.Runtime(ref)
	if !ok {
		return CancelReceipt{Ref: ref, Accepted: true, Phase: RuntimeIdle}, nil
	}
	if runtime.Cancel() {
		return CancelReceipt{
			Ref:              ref,
			Accepted:         true,
			RuntimeEpoch:     runtime.epoch,
			ActivityRevision: runtime.revision.Load(),
			Phase:            RuntimeCancelling,
		}, nil
	}
	snapshot := runtime.activitySnapshot()
	return CancelReceipt{Ref: ref, Accepted: true, RuntimeEpoch: snapshot.Epoch, ActivityRevision: snapshot.ActivityRevision, Phase: snapshot.Phase}, nil
}

func (s *Service) Flush(ctx context.Context, ref SessionRef) (DurableReceipt, error) {
	runtime, ok := s.Runtime(ref)
	if !ok {
		return DurableReceipt{}, ErrSessionNotRunning
	}
	return runtime.session.Flush(ctx)
}

// ContinueLegacy freezes one legacy head, publishes its deterministic final
// session, then attaches that exact session. It never writes the source and it
// does not accept the caller's pending submission; hosts enqueue the unchanged
// submission only after this method returns the new immutable identity.
func (s *Service) ContinueLegacy(ctx context.Context, sourcePath, headID string) (*Runtime, MigrationResult, error) {
	filesystem, ok := s.persistence.(*FilesystemPersistence)
	if !ok {
		return nil, MigrationResult{}, errors.New("session: persistence does not support legacy migration")
	}
	result, err := migrateLegacyHeadForHost(ctx, sourcePath, filesystem.Root, headID)
	if err != nil {
		return nil, result, err
	}
	runtime, err := s.openRuntime(ctx, SessionRef{HostID: s.hostID, SessionID: result.TargetID})
	return runtime, result, err
}

// ContinueImported resolves the paired legacy transcript and retired event
// sidecar as one frozen migration decision. It refuses divergent histories
// instead of letting a caller accidentally resume whichever source it opened
// first.
func (s *Service) ContinueImported(ctx context.Context, sourcePath, headID string) (*Runtime, ImportResult, error) {
	return s.ContinueImportedWithHeader(ctx, sourcePath, headID, CreateOptions{})
}

// ContinueImportedWithHeader publishes Desktop ownership in the same atomic
// directory publication as the imported history.
func (s *Service) ContinueImportedWithHeader(ctx context.Context, sourcePath, headID string, options CreateOptions) (*Runtime, ImportResult, error) {
	filesystem, ok := s.persistence.(*FilesystemPersistence)
	if !ok {
		return nil, ImportResult{}, errors.New("session: persistence does not support imported sessions")
	}
	result, err := importSourceForLegacyWithHeader(ctx, sourcePath, filesystem.Root, headID, options)
	if err != nil {
		return nil, result, err
	}
	runtime, err := s.openRuntime(ctx, SessionRef{HostID: s.hostID, SessionID: result.TargetID})
	return runtime, result, err
}

// ContinuePrototype is the explicit, fail-closed bridge for the retired
// sidecar codec. Unknown required events or conflicting tails remain read-only.
func (s *Service) ContinuePrototype(ctx context.Context, sourceDir string) (*Runtime, PrototypeImportResult, error) {
	filesystem, ok := s.persistence.(*FilesystemPersistence)
	if !ok {
		return nil, PrototypeImportResult{}, errors.New("session: persistence does not support prototype import")
	}
	result, err := ImportPrototype(ctx, sourceDir, filesystem.Root)
	if err != nil {
		return nil, result, err
	}
	runtime, err := s.openRuntime(ctx, SessionRef{HostID: s.hostID, SessionID: result.TargetID})
	return runtime, result, err
}

// ContinueImportedFrom resolves legacy history against its original paired
// store while publishing only into this service's separate staging root.
func (s *Service) ContinueImportedFrom(ctx context.Context, sourcePath, sourceRoot, headID string) (*Runtime, ImportResult, error) {
	return s.ContinueImportedSource(ctx, sourcePath, filepath.Join(sourceRoot, agent.BranchID(sourcePath)), headID)
}

// ContinueImportedSource accepts a provenance-linked directory whose identity
// may have changed when a legacy head was previously converted.
func (s *Service) ContinueImportedSource(ctx context.Context, sourcePath, sourceDir, headID string) (*Runtime, ImportResult, error) {
	filesystem, ok := s.persistence.(*FilesystemPersistence)
	if !ok {
		return nil, ImportResult{}, errors.New("session: persistence does not support imported sessions")
	}
	result, err := importSourceForLegacyAt(ctx, sourcePath, sourceDir, filesystem.Root, headID, CreateOptions{})
	if err != nil {
		return nil, result, err
	}
	sourceRoot := filepath.Dir(sourceDir)
	if result.Kind == "final" && filepath.Clean(sourceRoot) != filepath.Clean(filesystem.Root) {
		tmp, err := os.MkdirTemp("", "reasonix-canonical-stage-")
		if err != nil {
			return nil, result, err
		}
		defer os.RemoveAll(tmp)
		bundle := filepath.Join(tmp, "bundle")
		if err := NewFilesystemPersistence(sourceRoot).exportCold(ctx, result.TargetID, bundle); err != nil {
			return nil, result, err
		}
		if _, err := s.Import(ctx, bundle); err != nil {
			return nil, result, err
		}
	}
	runtime, err := s.openRuntime(ctx, SessionRef{HostID: s.hostID, SessionID: result.TargetID})
	return runtime, result, err
}

// ContinueStoredPreview upgrades a pre-ownership linear store selected by its
// former session id. The old directory remains read-only; execution resumes on
// the deterministic final-codec identity returned here.
func (s *Service) ContinueStoredPreview(ctx context.Context, sessionID string) (*Runtime, PrototypeImportResult, error) {
	filesystem, ok := s.persistence.(*FilesystemPersistence)
	if !ok {
		return nil, PrototypeImportResult{}, errors.New("session: persistence does not support preview import")
	}
	if err := validateSessionID(sessionID); err != nil {
		return nil, PrototypeImportResult{}, err
	}
	sourceDir, err := filesystem.sessionDir(sessionID, true)
	if err != nil {
		return nil, PrototypeImportResult{}, err
	}
	result, err := ImportStoredPreview(ctx, sourceDir, filesystem.Root)
	if err != nil {
		return nil, result, err
	}
	runtime, err := s.openRuntime(ctx, SessionRef{HostID: s.hostID, SessionID: result.TargetID})
	return runtime, result, err
}

// Fork creates an independent child at the exact end event of a completed
// turn. No message-count inference is involved.
func (s *Service) Fork(ctx context.Context, ref SessionRef, afterTurnID, childID string) (*Runtime, error) {
	runtime, ok := s.Runtime(ref)
	if !ok {
		return nil, ErrSessionNotRunning
	}
	turn, ok := completedTurn(runtime.session.Snapshot().Projection.Turns, afterTurnID)
	if !ok {
		return nil, fmt.Errorf("session: completed turn %q not found", afterTurnID)
	}
	return s.forkAt(ctx, runtime, turn.EndSequence, childID)
}

// Rewind creates a child from the event immediately before beforeTurnID.
func (s *Service) Rewind(ctx context.Context, ref SessionRef, beforeTurnID, childID string) (*Runtime, error) {
	runtime, ok := s.Runtime(ref)
	if !ok {
		return nil, ErrSessionNotRunning
	}
	turn, ok := completedTurn(runtime.session.Snapshot().Projection.Turns, beforeTurnID)
	if !ok {
		return nil, fmt.Errorf("session: completed turn %q not found", beforeTurnID)
	}
	return s.forkAt(ctx, runtime, turn.StartSequence-1, childID)
}

func completedTurn(turns []TurnBoundary, id string) (TurnBoundary, bool) {
	for _, turn := range turns {
		if turn.TurnID == id {
			return turn, true
		}
	}
	return TurnBoundary{}, false
}

func (s *Service) forkAt(ctx context.Context, parent *Runtime, sequence uint64, childID string) (*Runtime, error) {
	filesystem, ok := s.persistence.(*FilesystemPersistence)
	if !ok {
		return nil, errors.New("session: persistence does not support filesystem fork")
	}
	if childID == "" {
		childID = randomID()
	}
	if err := validateSessionID(childID); err != nil {
		return nil, err
	}
	childDir := filepath.Join(filesystem.Root, childID)
	if _, err := parent.session.Fork(ctx, childDir, childID, sequence); err != nil {
		return nil, err
	}
	return s.openRuntime(ctx, SessionRef{HostID: s.hostID, SessionID: childID})
}

func (s *Service) Close(ctx context.Context, ref SessionRef) error {
	if err := ref.validate(s.hostID); err != nil {
		return err
	}
	s.mu.Lock()
	runtime := s.active[ref]
	closedErr, closed := s.closed[ref]
	s.mu.Unlock()
	if runtime == nil {
		if closed {
			return closedErr
		}
		return ErrSessionNotRunning
	}
	return s.closeOwned(ctx, runtime, "")
}

// closeOwned is the teardown entry point for a RuntimeOwner holding one exact
// instance grant. A delayed old disposer must never close its same-ID
// successor, and a client-bound runtime is never torn down underneath it.
func (s *Service) closeOwned(ctx context.Context, runtime *Runtime, instance string) error {
	return s.closeRuntime(ctx, runtime, instance, false)
}

// closeRuntime adds the terminal variant Shutdown needs. Refusing a bound
// runtime is right while the process keeps running, but at shutdown it would
// strand the writer lease and recovery handles for the process lifetime, so
// the final teardown releases them and still reports the leaked binding.
func (s *Service) closeRuntime(ctx context.Context, runtime *Runtime, instance string, terminal bool) error {
	if runtime == nil {
		return ErrSessionNotRunning
	}
	if err := runtime.ref.validate(s.hostID); err != nil {
		return err
	}
	if instance != "" && runtime.instance != instance {
		return ErrSessionNotRunning
	}
	var leaked error
	s.mu.Lock()
	if timer := s.idleTimers[runtime]; timer != nil {
		s.removeIdleCacheLocked(runtime, true)
	}
	if s.bindings[runtime] != 0 {
		if !terminal {
			s.mu.Unlock()
			return ErrRuntimeBound
		}
		delete(s.bindings, runtime)
		leaked = fmt.Errorf("%w: %s", ErrRuntimeBound, runtime.ref.SessionID)
	}
	if s.active[runtime.ref] != runtime {
		s.mu.Unlock()
		return errors.Join(leaked, runtime.close(ctx))
	}
	if done := s.retiring[runtime]; done != nil {
		s.mu.Unlock()
		select {
		case <-done:
			return errors.Join(leaked, runtime.close(ctx))
		case <-ctx.Done():
			return errors.Join(leaked, ctx.Err())
		}
	}
	done := make(chan struct{})
	s.retiring[runtime] = done
	s.mu.Unlock()
	err := runtime.close(ctx)
	s.mu.Lock()
	delete(s.retiring, runtime)
	if !errors.Is(err, ErrRuntimeBusy) && s.active[runtime.ref] == runtime {
		delete(s.active, runtime.ref)
		delete(s.retireIdle, runtime)
		s.removeIdleCacheLocked(runtime, true)
		s.closed[runtime.ref] = err
		s.revision.Add(1)
	}
	close(done)
	s.mu.Unlock()
	return errors.Join(leaked, err)
}

// Detach removes a runtime only if it is still the exact published instance.
// It is used by host callbacks that may arrive after a replacement.
func (s *Service) Detach(runtime *Runtime) bool {
	if runtime == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active[runtime.ref] != runtime {
		return false
	}
	if timer := s.idleTimers[runtime]; timer != nil {
		s.removeIdleCacheLocked(runtime, true)
	}
	delete(s.active, runtime.ref)
	s.revision.Add(1)
	return true
}

type ObserveResult struct {
	Runtime *RuntimeSnapshot `json:"runtime,omitempty"`
	Events  EventPage        `json:"events"`
}

// SessionDir resolves the on-disk directory of a final-format identity
// without opening it. Hosts use it to probe writer occupancy for takeover
// flows; the writer lease itself is never taken here.
func (s *Service) SessionDir(ctx context.Context, ref SessionRef) (string, error) {
	if err := ref.validate(s.hostID); err != nil {
		return "", err
	}
	info, err := s.persistence.Stat(ctx, ref.SessionID)
	if err != nil {
		return "", err
	}
	return info.Path, nil
}

func (s *Service) Observe(ctx context.Context, ref SessionRef, cursor uint64, limit int) (ObserveResult, error) {
	if err := ref.validate(s.hostID); err != nil {
		return ObserveResult{}, err
	}
	if runtime, ok := s.Runtime(ref); ok {
		// Observe reports runtime state plus an explicitly paged event tail. It
		// must not duplicate the provider model workset into every poll.
		snapshot := runtime.StateSnapshot()
		page, err := runtime.session.AcceptedPage(ctx, cursor, limit)
		return ObserveResult{Runtime: &snapshot, Events: page}, err
	}
	handle, err := s.persistence.Open(ref.SessionID, ReadOnly)
	if err != nil {
		return ObserveResult{}, err
	}
	defer handle.Close(context.Background())
	page, err := handle.Read(ctx, cursor, limit)
	return ObserveResult{Events: page}, err
}
