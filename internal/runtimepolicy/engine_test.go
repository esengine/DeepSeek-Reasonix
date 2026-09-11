package runtimepolicy

import (
	"encoding/json"
	"sync"
	"testing"

	"reasonix/internal/evidence"
	"reasonix/internal/taskcontract"
)

func TestMergeDecisionsIsMonotonic(t *testing.T) {
	allow := GuardDecision{Action: GuardAllow}
	ask := GuardDecision{Action: GuardAsk, Message: "ask"}
	deny := GuardDecision{Action: GuardDeny, Message: "deny"}
	got := MergeDecisions(allow, deny, ask)
	if got.Action != GuardDeny || got.Message != "deny" {
		t.Fatalf("merge = %+v", got)
	}
	got = MergeDecisions(ask, allow)
	if got.Action != GuardAsk {
		t.Fatalf("ask must outrank allow: %+v", got)
	}
}

func TestPlanGuardBeatsYOLO(t *testing.T) {
	ctx := CallContext{
		PlanReadOnly: true,
		Profile:      evidence.EffectProfile{Known: true, WorkspaceWrite: true},
	}
	if (PlanGuard{}).BeforeTool(ctx).Action != GuardDeny {
		t.Fatal("plan writes must deny even when permission would allow")
	}
}

func TestOpaqueWriterAskOrDeny(t *testing.T) {
	ctx := CallContext{Profile: evidence.EffectProfile{WorkspaceWrite: true, Reason: evidence.ReasonOpaqueWriter}}
	if (OpaqueWriterGuard{}).BeforeTool(ctx).Action != GuardDeny {
		t.Fatal("headless unknown writer must deny")
	}
	ctx.Interactive = true
	if (OpaqueWriterGuard{}).BeforeTool(ctx).Action != GuardAsk {
		t.Fatal("interactive unknown writer must ask")
	}
}

func TestConstraintNoWrite(t *testing.T) {
	g := ConstraintGuard{Constraints: Constraints{ForbidMutation: true}}
	ctx := CallContext{Profile: evidence.EffectProfile{Known: true, WorkspaceWrite: true}}
	if g.BeforeTool(ctx).Action != GuardDeny {
		t.Fatal("explicit no-write must deny")
	}
}

func TestEngineCommitInvalidatesWithoutRace(t *testing.T) {
	e := NewEngine(Constraints{})
	write := ResultContext{
		Receipt: evidence.Receipt{
			ToolName: "edit_file", Success: true, Write: true, Mutation: true,
			Args: json.RawMessage(`{"path":"internal/agent/agent.go"}`), Paths: []string{"internal/agent/agent.go"},
		},
		Profile: evidence.ClassifyEffect(evidence.EffectInput{
			ToolName: "edit_file", Args: json.RawMessage(`{"path":"internal/agent/agent.go"}`),
			ActualPaths: []string{"internal/agent/agent.go"},
		}),
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		e.CommitReceipt(write)
	}()
	go func() {
		defer wg.Done()
		<-start
		_ = e.BeforeStop(StopContext{})
	}()
	close(start)
	wg.Wait()
	snap := e.Snapshot()
	if len(snap.Obligations) == 0 {
		t.Fatal("successful writer must create obligations")
	}
}

func TestConcurrentReadOnlyBeforeTool(t *testing.T) {
	e := NewEngine(Constraints{})
	ctx := CallContext{Profile: evidence.EffectProfile{Known: true, ReadOnly: true}}
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	for range 2 {
		go func() {
			defer wg.Done()
			<-start
			if e.BeforeTool(ctx).Action == GuardDeny {
				t.Error("read-only overlap must not deny")
			}
		}()
	}
	close(start)
	wg.Wait()
}

func TestFailedWriterBarrier(t *testing.T) {
	g := MutationDependencyGuard{Blocked: true}
	ctx := CallContext{Profile: evidence.EffectProfile{Known: true, WorkspaceWrite: true}}
	if g.BeforeTool(ctx).Action != GuardDeny {
		t.Fatal("failed writer must block later mutations")
	}
	read := CallContext{Profile: evidence.EffectProfile{Known: true, ReadOnly: true}}
	if g.BeforeTool(read).Action != GuardAbstain {
		t.Fatal("read-only diagnosis may still run after a failed writer")
	}
}

