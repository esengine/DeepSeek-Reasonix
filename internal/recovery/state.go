package recovery

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// taskRuntime is the single source of truth for one task inside the current
// Episode. Pending proposals, approval ids, and reply channels live only in the
// waiter table so they cannot be restored as durable authorizations after restart.
//
// Failure/reviewer budgets are runtime-only and scoped to EpisodeID. Task grants
// use TaskScope and intentionally survive Episode rotation.
type taskRuntime struct {
	episodeID string

	// operationFailures counts qualifying failures per exact fingerprint.
	operationFailures map[string]uint8
	// totalFailures counts all qualifying execution failures since the last
	// real progress inside this Episode. Fingerprint changes do not reset it.
	totalFailures uint8
	// reviewRejects is the cumulative reviewer rejection count for this Episode.
	// Different candidate proposals share the budget.
	reviewRejects uint8
	// stoppedOps records fingerprints that already hit the per-operation limit.
	stoppedOps map[string]struct{}
	// stoppedOpRetries counts re-proposals of already-stopped operations.
	stoppedOpRetries uint8

	// lastFailure is reviewer/diagnostic evidence for the most recent failure.
	// It does not itself act as a task-wide lock.
	lastFailure *activeFailure

	// episodeStopped is true when the Episode hard limit for this task was hit.
	episodeStopped bool
	stopReason     StopReason
	// finalizationOffered is true after the host injects the one-shot
	// summarize-only nudge for this Episode stop.
	finalizationOffered bool
	// finalizationConsumed is true after the model used its finalization round
	// but still attempted tools.
	finalizationConsumed bool

	guidanceSent bool
	// taskGrants are runtime-only semantic authorizations. Snapshot/Restore never
	// serializes them, so a restart or session switch always drops the grant.
	taskGrants     map[string]struct{}
	taskGrantScope string
}

// activeFailure is the latest failure evidence used by the reviewer and UI.
// Per-operation and Episode budgets live on taskRuntime, not here.
type activeFailure struct {
	evidence      FailureEvent
	safeRetryUsed bool
	diagnosis     []string
}

const (
	maxDiagnosisNotes      = 4
	maxDiagnosisNoteBytes  = 400
	maxDiagnosisTotalBytes = 1600 // 1.6 KiB hard cap across all notes
)

func (st *taskRuntime) empty() bool {
	if st == nil {
		return true
	}
	if st.lastFailure != nil || st.guidanceSent || st.reviewRejects > 0 {
		return false
	}
	if st.totalFailures > 0 || st.stoppedOpRetries > 0 || st.episodeStopped {
		return false
	}
	if len(st.operationFailures) > 0 || len(st.stoppedOps) > 0 {
		return false
	}
	return true
}

func (st *taskRuntime) hasTaskGrant(key string) bool {
	if st == nil || key == "" || st.taskGrants == nil {
		return false
	}
	_, ok := st.taskGrants[key]
	return ok
}

func (st *taskRuntime) addTaskGrant(key string) {
	if st == nil || key == "" {
		return
	}
	if st.taskGrants == nil {
		st.taskGrants = map[string]struct{}{}
	}
	st.taskGrants[key] = struct{}{}
}

func (st *taskRuntime) useTaskGrantScope(scope string) {
	if st == nil || scope == "" {
		return
	}
	if st.taskGrantScope != "" && st.taskGrantScope != scope {
		clear(st.taskGrants)
	}
	st.taskGrantScope = scope
}

func (st *taskRuntime) hasTaskGrants() bool {
	return st != nil && len(st.taskGrants) > 0
}

// clearNoProgressBudgets drops failure, reviewer, and stop counters after real
// progress or an Episode rotation. Task grants are intentionally preserved.
func (st *taskRuntime) clearNoProgressBudgets() {
	if st == nil {
		return
	}
	st.operationFailures = nil
	st.totalFailures = 0
	st.reviewRejects = 0
	st.stoppedOps = nil
	st.stoppedOpRetries = 0
	st.lastFailure = nil
	st.episodeStopped = false
	st.stopReason = StopReasonNone
	st.finalizationOffered = false
	st.finalizationConsumed = false
	st.guidanceSent = false
}

