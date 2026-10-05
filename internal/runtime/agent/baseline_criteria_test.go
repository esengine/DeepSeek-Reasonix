package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"

	"reasonix/internal/base/diff"
	"reasonix/internal/base/testenv"
	"reasonix/internal/safety/evidence"
)

const (
	criterionBefore = "package p\n\nimport \"testing\"\n\nfunc TestFoo(t *testing.T) { assertRealInvariant(t) }\n"
	criterionAfter  = "package p\n\nimport \"testing\"\n\nfunc TestFoo(t *testing.T) { _ = true }\n"
)

func agentWithCriteriaStore(t *testing.T) (*Agent, string) {
	t.Helper()
	archive := testenv.TempDir(t)
	a := &Agent{}
	a.archiveDir = archive
	return a, archive
}

// The bytes are taken while the host still has them. Once the edit lands, the
// workspace holds only what replaced them — which is why nothing may go back
// and read the criterion from there.
func TestCapturingACriterionKeepsWhatItSaidBeforeTheRewrite(t *testing.T) {
	a, archive := agentWithCriteriaStore(t)
	change := diff.Change{Path: "cache_test.go", OldText: criterionBefore, NewText: criterionAfter}
	rewritten := evidence.RewrittenTestCriteria(change.Path, change.OldText, change.NewText)
	if len(rewritten) == 0 {
		t.Fatal("the fixture rewrites a test and the host did not read it as one")
	}

	a.captureRewrittenCriteria(change, rewritten)
	held, ok := a.task.baselineCriteria[change.Path]
	if !ok {
		t.Fatal("nothing was captured for a rewritten criterion")
	}
	content, err := a.baselineCriteriaStore().Open(held)
	if err != nil || string(content) != criterionBefore {
		t.Fatalf("Open = %q, %v; want what the criterion said before", content, err)
	}
	if held.Digest != evidence.DigestOf([]byte(criterionBefore)) {
		t.Fatalf("digest = %q, want the pre-image's", held.Digest)
	}
	// The store lives outside the workspace, under host state.
	if filepath.Dir(archive) == archive {
		t.Fatal("fixture: the archive root is not a directory of its own")
	}
	if _, err := os.Stat(baselineCriteriaRoot(archive)); err != nil {
		t.Fatalf("stat store root: %v", err)
	}
}

// A later edit rewrites what an earlier one already replaced. The task is held
// to what it began with, so the first capture is the one that stands.
func TestTheFirstCaptureOfAPathIsTheOneThatStands(t *testing.T) {
	a, _ := agentWithCriteriaStore(t)
	first := diff.Change{Path: "cache_test.go", OldText: criterionBefore, NewText: criterionAfter}
	a.captureRewrittenCriteria(first, evidence.RewrittenTestCriteria(first.Path, first.OldText, first.NewText))

	second := diff.Change{Path: "cache_test.go", OldText: criterionAfter, NewText: "package p\n"}
	a.captureRewrittenCriteria(second, evidence.RewrittenTestCriteria(second.Path, second.OldText, second.NewText))

	held := a.task.baselineCriteria["cache_test.go"]
	if held.Digest != evidence.DigestOf([]byte(criterionBefore)) {
		t.Fatalf("digest = %q, want the criterion the task began under", held.Digest)
	}
}

// An edit that touches no criterion captures nothing: the store holds criteria,
// not every file a turn wrote.
func TestAnEditThatRewritesNoCriterionCapturesNothing(t *testing.T) {
	a, _ := agentWithCriteriaStore(t)
	change := diff.Change{Path: "cache.go", OldText: "package p\n", NewText: "package p\n\nvar x = 1\n"}
	a.captureRewrittenCriteria(change, evidence.RewrittenTestCriteria(change.Path, change.OldText, change.NewText))
	if len(a.task.baselineCriteria) != 0 {
		t.Fatalf("captured %v, want nothing", a.task.baselineCriteria)
	}
}