func TestWriterPreflightInterleavesWithTodoRebuild(t *testing.T) {
	e := NewEngine(Constraints{})
	auth := authWriteCall()
	if e.BeforeTool(auth).Action != GuardDeny {
		t.Fatal("auth write without todo must deny")
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	var decision GuardDecision
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		decision = e.BeforeTool(auth)
	}()
	go func() {
		defer wg.Done()
		<-start
		e.Rebuild(taskcontract.RebuildFacts{
			Todos: []evidence.TodoItem{{Content: "fix login timeout", Status: "in_progress"}},
		})
	}()
	close(start)
	wg.Wait()
	if decision.Action != GuardDeny && decision.Action != GuardAllow {
		t.Fatalf("interleaved preflight = %+v", decision)
	}
	if len(e.Snapshot().Requirements) == 0 {
		t.Fatal("todo rebuild must remain after interleaved preflight")
	}
	if e.BeforeTool(auth).Action == GuardDeny {
		t.Fatal("auth write after todo rebuild must not deny for missing preconditions")
	}
}

func TestFailedWriterCommitInterleavesWithoutSuccessObligations(t *testing.T) {
	e := NewEngine(Constraints{})
	failed := ResultContext{
		Receipt: evidence.Receipt{
			ToolName: "edit_file", Success: false, Write: true, Mutation: true,
			Args: json.RawMessage(`{"path":"internal/agent/agent.go"}`), Paths: []string{"internal/agent/agent.go"},
		},
		Profile: evidence.ClassifyEffect(evidence.EffectInput{
			ToolName: "edit_file", Args: json.RawMessage(`{"path":"internal/agent/agent.go"}`),
		}),
	}
	later := CallContext{
		Profile: evidence.EffectProfile{Known: true, WorkspaceWrite: true},
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		<-start
		e.CommitReceipt(failed)
	}()
	go func() {
		defer wg.Done()
		<-start
		_ = e.BeforeTool(later)
	}()
	go func() {
		defer wg.Done()
		<-start
		_ = e.BeforeStop(StopContext{})
	}()
	close(start)
	wg.Wait()
	for _, o := range e.Snapshot().Obligations {
		if o.Kind == taskcontract.ObligationTargetedVerify || o.Kind == taskcontract.ObligationDiffReview {
			t.Fatalf("failed writer created success obligation: %+v", o)
		}
	}
}

func TestRebuildInterleavesWithSnapshotAndStop(t *testing.T) {
	facts := taskcontract.RebuildFacts{Receipts: []evidence.Receipt{{
		ToolName: "edit_file", Success: true, Write: true, Mutation: true,
		Args: json.RawMessage(`{"path":"internal/agent/agent.go"}`), Paths: []string{"internal/agent/agent.go"},
	}}}
	e := NewEngine(Constraints{})
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		<-start
		e.Rebuild(facts)
	}()
	go func() {
		defer wg.Done()
		<-start
		_ = e.Snapshot()
	}()
	go func() {
		defer wg.Done()
		<-start
		_ = e.BeforeStop(StopContext{})
	}()
	close(start)
	wg.Wait()
	want := taskcontract.Rebuild(facts)
	got := e.Snapshot()
	if len(got.Obligations) != len(want.Obligations) {
		t.Fatalf("replay drifted under interleaving: %+v vs %+v", got.Obligations, want.Obligations)
	}
}

func TestChildMergeInterleavesWithParentStop(t *testing.T) {
	parent := evidence.NewLedger()
	child := evidence.NewLedger()
	child.Record(evidence.Receipt{
		ToolName: "edit_file", Success: true, Write: true, Mutation: true,
		Args: json.RawMessage(`{"path":"internal/agent/agent.go"}`), Paths: []string{"internal/agent/agent.go"},
	})
	e := NewEngine(Constraints{})
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		parent.MergeChild(child.Summary())
	}()
	go func() {
		defer wg.Done()
		<-start
		_ = e.BeforeStop(StopContext{})
	}()
	close(start)
	wg.Wait()
	e.Rebuild(taskcontract.RebuildFacts{Receipts: parent.Receipts()})
	if len(e.Snapshot().Unsatisfied()) == 0 {
		t.Fatal("merged child write must replay into the parent contract")
	}
}