// clearFailure is retained as the semantic "progress cleared recovery state"
// helper used by success paths and revise.
func (st *taskRuntime) clearFailure() {
	st.clearNoProgressBudgets()
}

func (st *taskRuntime) ensureMaps() {
	if st == nil {
		return
	}
	if st.operationFailures == nil {
		st.operationFailures = map[string]uint8{}
	}
	if st.stoppedOps == nil {
		st.stoppedOps = map[string]struct{}{}
	}
}

func (st *taskRuntime) operationFailureCount(fp string) uint8 {
	if st == nil || fp == "" || st.operationFailures == nil {
		return 0
	}
	return st.operationFailures[fp]
}

func (st *taskRuntime) isOperationStopped(fp string) bool {
	if st == nil || fp == "" {
		return false
	}
	if st.stoppedOps != nil {
		if _, ok := st.stoppedOps[fp]; ok {
			return true
		}
	}
	return st.operationFailureCount(fp) >= MaxOperationFailures
}

func (st *taskRuntime) markOperationStopped(fp string) {
	if st == nil || fp == "" {
		return
	}
	st.ensureMaps()
	st.stoppedOps[fp] = struct{}{}
}

func (st *taskRuntime) failureCount() uint8 {
	// Compatibility view: expose the latest operation's count when present,
	// otherwise the Episode total. Prefer the active failure fingerprint.
	if st == nil {
		return 0
	}
	if st.lastFailure != nil {
		fp := strings.TrimSpace(st.lastFailure.evidence.Fingerprint)
		if fp != "" {
			if n := st.operationFailureCount(fp); n > 0 {
				return n
			}
		}
	}
	return st.totalFailures
}

func (st *taskRuntime) safeRetryAvailable() bool {
	if st == nil || st.lastFailure == nil {
		return false
	}
	return !st.lastFailure.safeRetryUsed
}

func (st *taskRuntime) diagnosisNotes() []string {
	if st == nil || st.lastFailure == nil {
		return nil
	}
	return append([]string(nil), st.lastFailure.diagnosis...)
}

func (st *taskRuntime) evidenceCopy() *FailureEvent {
	if st == nil || st.lastFailure == nil {
		return nil
	}
	return cloneFailureEvent(&st.lastFailure.evidence, st.lastFailure, st)
}

// cloneFailureEvent builds a wire FailureEvent with compatibility fields
// derived from the runtime truth.
func cloneFailureEvent(ev *FailureEvent, af *activeFailure, st *taskRuntime) *FailureEvent {
	if ev == nil {
		return nil
	}
	cp := *ev
	cp.Args = append(json.RawMessage(nil), ev.Args...)
	if af != nil {
		fp := strings.TrimSpace(ev.Fingerprint)
		if st != nil && fp != "" {
			cp.RepeatCount = int(st.operationFailureCount(fp))
		} else if st != nil {
			cp.RepeatCount = int(st.totalFailures)
		}
		if af.safeRetryUsed {
			cp.SafeRetryLeft = 0
		} else {
			cp.SafeRetryLeft = 1
		}
		cp.DiagnosisNotes = append([]string(nil), af.diagnosis...)
	} else {
		cp.DiagnosisNotes = append([]string(nil), ev.DiagnosisNotes...)
	}
	return &cp
}

// toTaskState projects live runtime truth for debugging / Snapshot().
func (st *taskRuntime) toTaskState(phase Phase) *TaskState {
	if st == nil || st.empty() {
		return nil
	}
	out := &TaskState{
		Phase:        phase,
		ReviewBlocks: int(st.reviewRejects),
		TailInjected: st.guidanceSent,
		EpisodeID:    st.episodeID,
		StopReason:   string(st.stopReason),
	}
	if st.episodeStopped {
		out.EpisodeStopped = true
	}
	if st.lastFailure != nil {
		out.Failure = cloneFailureEvent(&st.lastFailure.evidence, st.lastFailure, st)
		out.LastFailure = cloneFailureEvent(&st.lastFailure.evidence, st.lastFailure, st)
		out.ConsecutiveFails = int(st.failureCount())
		if out.Phase == PhaseIdle {
			out.Phase = PhaseDiagnosing
		}
	}
	// Pending and ApprovalID are intentionally never written: restore must not
	// revive a transient authorization or waiter across restarts.
	return out
}