// A shell command can overwrite a test with no preview and no old text, so the
// capture cannot wait for one. Everything a call of unreadable scope could
// reach is held before it runs.
func TestABroadScopeCallHoldsCriteriaItHasNoPreviewFor(t *testing.T) {
	a, _ := agentWithCriteriaStore(t)
	a.writeWorkspaceRoot = testenv.TempDir(t)
	criterion := filepath.Join(a.writeWorkspaceRoot, "cache_test.go")
	if err := os.WriteFile(criterion, []byte(criterionBefore), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.writeWorkspaceRoot, "cache.go"), []byte("package p\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// `python -c` names no path and previews nothing.
	plan := &toolCallPlan{evidenceName: "bash", evidenceArgs: []byte(`{"command":"python3 -c 'open(\"cache_test.go\",\"w\").write(\"\")'"}`)}
	if err := a.captureCriteriaBefore(t.Context(), plan); err != nil {
		t.Fatalf("captureCriteriaBefore: %v", err)
	}

	held, ok := a.task.baselineCriteria[criterion]
	if !ok {
		t.Fatal("a call that could overwrite a criterion ran without the host holding it")
	}
	content, err := a.baselineCriteriaStore().Open(held)
	if err != nil || string(content) != criterionBefore {
		t.Fatalf("Open = %q, %v; want the criterion as it stood before", content, err)
	}
	// Only criteria are held; the rest of the tree is not the host's to keep.
	if _, kept := a.task.baselineCriteria[filepath.Join(a.writeWorkspaceRoot, "cache.go")]; kept {
		t.Fatal("a file carrying no criteria was captured")
	}
}

// A writer names what it will touch, so it pays only for that.
func TestAPathAwareWriterHoldsOnlyWhatItNames(t *testing.T) {
	a, _ := agentWithCriteriaStore(t)
	a.writeWorkspaceRoot = testenv.TempDir(t)
	named := filepath.Join(a.writeWorkspaceRoot, "cache_test.go")
	other := filepath.Join(a.writeWorkspaceRoot, "other_test.go")
	for _, p := range []string{named, other} {
		if err := os.WriteFile(p, []byte(criterionBefore), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	args := []byte(`{"path":` + quoteJSON(named) + `,"content":"package p\n"}`)
	plan := &toolCallPlan{evidenceName: "write_file", evidenceArgs: args}
	if err := a.captureCriteriaBefore(t.Context(), plan); err != nil {
		t.Fatalf("captureCriteriaBefore: %v", err)
	}
	if _, ok := a.task.baselineCriteria[named]; !ok {
		t.Fatal("the named criterion was not held")
	}
	if _, ok := a.task.baselineCriteria[other]; ok {
		t.Fatal("a criterion the call never named was captured")
	}
}

// Under a session that promised to keep the criteria, a criterion that would be
// destroyed and cannot be kept stops the call. The loss is the one kind nothing
// afterwards can settle, so it belongs on the hard side of the gate.
func TestDeliveryRefusesToOverwriteACriterionItCannotKeep(t *testing.T) {
	a := &Agent{deliveryProfile: true}
	a.writeWorkspaceRoot = testenv.TempDir(t)
	// No archive root: there is nowhere durable to put it.
	criterion := filepath.Join(a.writeWorkspaceRoot, "cache_test.go")
	if err := os.WriteFile(criterion, []byte(criterionBefore), 0o600); err != nil {
		t.Fatal(err)
	}
	plan := &toolCallPlan{evidenceName: "bash", evidenceArgs: []byte(`{"command":"python3 -c 'pass'"}`)}
	if err := a.captureCriteriaBefore(t.Context(), plan); err == nil {
		t.Fatal("a criterion was about to be destroyed with nowhere to keep it, and the call was allowed")
	}
}

// A criterion already kept is safe whatever the store can do now, so a backend
// that has gone unwritable does not re-block a change to something held.
func TestAnAlreadyHeldCriterionIsNotRecaptured(t *testing.T) {
	a, _ := agentWithCriteriaStore(t)
	a.deliveryProfile = true
	a.writeWorkspaceRoot = testenv.TempDir(t)
	criterion := filepath.Join(a.writeWorkspaceRoot, "cache_test.go")
	if err := os.WriteFile(criterion, []byte(criterionBefore), 0o600); err != nil {
		t.Fatal(err)
	}
	plan := &toolCallPlan{evidenceName: "bash", evidenceArgs: []byte(`{"command":"python3 -c 'pass'"}`)}
	if err := a.captureCriteriaBefore(t.Context(), plan); err != nil {
		t.Fatalf("first capture: %v", err)
	}
	held := a.task.baselineCriteria[criterion]

	// The store goes away; the criterion is still kept, so the next rewrite runs.
	a.archiveDir = ""
	if err := a.captureCriteriaBefore(t.Context(), plan); err != nil {
		t.Fatalf("a held criterion was re-blocked when the store went unwritable: %v", err)
	}
	if a.task.baselineCriteria[criterion] != held {
		t.Fatal("the held criterion changed under a later call")
	}
}

// A session that never promised baseline provenance does not block for it — and
// does not pretend to have kept anything either. Capability is a session-level
// choice; a capture failure is an execution-time error under a promise.
func TestASessionThatPromisedNothingIsNotBlockedAndClaimsNothing(t *testing.T) {
	a := &Agent{}
	a.writeWorkspaceRoot = testenv.TempDir(t)
	criterion := filepath.Join(a.writeWorkspaceRoot, "cache_test.go")
	if err := os.WriteFile(criterion, []byte(criterionBefore), 0o600); err != nil {
		t.Fatal(err)
	}
	plan := &toolCallPlan{evidenceName: "bash", evidenceArgs: []byte(`{"command":"python3 -c 'pass'"}`)}
	if err := a.captureCriteriaBefore(t.Context(), plan); err != nil {
		t.Fatalf("captureCriteriaBefore = %v, want no refusal where nothing was promised", err)
	}
	if a.guaranteesBaselineProvenance() {
		t.Fatal("a session with no delivery profile claims to keep baseline criteria")
	}
	if len(a.task.baselineCriteria) != 0 {
		t.Fatalf("held %v, want nothing claimed", a.task.baselineCriteria)
	}
}

func quoteJSON(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// A subtree the host cannot enter used to end the walk, and a walk that ends in
// an error refused the call. One `D:\System Volume Information` under a
// workspace at a drive root is what that cost: every write blocked, for the
// rest of the session, over a directory nobody can open and nothing can write.
func TestASubtreeTheWalkCannotEnterDoesNotRefuseTheCall(t *testing.T) {
	a, _ := agentWithCriteriaStore(t)
	a.deliveryProfile = true
	missing := filepath.Join(testenv.TempDir(t), "no-such-tree")
	if err := a.captureCriteriaUnder(t.Context(), a.baselineCriteriaStore(), missing); err != nil {
		t.Fatalf("captureCriteriaUnder = %v; a root the walk cannot enter is a gap, not a refusal", err)
	}
}

// The name settles what could hold a criterion, so the bytes are never the
// question. Asking them instead read every file under the workspace, on every
// broad-scope call of the task — the whole drive, where the workspace was one.
func TestABroadScopeCaptureDoesNotReadWhatCannotHoldACriterion(t *testing.T) {
	root := testenv.TempDir(t)
	const bulk = 256 << 20
	blob, err := os.Create(filepath.Join(root, "payload.bin"))
	if err != nil {
		t.Fatal(err)
	}
	// Sparse where the filesystem allows it: the test is about what the host
	// reads, not about what it costs to lay the fixture down.
	if err := blob.Truncate(bulk); err != nil {
		blob.Close()
		t.Skipf("cannot size the fixture: %v", err)
	}
	blob.Close()
	if err := os.WriteFile(filepath.Join(root, "unit_test.go"), []byte(criterionBefore), 0o644); err != nil {
		t.Fatal(err)
	}

	a, _ := agentWithCriteriaStore(t)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if err := a.captureCriteriaUnder(t.Context(), a.baselineCriteriaStore(), root); err != nil {
		t.Fatalf("captureCriteriaUnder: %v", err)
	}
	runtime.ReadMemStats(&after)

	if _, held := a.task.baselineCriteria[filepath.Join(root, "unit_test.go")]; !held {
		t.Fatal("the criterion beside the payload was not held")
	}
	if grew := after.TotalAlloc - before.TotalAlloc; grew > bulk/4 {
		t.Fatalf("the capture allocated %d bytes walking past a %d-byte non-criterion; it read the file", grew, bulk)
	}
}

// Which criteria exist is a fact about the workspace, not about the call. A
// second broad-scope call at the same state used to re-derive it — on a 650k
// file root that was 6.5 seconds, per call, for an answer nothing had moved.
func TestASecondBroadScopeCallAtTheSameStateDoesNotWalkAgain(t *testing.T) {
	root := testenv.TempDir(t)
	if err := os.WriteFile(filepath.Join(root, "unit_test.go"), []byte(criterionBefore), 0o644); err != nil {
		t.Fatal(err)
	}
	a, _ := agentWithCriteriaStore(t)
	a.writeWorkspaceRoot = root
	a.task = taskRuntime{ledger: evidence.NewLedger(), criteriaEpoch: new(atomic.Uint64)}
	if err := a.captureCriteriaBefore(t.Context(), broadScopePlan()); err != nil {
		t.Fatalf("first capture: %v", err)
	}
	held := len(a.task.baselineCriteria)
	if held == 0 {
		t.Fatal("the first call captured nothing to hold")
	}

	// A criterion appearing with no recorded mutation is the unobserved external
	// writer already declared a gap. It is what makes the skip observable here.
	if err := os.WriteFile(filepath.Join(root, "late_test.go"), []byte(criterionBefore), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.captureCriteriaBefore(t.Context(), broadScopePlan()); err != nil {
		t.Fatalf("second capture: %v", err)
	}
	if len(a.task.baselineCriteria) != held {
		t.Fatalf("the capture walked again at an unmoved epoch: held %d then %d", held, len(a.task.baselineCriteria))
	}
}

// ...and a state that did move is walked again, or the capture would answer for
// a workspace it never saw.
func TestAMovedStateIsCapturedAgain(t *testing.T) {
	root := testenv.TempDir(t)
	if err := os.WriteFile(filepath.Join(root, "unit_test.go"), []byte(criterionBefore), 0o644); err != nil {
		t.Fatal(err)
	}
	a, _ := agentWithCriteriaStore(t)
	a.writeWorkspaceRoot = root
	a.task = taskRuntime{ledger: evidence.NewLedger(), criteriaEpoch: new(atomic.Uint64)}
	if err := a.captureCriteriaBefore(t.Context(), broadScopePlan()); err != nil {
		t.Fatal(err)
	}
	before := len(a.task.baselineCriteria)

	if err := os.WriteFile(filepath.Join(root, "late_test.go"), []byte(criterionBefore), 0o644); err != nil {
		t.Fatal(err)
	}
	a.task.ledger.Record(evidence.Receipt{ToolName: "bash", Success: true, Mutation: true})
	if err := a.captureCriteriaBefore(t.Context(), broadScopePlan()); err != nil {
		t.Fatal(err)
	}
	if len(a.task.baselineCriteria) <= before {
		t.Fatalf("a recorded mutation left the capture stale: held %d, still %d", before, len(a.task.baselineCriteria))
	}
}

// A bash command naming no path: the shape whose scope the host cannot read,
// which is what sends a call down the broad-scope capture.
func broadScopePlan() *toolCallPlan {
	return &toolCallPlan{evidenceName: "bash", evidenceArgs: []byte(`{"command":"python3 -c 'pass'"}`)}
}
