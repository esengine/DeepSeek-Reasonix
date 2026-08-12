package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/capability"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/taskintent"
	"reasonix/internal/tool"
)

// fakeReadFileTool is a minimal read-only tool whose successful calls produce
// Read receipts with an extractable path, like the real read_file.
type fakeReadFileTool struct{}

func (fakeReadFileTool) Name() string            { return "read_file" }
func (fakeReadFileTool) Description() string     { return "fake read" }
func (fakeReadFileTool) ReadOnly() bool          { return true }
func (fakeReadFileTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (fakeReadFileTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "contents", nil
}

// fakeWriterTool is registered (never called) so a registry counts as
// writer-capable for delivery mutation expectations.
type fakeWriterTool struct{}

func (fakeWriterTool) Name() string            { return "fake_write" }
func (fakeWriterTool) Description() string     { return "fake write" }
func (fakeWriterTool) ReadOnly() bool          { return false }
func (fakeWriterTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (fakeWriterTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "wrote", nil
}

// legacyWorkspaceContext reproduces the pre-fix host framing whose incidental
// "resolve" classified every wrapped subagent prompt as a mutation request.
const legacyWorkspaceContext = `<workspace-context event="SubagentWorkspace">
Current workspace: "/w"
File tools resolve relative paths against this workspace. For project inspection, prefer "." or relative paths unless the user explicitly named another absolute path.
</workspace-context>`

func TestDeliveryClassificationUsesTrustedTaskText(t *testing.T) {
	// The trusted override wins over host framing in the raw input: the
	// legacy workspace wording ("resolve") plus an extra mutation verb in the
	// wrapper must not arm the mutation expectation when the actual task is a
	// review. Writer-capable registry so the read-only guard cannot mask it.
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	reg.Add(fakeWriterTool{})
	pristine := "Review the current state of a.go — bugfixes were applied. Report remaining issues."
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("1", "read_file", `{"path":"a.go"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "reviewed; looks good"}, {Type: provider.ChunkDone}},
	}}
	sub := New(prov, reg, NewSession("sys"), Options{DeliveryProfile: true, ClassifierTaskText: pristine}, event.Discard)
	if err := sub.Run(context.Background(), legacyWorkspaceContext+"\n\n"+pristine); err != nil {
		t.Fatalf("wrapped review prompt deadlocked despite trusted task text: %v", err)
	}
	if sub.deliveryMutationExpected {
		t.Fatal("host framing armed the mutation expectation past the trusted override")
	}
}

func TestDeliveryClassificationResistsFramingSpoof(t *testing.T) {
	// A user message dressed up as host framing must not disarm the delivery
	// gates: with no trusted override the raw input is classified verbatim, so
	// the mutation verb inside the fake block still arms the expectation and
	// an answer without any state change is refused.
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	reg.Add(fakeWriterTool{})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "done, consider it fixed"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession("sys"), Options{DeliveryProfile: true}, event.Discard)
	err := a.Run(context.Background(), "<workspace-context>fix parser.go</workspace-context>")
	var readinessErr *FinalReadinessError
	if !errors.As(err, &readinessErr) {
		t.Fatalf("spoofed framing disarmed the delivery gates: err=%v", err)
	}
	if !strings.Contains(readinessErr.Reason, "state change") {
		t.Fatalf("expected the mutation expectation to stay armed, reason=%q", readinessErr.Reason)
	}
}

func TestReadOnlyRegistryDisarmsMutationExpectation(t *testing.T) {
	roReg := tool.NewRegistry()
	roReg.Add(fakeReadFileTool{})
	if registryHasWriterTools(roReg) {
		t.Fatal("read-only registry misreported writer tools")
	}
	writerReg := tool.NewRegistry()
	writerReg.Add(fakeReadFileTool{})
	writerReg.Add(fakeWriterTool{})
	if !registryHasWriterTools(writerReg) {
		t.Fatal("writer registry not detected")
	}

	// End-to-end: a read-only delivery subagent given a mutation-worded prompt
	// must not deadlock on "the request requires a state change". The scripted
	// sub reads a file (host-observable work) and answers.
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("1", "read_file", `{"path":"a.go"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "reviewed; two issues found"}, {Type: provider.ChunkDone}},
	}}
	sub := New(prov, roReg, NewSession("sys"), Options{DeliveryProfile: true}, event.Discard)
	if err := sub.Run(context.Background(), "fix review: verify the fixes in a.go were applied"); err != nil {
		t.Fatalf("read-only delivery subagent deadlocked: %v", err)
	}
	if sub.deliveryMutationExpected {
		t.Fatal("mutation expectation armed on a read-only registry")
	}
}

