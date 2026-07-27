package recovery

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"reasonix/internal/agent"
)

// ModeProvider reports the current tool-approval mode (ask|auto|yolo).
type ModeProvider func() string

// EmitPromptFunc shows a fresh Auto Guard card and returns its id.
// It must not grant session or persistent authorization. The gate waits until
// Resolve is called for that id (or ctx ends).
type EmitPromptFunc func(ctx context.Context, taskID string, pending PendingProposal, failure *FailureEvent) (approvalID string, err error)

// Reviewer evaluates ambiguous failure-recovery proposals.
type Reviewer interface {
	Review(ctx context.Context, failure *FailureEvent, diagnosis []string, proposal Proposal, taskSummary string) (ReviewVerdict, error)
}

// Options configures a Gate.
type Options struct {
	Mode            ModeProvider
	EmitPrompt      EmitPromptFunc
	Reviewer        Reviewer
	TaskSummary     func() string
	MaxReviewBlocks int // consecutive reviewer blocks before stop-and-report guidance
	Now             func() time.Time
	// Headless, when true, never waits for a human: blocks the mutation with a
	// structured blocker message instead.
	Headless bool
	// PersistenceKey is sampled synchronously when a state change is scheduled.
	// Persist receives that captured key so an asynchronous write cannot follow
	// a later session switch and land in the wrong sidecar.
	PersistenceKey func() string
	// Persist is invoked after meaningful state changes (optional).
	// Receives the persistence projection (never active locks).
	Persist func(key string, snapshot Snapshot)
}

// Gate is the Auto Guard coordinator for one controller session.
// Root, foreground sub-agents, and background writer sub-agents share it.
// Exact-operation failure counts are isolated by TaskID; Episode totals,
// reviewer rejects, and hard stop are shared on episode so a new sub-agent
// cannot reset the hard ceiling. Pure routing lives in Decide.
//
// EpisodeID is host-owned temporary execution-round state. TaskScopeID continues
// to scope Goal and task grants. Episode/generation/waiters never persist.
type Gate struct {
	mu      sync.Mutex
	opts    Options
	tasks   map[string]*taskRuntime
	metrics Metrics
	waiters map[string]chan resolvePayload // keyed by approval id
	taskOf  map[string]string              // approval id -> task id
	pending map[string]PendingProposal     // approval id -> transient proposal scope
	// awaiting tracks in-flight human prompts so Phase can be derived without
	// storing Pending on the task runtime.
	awaiting map[string]struct{} // task ids with an open waiter

	// episodeSeq / episodeID identify the current host-owned Recovery Episode.
	// generation invalidates in-flight tool observations across mode switches.
	// episode holds totals and hard-stop shared by every TaskID in the Episode.
	episodeSeq uint64
	episodeID  string
	generation uint64
	episode    episodeBudget
	lastMode   string
	haveMode   bool

	// persistMu orders asynchronous snapshots. A newer state may be scheduled
	// before an older goroutine reaches disk; sequence checks prevent that older
	// snapshot from overwriting the newer checkpoint.
	persistMu   sync.Mutex
	persistSeq  uint64
	persistCond *sync.Cond
	// persistPending and persistDone are tracked per session key so old and new
	// sessions can drain independently without retaining keys after completion.
	persistPending map[string]int
	persistDone    map[string]uint64
}

type resolvePayload struct {
	action   Action
	feedback string
}

// dismissedWaiter is a recovery waiter cancelled by mode switch / episode rotate.
type dismissedWaiter struct {
	id      string
	taskID  string
	reply   chan resolvePayload
	payload resolvePayload
}

