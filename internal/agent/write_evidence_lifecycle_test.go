package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func evidenceRound(t *testing.T, a *Agent, id, name string, args any) string {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	a.freezeVisibleReads(a.Session().Snapshot())
	calls := []provider.ToolCall{{ID: id, Name: name, Arguments: string(raw)}}
	a.sess.conversation.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: calls})
	b := a.executeBatch(context.Background(), &a.turn, calls)
	if len(b.results) != 1 {
		t.Fatalf("results: %v", b.results)
	}
	return b.results[0]
}

func TestUnversionedWindowCanRetireChangedTargetWithoutFullRead(t *testing.T) {
	w := evidenceWriter{target: tool.EvidenceTargetInfo{Path: "/w/a", Snapshot: "prior", Ranges: []tool.ReadRange{{Start: 10, End: 11}}, Hashes: hashesFor("old")}}
	a, ledger := newEvidenceAgent(t, w, true)
	if _, blocked := runEvidenceGate(a, "/w/a"); !blocked {
		t.Fatal("fixture missing block")
	}
	before := ledger.ObservationBoundary()
	ledger.RecordTextObservation(evidence.TextObservation{Path: "/w/a", StartLine: 11, LineHashes: hashesFor("new")})
	if len(a.outstandingReadEvidence(context.Background(), before)) != 1 {
		t.Fatal("same-batch changed window retired requirement")
	}
	if len(a.outstandingReadEvidence(context.Background(), ledger.ObservationBoundary())) != 0 {
		t.Fatal("changed target requires an unnecessary whole-file read")
	}
	if _, blocked := runEvidenceGate(a, "/w/a"); !blocked {
		t.Fatal("retirement granted a new write based on mismatching text")
	}
}

func TestNoopEditCannotClearAnotherRejectedOperation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("alpha\nbeta\n"), 0600); err != nil {
		t.Fatal(err)
	}
	writer, _ := tool.LookupBuiltin("edit_file")
	a := newIncompleteReadTestAgent(&scriptedProvider{}, incompleteReadBuiltin(t), NewSession("sys"), event.Discard, writer, undeclaredWriter{name: "bash"})
	evidenceRound(t, a, "blocked", "edit_file", map[string]any{"path": path, "old_string": "beta", "new_string": "changed"})
	evidenceRound(t, a, "noop", "edit_file", map[string]any{"path": path, "old_string": "alpha", "new_string": "alpha"})
	if out := evidenceRound(t, a, "bash", "bash", map[string]any{"command": "python write.py"}); !strings.Contains(out, "evidence required") {
		t.Fatalf("no-op cleared real requirement: %s", out)
	}
}

func TestEvidenceMemoAndRetirementAreOperationScoped(t *testing.T) {
	var s evidenceBlockState
	first := provider.ToolCall{ID: "reused", Name: "edit_file", Arguments: `{"path":"a","old_string":"before"}`}
	check := evidenceCheck{Path: "/w/a", Supported: true, Target: tool.EvidenceTargetInfo{Path: "/w/a", Snapshot: "old", Ranges: []tool.ReadRange{{Start: 0, End: 1}}, Hashes: []string{"old-hash"}}}
	s.record(check, first, 3)
	old := s.pending()[0]
	s.memoCheck(first, 3, check)
	changed := first
	changed.Arguments = `{"path":"a","old_string":"after"}`
	if _, ok := s.memoizedCheck(changed, 3); ok {
		t.Fatal("reused ID borrowed another operation's verdict")
	}
	if _, ok := s.memoizedCheck(first, 4); ok {
		t.Fatal("another boundary borrowed a frozen verdict")
	}
	check.Target.Snapshot = "new"
	check.Target.Hashes[0] = "new-hash"
	s.record(check, changed, 4)
	if got := old.Target.Hashes[0]; got != "old-hash" {
		t.Fatal("requirement aliases mutable target")
	}
	ready, done := make(chan struct{}), make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() { close(ready); <-done; s.retire(old) })
	<-ready
	if len(s.pending()) != 2 {
		t.Fatal("new requirement replaced old operation")
	}
	close(done)
	wg.Wait()
	remaining := s.pending()
	if len(remaining) != 1 || remaining[0].Target.Snapshot != "new" {
		t.Fatalf("late cleanup removed current requirement: %+v", remaining)
	}
	s.clearChecks()
	if _, ok := s.memoizedCheck(first, 3); ok {
		t.Fatal("batch memo survived cleanup")
	}
}