// toPersistenceState projects only historical evidence. Active locks, Episode
// counters, generation, waiters, and task grants never land on disk.
func (st *taskRuntime) toPersistenceState() *TaskState {
	if st == nil || st.lastFailure == nil {
		return nil
	}
	// Evidence-only: no consecutive_fails / review_blocks as re-armable locks.
	return &TaskState{
		Phase:       PhaseIdle,
		LastFailure: cloneFailureEvent(&st.lastFailure.evidence, st.lastFailure, st),
	}
}

// taskRuntimeFromState migrates old or new snapshots into historical evidence
// only. Counters are never re-armed so a restart cannot re-block the user.
func taskRuntimeFromState(st *TaskState) *taskRuntime {
	if st == nil {
		return nil
	}
	src := st.LastFailure
	if src == nil {
		src = st.Failure
	}
	if src == nil {
		return nil
	}
	af := &activeFailure{
		evidence: FailureEvent{
			Tool:          src.Tool,
			ArgsSummary:   src.ArgsSummary,
			Subject:       src.Subject,
			ErrSummary:    src.ErrSummary,
			OutputExcerpt: src.OutputExcerpt,
			SourceAgent:   src.SourceAgent,
			TaskID:        src.TaskID,
			TaskScopeID:   src.TaskScopeID,
			ReadOnly:      src.ReadOnly,
			Verification:  src.Verification,
			Mutates:       src.Mutates,
			CreatedAt:     src.CreatedAt,
			Args:          append(json.RawMessage(nil), src.Args...),
			Fingerprint:   src.Fingerprint,
		},
		// Historical evidence only — fail closed for automatic safe retry after
		// restore so a restart cannot grant a free second attempt.
		safeRetryUsed: true,
		diagnosis:     append([]string(nil), src.DiagnosisNotes...),
	}
	trimDiagnosis(af)
	// Do not restore consecutive_fails / review_blocks as live locks.
	return &taskRuntime{
		lastFailure: af,
	}
}

func clampU8(v int) uint8 {
	if v <= 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func trimDiagnosis(af *activeFailure) {
	if af == nil {
		return
	}
	notes := af.diagnosis
	if len(notes) > maxDiagnosisNotes {
		notes = notes[len(notes)-maxDiagnosisNotes:]
	}
	total := 0
	kept := make([]string, 0, len(notes))
	// Keep the newest notes within the total budget.
	for i := len(notes) - 1; i >= 0; i-- {
		n := clipDiagnosisNote(notes[i])
		if n == "" {
			continue
		}
		if total+len(n) > maxDiagnosisTotalBytes {
			if len(kept) == 0 {
				n = clipBytes(n, maxDiagnosisTotalBytes)
				if n != "" {
					kept = append(kept, n)
				}
			}
			break
		}
		kept = append(kept, n)
		total += len(n)
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	af.diagnosis = kept
	af.evidence.DiagnosisNotes = append([]string(nil), kept...)
}

func appendDiagnosisNote(af *activeFailure, note string) bool {
	if af == nil {
		return false
	}
	note = clipDiagnosisNote(note)
	if note == "" {
		return false
	}
	for _, existing := range af.diagnosis {
		if existing == note {
			return false
		}
	}
	af.diagnosis = append(af.diagnosis, note)
	trimDiagnosis(af)
	return true
}

func clipDiagnosisNote(note string) string {
	return clipBytes(strings.TrimSpace(note), maxDiagnosisNoteBytes)
}

func clipBytes(s string, n int) string {
	s = strings.TrimSpace(s)
	if n <= 0 || len(s) <= n {
		return s
	}
	const ellipsis = "…"
	cut := n - len(ellipsis)
	if cut <= 0 {
		return ellipsis
	}
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + ellipsis
}

func normalizeTaskID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "root"
	}
	return id
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if n <= 0 || len(s) <= n {
		return s
	}
	return clipBytes(s, n)
}
