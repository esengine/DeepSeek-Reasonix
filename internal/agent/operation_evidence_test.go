package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/runtimepolicy"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// evidenceWriter is a stand-in writer whose declared target is fixed, so the
// tests exercise the host check rather than a built-in's own resolution.
type evidenceWriter struct {
	target     tool.EvidenceTargetInfo
	err        error
	executions *int
}

func (evidenceWriter) Name() string            { return "write_file" }
func (evidenceWriter) Description() string     { return "fake writer" }
func (evidenceWriter) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (evidenceWriter) ReadOnly() bool          { return false }
func (w evidenceWriter) Execute(context.Context, json.RawMessage) (string, error) {
	if w.executions != nil {
		*w.executions++
	}
	return "", nil
}

func (w evidenceWriter) DeclareEvidenceTarget(context.Context, json.RawMessage) (tool.EvidenceTargetInfo, error) {
	return w.target, w.err
}

type undeclaredWriter struct{ name string }

func (w undeclaredWriter) Name() string {
	if w.name != "" {
		return w.name
	}
	return "write_file"
}
func (undeclaredWriter) Description() string     { return "writer without a declaration" }
func (undeclaredWriter) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (undeclaredWriter) ReadOnly() bool          { return false }
func (undeclaredWriter) Execute(context.Context, json.RawMessage) (string, error) {
	return "", nil
}

func newEvidenceAgent(t *testing.T, writer tool.Tool, gates bool) (*Agent, *evidence.Ledger) {
	t.Helper()
	reg := tool.NewRegistry()
	reg.Add(writer)
	a := New(&userInputCaptureProvider{}, reg, NewSession("system"), Options{ReadPipeline: ReadPipelineOptions{LegacyEvidenceGates: !gates}}, event.Discard)
	ledger := evidence.NewLedger()
	a.task.ledger = ledger
	return a, ledger
}

func runEvidenceGate(a *Agent, path string) (toolOutcome, bool) {
	plan := &toolCallPlan{call: provider.ToolCall{Name: "write_file", Arguments: `{"path":"` + path + `"}`}}
	return a.applyEvidenceGates(context.Background(), plan)
}

func hashesFor(lines ...string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, hashLine(line))
	}
	return out
}

func TestEvidenceGateBlocksAnUnreadOverwrite(t *testing.T) {
	writer := evidenceWriter{target: tool.EvidenceTargetInfo{
		Path: "/w/a.go", WholeFile: true, Hashes: hashesFor("alpha", "beta"),
	}}
	a, _ := newEvidenceAgent(t, writer, true)

	out, blocked := runEvidenceGate(a, "/w/a.go")
	if !blocked || !out.blocked {
		t.Fatalf("an overwrite without evidence must be blocked: %+v (blocked=%v)", out, blocked)
	}
	if !strings.Contains(out.output, "evidence required") || !strings.Contains(out.output, "1-2") {
		t.Fatalf("block message must name the requirement and missing lines: %q", out.output)
	}
}

func TestEvidenceGateAllowsAfterTheModelSawTheContent(t *testing.T) {
	writer := evidenceWriter{target: tool.EvidenceTargetInfo{
		Path: "/w/a.go", WholeFile: true, Hashes: hashesFor("alpha", "beta"),
	}}
	a, ledger := newEvidenceAgent(t, writer, true)
	ledger.RecordTextObservation(evidence.TextObservation{
		Path: "/w/a.go", StartLine: 1, Snapshot: "ss2:1", LineHashes: hashesFor("alpha", "beta"),
	})

	if out, blocked := runEvidenceGate(a, "/w/a.go"); blocked {
		t.Fatalf("a full read must satisfy the requirement: %+v", out)
	}
}

func TestEvidenceGateRejectsStaleContent(t *testing.T) {
	writer := evidenceWriter{target: tool.EvidenceTargetInfo{
		Path: "/w/a.go", WholeFile: true, Hashes: hashesFor("alpha", "gamma"),
	}}
	a, ledger := newEvidenceAgent(t, writer, true)
	ledger.RecordTextObservation(evidence.TextObservation{
		Path: "/w/a.go", StartLine: 1, Snapshot: "ss2:1", LineHashes: hashesFor("alpha", "beta"),
	})

	if out, blocked := runEvidenceGate(a, "/w/a.go"); !blocked {
		t.Fatalf("changed content must not reuse the old observation: %+v", out)
	}
}