func TestEvidenceShellScopeDoesNotBlockIndependentTargets(t *testing.T) {
	w := evidenceWriter{target: tool.EvidenceTargetInfo{Path: "/w/a", Ranges: []tool.ReadRange{{Start: 0, End: 1}}, Hashes: hashesFor("alpha")}}
	a, _ := newEvidenceAgent(t, w, true)
	a.svc.tools.Add(undeclaredWriter{name: "bash"})
	if _, blocked := runEvidenceGate(a, "/w/a"); !blocked {
		t.Fatal("fixture missing block")
	}
	for _, tc := range []struct {
		command string
		blocked bool
	}{
		{`echo x > /w/a`, true}, {`echo x > /w/b`, false}, {`python write.py`, true},
		{`git --no-pager diff --stat`, false}, {`git --no-pager status`, false}, {`git --no-pager log`, false},
		{`git commit -m done`, false}, {`git commit -a -m done`, true},
		{`git --no-pager diff --output=/w/a`, true},
	} {
		args, _ := json.Marshal(map[string]string{"command": tc.command})
		_, blocked := a.applyEvidenceGates(context.Background(), &toolCallPlan{call: provider.ToolCall{Name: "bash", Arguments: string(args)}})
		if blocked != tc.blocked {
			t.Errorf("%s blocked=%v", tc.command, blocked)
		}
	}
}

func TestWriteEvidenceRepeatedRepairDoesNotLeaveHistoricalBlocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "规则.txt")
	if err := os.WriteFile(path, []byte("alpha\r\nbeta\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	writer, _ := tool.LookupBuiltin("edit_file")
	a := newIncompleteReadTestAgent(&scriptedProvider{}, incompleteReadBuiltin(t), NewSession("sys"), event.Discard, writer, undeclaredWriter{name: "bash"})
	for i := range 3 {
		old, next := "beta", fmt.Sprintf("beta-%d", i)
		if i > 0 {
			old = fmt.Sprintf("beta-%d", i-1)
		}
		edit := map[string]any{"path": path, "old_string": old, "new_string": next}
		prefix := fmt.Sprintf("round-%d", i)
		if out := evidenceRound(t, a, prefix+"-blocked", "edit_file", edit); !strings.Contains(out, "evidence required") {
			t.Fatalf("expected missing current evidence: %s", out)
		}
		evidenceRound(t, a, prefix+"-read", "read_file", map[string]any{"path": path, "intent": "full"})
		if out := evidenceRound(t, a, prefix+"-retry", "edit_file", edit); strings.Contains(out, "blocked:") || strings.Contains(out, "error:") {
			t.Fatalf("retry failed: %s", out)
		}
		if out := evidenceRound(t, a, prefix+"-bash", "bash", map[string]any{"command": "python unrelated.py"}); strings.Contains(out, "evidence required") {
			t.Fatalf("successful retry left historical block: %s", out)
		}
	}
}

func TestWriteEvidenceFullReadClearsFrozenRequirementAfterAnchorChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "changed.txt")
	if err := os.WriteFile(path, []byte("old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	writer, _ := tool.LookupBuiltin("edit_file")
	a := newIncompleteReadTestAgent(&scriptedProvider{}, incompleteReadBuiltin(t), NewSession("sys"), event.Discard, writer, undeclaredWriter{name: "bash"})
	edit := map[string]any{"path": path, "old_string": "old", "new_string": "new"}
	evidenceRound(t, a, "blocked", "edit_file", edit)
	if err := os.WriteFile(path, []byte("external\n"), 0600); err != nil {
		t.Fatal(err)
	}
	evidenceRound(t, a, "fresh", "read_file", map[string]any{"path": path, "intent": "full"})
	if out := evidenceRound(t, a, "bash", "bash", map[string]any{"command": "python unrelated.py"}); strings.Contains(out, "evidence required") {
		t.Fatalf("obsolete operation remained pending: %s", out)
	}
}