// NewGate constructs Auto Guard. The gate is active whenever approval mode is
// Auto; Ask and YOLO bypass it through the mode provider.
func NewGate(opts Options) *Gate {
	if opts.Mode == nil {
		opts.Mode = func() string { return "auto" }
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.MaxReviewBlocks <= 0 {
		opts.MaxReviewBlocks = MaxReviewRejects
	}
	g := &Gate{
		opts:           opts,
		tasks:          map[string]*taskRuntime{},
		waiters:        map[string]chan resolvePayload{},
		taskOf:         map[string]string{},
		pending:        map[string]PendingProposal{},
		awaiting:       map[string]struct{}{},
		episodeSeq:     1,
		episodeID:      "ep:1",
		generation:     1,
		persistPending: map[string]int{},
		persistDone:    map[string]uint64{},
	}
	g.persistCond = sync.NewCond(&g.persistMu)
	return g
}

// EpisodeID returns the current host-owned Recovery Episode id.
func (g *Gate) EpisodeID() string {
	if g == nil {
		return ""
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.episodeID
}

// Generation returns the current observation/proposal generation.
func (g *Gate) Generation() uint64 {
	if g == nil {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.generation
}

// BeginEpisode rotates into a fresh Recovery Episode. Failure, reviewer, and
// stop budgets clear. Explicit task grants and TaskScope authorizations are
// preserved. Call on: real user messages, Plan "start execution", Recovery
// "try another approach", real tool-approval mode changes, and new Session /
// Controller restore. Same-value mode replays must not call this.
func (g *Gate) BeginEpisode() {
	if g == nil {
		return
	}
	dismissed := g.beginEpisodeLockedCollect(true)
	g.finishDismissed(dismissed)
	g.persist()
}

// OnModeChange rotates Episode and generation when the tool-approval mode
// actually changes. Same-value replays (desktop hydration/reconcile) are no-ops
// so in-flight Auto state is not wiped. Returns dismissed recovery approval ids
// so the controller can clear matching cards outside the gate lock.
func (g *Gate) OnModeChange(mode string) []string {
	if g == nil {
		return nil
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return nil
	}
	g.mu.Lock()
	if g.haveMode && g.lastMode == mode {
		g.mu.Unlock()
		return nil
	}
	// First observation only pins the baseline mode (desktop hydrate / initial
	// ApplyToolApprovalMode). Same-value later replays are no-ops above; a real
	// change rotates Episode and generation.
	if !g.haveMode {
		g.lastMode = mode
		g.haveMode = true
		g.mu.Unlock()
		return nil
	}
	g.lastMode = mode
	g.metrics.ModeResets++
	dismissed := g.beginEpisodeLockedCollect(false)
	// Bump generation even when episode collection already did — mode switch
	// must invalidate in-flight observations.
	if g.generation == 0 {
		g.generation = 1
	}
	ids := make([]string, 0, len(dismissed))
	for _, d := range dismissed {
		ids = append(ids, d.id)
	}
	g.mu.Unlock()
	g.finishDismissed(dismissed)
	g.persist()
	return ids
}

// beginEpisodeLockedCollect must be called with g.mu held when alreadyLocked is
// false it acquires the lock. When alreadyHeld is true, caller holds g.mu.
func (g *Gate) beginEpisodeLockedCollect(lock bool) []dismissedWaiter {
	if lock {
		g.mu.Lock()
	}
	g.episodeSeq++
	if g.episodeSeq == 0 {
		g.episodeSeq = 1
	}
	g.episodeID = fmt.Sprintf("ep:%d", g.episodeSeq)
	g.generation++
	if g.generation == 0 {
		g.generation = 1
	}
	g.metrics.EpisodeRotations++
	// Episode-level hard-stop budgets reset for every TaskID together.
	g.episode.clear()
	// Clear task-local operation counters; preserve task grants.
	for id, st := range g.tasks {
		if st == nil {
			delete(g.tasks, id)
			continue
		}
		grants := st.taskGrants
		grantScope := st.taskGrantScope
		st.clearTaskRecoveryState()
		st.episodeID = g.episodeID
		st.taskGrants = grants
		st.taskGrantScope = grantScope
		if !st.hasTaskGrants() && st.empty() {
			delete(g.tasks, id)
		}
	}
	dismissed := g.collectWaitersLocked(resolvePayload{
		action:   ActionRevise,
		feedback: "Tool approval mode or recovery episode changed. Re-evaluate under the new mode; the previous proposal was not approved.",
	})
	if lock {
		g.mu.Unlock()
	}
	return dismissed
}

func (g *Gate) collectWaitersLocked(payload resolvePayload) []dismissedWaiter {
	out := make([]dismissedWaiter, 0, len(g.waiters))
	for id, ch := range g.waiters {
		taskID := g.taskOf[id]
		out = append(out, dismissedWaiter{id: id, taskID: taskID, reply: ch, payload: payload})
		delete(g.waiters, id)
		delete(g.taskOf, id)
		delete(g.pending, id)
		delete(g.awaiting, taskID)
	}
	return out
}

func (g *Gate) finishDismissed(dismissed []dismissedWaiter) {
	for _, d := range dismissed {
		if d.reply == nil {
			continue
		}
		select {
		case d.reply <- d.payload:
		default:
		}
	}
}

// Metrics returns a copy of content-free counters accumulated since gate
// construction or the most recent DrainMetrics call.
func (g *Gate) Metrics() Metrics {
	if g == nil {
		return Metrics{}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.metrics
}

// DrainMetrics atomically returns and clears recovery counters accumulated
// since the last drain. Desktop telemetry uses this delta API at TurnDone so a
// historical event is never counted again on later turns.
func (g *Gate) DrainMetrics() Metrics {
	if g == nil {
		return Metrics{}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	out := g.metrics
	g.metrics = Metrics{}
	return out
}

// FlushPersistence waits until every snapshot already scheduled for key has
// finished. Session destruction uses this before removing sidecars so a late
// asynchronous write cannot resurrect an artifact that was just deleted.
func (g *Gate) FlushPersistence(key string) {
	if g == nil || g.opts.Persist == nil {
		return
	}
	g.persistMu.Lock()
	for g.persistPending[key] > 0 {
		g.persistCond.Wait()
	}
	g.persistMu.Unlock()
}

// HasApproval reports whether a live Auto decision waiter is parked under id.
// Unlike Snapshot, this includes normal-execution plan transitions that have a
// waiter but no armed failure/taskRuntime yet. Legacy Approve paths must use
// this (or Resolve) instead of inferring from a persistence snapshot.
func (g *Gate) HasApproval(id string) bool {
	if g == nil {
		return false
	}
	id = strings.TrimSpace(id)
	if id == "" || strings.HasPrefix(id, "pending:") {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.waiters[id]; ok {
		return true
	}
	_, ok := g.taskOf[id]
	return ok
}

// Snapshot returns a live debug copy of task state (may include budgets).
func (g *Gate) Snapshot() Snapshot {
	if g == nil {
		return Snapshot{}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.snapshotLocked(false)
}

// PersistenceSnapshot returns the disk projection: historical last_failure
// evidence only. Active locks, Episode counters, generation, and waiters never
// appear.
func (g *Gate) PersistenceSnapshot() Snapshot {
	if g == nil {
		return Snapshot{}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.snapshotLocked(true)
}

func (g *Gate) snapshotLocked(persistence bool) Snapshot {
	// Map task id -> live approval id for observability. Restore always drops
	// these fields so a restart never replays a transient authorization.
	approvalByTask := map[string]string{}
	if !persistence {
		for approvalID, taskID := range g.taskOf {
			if strings.HasPrefix(approvalID, "pending:") {
				continue
			}
			approvalByTask[taskID] = approvalID
		}
	}
	out := Snapshot{Tasks: map[string]*TaskState{}}
	for id, st := range g.tasks {
		var cp *TaskState
		if persistence {
			cp = st.toPersistenceState()
		} else {
			phase := PhaseDiagnosing
			if _, waiting := g.awaiting[id]; waiting {
				phase = PhaseAwaitingDecision
			}
			cp = st.toTaskState(phase)
		}
		if cp == nil {
			continue
		}
		if !persistence {
			if aid := approvalByTask[id]; aid != "" {
				cp.ApprovalID = aid
				cp.Phase = PhaseAwaitingDecision
			}
			// Project shared Episode budgets onto each task for live debug views.
			cp.ReviewBlocks = int(g.episode.reviewRejects)
			cp.EpisodeStopped = g.episode.stopped
			cp.StopReason = string(g.episode.stopReason)
			if cp.EpisodeID == "" {
				cp.EpisodeID = g.episodeID
			}
		}
		out.Tasks[id] = cp
	}
	return out
}

// Restore loads persisted failure evidence after restart/controller rebuild.
// Live prompts, budgets, Episode counters, and task-local grants are never
// replayed: old consecutive_fails / review_blocks become historical evidence
// only and do not re-arm locks.
func (g *Gate) Restore(snap Snapshot) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.tasks = map[string]*taskRuntime{}
	g.waiters = map[string]chan resolvePayload{}
	g.taskOf = map[string]string{}
	g.pending = map[string]PendingProposal{}
	g.awaiting = map[string]struct{}{}
	// Fresh Episode after restore/session switch so prior runtime budgets
	// cannot block the user.
	g.episodeSeq++
	if g.episodeSeq == 0 {
		g.episodeSeq = 1
	}
	g.episodeID = fmt.Sprintf("ep:%d", g.episodeSeq)
	g.generation++
	if g.generation == 0 {
		g.generation = 1
	}
	g.episode.clear()
	g.metrics.EpisodeRotations++
	for id, st := range snap.Tasks {
		rt := taskRuntimeFromState(st)
		if rt == nil {
			continue
		}
		rt.episodeID = g.episodeID
		// Ignore old Pending and ApprovalID — never restore as authorization.
		g.tasks[id] = rt
	}
}

// BindApprovalID associates a prompt id with the task waiting on it so
// Resolve can find the waiter after EmitPrompt returns. If a provisional
// waiter is parked under pending:<taskID>, it is re-keyed to approvalID.
func (g *Gate) BindApprovalID(taskID, approvalID string) {
	if g == nil {
		return
	}
	taskID = normalizeTaskID(taskID)
	approvalID = strings.TrimSpace(approvalID)
	if approvalID == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	provisional := "pending:" + taskID
	if ch := g.waiters[provisional]; ch != nil {
		delete(g.waiters, provisional)
		delete(g.taskOf, provisional)
		g.waiters[approvalID] = ch
	}
	if pending, ok := g.pending[provisional]; ok {
		delete(g.pending, provisional)
		g.pending[approvalID] = pending
	}
	g.taskOf[approvalID] = taskID
	g.awaiting[taskID] = struct{}{}
}

// Resolve applies a user decision to a pending Auto Guard approval.
// action is continue|continue_task|revise. For revise, feedback is returned through the
// blocked tool result and the current mutation is refused in the same operation.
func (g *Gate) Resolve(id string, action Action, feedback string) error {
	if g == nil {
		return fmt.Errorf("recovery gate is nil")
	}
	id = strings.TrimSpace(id)
	g.mu.Lock()
	ch := g.waiters[id]
	taskID := g.taskOf[id]
	pending := g.pending[id]
	if taskID == "" {
		g.mu.Unlock()
		return fmt.Errorf("unknown recovery approval %q", id)
	}
	st := g.tasks[taskID]
	rotateEpisode := false
	switch action {
	case ActionContinue, ActionContinueTask:
		if action == ActionContinueTask {
			if pending.TaskGrantKey == "" {
				g.mu.Unlock()
				return fmt.Errorf("recovery approval %q cannot grant similar actions", id)
			}
			if st == nil {
				st = &taskRuntime{episodeID: g.episodeID}
				g.tasks[taskID] = st
			}
			st.useTaskGrantScope(pending.TaskGrantTaskScope)
			st.addTaskGrant(pending.TaskGrantKey)
			g.metrics.TaskGrantContinues++
		}
		// Human continue does not reset Episode reviewer rejects; only real
		// mutation/verification progress, a new Episode, or revise does.
		g.metrics.HumanContinues++
	case ActionRevise:
		// Revise rejects the pending action and starts a fresh Recovery Episode
		// so alternative approaches get a clean budget.
		rotateEpisode = true
		g.metrics.HumanRevises++
		if strings.TrimSpace(feedback) == "" {
			feedback = DefaultReviseFeedback
		}
	default:
		g.mu.Unlock()
		return fmt.Errorf("unknown recovery action %q", action)
	}
	delete(g.waiters, id)
	delete(g.taskOf, id)
	delete(g.pending, id)
	delete(g.awaiting, taskID)
	if !rotateEpisode {
		if st == nil || (st.empty() && !st.hasTaskGrants()) {
			delete(g.tasks, taskID)
		}
	}
	g.mu.Unlock()

	if ch != nil {
		select {
		case ch <- resolvePayload{action: action, feedback: feedback}:
		default:
		}
	}
	if rotateEpisode {
		// Fresh Episode after "try another approach" so alternatives get a clean
		// budget. BeginEpisode also dismisses any other waiters safely.
		g.BeginEpisode()
	} else {
		g.persist()
	}
	return nil
}

// ObserveResult implements agent.RecoveryGate. It returns one-shot guidance
// for the caller to enqueue on the exact Agent.Run that observed the failure.
func (g *Gate) ObserveResult(_ context.Context, obs Observation) string {
	if g == nil || !g.activeMode() {
		return ""
	}
	taskID := normalizeTaskID(obs.TaskID)

	g.mu.Lock()
	defer g.mu.Unlock()

	// Stale observations from a previous generation (mode switch / episode
	// rotate mid-flight) are ignored so they cannot re-arm old locks.
	if obs.Generation != 0 && obs.Generation != g.generation {
		g.metrics.StaleObservationsIgnored++
		return ""
	}

	st := g.ensureTaskLocked(taskID)

	// Successful host-recognized verification clears Episode no-progress budgets.
	if obs.Success && obs.Verification {
		g.clearNoProgressLocked(taskID, st)
		g.persistUnlocked()
		return ""
	}
	// Any successful mutation ends the current no-progress budget.
	if obs.Success && obs.Mutates {
		g.clearNoProgressLocked(taskID, st)
		g.persistUnlocked()
		return ""
	}
	// Diagnostic read successes do not clear failure state. Preserve a bounded
	// evidence excerpt for the isolated reviewer; otherwise it sees the failure
	// and proposed diff but none of the investigation that connected them.
	if obs.Success {
		if st.lastFailure != nil && IsDiagnosticSuccess(obs) {
			if appendDiagnosisNote(st.lastFailure, diagnosticObservationNote(obs)) {
				g.persistUnlocked()
			}
		}
		return ""
	}
	if !QualifyingFailure(obs) {
		return ""
	}

	fp := observationFingerprint(obs)
	st.ensureMaps()
	st.episodeID = g.episodeID
	if st.operationFailures[fp] < 255 {
		st.operationFailures[fp]++
	}
	// Episode totals accumulate across every TaskID (root + sub-agents).
	if g.episode.totalFailures < 255 {
		g.episode.totalFailures++
	}
	if st.operationFailures[fp] >= MaxOperationFailures {
		st.markOperationStopped(fp)
		g.metrics.OperationStops++
	}
	if g.episode.totalFailures >= MaxEpisodeFailures {
		g.episode.stopped = true
		g.episode.stopReason = StopReasonEpisodeFailures
		g.metrics.EpisodeFailureStops++
	}

	st.lastFailure = &activeFailure{
		evidence: FailureEvent{
			Class:         ClassifyFailure(obs),
			Tool:          obs.Tool,
			ArgsSummary:   ArgsSummary(obs.Args, 200),
			Subject:       obs.Subject,
			ErrSummary:    obs.ErrSummary,
			OutputExcerpt: clip(obs.Output, 1500),
			SourceAgent:   obs.AgentID,
			TaskID:        taskID,
			TaskScopeID:   persistentRecoveryScope(obs.TaskScopeID),
			ReadOnly:      obs.ReadOnly,
			Verification:  obs.Verification,
			Mutates:       obs.Mutates,
			CreatedAt:     g.opts.Now(),
			Args:          append(json.RawMessage(nil), obs.Args...),
			Fingerprint:   fp,
		},
		safeRetryUsed: false,
	}
	// Keep diagnosis notes if same fingerprint; otherwise start fresh list.
	g.metrics.FailureEvents++
	guidance := g.recoveryGuidanceLocked(st)
	g.persistUnlocked()
	return guidance
}

// BeforeMutation implements agent.RecoveryGate.
func (g *Gate) BeforeMutation(ctx context.Context, proposal Proposal) (Decision, error) {
	if g == nil {
		return Decision{Allow: true}, nil
	}

	// Host-proven read-only diagnostics always continue, including after the
	// Episode execution budget is exhausted. Decide also encodes the non-Auto
	// bypass so Ask and YOLO keep their existing semantics.
	facts, failure, diagNotes, taskID, fp, gen := g.classify(proposal)
	route := Decide(facts)

	// Escalation: re-proposing an already-stopped operation burns the
	// stopped-op retry budget and may stop the whole turn.
	if facts.AutoMode && facts.OperationAlreadyStopped && facts.SameFailedOperation {
		dec, escalated := g.noteStoppedOpRetry(taskID, fp, gen, proposal)
		if escalated {
			return dec, nil
		}
		// Still under retry budget: fall through to RouteStop for this op.
		route = DecisionResult{Route: RouteStop, StopReason: StopReasonOperationFailures}
	}

	// Episode total failure hard stop before reviewer work.
	if facts.AutoMode && facts.EpisodeFailureCount >= MaxEpisodeFailures && (facts.Mutates || facts.Verification) {
		return g.stopTurnDecision(taskID, gen, StopReasonEpisodeFailures, proposal), nil
	}

	switch route.Route {
	case RouteBypass, RouteAllow:
		if route.ConsumeSafeRetry {
			g.mu.Lock()
			if st := g.tasks[taskID]; st != nil && st.lastFailure != nil && !st.lastFailure.safeRetryUsed {
				st.lastFailure.safeRetryUsed = true
				g.metrics.RuleContinues++
			}
			g.mu.Unlock()
			g.persist()
		}
		return Decision{Allow: true, Generation: gen}, nil
	case RouteReview:
		return g.reviewOrEscalate(ctx, taskID, fp, gen, proposal, failure, diagNotes)
	case RouteStop:
		return Decision{
			Allow:      false,
			Blocked:    true,
			Message:    repeatedFailureStopMessage(int(facts.FailureCount), proposal),
			Generation: gen,
			StopReason: string(StopReasonOperationFailures),
		}, nil
	case RouteStopTurn:
		return g.stopTurnDecision(taskID, gen, route.StopReason, proposal), nil
	default:
		return Decision{Allow: true, Generation: gen}, nil
	}
}

func (g *Gate) noteStoppedOpRetry(taskID, _ string, gen uint64, proposal Proposal) (Decision, bool) {
	g.mu.Lock()
	_ = g.ensureTaskLocked(taskID)
	if g.episode.stoppedOpRetries < 255 {
		g.episode.stoppedOpRetries++
	}
	retries := g.episode.stoppedOpRetries
	if retries >= MaxStoppedOperationRetries {
		g.episode.stopped = true
		if g.episode.stopReason == StopReasonNone {
			g.episode.stopReason = StopReasonStoppedOpRetries
		}
		g.metrics.StoppedOpRetryStops++
		g.mu.Unlock()
		g.persist()
		return g.stopTurnDecision(taskID, gen, StopReasonStoppedOpRetries, proposal), true
	}
	g.mu.Unlock()
	g.persist()
	return Decision{}, false
}

func (g *Gate) stopTurnDecision(taskID string, gen uint64, reason StopReason, proposal Proposal) Decision {
	g.mu.Lock()
	_ = g.ensureTaskLocked(taskID)
	g.episode.stopped = true
	if g.episode.stopReason == StopReasonNone {
		g.episode.stopReason = reason
	}
	stopReason := g.episode.stopReason
	g.mu.Unlock()
	g.persist()
	msg := episodeStopMessage(stopReason, proposal)
	return Decision{
		Allow:      false,
		Blocked:    true,
		Message:    msg,
		Generation: gen,
		StopTurn:   true,
		StopReason: string(stopReason),
	}
}

// MarkFinalizationOffered records that the agent was given its one summarize-only
// round after an Episode stop. Subsequent tool proposals while still stopped
// should surface RecoveryPauseError.
func (g *Gate) MarkFinalizationOffered(taskID string) {
	if g == nil {
		return
	}
	_ = taskID
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.episode.stopped {
		g.episode.finalizationOffered = true
	}
}

// ConsumeFinalization reports whether the finalization round already ran and
// the model still attempted tools. Also marks it consumed on first true check
// after offered. Finalization is Episode-scoped (shared by all TaskIDs).
func (g *Gate) ConsumeFinalization(taskID string) (offered, alreadyConsumed bool) {
	if g == nil {
		return false, false
	}
	_ = taskID
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.episode.stopped {
		return false, false
	}
	offered = g.episode.finalizationOffered
	alreadyConsumed = g.episode.finalizationConsumed
	if offered && !alreadyConsumed {
		g.episode.finalizationConsumed = true
	}
	return offered, alreadyConsumed
}

// EpisodeStopped reports whether the shared Recovery Episode is exhausted for
// any TaskID (root or sub-agent).
func (g *Gate) EpisodeStopped(taskID string) bool {
	if g == nil {
		return false
	}
	_ = taskID
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.episode.stopped
}

// clearNoProgressLocked clears Episode totals and the observing task's local
// counters after real mutation/verification success. Caller holds g.mu.
func (g *Gate) clearNoProgressLocked(taskID string, st *taskRuntime) {
	g.episode.clear()
	if st != nil {
		st.clearTaskRecoveryState()
		st.episodeID = g.episodeID
		if !st.hasTaskGrants() {
			delete(g.tasks, taskID)
		}
	}
}

// classify builds pure Facts for Decide. It never calls the model or UI.
func (g *Gate) classify(proposal Proposal) (Facts, *FailureEvent, []string, string, string, uint64) {
	facts := Facts{
		AutoMode:       g.activeMode(),
		ReadOnly:       proposal.ReadOnly,
		Mutates:        proposal.Mutates,
		Verification:   proposal.Verification,
		PlanTransition: proposal.PlanTransition,
	}
	// Deterministic boundary checks run before the failure-recovery path.
	boundary := riskBoundaryForProposal(proposal)
	proposal.HighRisk = boundary.highRisk
	facts.HighRisk = boundary.highRisk

	taskID := normalizeTaskID(proposal.TaskID)
	// Operation failure accounting intentionally excludes Preview. Agent calls
	// always carry a display/approval preview, while completed observations do
	// not; mixing the two shapes would make an exact retry look like an unseen
	// operation and bypass its three-failure stop. Keep the preview-bound
	// fingerprint for one-shot human approval below.
	operationFP := CallFingerprint(proposal.Tool, proposal.Subject, "", proposal.Args)
	approvalFP := CallFingerprint(proposal.Tool, proposal.Subject, proposal.Preview, proposal.Args)

	g.mu.Lock()
	gen := g.generation
	// Leaving Auto does not wait for the next proposal: OnModeChange handles
	// real mode switches. Here we still clear when mode is non-Auto so a
	// bypass path cannot keep armed Auto locks if OnModeChange was skipped.
	st := g.tasks[taskID]
	var failure *FailureEvent
	var diagNotes []string
	stateChanged := false
	// Shared Episode budget applies even when this TaskID has no local state.
	facts.EpisodeStopped = g.episode.stopped
	facts.StopReason = g.episode.stopReason
	facts.EpisodeFailureCount = g.episode.totalFailures
	facts.ReviewRejects = g.episode.reviewRejects
	if st != nil && !facts.AutoMode {
		if !st.empty() || g.episode.totalFailures > 0 || g.episode.reviewRejects > 0 || g.episode.stopped {
			st.clearTaskRecoveryState()
			g.episode.clear()
			stateChanged = true
		}
		if !st.hasTaskGrants() && st.empty() {
			delete(g.tasks, taskID)
			st = nil
		}
	}
	if st != nil {
		// Align task runtime with current Episode without wiping mid-Episode.
		if st.episodeID != "" && st.episodeID != g.episodeID {
			grants := st.taskGrants
			grantScope := st.taskGrantScope
			st.clearTaskRecoveryState()
			st.taskGrants = grants
			st.taskGrantScope = grantScope
			st.episodeID = g.episodeID
			stateChanged = true
		} else if st.episodeID == "" {
			st.episodeID = g.episodeID
		}
		facts.OperationAlreadyStopped = st.isOperationStopped(operationFP)
		facts.FailureCount = st.operationFailureCount(operationFP)
		if st.lastFailure != nil {
			failure = st.evidenceCopy()
			diagNotes = st.diagnosisNotes()
			facts.HasActiveFailure = true
			facts.SameFailedOperation = sameFailedOperation(failure, proposal)
			// When proposing the same op, FailureCount is the map value.
			// When proposing a different op after failures, HasActiveFailure
			// remains true for accounting, but Decide keeps that unrelated
			// operation on the automatic path.
			if facts.SameFailedOperation && facts.FailureCount == 0 {
				// Evidence exists but count was cleared somehow — treat as 1.
				facts.FailureCount = 1
			}
			if IsSafeVerificationRetry(failure, proposal) && st.safeRetryAvailable() {
				facts.SafeRetryAvailable = true
			}
		}
		taskScope := taskGrantScopeKey(proposal)
		st.useTaskGrantScope(taskScope)
		if st.empty() && !st.hasTaskGrants() {
			delete(g.tasks, taskID)
			st = nil
		}
		runtimeGrantKey := taskGrantRuntimeKey(boundary.taskGrantKey, taskScope)
		if facts.HighRisk && runtimeGrantKey != "" && st != nil && st.hasTaskGrant(runtimeGrantKey) {
			facts.HighRisk = false
			g.metrics.TaskGrantUses++
		}
	}
	g.mu.Unlock()
	if stateChanged {
		g.persist()
	}

	if failure != nil {
		if !proposal.ExpandedScope {
			proposal.ExpandedScope = ScopeExpanded(failure, proposal)
		}
		if !proposal.StrategyChanged {
			proposal.StrategyChanged = StrategyChanged(failure, proposal)
		}
		facts.ExpandedScope = proposal.ExpandedScope
		facts.StrategyChanged = proposal.StrategyChanged
		if facts.SafeRetryAvailable && (facts.ExpandedScope || facts.StrategyChanged || facts.HighRisk) {
			facts.SafeRetryAvailable = false
		}
	}
	return facts, failure, diagNotes, taskID, approvalFP, gen
}

func (g *Gate) ensureTaskLocked(taskID string) *taskRuntime {
	st := g.tasks[taskID]
	if st == nil {
		st = &taskRuntime{episodeID: g.episodeID}
		g.tasks[taskID] = st
	}
	if st.episodeID == "" {
		st.episodeID = g.episodeID
	}
	return st
}

func (g *Gate) reviewOrEscalate(ctx context.Context, taskID, fp string, gen uint64, proposal Proposal, failure *FailureEvent, diagNotes []string) (Decision, error) {
	// If Episode reviewer budget already exhausted, stop the turn.
	g.mu.Lock()
	if g.episode.reviewRejects >= uint8(g.opts.MaxReviewBlocks) {
		g.episode.stopped = true
		if g.episode.stopReason == StopReasonNone {
			g.episode.stopReason = StopReasonReviewRejects
		}
		g.metrics.ReviewStops++
		g.mu.Unlock()
		return g.stopTurnDecision(taskID, gen, StopReasonReviewRejects, proposal), nil
	}
	g.mu.Unlock()

	var verdict ReviewVerdict
	if g.opts.Reviewer != nil {
		start := g.opts.Now()
		taskSummary := strings.TrimSpace(proposal.TaskSummary)
		if taskSummary == "" && g.opts.TaskSummary != nil {
			taskSummary = g.opts.TaskSummary()
		}
		v, err := g.opts.Reviewer.Review(ctx, failure, diagNotes, proposal, taskSummary)
		latency := g.opts.Now().Sub(start).Milliseconds()
		g.mu.Lock()
		g.metrics.ReviewLatencyMsSum += latency
		g.metrics.ReviewLatencyCount++
		if err != nil {
			g.metrics.ReviewErrors++
		}
		g.mu.Unlock()
		if err != nil {
			if proposal.PlanTransition {
				return g.askHuman(ctx, taskID, fp, gen, proposal, failure, diagNotes, ChangeScope,
					"The active execution plan changed, but the independent plan reviewer is unavailable.")
			}
			g.mu.Lock()
			g.metrics.RuleContinues++
			g.mu.Unlock()
			return Decision{Allow: true, Generation: gen}, nil
		}
		verdict = normalizeVerdict(v, failure, proposal, diagNotes)
		if verdict.Outcome == ReviewContinue && reviewerContinueKind(verdict.ChangeKind) {
			// Reviewer Continue does NOT reset cumulative rejects. Only real
			// mutation/verification success, a new Episode, or revise does.
			g.mu.Lock()
			g.metrics.ReviewContinues++
			g.mu.Unlock()
			return Decision{
				Allow:                    true,
				AuthorizePlanReplacement: proposal.PlanTransition,
				Generation:               gen,
			}, nil
		}
		if proposal.PlanTransition && reviewerPlanDecision(verdict) {
			return g.askHuman(ctx, taskID, fp, gen, proposal, failure, diagNotes, verdict.ChangeKind, verdict.Rationale)
		}
		blocks := g.recordReviewBlock(taskID, verdict)
		if blocks < g.opts.MaxReviewBlocks {
			return Decision{
				Allow:      false,
				Blocked:    true,
				Message:    reviewerBlockerMessage(verdict, blocks, g.opts.MaxReviewBlocks),
				Generation: gen,
			}, nil
		}
		g.mu.Lock()
		g.episode.stopped = true
		if g.episode.stopReason == StopReasonNone {
			g.episode.stopReason = StopReasonReviewRejects
		}
		g.metrics.ReviewStops++
		g.mu.Unlock()
		return g.stopTurnDecision(taskID, gen, StopReasonReviewRejects, proposal), nil
	}
	if proposal.PlanTransition {
		return g.askHuman(ctx, taskID, fp, gen, proposal, failure, diagNotes, ChangeScope,
			"The active execution plan changed and needs your choice because no independent plan reviewer is configured.")
	}
	g.mu.Lock()
	g.metrics.RuleContinues++
	g.mu.Unlock()
	return Decision{Allow: true, Generation: gen}, nil
}

func (g *Gate) askHuman(ctx context.Context, taskID, fp string, gen uint64, proposal Proposal, failure *FailureEvent, diagNotes []string, kind ChangeKind, rationale string) (Decision, error) {
	failureSource := ""
	failureSummary := ""
	if failure != nil {
		failureSource = failure.SourceAgent
		failureSummary = failure.ErrSummary
	}
	pending := PendingProposal{
		Tool:        proposal.Tool,
		Subject:     proposal.Subject,
		Preview:     proposal.Preview,
		Args:        append(json.RawMessage(nil), proposal.Args...),
		Fingerprint: fp,
		SourceAgent: firstNonEmpty(proposal.AgentID, failureSource),
		ChangeKind:  kind,
		Rationale:   firstNonEmpty(rationale, userFacingReason(kind)),
		Diagnosis:   strings.Join(diagNotes, "\n"),
		Failure:     failureSummary,
		Proposed:    firstNonEmpty(proposal.Subject, proposal.Preview, proposal.Tool),
		PlanBefore:  proposal.PlanBefore,
		PlanAfter:   proposal.PlanAfter,
	}

	if g.opts.Headless || g.opts.EmitPrompt == nil {
		return Decision{
			Allow:      false,
			Blocked:    true,
			Message:    headlessBlockerMessage(pending, failure),
			Generation: gen,
		}, nil
	}

	// Create the waiter channel before EmitPrompt. Resolve may race in as soon
	// as the approval id is known (desktop/bot), so re-key the waiter under the
	// real id immediately after EmitPrompt returns.
	reply := make(chan resolvePayload, 1)
	g.mu.Lock()
	g.metrics.HumanPrompts++
	if st := g.tasks[taskID]; st != nil && st.failureCount() > 1 {
		g.metrics.RepeatPrompts++
	}
	provisional := "pending:" + taskID
	g.waiters[provisional] = reply
	g.taskOf[provisional] = taskID
	g.pending[provisional] = pending
	g.awaiting[taskID] = struct{}{}
	g.mu.Unlock()

	approvalID, err := g.opts.EmitPrompt(ctx, taskID, pending, failure)
	if err != nil {
		g.mu.Lock()
		delete(g.waiters, provisional)
		delete(g.taskOf, provisional)
		delete(g.pending, provisional)
		delete(g.awaiting, taskID)
		g.mu.Unlock()
		return Decision{Allow: false, Blocked: true, Message: "blocked: Auto Guard prompt failed: " + err.Error(), Generation: gen}, err
	}
	approvalID = strings.TrimSpace(approvalID)
	if approvalID == "" {
		g.mu.Lock()
		delete(g.waiters, provisional)
		delete(g.taskOf, provisional)
		delete(g.pending, provisional)
		delete(g.awaiting, taskID)
		g.mu.Unlock()
		return Decision{Allow: false, Blocked: true, Message: "blocked: Auto Guard prompt returned empty id", Generation: gen}, fmt.Errorf("empty Auto Guard approval id")
	}

	g.mu.Lock()
	// EmitPrompt implementations may bind the real id before emitting, which
	// lets a synchronous frontend resolve the card before EmitPrompt returns.
	// Only re-key a waiter that is still provisional; if both mappings are gone,
	// Resolve already completed and its buffered payload is waiting on reply.
	if provisionalReply, ok := g.waiters[provisional]; ok && provisionalReply != nil {
		delete(g.waiters, provisional)
		delete(g.taskOf, provisional)
		if p, exists := g.pending[provisional]; exists {
			delete(g.pending, provisional)
			g.pending[approvalID] = p
		}
		if existing, exists := g.waiters[approvalID]; exists && existing != nil {
			reply = existing
		} else {
			reply = provisionalReply
			g.waiters[approvalID] = reply
			g.taskOf[approvalID] = taskID
		}
	} else if existing, ok := g.waiters[approvalID]; ok && existing != nil {
		reply = existing
	}
	g.awaiting[taskID] = struct{}{}
	g.mu.Unlock()
	g.persist()

	select {
	case payload := <-reply:
		decision, err := g.decisionFromResolve(payload)
		if err == nil && decision.Allow && proposal.PlanTransition {
			decision.AuthorizePlanReplacement = true
		}
		decision.Generation = gen
		return decision, err
	case <-ctx.Done():
		g.mu.Lock()
		delete(g.waiters, approvalID)
		delete(g.taskOf, approvalID)
		delete(g.pending, approvalID)
		delete(g.awaiting, taskID)
		g.mu.Unlock()
		g.persist()
		return Decision{Allow: false, Blocked: true, Message: "blocked: Auto Guard confirmation cancelled", Generation: gen}, ctx.Err()
	}
}

func taskGrantScopeKey(proposal Proposal) string {
	// Root task ids span a controller session. TaskScopeID is host-owned and
	// unique per ordinary turn, while goal continuations reuse their delivery
	// scope. Hash it so task-local runtime state never contains raw task text.
	taskScope := strings.TrimSpace(proposal.TaskScopeID)
	if taskScope == "" {
		taskScope = strings.TrimSpace(proposal.TaskSummary)
	}
	return CallFingerprint(
		"task-grant",
		normalizeTaskID(proposal.TaskID),
		taskScope,
		nil,
	)
}

func taskGrantRuntimeKey(semanticKey, taskScope string) string {
	if semanticKey == "" || taskScope == "" {
		return ""
	}
	return semanticKey + "#" + taskScope
}

func (g *Gate) decisionFromResolve(payload resolvePayload) (Decision, error) {
	switch payload.action {
	case ActionContinue, ActionContinueTask:
		return Decision{Allow: true}, nil
	case ActionRevise:
		msg := "blocked: user requested a revised Auto Guard action"
		feedback := strings.TrimSpace(payload.feedback)
		if feedback == "" {
			feedback = DefaultReviseFeedback
		}
		msg += ": " + feedback
		return Decision{Allow: false, Blocked: true, Message: msg}, nil
	default:
		return Decision{Allow: false, Blocked: true, Message: "blocked: unknown Auto Guard action"}, nil
	}
}

// RecordDiagnosis appends a diagnosis note while recovering.
func (g *Gate) RecordDiagnosis(taskID, note string) {
	if g == nil || strings.TrimSpace(note) == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	st := g.tasks[normalizeTaskID(taskID)]
	if st == nil || st.lastFailure == nil {
		return
	}
	if appendDiagnosisNote(st.lastFailure, note) {
		g.persistUnlocked()
	}
}

// --- internals ---

func (g *Gate) activeMode() bool {
	mode := strings.ToLower(strings.TrimSpace(g.opts.Mode()))
	return mode == "auto"
}

func (g *Gate) recoveryGuidanceLocked(st *taskRuntime) string {
	if st.guidanceSent {
		return ""
	}
	st.guidanceSent = true
	if st.lastFailure != nil && st.lastFailure.evidence.Class == FailureClassTransient {
		return "The tool timed out or hit a transient execution limit. Inspect its current state and output before retrying so partial effects are not duplicated. " +
			"Read-only diagnosis and unrelated work remain available without asking the user; retry the exact operation only after ruling out partial effects."
	}
	return "A tool failed. Use read-only diagnosis as needed, continue unrelated work automatically, and do not ask the user unless a genuine product or plan choice is required. " +
		"Repeated retries of the exact failed operation remain bounded."
}

func diagnosticObservationNote(obs Observation) string {
	tool := clip(strings.TrimSpace(obs.Tool), 120)
	if tool == "" {
		tool = "diagnostic"
	}
	subject := clip(firstNonEmpty(obs.Subject, ArgsSummary(obs.Args, 160)), 160)
	header := tool
	if subject != "" && subject != tool {
		header += " (" + subject + ")"
	}
	output := strings.TrimSpace(obs.Output)
	if output == "" {
		return clipDiagnosisNote(header + ": completed successfully")
	}
	return clipDiagnosisNote(header + ": " + output)
}

func (g *Gate) persist() {
	if g == nil || g.opts.Persist == nil {
		return
	}
	// Disk never receives active lock state.
	g.schedulePersist(g.PersistenceSnapshot(), false)
}

func (g *Gate) persistUnlocked() {
	// Caller holds g.mu.
	if g == nil || g.opts.Persist == nil {
		return
	}
	g.schedulePersist(g.snapshotLocked(true), true)
}

func (g *Gate) schedulePersist(snap Snapshot, async bool) {
	if g == nil || g.opts.Persist == nil {
		return
	}
	key := ""
	if g.opts.PersistenceKey != nil {
		key = g.opts.PersistenceKey()
	}
	g.persistMu.Lock()
	g.persistSeq++
	seq := g.persistSeq
	g.persistPending[key]++
	g.persistMu.Unlock()
	write := func() {
		g.persistMu.Lock()
		defer g.persistMu.Unlock()
		defer func() {
			g.persistPending[key]--
			if g.persistPending[key] == 0 {
				delete(g.persistPending, key)
				delete(g.persistDone, key)
				g.persistCond.Broadcast()
			}
		}()
		if seq < g.persistDone[key] {
			return
		}
		g.opts.Persist(key, snap)
		g.persistDone[key] = seq
	}
	if async {
		go write()
		return
	}
	write()
}

// userFacingReason is the short localized-friendly reason shown on the card.
func userFacingReason(kind ChangeKind) string {
	switch kind {
	case ChangeRisk:
		return "This proposal is a technical execution-risk blocker, not a user-owned plan choice."
	case ChangeScope:
		return "This step would expand the change scope."
	case ChangeStrategy:
		return "Auto is about to try a different approach."
	default:
		return "Auto cannot establish how this proposal relates to the active task and plan."
	}
}

func headlessBlockerMessage(pending PendingProposal, failure *FailureEvent) string {
	var b strings.Builder
	b.WriteString("blocked: Auto Guard requires human confirmation, but this environment has no decision channel.\n")
	if failure != nil {
		b.WriteString("Failure: ")
		b.WriteString(firstNonEmpty(failure.ErrSummary, failure.Tool))
		b.WriteString("\n")
	}
	if pending.Diagnosis != "" {
		b.WriteString("Diagnosis: ")
		b.WriteString(pending.Diagnosis)
		b.WriteString("\n")
	}
	b.WriteString("Proposed: ")
	b.WriteString(firstNonEmpty(pending.Proposed, pending.Subject, pending.Tool))
	b.WriteString("\n")
	if pending.Rationale != "" {
		b.WriteString("Why confirm: ")
		b.WriteString(pending.Rationale)
	}
	return b.String()
}

func (g *Gate) recordReviewBlock(taskID string, verdict ReviewVerdict) int {
	g.mu.Lock()
	st := g.ensureTaskLocked(taskID)
	// Cumulative across all candidates and TaskIDs inside the Episode.
	if g.episode.reviewRejects < 255 {
		g.episode.reviewRejects++
	}
	blocks := int(g.episode.reviewRejects)
	if st.lastFailure != nil {
		note := "Auto Guard reviewer blocked the proposal: " + firstNonEmpty(verdict.Rationale, string(verdict.ChangeKind))
		appendDiagnosisNote(st.lastFailure, note)
	}
	g.mu.Unlock()
	g.persist()
	return blocks
}

func reviewerBlockerMessage(verdict ReviewVerdict, attempt, limit int) string {
	reason := firstNonEmpty(verdict.Rationale, "the proposal could not be classified as a bounded plan continuation")
	return fmt.Sprintf(
		"blocked: Auto plan reviewer could not accept this transition (attempt %d/%d): %s. Continue the current plan, propose a task-aligned plan, or ask the user about a genuine product choice.",
		attempt, limit, reason,
	)
}

func repeatedFailureStopMessage(failures int, proposal Proposal) string {
	operation := clip(firstNonEmpty(proposal.Subject, proposal.Tool), 240)
	return fmt.Sprintf(
		"blocked: Auto stopped repeating this operation after %d consecutive failures: %s. Do not retry the same operation in this turn. Diagnose it with read-only tools, then use a different task-aligned edit or verification approach; other operations remain available. Ask the user only for a genuine product or plan choice.",
		failures, operation,
	)
}

func episodeStopMessage(reason StopReason, proposal Proposal) string {
	operation := clip(firstNonEmpty(proposal.Subject, proposal.Tool), 240)
	switch reason {
	case StopReasonReviewRejects:
		return fmt.Sprintf(
			"blocked: Auto recovery paused this turn after %d reviewer rejections (last proposal: %s). Do not call more tools; summarize what was tried and what remains. The user can continue in the next message.",
			MaxReviewRejects, operation,
		)
	case StopReasonStoppedOpRetries:
		return fmt.Sprintf(
			"blocked: Auto recovery paused this turn after repeated attempts of already-stopped operations (last: %s). Do not call more tools; summarize completed work and blockers. The user can continue in the next message.",
			operation,
		)
	default:
		return fmt.Sprintf(
			"blocked: Auto recovery paused this turn after %d execution failures without progress (last: %s). Do not call more tools; summarize completed work and blockers. The user can continue in the next message.",
			MaxEpisodeFailures, operation,
		)
	}
}

func observationFingerprint(obs Observation) string {
	return CallFingerprint(obs.Tool, obs.Subject, "", obs.Args)
}

func persistentRecoveryScope(scope string) string {
	scope = strings.TrimSpace(scope)
	if strings.HasPrefix(scope, "goal:") {
		return scope
	}
	return ""
}

func sameFailedOperation(failure *FailureEvent, proposal Proposal) bool {
	if failure == nil {
		return false
	}
	want := strings.TrimSpace(failure.Fingerprint)
	if want == "" {
		want = CallFingerprint(failure.Tool, failure.Subject, "", failure.Args)
	}
	return want == CallFingerprint(proposal.Tool, proposal.Subject, "", proposal.Args)
}

func normalizeVerdict(v ReviewVerdict, failure *FailureEvent, proposal Proposal, diagNotes []string) ReviewVerdict {
	switch strings.ToLower(strings.TrimSpace(string(v.Outcome))) {
	case "continue":
		v.Outcome = ReviewContinue
	case "confirm":
		v.Outcome = ReviewConfirm
	default:
		// Unparseable/unknown outcome fails closed.
		v.Outcome = ReviewConfirm
		if v.ChangeKind == "" {
			v.ChangeKind = ChangeUncertain
		}
	}
	switch ChangeKind(strings.ToLower(strings.TrimSpace(string(v.ChangeKind)))) {
	case ChangeSameStrategy, ChangeStrategy, ChangeScope, ChangeRisk, ChangeUncertain:
		v.ChangeKind = ChangeKind(strings.ToLower(strings.TrimSpace(string(v.ChangeKind))))
	default:
		if v.Outcome == ReviewContinue {
			// Cannot silently continue without a clear bounded-recovery label.
			v.Outcome = ReviewConfirm
		}
		v.ChangeKind = ChangeUncertain
	}
	// Risk and uncertainty cannot silently continue, but they are technical
	// blockers rather than human approval requests. Strategy/scope may continue
	// when the reviewer established that the change remains task-aligned.
	if v.Outcome == ReviewContinue && !reviewerContinueKind(v.ChangeKind) {
		v.Outcome = ReviewConfirm
	}
	if strings.TrimSpace(v.FailureSummary) == "" && failure != nil {
		v.FailureSummary = failure.ErrSummary
	}
	if strings.TrimSpace(v.Diagnosis) == "" {
		v.Diagnosis = strings.Join(diagNotes, "\n")
	}
	if strings.TrimSpace(v.ProposedAction) == "" {
		v.ProposedAction = firstNonEmpty(proposal.Subject, proposal.Preview, proposal.Tool)
	}
	if strings.TrimSpace(v.Rationale) == "" {
		v.Rationale = userFacingReason(v.ChangeKind)
	} else {
		v.Rationale = clip(v.Rationale, 500)
	}
	return v
}

func reviewerContinueKind(kind ChangeKind) bool {
	switch kind {
	case ChangeSameStrategy, ChangeStrategy, ChangeScope:
		return true
	default:
		return false
	}
}

func reviewerPlanDecision(verdict ReviewVerdict) bool {
	if verdict.Outcome != ReviewConfirm {
		return false
	}
	switch verdict.ChangeKind {
	case ChangeStrategy, ChangeScope:
		return true
	default:
		return false
	}
}

// Ensure Gate implements agent.RecoveryGate.
var _ agent.RecoveryGate = (*Gate)(nil)