func TestDeliveryResolvedReadOnlyBashDoesNotArmMutationReadiness(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(stubBash{})
	prov := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{
		{toolCallChunk("pwd-base", "bash", `{"command":"basename \"$(pwd)\""}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "workspace basename inspected"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession("sys"), Options{DeliveryProfile: true}, event.Discard)
	if err := a.Run(context.Background(), "inspect and report the current workspace basename"); err != nil {
		t.Fatalf("resolved read-only delivery command: %v", err)
	}
	if _, ok := a.task.ledger.LatestSuccessfulMutationIndex(); ok {
		t.Fatal("resolved read-only bash was recorded as a mutation")
	}
	msgs := a.session.Snapshot()
	var resolved bool
	for _, msg := range msgs {
		for _, call := range msg.ToolCalls {
			if call.ID == "pwd-base" && call.ResolvedReadOnly != nil && *call.ResolvedReadOnly {
				resolved = true
			}
		}
	}
	if !resolved {
		t.Fatal("session receipt did not preserve resolved_read_only=true")
	}
}

func TestDeliveryConversationTokenSurvivesToNextTurnWithoutActionEvidence(t *testing.T) {
	prov := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "Understood."}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "ORBIT-42"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, tool.NewRegistry(), NewSession("sys"), Options{DeliveryProfile: true}, event.Discard)
	if err := a.Run(context.Background(), "Remember ORBIT-42 and answer on the next turn."); err != nil {
		t.Fatalf("deferred conversation turn was blocked: %v", err)
	}
	if err := a.Run(context.Background(), "What was the code?"); err != nil {
		t.Fatalf("answer turn was blocked: %v", err)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want exactly two conversational turns", prov.call)
	}
	if got := lastAssistantContent(a.Session()); got != "ORBIT-42" {
		t.Fatalf("last assistant text = %q, want ORBIT-42", got)
	}
}

func TestDeliveryDurableMemoryRequiresRememberWithoutCodeCeremony(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "remember", readOnly: false})
	prov := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{
		{toolCallChunk("remember", "remember", `{"description":"ORBIT code","body":"ORBIT-42"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "Saved for future sessions."}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession("sys"), Options{DeliveryProfile: true}, event.Discard)
	if err := a.Run(context.Background(), "Remember ORBIT-42 permanently across sessions"); err != nil {
		t.Fatalf("durable-memory workflow inherited code-delivery ceremony: %v", err)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want remember plus final answer", prov.call)
	}
	if a.deliveryCriteriaEstablished {
		t.Fatal("durable-memory-only workflow should not manufacture code acceptance criteria")
	}

	missing := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "I'll remember it."}, {Type: provider.ChunkDone}},
	}}
	b := New(missing, reg, NewSession("sys"), Options{DeliveryProfile: true}, event.Discard)
	err := b.Run(context.Background(), "Remember ORBIT-42 permanently across sessions")
	var readiness *FinalReadinessError
	if !errors.As(err, &readiness) || !strings.Contains(readiness.Reason, "remember tool") {
		t.Fatalf("text-only durable-memory claim err = %v", err)
	}
}