func TestEvidenceGateStitchesPagesOfOneSnapshot(t *testing.T) {
	writer := evidenceWriter{target: tool.EvidenceTargetInfo{
		Path: "/w/a.go", WholeFile: true, Hashes: hashesFor("a", "b", "c", "d"),
	}}
	a, ledger := newEvidenceAgent(t, writer, true)
	ledger.RecordTextObservation(evidence.TextObservation{Path: "/w/a.go", StartLine: 1, Snapshot: "ss2:1", LineHashes: hashesFor("a", "b")})
	ledger.RecordTextObservation(evidence.TextObservation{Path: "/w/a.go", StartLine: 3, Snapshot: "ss2:1", LineHashes: hashesFor("c", "d")})

	if out, blocked := runEvidenceGate(a, "/w/a.go"); blocked {
		t.Fatalf("two pages of one snapshot must prove the file: %+v", out)
	}
}

func TestEvidenceGateNeverStitchesAcrossSnapshots(t *testing.T) {
	writer := evidenceWriter{target: tool.EvidenceTargetInfo{
		Path: "/w/a.go", WholeFile: true, Hashes: hashesFor("a", "b", "c", "d"),
	}}
	a, ledger := newEvidenceAgent(t, writer, true)
	ledger.RecordTextObservation(evidence.TextObservation{Path: "/w/a.go", StartLine: 1, Snapshot: "ss2:1", LineHashes: hashesFor("a", "b")})
	ledger.RecordTextObservation(evidence.TextObservation{Path: "/w/a.go", StartLine: 3, Snapshot: "ss2:2", LineHashes: hashesFor("c", "d")})

	if out, blocked := runEvidenceGate(a, "/w/a.go"); !blocked {
		t.Fatalf("pages from different snapshots must not be combined: %+v", out)
	}
}

func TestEvidenceGateIgnoresSameBatchReads(t *testing.T) {
	writer := evidenceWriter{target: tool.EvidenceTargetInfo{
		Path: "/w/a.go", WholeFile: true, Hashes: hashesFor("alpha"),
	}}
	a, ledger := newEvidenceAgent(t, writer, true)
	boundary := ledger.ObservationBoundary()
	ledger.RecordTextObservation(evidence.TextObservation{
		Path: "/w/a.go", StartLine: 1, Snapshot: "ss2:1", LineHashes: hashesFor("alpha"),
	})
	ctx := withObservationBoundary(context.Background(), boundary)

	plan := &toolCallPlan{call: provider.ToolCall{Name: "write_file", Arguments: `{"path":"/w/a.go"}`}}
	if out, blocked := a.applyEvidenceGates(ctx, plan); !blocked {
		t.Fatalf("a read from the same batch must not count: %+v", out)
	}
}

func TestEvidenceGateAllowsCreatingANewFile(t *testing.T) {
	writer := evidenceWriter{target: tool.EvidenceTargetInfo{Path: "/w/new.go"}}
	a, _ := newEvidenceAgent(t, writer, true)
	if out, blocked := runEvidenceGate(a, "/w/new.go"); blocked {
		t.Fatalf("creating a new file needs no prior evidence: %+v", out)
	}
}

func TestEvidenceGateLeavesUndeclaredWritersAlone(t *testing.T) {
	a, _ := newEvidenceAgent(t, undeclaredWriter{}, true)
	if out, blocked := runEvidenceGate(a, "/w/a.go"); blocked {
		t.Fatalf("a writer without a declaration must keep the existing boundary: %+v", out)
	}
}

func TestEvidenceGateIsOffByDefault(t *testing.T) {
	writer := evidenceWriter{target: tool.EvidenceTargetInfo{
		Path: "/w/a.go", WholeFile: true, Hashes: hashesFor("alpha"),
	}}
	a, _ := newEvidenceAgent(t, writer, false)
	if out, blocked := runEvidenceGate(a, "/w/a.go"); blocked {
		t.Fatalf("the gate must stay off unless the run enabled it: %+v", out)
	}
}

