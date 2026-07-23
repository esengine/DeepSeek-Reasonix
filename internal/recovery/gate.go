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
	Persist func(key string, snapshot Snapshot)
}

// Gate is the Auto Guard coordinator for one controller session.
// Root, foreground sub-agents, and background writer sub-agents share it;
// state is isolated by TaskID. Pure routing lives in Decide; this type owns
// state updates, reviewer calls, approval waiters, and persistence.
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
		opts.MaxReviewBlocks = 3
	}
	g := &Gate{
		opts:           opts,
		tasks:          map[string]*taskRuntime{},
		waiters:        map[string]chan resolvePayload{},
		taskOf:         map[string]string{},
		pending:        map[string]PendingProposal{},
		awaiting:       map[string]struct{}{},
		persistPending: map[string]int{},
		persistDone:    map[string]uint64{},
	}
	g.persistCond = sync.NewCond(&g.persistMu)
	return g
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

// Snapshot returns a copy of task state for persistence.
func (g *Gate) Snapshot() Snapshot {
	if g == nil {
		return Snapshot{}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.snapshotLocked()
}

func (g *Gate) snapshotLocked() Snapshot {
	// Map task id -> live approval id for observability. Restore always drops
	// these fields so a restart never replays a transient authorization.
	approvalByTask := map[string]string{}
	for approvalID, taskID := range g.taskOf {
		if strings.HasPrefix(approvalID, "pending:") {
			continue
		}
		approvalByTask[taskID] = approvalID
	}
	out := Snapshot{Tasks: map[string]*TaskState{}}
	for id, st := range g.tasks {
		phase := PhaseDiagnosing
		if _, waiting := g.awaiting[id]; waiting {
			phase = PhaseAwaitingDecision
		}
		cp := st.toTaskState(phase)
		if cp == nil {
			// Task may only be waiting without residual failure (rare); skip.
			continue
		}
		if aid := approvalByTask[id]; aid != "" {
			cp.ApprovalID = aid
			cp.Phase = PhaseAwaitingDecision
		}
		out.Tasks[id] = cp
	}
	return out
}

// Restore loads persisted failure context after restart/controller rebuild.
// Live prompts and task-local grants are deliberately not replayed: the
// interrupted call no longer exists, so the next proposed action must be
// classified again.
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
	for id, st := range snap.Tasks {
		rt := taskRuntimeFromState(st)
		if rt == nil {
			continue
		}
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
	switch action {
	case ActionContinue, ActionContinueTask:
		if action == ActionContinueTask {
			if pending.TaskGrantKey == "" {
				g.mu.Unlock()
				return fmt.Errorf("recovery approval %q cannot grant similar actions", id)
			}
			if st == nil {
				st = &taskRuntime{}
				g.tasks[taskID] = st
			}
			st.useTaskGrantScope(pending.TaskGrantTaskScope)
			st.addTaskGrant(pending.TaskGrantKey)
			g.metrics.TaskGrantContinues++
		}
		if st != nil {
			st.reviewRejects = 0
		}
		g.metrics.HumanContinues++
	case ActionRevise:
		if st != nil {
			st.reviewRejects = 0
		}
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
	if st == nil || (st.failure == nil && !st.hasTaskGrants()) {
		delete(g.tasks, taskID)
	}
	g.mu.Unlock()

	if ch != nil {
		select {
		case ch <- resolvePayload{action: action, feedback: feedback}:
		default:
		}
	}
	g.persist()
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
	st := g.tasks[taskID]

	// Successful host-recognized verification clears the failure event.
	if obs.Success && obs.Verification {
		if st != nil {
			st.clearFailure()
			if !st.hasTaskGrants() {
				delete(g.tasks, taskID)
			}
			g.persistUnlocked()
		}
		return ""
	}
	// Any successful mutation ends the current failure event.
	if obs.Success && obs.Mutates {
		if st != nil {
			st.clearFailure()
			if !st.hasTaskGrants() {
				delete(g.tasks, taskID)
			}
			g.persistUnlocked()
		}
		return ""
	}
	// Diagnostic read successes do not clear failure state. Preserve a bounded
	// evidence excerpt for the isolated reviewer; otherwise it sees the failure
	// and proposed diff but none of the investigation that connected them.
	if obs.Success {
		if st != nil && st.failure != nil && IsDiagnosticSuccess(obs) {
			if appendDiagnosisNote(st.failure, diagnosticObservationNote(obs)) {
				g.persistUnlocked()
			}
		}
		return ""
	}
	if !QualifyingFailure(obs) {
		return ""
	}
	if st == nil {
		st = &taskRuntime{}
		g.tasks[taskID] = st
	}

	// Already recovering and another recovery op failed: raise failure count.
	if st.failure != nil {
		if st.failure.failureCount < 255 {
			st.failure.failureCount++
		}
		st.failure.evidence.ErrSummary = firstNonEmpty(obs.ErrSummary, st.failure.evidence.ErrSummary)
		st.failure.evidence.OutputExcerpt = clip(obs.Output, 1500)
		st.reviewRejects = 0
		g.metrics.FailureEvents++
		guidance := g.recoveryGuidanceLocked(st)
		g.persistUnlocked()
		return guidance
	}

	fp := CallFingerprint(obs.Tool, obs.Subject, "", obs.Args)
	st.failure = &activeFailure{
		evidence: FailureEvent{
			Tool:          obs.Tool,
			ArgsSummary:   ArgsSummary(obs.Args, 200),
			Subject:       obs.Subject,
			ErrSummary:    obs.ErrSummary,
			OutputExcerpt: clip(obs.Output, 1500),
			SourceAgent:   obs.AgentID,
			TaskID:        taskID,
			ReadOnly:      obs.ReadOnly,
			Verification:  obs.Verification,
			Mutates:       obs.Mutates,
			CreatedAt:     g.opts.Now(),
			Args:          append(json.RawMessage(nil), obs.Args...),
			Fingerprint:   fp,
		},
		failureCount:  1,
		safeRetryUsed: false,
	}
	st.reviewRejects = 0
	st.guidanceSent = false
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

	// Host-proven read-only diagnostics always continue. Decide also encodes the
	// non-Auto bypass so Ask and YOLO keep their existing semantics.
	facts, failure, diagNotes, taskID, fp := g.classify(proposal)
	route := Decide(facts)

	switch route.Route {
	case RouteBypass, RouteAllow:
		if route.ConsumeSafeRetry {
			g.mu.Lock()
			if st := g.tasks[taskID]; st != nil && st.failure != nil && !st.failure.safeRetryUsed {
				st.failure.safeRetryUsed = true
				g.metrics.RuleContinues++
			}
			g.mu.Unlock()
			g.persist()
		}
		return Decision{Allow: true}, nil
	case RouteReview:
		return g.reviewOrEscalate(ctx, taskID, fp, proposal, failure, diagNotes)
	case RouteStop:
		return Decision{
			Allow:   false,
			Blocked: true,
			Message: repeatedFailureStopMessage(int(facts.FailureCount)),
		}, nil
	default:
		return Decision{Allow: true}, nil
	}
}

// classify builds pure Facts for Decide. It never calls the model or UI.
func (g *Gate) classify(proposal Proposal) (Facts, *FailureEvent, []string, string, string) {
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
	fp := CallFingerprint(proposal.Tool, proposal.Subject, proposal.Preview, proposal.Args)

	g.mu.Lock()
	st := g.tasks[taskID]
	var failure *FailureEvent
	var diagNotes []string
	if st != nil && st.failure != nil {
		failure = st.evidenceCopy()
		diagNotes = st.diagnosisNotes()
		facts.HasActiveFailure = true
		facts.FailureCount = st.failureCount()
		// Safe verification retry availability is host-classified.
		if IsSafeVerificationRetry(failure, proposal) && st.safeRetryAvailable() {
			facts.SafeRetryAvailable = true
		}
	}
	taskScope := taskGrantScopeKey(proposal)
	if st != nil {
		st.useTaskGrantScope(taskScope)
		if st.empty() && !st.hasTaskGrants() {
			delete(g.tasks, taskID)
			st = nil
		}
	}
	runtimeGrantKey := taskGrantRuntimeKey(boundary.taskGrantKey, taskScope)
	if facts.HighRisk && runtimeGrantKey != "" && st != nil && st.hasTaskGrant(runtimeGrantKey) {
		facts.HighRisk = false
		g.metrics.TaskGrantUses++
	}
	g.mu.Unlock()

	if failure != nil {
		if !proposal.ExpandedScope {
			proposal.ExpandedScope = ScopeExpanded(failure, proposal)
		}
		if !proposal.StrategyChanged {
			proposal.StrategyChanged = StrategyChanged(failure, proposal)
		}
		facts.ExpandedScope = proposal.ExpandedScope
		facts.StrategyChanged = proposal.StrategyChanged
		// Safe retry cannot combine with scope/strategy/high-risk; classifier
		// already requires those flags clear in IsSafeVerificationRetry.
		if facts.SafeRetryAvailable && (facts.ExpandedScope || facts.StrategyChanged || facts.HighRisk) {
			facts.SafeRetryAvailable = false
		}
	}
	return facts, failure, diagNotes, taskID, fp
}

func (g *Gate) reviewOrEscalate(ctx context.Context, taskID, fp string, proposal Proposal, failure *FailureEvent, diagNotes []string) (Decision, error) {
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
			// A structural plan transition cannot be silently approved when its
			// independent reviewer is unavailable. Ask once about the plan itself;
			// ordinary recovery work still continues through infrastructure errors.
			if proposal.PlanTransition {
				return g.askHuman(ctx, taskID, fp, proposal, failure, diagNotes, ChangeScope,
					"The active execution plan changed, but the independent plan reviewer is unavailable.")
			}
			g.mu.Lock()
			g.metrics.RuleContinues++
			g.mu.Unlock()
			return Decision{Allow: true}, nil
		}
		verdict = normalizeVerdict(v, failure, proposal, diagNotes)
		if verdict.Outcome == ReviewContinue && reviewerContinueKind(verdict.ChangeKind) {
			g.mu.Lock()
			g.metrics.ReviewContinues++
			if current := g.tasks[taskID]; current != nil {
				current.reviewRejects = 0
			}
			g.mu.Unlock()
			g.persist()
			return Decision{
				Allow:                    true,
				AuthorizePlanReplacement: proposal.PlanTransition,
			}, nil
		}
		// A reviewer-confirmed strategy or scope decision is a material plan
		// boundary, not another unsafe tool attempt for the agent to reword. Ask
		// once at the boundary so Auto supervises plan transitions while ordinary
		// bounded implementation changes above remain interruption-free.
		if reviewerPlanDecision(verdict) {
			return g.askHuman(ctx, taskID, fp, proposal, failure, diagNotes, verdict.ChangeKind, verdict.Rationale)
		}
		// Risk and uncertainty are technical blockers, not user-owned choices.
		// Give the exact reason back to the agent; repeated blocks stop and report
		// instead of escalating into an execution-safety prompt.
		blocks := g.recordReviewBlock(taskID, verdict)
		if blocks < g.opts.MaxReviewBlocks {
			return Decision{
				Allow:   false,
				Blocked: true,
				Message: reviewerBlockerMessage(verdict, blocks, g.opts.MaxReviewBlocks),
			}, nil
		}
		return Decision{
			Allow:   false,
			Blocked: true,
			Message: reviewerStopMessage(verdict, blocks),
		}, nil
	}
	if proposal.PlanTransition {
		return g.askHuman(ctx, taskID, fp, proposal, failure, diagNotes, ChangeScope,
			"The active execution plan changed and needs your choice because no independent plan reviewer is configured.")
	}
	// A missing optional reviewer must not make ordinary recovery work
	// interactive. Execution permissions remain owned by their existing gates.
	g.mu.Lock()
	g.metrics.RuleContinues++
	g.mu.Unlock()
	return Decision{Allow: true}, nil
}