func TestSequentialProductionWritesEscalatePreconditions(t *testing.T) {
	e := NewEngine(Constraints{})
	e.CommitReceipt(writeResult("internal/agent/agent.go"))

	if got := e.BeforeTool(writeCall("internal/agent/run_loop.go")); got.Action != GuardDeny {
		t.Fatalf("second production target without todo and criteria = %+v, want deny", got)
	}
	if got := e.BeforeTool(writeCall("internal/agent/agent.go")); got.Action == GuardDeny {
		t.Fatalf("repeat write to the same target must not become multi-file: %+v", got)
	}

	docs := NewEngine(Constraints{})
	docs.CommitReceipt(writeResult("README.md"))
	if got := docs.BeforeTool(writeCall("docs/usage.md")); got.Action == GuardDeny {
		t.Fatalf("two docs targets must remain lightweight: %+v", got)
	}

	pathless := CallContext{Profile: evidence.EffectProfile{Known: true, WorkspaceWrite: true}}
	if got := e.BeforeTool(pathless); got.Action != GuardDeny {
		t.Fatalf("pathless writer after a production write must establish preconditions: %+v", got)
	}
}

func TestRequireFullVerificationSurvivesParseRebuildAndReplay(t *testing.T) {
	constraints := ParseConstraints("请闭环交付")
	if !constraints.RequireFullVerification {
		t.Fatal("explicit closed-loop request must require full verification")
	}
	e := NewEngine(constraints)
	e.CommitReceipt(writeResult("README.md"))
	if !hasUnsatisfiedKind(e.Snapshot(), taskcontract.ObligationFullVerify, taskcontract.EnforcementStrict) {
		t.Fatalf("explicit full verification constraint missing after write: %+v", e.Snapshot().Obligations)
	}
	e.CommitReceipt(ResultContext{Receipt: evidence.Receipt{
		ToolName: "bash", Success: true, Command: "go test ./internal/taskcontract",
		Verification: evidence.VerificationPassed,
	}})
	if !hasUnsatisfiedKind(e.Snapshot(), taskcontract.ObligationFullVerify, taskcontract.EnforcementStrict) {
		t.Fatal("targeted verification must not clear explicit full verification")
	}
	e.CommitReceipt(ResultContext{Receipt: evidence.Receipt{
		ToolName: "bash", Success: true, Command: "go test ./...",
		Verification: evidence.VerificationPassed,
	}})
	if hasUnsatisfiedKind(e.Snapshot(), taskcontract.ObligationFullVerify, taskcontract.EnforcementStrict) {
		t.Fatal("full-project verification must clear explicit full verification")
	}
}

func TestRebuildThenSyncDoesNotReplayReceipts(t *testing.T) {
	receipts := []evidence.Receipt{writeResult("internal/agent/agent.go").Receipt}
	e := NewEngine(Constraints{})
	e.Rebuild(taskcontract.RebuildFacts{Receipts: receipts})
	if got := e.Snapshot().Epoch(); got != 1 {
		t.Fatalf("epoch after rebuild = %d, want 1", got)
	}
	e.SyncReceipts(receipts, "", false)
	if got := e.Snapshot().Epoch(); got != 1 {
		t.Fatalf("epoch after syncing rebuilt receipts = %d, want 1", got)
	}
}

func writeCall(path string) CallContext {
	args := json.RawMessage(`{"path":"` + path + `"}`)
	return CallContext{
		ToolName: "edit_file",
		Args:     args,
		Profile: evidence.ClassifyEffect(evidence.EffectInput{
			ToolName: "edit_file", Args: args, ActualPaths: []string{path},
		}),
	}
}

func writeResult(path string) ResultContext {
	call := writeCall(path)
	return ResultContext{
		Receipt: evidence.Receipt{
			ToolName: "edit_file", Success: true, Write: true, Mutation: true,
			Args: call.Args, Paths: []string{path},
		},
		Profile: call.Profile,
	}
}

func hasUnsatisfiedKind(c *taskcontract.Contract, kind taskcontract.ObligationKind, enforcement taskcontract.Enforcement) bool {
	for _, obligation := range c.Unsatisfied() {
		if obligation.Kind == kind && obligation.Enforcement == enforcement {
			return true
		}
	}
	return false
}

func authWriteCall() CallContext {
	return CallContext{
		ToolName: "edit_file",
		Args:     json.RawMessage(`{"path":"internal/auth/session.go"}`),
		Profile: evidence.ClassifyEffect(evidence.EffectInput{
			ToolName: "edit_file", Args: json.RawMessage(`{"path":"internal/auth/session.go"}`),
		}),
	}
}