func TestEvidenceGateLeavesInvalidTargetToNativeValidation(t *testing.T) {
	writer := evidenceWriter{err: errors.New("anchor not found")}
	a, _ := newEvidenceAgent(t, writer, true)
	out, blocked := runEvidenceGate(a, "/w/a.go")
	if blocked || len(a.turn.evidenceBlocked.snapshot()) != 0 {
		t.Fatalf("an invalid target must reach native validation without creating an empty-path obligation: %+v", out)
	}
}

// TestEvidenceGateBlocksUnknownScopeWriterAfterABlock pins the conservative
// rule: a writer that cannot declare its target never becomes the way around an
// outstanding evidence requirement.
func TestEvidenceGateBlocksUnknownScopeWriterAfterABlock(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(evidenceWriter{target: tool.EvidenceTargetInfo{
		Path: "/w/a.go", WholeFile: true, Hashes: hashesFor("alpha"),
	}})
	reg.Add(undeclaredWriter{name: "bash"})
	a := New(&userInputCaptureProvider{}, reg, NewSession("system"), Options{}, event.Discard)
	a.task.ledger = evidence.NewLedger()

	if out, blocked := runEvidenceGate(a, "/w/a.go"); !blocked {
		t.Fatalf("the declarer must be blocked first: %+v", out)
	}
	plan := &toolCallPlan{call: provider.ToolCall{Name: "bash", Arguments: `{"command":"echo x > /w/a.go"}`}}
	out, blocked := a.applyEvidenceGates(context.Background(), plan)
	if !blocked || !strings.Contains(out.output, "cannot declare which files it changes") {
		t.Fatalf("an unknown-scope writer must not route around the block: %+v (blocked=%v)", out, blocked)
	}
}

// TestEvidenceGateHonorsAnExplicitRebuildInstruction pins the one waiver: the
// user's own instruction names the file and asks for a full rewrite. The model
// cannot grant it to itself, and it never covers a file the instruction does
// not name.
func TestEvidenceGateHonorsAnExplicitRebuildInstruction(t *testing.T) {
	writer := evidenceWriter{target: tool.EvidenceTargetInfo{Path: "/w/notes.md", WholeFile: true}}
	a, _ := newEvidenceAgent(t, writer, true)
	a.turn.turnInput = "Please rewrite notes.md from scratch."
	a.writeWorkspaceRoot = "/w"
	a.turn.constraints = runtimepolicy.ParseConstraints(a.turn.turnInput)
	a.recordRebuildAuthorization()
	if out, blocked := runEvidenceGate(a, "/w/notes.md"); blocked {
		t.Fatalf("an explicit rebuild instruction must waive the read: %+v", out)
	}

	other := evidenceWriter{target: tool.EvidenceTargetInfo{Path: "/w/other.md", WholeFile: true}}
	b, _ := newEvidenceAgent(t, other, true)
	b.turn.turnInput = a.turn.turnInput
	b.writeWorkspaceRoot = "/w"
	b.turn.constraints = runtimepolicy.ParseConstraints(b.turn.turnInput)
	b.recordRebuildAuthorization()
	if out, blocked := runEvidenceGate(b, "/w/other.md"); !blocked {
		t.Fatalf("a file the instruction does not name must still require evidence: %+v", out)
	}

	c, _ := newEvidenceAgent(t, writer, true)
	if out, blocked := runEvidenceGate(c, "/w/notes.md"); !blocked {
		t.Fatalf("without a user instruction the requirement stands: %+v", out)
	}
}

// TestEvidencePreflightBlocksBeforeTheBatchRuns pins the batch rule: a writer
// whose evidence is missing is reported without ever starting.
func TestEvidencePreflightBlocksBeforeTheBatchRuns(t *testing.T) {
	executions := 0
	writer := evidenceWriter{
		target:     tool.EvidenceTargetInfo{Path: "/w/a.go", WholeFile: true, Hashes: hashesFor("alpha")},
		executions: &executions,
	}
	reg := tool.NewRegistry()
	reg.Add(writer)
	a := New(&userInputCaptureProvider{}, reg, NewSession("system"), Options{}, event.Discard)
	a.task.ledger = evidence.NewLedger()

	batch := a.executeBatch(context.Background(), &a.turn, []provider.ToolCall{{ID: "c1", Name: "write_file", Arguments: `{"path":"/w/a.go"}`}})
	if executions != 0 {
		t.Fatalf("a blocked call must not start: executions=%d", executions)
	}
	if len(batch.results) != 1 || !strings.Contains(batch.results[0], "evidence required") {
		t.Fatalf("batch results = %v, want a blocked evidence result", batch.results)
	}
}