func (g *Gate) askHuman(ctx context.Context, taskID, fp string, proposal Proposal, failure *FailureEvent, diagNotes []string, kind ChangeKind, rationale string) (Decision, error) {
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
			Allow:   false,
			Blocked: true,
			Message: headlessBlockerMessage(pending, failure),
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
		return Decision{Allow: false, Blocked: true, Message: "blocked: Auto Guard prompt failed: " + err.Error()}, err
	}
	approvalID = strings.TrimSpace(approvalID)
	if approvalID == "" {
		g.mu.Lock()
		delete(g.waiters, provisional)
		delete(g.taskOf, provisional)
		delete(g.pending, provisional)
		delete(g.awaiting, taskID)
		g.mu.Unlock()
		return Decision{Allow: false, Blocked: true, Message: "blocked: Auto Guard prompt returned empty id"}, fmt.Errorf("empty Auto Guard approval id")
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
		return decision, err
	case <-ctx.Done():
		g.mu.Lock()
		delete(g.waiters, approvalID)
		delete(g.taskOf, approvalID)
		delete(g.pending, approvalID)
		delete(g.awaiting, taskID)
		g.mu.Unlock()
		g.persist()
		return Decision{Allow: false, Blocked: true, Message: "blocked: Auto Guard confirmation cancelled"}, ctx.Err()
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
	if st == nil || st.failure == nil {
		return
	}
	if appendDiagnosisNote(st.failure, note) {
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
	return "Auto Guard is active after a tool/verification failure. " +
		"Diagnose with read-only tools first (read logs, search code, inspect the failing command output). " +
		"Before changing strategy, scope, or risk of the next write, explain the diagnosis and the proposed recovery step. " +
		"The host will block uncertain proposals with a reason and escalate repeated or high-risk changes automatically."
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
	g.schedulePersist(g.Snapshot(), false)
}

func (g *Gate) persistUnlocked() {
	// Caller holds g.mu.
	if g == nil || g.opts.Persist == nil {
		return
	}
	g.schedulePersist(g.snapshotLocked(), true)
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
	st := g.tasks[taskID]
	if st == nil {
		st = &taskRuntime{}
		g.tasks[taskID] = st
	}
	if st.reviewRejects < 255 {
		st.reviewRejects++
	}
	blocks := int(st.reviewRejects)
	if st.failure != nil {
		note := "Auto Guard reviewer blocked the proposal: " + firstNonEmpty(verdict.Rationale, string(verdict.ChangeKind))
		appendDiagnosisNote(st.failure, note)
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

func reviewerStopMessage(verdict ReviewVerdict, attempts int) string {
	reason := firstNonEmpty(verdict.Rationale, "the proposed transition remains technically unresolved")
	return fmt.Sprintf(
		"blocked: Auto stopped after %d rejected plan or recovery proposals: %s. Stop retrying mutations and report the technical blocker to the user; only use the ask tool if a genuine user-owned choice exists.",
		attempts, reason,
	)
}

func repeatedFailureStopMessage(failures int) string {
	return fmt.Sprintf(
		"blocked: Auto stopped after %d consecutive execution failures. Do not ask the user to approve execution risk; report the blocker and evidence, or ask only if resolving it requires a genuine user-owned product or plan choice.",
		failures,
	)
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
