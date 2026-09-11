package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"sync"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// A rejected operation keeps its frozen target, never an instruction to replay
// old edit arguments. After a successful edit those arguments may no longer
// match anything. Requirements are run-local and are not restored from history.
type writeEvidenceRequirement struct {
	Key         string
	OperationID string
	Boundary    uint64
	Target      tool.EvidenceTargetInfo
}

type evidenceMemoKey struct {
	ID, Name, Arguments string
	Boundary            uint64
}

type evidenceBlockState struct {
	mu           sync.Mutex
	requirements map[string]writeEvidenceRequirement
	checks       map[evidenceMemoKey]evidenceCheck
}

func cloneEvidenceTarget(t tool.EvidenceTargetInfo) tool.EvidenceTargetInfo {
	t.Ranges = slices.Clone(t.Ranges)
	t.Hashes = slices.Clone(t.Hashes)
	return t
}

func memoKey(c provider.ToolCall, boundary uint64) evidenceMemoKey {
	return evidenceMemoKey{c.ID, c.Name, c.Arguments, boundary}
}

func (s *evidenceBlockState) memoCheck(c provider.ToolCall, boundary uint64, check evidenceCheck) {
	if c.ID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.checks == nil {
		s.checks = map[evidenceMemoKey]evidenceCheck{}
	}
	check.Target = cloneEvidenceTarget(check.Target)
	check.Missing = slices.Clone(check.Missing)
	s.checks[memoKey(c, boundary)] = check
}

func (s *evidenceBlockState) memoizedCheck(c provider.ToolCall, boundary uint64) (evidenceCheck, bool) {
	if c.ID == "" {
		return evidenceCheck{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cached, ok := s.checks[memoKey(c, boundary)]
	cached.Target = cloneEvidenceTarget(cached.Target)
	cached.Missing = slices.Clone(cached.Missing)
	return cached, ok
}

func (s *evidenceBlockState) clearChecks() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checks = nil
}

// Only an already-approved, single-file writer on a disjoint target can pass
// a preflight evidence failure. Unknown scopes, hooks and execution failures
// retain the normal dependency barrier.
func (a *Agent) independentEvidenceWriter(call provider.ToolCall) bool {
	if toolHooksMayMutateWorkspace(a.svc.hooks) {
		return false
	}
	switch call.Name {
	case "edit_file", "multi_edit", "write_file", "delete_symbol":
	default:
		return false
	}
	s := &a.turn.evidenceBlocked
	target := s.approvedTarget(call)
	if target == "" {
		return false
	}
	for _, r := range s.pending() {
		if evidencePathsOverlap(target, r.Target.Path) {
			return false
		}
	}
	return true
}

func (s *evidenceBlockState) approvedTarget(call provider.ToolCall) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, check := range s.checks {
		if key.ID != call.ID || key.Name != call.Name || key.Arguments != call.Arguments || !check.Satisfied || check.Target.Path == "" {
			continue
		}
		return check.Target.Path
	}
	return ""
}

func (s *evidenceBlockState) record(check evidenceCheck, call provider.ToolCall, boundary uint64) {
	if check.Path == "" || check.Target.Path == "" {
		return
	}
	target := cloneEvidenceTarget(check.Target)
	target.Path = filepath.Clean(check.Path)
	identity, _ := json.Marshal(struct {
		Call     provider.ToolCall
		Target   tool.EvidenceTargetInfo
		Boundary uint64
	}{call, target, boundary})
	key := hashLine(string(identity))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.requirements == nil {
		s.requirements = map[string]writeEvidenceRequirement{}
	}
	// The stable operation identity, not the provider's per-round call ID: a
	// requirement retired by a changed source must reopen recovery for the same
	// operation the model will resubmit under a new call ID.
	operationID := evidence.OperationID(call.Name, json.RawMessage(call.Arguments))
	s.requirements[key] = writeEvidenceRequirement{key, operationID, boundary, target}
}