// TestRebuildWaiverRequiresHostRecordedPaths proves a model-authored clause
// cannot authorize a blind overwrite: the AllowRebuild bit alone never waives
// the evidence gate, only a path the host recorded from the user's instruction.
func TestRebuildWaiverRequiresHostRecordedPaths(t *testing.T) {
	a := &Agent{}
	a.writeWorkspaceRoot = "/w"
	a.turn.turnInput = "rewrite the file completely: `secret.go`"
	a.turn.constraints = runtimepolicy.ParseConstraints(a.turn.turnInput)
	if !a.turn.constraints.AllowRebuild {
		t.Fatal("fixture must set AllowRebuild so the path set is what gates the waiver")
	}
	if a.rebuildAuthorized("/w/secret.go") {
		t.Fatal("a clause without a host-recorded path must not authorize a rebuild")
	}
	a.recordRebuildAuthorization()
	if !a.rebuildAuthorized("/w/secret.go") || a.rebuildAuthorized("/w/other.go") {
		t.Fatal("the host-recorded set must authorize exactly the named file")
	}
}

// TestBackgroundJobInheritsHostConstraints proves a background sub-agent keeps
// the spawning turn's host constraints instead of re-deriving them from the
// model-authored task prompt.
func TestBackgroundJobInheritsHostConstraints(t *testing.T) {
	if _, ok := runtimepolicy.FromContext(context.Background()); ok {
		t.Fatal("fixture must start from a context without host constraints")
	}
	parent := runtimepolicy.WithContext(context.Background(), runtimepolicy.Constraints{
		AllowRebuild: true,
		RebuildPaths: []string{"/w/a.go"},
	})
	got, ok := runtimepolicy.FromContext(withInheritedHostConstraints(parent, context.Background()))
	if !ok || !got.AllowRebuild || len(got.RebuildPaths) != 1 || got.RebuildPaths[0] != "/w/a.go" {
		t.Fatalf("background job lost host constraints: %+v ok=%v", got, ok)
	}
}

// TestRebuildAuthorizationRecordsFromHostConstraints pins the controller path:
// the host publishes constraints parsed from the user's text, so the turn must
// still record the authorized paths from its own instruction.
func TestRebuildAuthorizationRecordsFromHostConstraints(t *testing.T) {
	a, _ := newEvidenceAgent(t, evidenceWriter{target: tool.EvidenceTargetInfo{Path: "/w/notes.md"}}, true)
	a.writeWorkspaceRoot = "/w"
	ctx := runtimepolicy.WithContext(context.Background(), runtimepolicy.Constraints{AllowRebuild: true})
	a.beginRunTurn(ctx, "please rewrite notes.md from scratch", pinnedRevisionPlan{})
	if !a.rebuildAuthorized("/w/notes.md") {
		t.Fatal("host-published constraints must still record the user's rebuild paths")
	}
	if a.rebuildAuthorized("/w/other.md") {
		t.Fatal("the waiver must not cover a file the instruction did not name")
	}
}

// TestSubagentPromptCannotRecordRebuildPaths proves a sub-agent's model-authored
// prompt never records a host authorization, even when it parses as a rebuild.
func TestSubagentPromptCannotRecordRebuildPaths(t *testing.T) {
	a, _ := newEvidenceAgent(t, evidenceWriter{target: tool.EvidenceTargetInfo{Path: "/w/secret.go"}}, true)
	a.writeWorkspaceRoot = "/w"
	a.classifierTaskText = "rewrite the file completely: secret.go"
	ctx := runtimepolicy.WithContext(context.Background(), runtimepolicy.Constraints{AllowRebuild: true})
	a.beginRunTurn(ctx, a.classifierTaskText, pinnedRevisionPlan{})
	if a.rebuildAuthorized("/w/secret.go") {
		t.Fatal("a sub-agent prompt must not authorize a rebuild")
	}
}
