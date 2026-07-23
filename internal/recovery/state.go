package recovery

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// taskRuntime is the single source of truth for one task.
// Pending proposals, approval ids, and reply channels live only in the waiter
// table so they cannot be restored as durable authorizations after restart.
type taskRuntime struct {
	failure       *activeFailure
	reviewRejects uint8
	guidanceSent  bool
	// taskGrants are runtime-only semantic authorizations. Snapshot/Restore never
	// serializes them, so a restart or session switch always drops the grant.
	taskGrants     map[string]struct{}
	taskGrantScope string
}

// activeFailure is the armed failure-recovery context for one task.
type activeFailure struct {
	evidence      FailureEvent
	failureCount  uint8
	safeRetryUsed bool
	diagnosis     []string
}

const (
	maxDiagnosisNotes      = 4
	maxDiagnosisNoteBytes  = 400
	maxDiagnosisTotalBytes = 1600 // 1.6 KiB hard cap across all notes
)

func (st *taskRuntime) empty() bool {
	return st == nil || (st.failure == nil && !st.guidanceSent && st.reviewRejects == 0)
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

func (st *taskRuntime) clearFailure() {
	if st == nil {
		return
	}
	st.failure = nil
	st.reviewRejects = 0
	st.guidanceSent = false
}

func (st *taskRuntime) failureCount() uint8 {
	if st == nil || st.failure == nil {
		return 0
	}
	return st.failure.failureCount
}

func (st *taskRuntime) safeRetryAvailable() bool {
	if st == nil || st.failure == nil {
		return false
	}
	return !st.failure.safeRetryUsed
}

func (st *taskRuntime) diagnosisNotes() []string {
	if st == nil || st.failure == nil {
		return nil
	}
	return append([]string(nil), st.failure.diagnosis...)
}

func (st *taskRuntime) evidenceCopy() *FailureEvent {
	if st == nil || st.failure == nil {
		return nil
	}
	return cloneFailureEvent(&st.failure.evidence, st.failure)
}

// cloneFailureEvent builds a wire FailureEvent with compatibility fields
// derived from the active failure runtime truth.
func cloneFailureEvent(ev *FailureEvent, af *activeFailure) *FailureEvent {
	if ev == nil {
		return nil
	}
	cp := *ev
	cp.Args = append(json.RawMessage(nil), ev.Args...)
	if af != nil {
		cp.RepeatCount = int(af.failureCount)
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

func (st *taskRuntime) toTaskState(phase Phase) *TaskState {
	if st == nil || st.empty() {
		return nil
	}
	out := &TaskState{
		Phase:        phase,
		ReviewBlocks: int(st.reviewRejects),
		TailInjected: st.guidanceSent,
	}
	if st.failure != nil {
		out.Failure = cloneFailureEvent(&st.failure.evidence, st.failure)
		out.ConsecutiveFails = int(st.failure.failureCount)
		if out.Phase == PhaseIdle {
			out.Phase = PhaseDiagnosing
		}
	}
	// Pending and ApprovalID are intentionally never written: restore must not
	// revive a transient authorization or waiter across restarts.
	return out
}

func taskRuntimeFromState(st *TaskState) *taskRuntime {
	if st == nil || st.Failure == nil {
		return nil
	}
	af := &activeFailure{
		evidence: FailureEvent{
			Tool:          st.Failure.Tool,
			ArgsSummary:   st.Failure.ArgsSummary,
			Subject:       st.Failure.Subject,
			ErrSummary:    st.Failure.ErrSummary,
			OutputExcerpt: st.Failure.OutputExcerpt,
			SourceAgent:   st.Failure.SourceAgent,
			TaskID:        st.Failure.TaskID,
			ReadOnly:      st.Failure.ReadOnly,
			Verification:  st.Failure.Verification,
			Mutates:       st.Failure.Mutates,
			CreatedAt:     st.Failure.CreatedAt,
			Args:          append(json.RawMessage(nil), st.Failure.Args...),
			Fingerprint:   st.Failure.Fingerprint,
		},
		failureCount: 1,
		diagnosis:    append([]string(nil), st.Failure.DiagnosisNotes...),
	}
	switch {
	case st.ConsecutiveFails > 0:
		af.failureCount = clampU8(st.ConsecutiveFails)
	case st.Failure.RepeatCount > 0:
		af.failureCount = clampU8(st.Failure.RepeatCount)
	}
	// SafeRetryLeft > 0 means budget remains. Zero/missing means spent:
	// old armed snapshots always wrote 1, and fail-closed after restart is
	// safer than granting a second automatic retry.
	af.safeRetryUsed = st.Failure.SafeRetryLeft <= 0

	trimDiagnosis(af)
	return &taskRuntime{
		failure:       af,
		reviewRejects: clampU8(st.ReviewBlocks),
		guidanceSent:  st.TailInjected,
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