func TestNonGoalUpdateGoalWithVisibleTextDoesNotSpendRepairRound(t *testing.T) {
	goalTool, ok := tool.LookupBuiltin("update_goal")
	if !ok {
		t.Fatal("update_goal builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(goalTool)
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "Here is the answer."}, toolCallChunk("goal", "update_goal", `{"status":"complete"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "unexpected repair"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession("sys"), Options{}, event.Discard)
	if err := a.Run(context.Background(), "answer normally"); err != nil {
		t.Fatalf("non-Goal update_goal with text: %v", err)
	}
	if prov.call != 1 {
		t.Fatalf("provider calls = %d, want no repair round", prov.call)
	}
	if got := lastAssistantContent(a.Session()); got != "Here is the answer." {
		t.Fatalf("last assistant text = %q", got)
	}
	if got := lastToolResult(a.Session(), "update_goal"); !strings.Contains(got, "only available while an active goal turn") {
		t.Fatalf("paired update_goal result = %q", got)
	}
}

func TestNonGoalToolOnlyUpdateGoalGetsAtMostOneRepairRound(t *testing.T) {
	goalTool, ok := tool.LookupBuiltin("update_goal")
	if !ok {
		t.Fatal("update_goal builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(goalTool)
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("goal-1", "update_goal", `{"status":"complete"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("goal-2", "update_goal", `{"status":"complete"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "unexpected third round"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession("sys"), Options{}, event.Discard)
	err := a.Run(context.Background(), "answer normally")
	if err == nil || !strings.Contains(err.Error(), "repeatedly called update_goal outside Goal mode") {
		t.Fatalf("repeated tool-only misuse error = %v", err)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want one repair round", prov.call)
	}
}

func TestDeliveryPlanModeReturnsProposalBeforeExecutionReadiness(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	reg.Add(fakeWriterTool{})
	proposal := "1. Fix the parser\n   - update a.go\n   - run the focused tests"
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: proposal}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession("sys"), Options{DeliveryProfile: true}, event.Discard)
	a.SetPlanMode(true)

	if err := a.Run(context.Background(), "fix the parser bug in a.go"); err != nil {
		t.Fatalf("delivery plan proposal was blocked by execution readiness: %v", err)
	}
	if prov.call != 1 {
		t.Fatalf("provider calls = %d, want 1 without readiness retries in plan mode", prov.call)
	}
	if got := lastAssistantContent(a.Session()); got != proposal {
		t.Fatalf("last assistant text = %q, want proposal %q", got, proposal)
	}

	// Approval disables plan mode before the controller starts execution. The
	// same delivery expectations must become enforceable again at that boundary.
	a.SetPlanMode(false)
	if got := a.ReadinessResult(); !strings.Contains(got.Reason, "state change") {
		t.Fatalf("execution readiness did not resume after plan mode: %q", got.Reason)
	}
}

// TestPlanModeDefersCapabilityRequirementsUntilExecution ensures Delivery does
// not force a required writer capability while the model is drafting a plan.
// The same requirement becomes active immediately after Plan is disabled.
func TestPlanModeDefersCapabilityRequirementsUntilExecution(t *testing.T) {
	reg := tool.NewRegistry()
	a := New(&scriptedProvider{name: "p"}, reg, NewSession("sys"),
		Options{DeliveryProfile: true, CapabilityLedger: capability.NewLedger()}, event.Discard)
	a.SetPlanMode(true)
	a.SeedCapabilityRoute(capability.RouteDecision{Candidates: []capability.RouteCandidate{
		{Entry: capability.Entry{ID: "skill:deploy"}, Policy: capability.AutoUseRequire},
	}})

	if got := a.finalReadinessCheckFor(); got.applies || got.reason != "" {
		t.Fatalf("Plan proposal was forced through delivery capability gates: %+v", got)
	}

	a.SetPlanMode(false)
	got := a.finalReadinessCheckFor()
	if !got.applies || !strings.Contains(got.reason, "required capabilities") {
		t.Fatalf("execution did not restore required capability gate: %+v", got)
	}
}

func TestRunSubAgentReviewReportNudgeRecovers(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	AttachReviewReportTool(reg)
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		// Run 1: reads the file, then finishes with prose only — no report.
		{toolCallChunk("1", "read_file", `{"path":"a.go"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "verdict: pass, no issues"}, {Type: provider.ChunkDone}},
		// Nudge run: submits the typed report citing the run-1 read, then answers.
		{toolCallChunk("2", "review_report", `{"kind":"review","verdict":"pass","reviewed_paths":["a.go"]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "review_report submitted: pass"}, {Type: provider.ChunkDone}},
	}}
	sess := NewSession("sys")
	answer, err := RunSubAgentWithSession(context.Background(), prov, reg, sess, "review a.go",
		Options{RequireReviewReportKind: evidence.ReviewKindReview}, event.Discard)
	if err != nil {
		t.Fatalf("nudge recovery failed: %v", err)
	}
	if !strings.Contains(answer, "pass") {
		t.Fatalf("unexpected final answer %q", answer)
	}
	if !sessionHasUserMessageContaining(sess, "Call review_report now") {
		t.Fatal("expected the host completion nudge in the subagent session")
	}
	// The report cited a path read in run 1 — only possible because the nudge
	// run preserved the evidence ledger instead of resetting it.
	if got := lastToolResult(sess, "review_report"); !strings.Contains(got, "review_report accepted") {
		t.Fatalf("review_report result = %q", got)
	}
}

func TestRunSubAgentReviewReportExhaustionNamesRecovery(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	AttachReviewReportTool(reg)
	dir := t.TempDir()
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "looks fine"}, {Type: provider.ChunkDone}},
	}}
	sess := NewSession("sys")
	_, err := RunSubAgentWithSession(context.Background(), prov, reg, sess, "review it",
		Options{RequireReviewReportKind: evidence.ReviewKindReview, ArchiveDir: dir}, event.Discard)
	if err == nil {
		t.Fatal("expected failure when the report never arrives")
	}
	if !IsReviewUnavailable(err) && !strings.Contains(err.Error(), "review") {
		t.Fatalf("error %q missing review failure signal", err.Error())
	}
	// The failed transcript is dumped for diagnosis.
	matches, globErr := filepath.Glob(filepath.Join(dir, "subagent-report-failures", "review-*.jsonl"))
	if globErr != nil || len(matches) != 1 {
		t.Fatalf("expected one dumped transcript, got %v (%v)", matches, globErr)
	}
	if data, readErr := os.ReadFile(matches[0]); readErr != nil || !strings.Contains(string(data), "looks fine") {
		t.Fatalf("dump unreadable or incomplete: %v", readErr)
	}
}

func TestRunSubAgentSalvagesReadinessExhaustedWork(t *testing.T) {
	// The child performs a real mutation, then keeps answering without the
	// delivery sign-off receipts until the readiness budget is exhausted. Its
	// work is on disk, so the run must degrade to an explicitly unverified
	// answer instead of a hard failure that tricks the parent into spawning
	// repair tasks for changes that already landed.
	reg := evidenceRegistry()
	finalText := []provider.Chunk{{Type: provider.ChunkText, Text: "done, explanations added"}, {Type: provider.ChunkDone}}
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("criteria", "todo_write", `{"todos":[{"content":"Add explanations","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		{toolCallChunk("write", "write_file", `{"path":"qa/bank.md"}`), {Type: provider.ChunkDone}},
		finalText, // block 1 — complete_step/verification receipts missing
		finalText, // block 2 — no new receipts, stalled
		finalText, // block 3 — budget exhausted
	}}
	sess := NewSession("sys")
	answer, err := RunSubAgentWithSession(context.Background(), prov, reg, sess,
		"add explanations to the question bank", Options{DeliveryProfile: true, SubagentDepth: 1}, event.Discard)
	if err != nil {
		t.Fatalf("readiness exhaustion with real work must salvage, got err: %v", err)
	}
	for _, want := range []string{"[unverified]", "done, explanations added", "already on disk"} {
		if !strings.Contains(answer, want) {
			t.Fatalf("salvaged answer %q missing %q", answer, want)
		}
	}
}

func TestRunSubAgentReadinessFailureWithoutMutationStillFails(t *testing.T) {
	// An unbacked "done" claim keeps failing: with a mutation expected and no
	// successful mutation receipt, salvage must not launder the claim into an
	// unverified answer.
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	reg.Add(fakeWriterTool{})
	finalText := []provider.Chunk{{Type: provider.ChunkText, Text: "done, all fixed"}, {Type: provider.ChunkDone}}
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{finalText, finalText, finalText}}
	sess := NewSession("sys")
	answer, err := RunSubAgentWithSession(context.Background(), prov, reg, sess,
		"fix the crash in a.go", Options{DeliveryProfile: true, SubagentDepth: 1}, event.Discard)
	var readinessErr *FinalReadinessError
	if !errors.As(err, &readinessErr) {
		t.Fatalf("expected wrapped FinalReadinessError, got %v", err)
	}
	if answer != "" {
		t.Fatalf("mutation-less readiness failure must not salvage, got %q", answer)
	}
}

func TestFinalReadinessFailsImmediatelyWithoutRetries(t *testing.T) {
	// Delivery no longer retries readiness with hidden model messages: the run
	// ends on the FIRST unsatisfied final answer, and the host decides what
	// happens next (Goal FSM auto-continues; plain turns surface the recovery
	// card). Repeated reads must not buy extra provider calls.
	newReg := func() *tool.Registry {
		reg := tool.NewRegistry()
		reg.Add(fakeReadFileTool{})
		reg.Add(fakeWriterTool{}) // writer-capable registry keeps mutation expected
		return reg
	}
	finalText := []provider.Chunk{{Type: provider.ChunkText, Text: "done, all fixed"}, {Type: provider.ChunkDone}}
	readCall := func(id string) []provider.Chunk {
		return []provider.Chunk{toolCallChunk(id, "read_file", `{"path":"a.go"}`), {Type: provider.ChunkDone}}
	}

	stalled := &scriptedProvider{name: "p", turns: [][]provider.Chunk{finalText}}
	a := New(stalled, newReg(), NewSession("sys"), Options{DeliveryProfile: true}, event.Discard)
	err := a.Run(context.Background(), "fix the crash in a.go")
	var readinessErr *FinalReadinessError
	if !errors.As(err, &readinessErr) {
		t.Fatalf("expected FinalReadinessError, got %v", err)
	}
	if readinessErr.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (no readiness retries)", readinessErr.Attempts)
	}
	if stalled.call != 1 {
		t.Fatalf("provider calls = %d, want 1 (no hidden retry messages)", stalled.call)
	}
	if !a.deliveryRecoveryPending {
		t.Fatal("delivery recovery must be pending for an explicit continuation")
	}

	// A read that changed nothing still ends the run at the first final answer.
	converging := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		readCall("1"), finalText,
		readCall("2"), finalText,
	}}
	a2 := New(converging, newReg(), NewSession("sys"), Options{DeliveryProfile: true}, event.Discard)
	err2 := a2.Run(context.Background(), "fix the crash in a.go")
	var readinessErr2 *FinalReadinessError
	if !errors.As(err2, &readinessErr2) {
		t.Fatalf("expected FinalReadinessError, got %v", err2)
	}
	if converging.call != 2 {
		t.Fatalf("provider calls = %d, want 2 (work turn + one final answer)", converging.call)
	}
}

func TestExplicitDeliveryRecoveryPreservesEvidenceOnce(t *testing.T) {
	reg := evidenceRegistry()
	reg.Add(fakeReadFileTool{})
	finalText := []provider.Chunk{{Type: provider.ChunkText, Text: "premature"}, {Type: provider.ChunkDone}}
	prov := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{
		{toolCallChunk("todo", "todo_write", `{"todos":[{"content":"Ship main","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		{toolCallChunk("write", "write_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		finalText,
		{toolCallChunk("review", "read_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("verify", "bash", `{"command":"go test ./..."}`), {Type: provider.ChunkDone}},
		{toolCallChunk("signoff", "complete_step", `{"step":"Ship main","result":"done","evidence":[{"kind":"verification","summary":"tests pass","command":"go test ./..."}]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "delivered"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession("sys"), Options{DeliveryProfile: true}, event.Discard)
	var readinessErr *FinalReadinessError
	if err := a.Run(context.Background(), "implement main"); !errors.As(err, &readinessErr) {
		t.Fatalf("first Run error = %v, want FinalReadinessError", err)
	}
	if !a.PrepareDeliveryRecovery() {
		t.Fatal("explicit recovery should consume the pending readiness failure")
	}
	if a.PrepareDeliveryRecovery() {
		t.Fatal("delivery recovery authorization must be one-shot")
	}
	if err := a.Run(context.Background(), "continue the remaining delivery checks"); err != nil {
		t.Fatalf("recovery Run: %v", err)
	}
	if _, ok := a.task.ledger.LatestSuccessfulMutationIndex(); !ok {
		t.Fatal("recovery turn lost the prior mutation receipt")
	}
}

func TestOrdinaryFollowUpDoesNotPreserveFailedDeliveryEvidence(t *testing.T) {
	reg := evidenceRegistry()
	finalText := []provider.Chunk{{Type: provider.ChunkText, Text: "premature"}, {Type: provider.ChunkDone}}
	prov := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{
		{toolCallChunk("todo", "todo_write", `{"todos":[{"content":"Ship main","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		{toolCallChunk("write", "write_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		finalText,
		finalText,
		finalText,
		finalText,
	}}
	a := New(prov, reg, NewSession("sys"), Options{DeliveryProfile: true}, event.Discard)
	var firstErr *FinalReadinessError
	if err := a.Run(context.Background(), "implement main"); !errors.As(err, &firstErr) {
		t.Fatalf("first Run error = %v, want FinalReadinessError", err)
	}
	if _, ok := a.task.ledger.LatestSuccessfulMutationIndex(); !ok {
		t.Fatal("first failed delivery should retain its mutation until the next turn is classified")
	}

	var followUpErr *FinalReadinessError
	if err := a.Run(context.Background(), "fix the unrelated crash in other.go"); !errors.As(err, &followUpErr) {
		t.Fatalf("ordinary follow-up error = %v, want FinalReadinessError", err)
	}
	if _, ok := a.task.ledger.LatestSuccessfulMutationIndex(); ok {
		t.Fatal("ordinary follow-up inherited stale mutation evidence without explicit recovery")
	}
}

func TestPreviewStripsDeliveryMarkerAndSyntheticTurns(t *testing.T) {
	first := "你是谁？\n\n" + DeliveryRuntimeMarker
	if got := UserPreviewText(first); got != "你是谁？" {
		t.Fatalf("UserPreviewText kept framing: %q", got)
	}
	// A literal <delivery-runtime> mention inside user prose is not the host
	// suffix: nothing may be cut. (The agent never appends the marker when the
	// input already mentions the tag, so this content carries no host suffix.)
	inline := "Explain this literal: <delivery-runtime>example</delivery-runtime> and keep this sentence"
	if got := UserPreviewText(inline); got != inline {
		t.Fatalf("inline delivery-runtime mention was mangled: %q", got)
	}
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: first},
		{Role: provider.RoleAssistant, Content: "hi"},
		{Role: provider.RoleUser, Content: MidTurnSteerPrefix + "\nslow down"},
		{Role: provider.RoleUser, Content: "帮我写一个魂斗罗游戏\n\n" + DeliveryRuntimeMarker},
	}
	preview, turns := SessionPreviewFromMessages(msgs)
	if preview != "你是谁？" {
		t.Fatalf("preview = %q", preview)
	}
	if turns != 2 {
		t.Fatalf("turns = %d, want 2 (steer excluded)", turns)
	}
}

func TestDeliveryTaskNeedsEvidenceSkipsDiagnosticConversations(t *testing.T) {
	// Diagnostic/troubleshooting conversations ask "what's wrong" or "why"
	// without requesting code changes. They must not demand host-observable
	// work — the agent can only give advice, not mutate files.
	diagnostic := []string{
		"what's wrong with my wifi",
		"I don't want to install dependencies",
		"please don't install any dependencies",
		"why can't I install the plugin?",
		"why can't I run WPS?",
		"why can't I check my email in Outlook?",
		"can you analyze why WPS won't open?",
		"why did the plugin update fail?",
		"can you explain why install keeps failing?",
		"why does this make a difference?",
		"why does the node selection matter?",
		"why is `Python` popular?",
		"what does `context.Context` mean?",
		"why can't I open github.com/?",
		"为什么wps导入zetero参考文献报错",
		"为什么无法安装插件",
		"为什么不能安装插件",
		"为什么 WPS 不能运行",
		"为什么无法检查 Outlook 邮件",
		"分析一下为什么 WPS 不能运行",
		"为什么安装插件失败",
		"为什么更新配置后报错",
		"帮我看看这是什么问题",
		"为什么zotero连接不上，我不敢重新安装",
		"诊断数据库连接失败的原因",
		"这软件打不开了，怎么回事",
	}
	for _, input := range diagnostic {
		if taskintent.NeedsEvidence(input) {
			t.Errorf("diagnostic input %q incorrectly classified as needing evidence", input)
		}
	}

	// Mutation-worded tasks still require evidence.
	taskInputs := []string{
		"fix the crash in a.go",
		"帮我修复wps的崩溃问题",
		"create a new login endpoint",
		"添加一个单元测试",
		"modify the existing config",
		"patch the parser",
		"replace the old endpoint",
		"make the requested changes",
		"调整现有配置",
		"替换旧接口",
		"thanks for fixing that, now update the tests",
		"谢谢你，请继续修改配置",
	}
	for _, input := range taskInputs {
		if !taskintent.NeedsEvidence(input) {
			t.Errorf("mutation task %q incorrectly classified as NOT needing evidence", input)
		}
	}
}

func TestDeliveryTaskNeedsEvidenceKeepsReadOnlyTechnicalWork(t *testing.T) {
	inputs := []string{
		"review this pull request and report whether it is correct",
		"run go test ./... and tell me why it fails",
		"why does go test fail?",
		"why does go build ./... fail?",
		"why does npm run build fail?",
		"why does git status fail?",
		"why does `custom-lint --strict` fail?",
		"why does ./scripts/verify.sh fail?",
		"为什么 go build ./... 会失败",
		"why can't I run main.go?",
		"why does README.md render incorrectly?",
		"reproduce the crash and identify the root cause",
		"inspect main.go for security vulnerabilities",
		"诊断当前项目的数据库连接失败原因",
	}
	for _, input := range inputs {
		if !taskintent.NeedsEvidence(input) {
			t.Errorf("read-only technical task %q did not require host-observable evidence", input)
		}
		if taskintent.NeedsMutation(input) {
			t.Errorf("read-only technical task %q incorrectly required a mutation", input)
		}
	}
}

func TestDeliveryTaskNeedsMutationHandlesMixedIntent(t *testing.T) {
	mutationInputs := []string{
		"modify the existing config",
		"patch the parser",
		"make the requested changes",
		"I don't want to install dependencies, but update the existing config",
		"I can't install dependencies; please edit the existing config instead",
		"I can't install dependencies and please update the config",
		"do not install dependencies and please update the config",
		"I can't install dependencies so update the config",
		"I can't install dependencies please update the existing config",
		"can you explain why it fails and fix it",
		"我不想安装新依赖，请修改现有配置修复这个问题",
		"我无法安装新依赖，但请修改现有配置",
		"无法安装新依赖请修改配置",
		"不要安装依赖请更新配置",
		"无法安装新依赖所以修改配置",
		"为什么这个方案失败，请修复它",
		"调整现有配置",
		"替换旧接口",
	}
	for _, input := range mutationInputs {
		if !taskintent.NeedsMutation(input) {
			t.Errorf("mixed-intent input %q did not require a mutation", input)
		}
	}

	readOnlyInputs := []string{
		"review only; do not fix anything",
		"I don't want to install dependencies",
		"please don't install any dependencies",
		"why can't I install the plugin?",
		"do not install and update dependencies",
		"don't fix or update anything",
		"只分析，不要修改代码",
		"请不要安装或更新依赖",
		"不想请团队修改代码",
		"禁止申请修改配置",
		"为什么无法安装插件",
		"为什么不能安装插件",
		"为什么zotero连接不上，我不敢重新安装",
	}
	for _, input := range readOnlyInputs {
		if taskintent.NeedsMutation(input) {
			t.Errorf("read-only input %q incorrectly required a mutation", input)
		}
	}
}

func TestDeliveryMixedIntentRequiresMutationEvidence(t *testing.T) {
	inputs := []string{
		"I can't install dependencies and please update the config",
		"无法安装新依赖请修改配置",
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			reg := tool.NewRegistry()
			reg.Add(fakeReadFileTool{})
			reg.Add(fakeWriterTool{})
			answer := []provider.Chunk{
				{Type: provider.ChunkText, Text: "Done; the config is updated."},
				{Type: provider.ChunkDone},
			}
			prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{answer, answer, answer}}
			a := New(prov, reg, NewSession("sys"), Options{DeliveryProfile: true}, event.Discard)
			err := a.Run(context.Background(), input)
			var readinessErr *FinalReadinessError
			if !errors.As(err, &readinessErr) {
				t.Fatalf("text-only completion escaped the mutation gate: %v", err)
			}
			if !strings.Contains(readinessErr.Reason, "state change") {
				t.Fatalf("readiness reason = %q, want missing state change", readinessErr.Reason)
			}
		})
	}
}

func TestDeliveryReadOnlyTechnicalTaskRequiresEvidence(t *testing.T) {
	inputs := []string{
		"review this pull request and report whether it is correct",
		"why does go build ./... fail?",
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			reg := tool.NewRegistry()
			reg.Add(fakeReadFileTool{})
			reg.Add(fakeWriterTool{})
			answer := []provider.Chunk{
				{Type: provider.ChunkText, Text: "Reviewed; everything is correct."},
				{Type: provider.ChunkDone},
			}
			prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{answer, answer, answer}}
			a := New(prov, reg, NewSession("sys"), Options{DeliveryProfile: true}, event.Discard)
			err := a.Run(context.Background(), input)
			var readinessErr *FinalReadinessError
			if !errors.As(err, &readinessErr) {
				t.Fatalf("text-only technical work escaped the evidence gate: %v", err)
			}
			if !strings.Contains(readinessErr.Reason, "host-observable work") {
				t.Fatalf("readiness reason = %q, want missing host-observable work", readinessErr.Reason)
			}
			if strings.Contains(readinessErr.Reason, "state change") {
				t.Fatalf("read-only work incorrectly required a mutation: %q", readinessErr.Reason)
			}
		})
	}
}

func TestDeliveryDiagnosticConversationCompletes(t *testing.T) {
	// End-to-end: a diagnostic troubleshooting conversation with no mutation
	// keywords must complete without a FinalReadinessError — the agent can
	// give advice but can't write files on the user's machine.
	inputs := []string{
		"为什么wps导入zetero参考文献报错，请你帮我诊断一下",
		"分析一下为什么 WPS 不能运行",
		"why can't I check my email in Outlook?",
		"what does `context.Context` mean?",
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			reg := tool.NewRegistry()
			reg.Add(fakeReadFileTool{})
			reg.Add(fakeWriterTool{})
			// The model gives advice text (no tool calls) — a diagnostic response.
			advice := []provider.Chunk{
				{Type: provider.ChunkText, Text: "请尝试以下步骤：1. 检查端口监听 2. 重新注册插件"},
				{Type: provider.ChunkDone},
			}
			prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{advice}}
			a := New(prov, reg, NewSession("sys"), Options{DeliveryProfile: true}, event.Discard)
			if err := a.Run(context.Background(), input); err != nil {
				t.Fatalf("diagnostic conversation deadlocked: %v", err)
			}
			if prov.call != 1 {
				t.Fatalf("diagnostic conversation had %d provider calls, want 1 (no readiness retries)", prov.call)
			}
		})
	}
}