func (s *evidenceBlockState) pending() []writeEvidenceRequirement {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]writeEvidenceRequirement, 0, len(s.requirements))
	for _, r := range s.requirements {
		r.Target = cloneEvidenceTarget(r.Target)
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b writeEvidenceRequirement) int {
		if a.Key < b.Key {
			return -1
		}
		if a.Key > b.Key {
			return 1
		}
		return 0
	})
	return out
}

func (s *evidenceBlockState) retire(r writeEvidenceRequirement) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.requirements, r.Key)
	// Preflight verdicts live until the batch ends. Removing one here would
	// allow a same-batch read to change another call's frozen verdict.
}

func (s *evidenceBlockState) snapshot() []string {
	var out []string
	for _, r := range s.pending() {
		out = append(out, r.Target.Path)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// outstandingReadEvidence reconciles frozen rejected operations against the
// model-visible ledger. Read coverage itself never blocks an opaque writer.
// No filesystem probe or replay of the old mutation is needed here.
func (a *Agent) outstandingReadEvidence(_ context.Context, boundary uint64) []string {
	s := &a.turn.evidenceBlocked
	for _, r := range s.pending() {
		if r.Boundary > boundary {
			continue
		}
		observations := a.eligibleObservations(r.Target.Path, boundary)
		for _, o := range observations {
			if o.Sequence <= r.Boundary {
				continue
			}
			if o.Absent {
				a.retireEvidenceRequirement(s, r)
				break
			}
			// A fresh, versioned observation makes an operation on a superseded
			// source obsolete. A future edit still checks its own current target.
			if r.Target.Snapshot != "" && o.Snapshot != "" && o.Snapshot != r.Target.Snapshot {
				a.retireEvidenceRequirement(s, r)
				break
			}
			if observedTargetChanged(o, r.Target) {
				a.retireEvidenceRequirement(s, r)
				break
			}
		}
		if ok, _ := evidenceCoversTarget(observations, r.Target); ok {
			a.retireEvidenceRequirement(s, r)
		}
	}
	return s.snapshot()
}

// retireEvidenceRequirement drops a frozen rejection and opens a new recovery
// epoch for its operation. Fresh evidence or a changed source version is real
// new information, so the next attempt is a first attempt — not the second
// identical failure that would hand the operation to the user.
func (a *Agent) retireEvidenceRequirement(s *evidenceBlockState, r writeEvidenceRequirement) {
	s.retire(r)
	a.operations().NewEpoch(r.OperationID)
	event.RecordOperationAudit(a.svc.sink, evidence.OperationAudit{Metric: evidence.MetricReadSourceChanged, OperationID: r.OperationID})
}

// Large bounded reads need not scan the whole source to establish a version.
// A changed hash inside the frozen target proves that operation obsolete even
// without a whole-file snapshot. It never grants evidence for a future write.
func observedTargetChanged(o evidence.TextObservation, target tool.EvidenceTargetInfo) bool {
	index := 0
	for _, r := range target.Ranges {
		if r.Lines() < 0 || index+r.Lines() > len(target.Hashes) {
			return false
		}
		for line := max(r.Start, o.StartLine-1); line < min(r.End, o.StartLine-1+len(o.LineHashes)); line++ {
			if target.Hashes[index+line-r.Start] != o.LineHashes[line-(o.StartLine-1)] {
				return true
			}
		}
		index += r.Lines()
	}
	return false
}

// A successful checked mutation supersedes requirements on its original
// source. An unobserved no-op cannot be used to clear a rejected edit and then
// route around it with bash. This runs only in the ordered result finalizer.
func (a *Agent) retireWrittenSource(source tool.EvidenceTargetInfo) {
	s := &a.turn.evidenceBlocked
	if source.Path == "" || (!source.WholeFile && !source.Absent && !source.PreservesContent && len(source.Ranges) == 0) {
		return
	}
	for _, r := range s.pending() {
		if r.Target.Path != source.Path {
			continue
		}
		if (source.Snapshot != "" && r.Target.Snapshot == source.Snapshot) || (source.SourceTextDigest != "" && r.Target.SourceTextDigest == source.SourceTextDigest) {
			a.retireEvidenceRequirement(s, r)
		}
	}
}
