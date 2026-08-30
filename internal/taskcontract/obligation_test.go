package taskcontract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/evidence"
)

func TestNewDoesNotClassifyInput(t *testing.T) {
	if !New("fix the bug in utils.py").Empty() {
		t.Fatal("ordinary text must start with an empty contract")
	}
	if !New("what does this function do?").Empty() {
		t.Fatal("advisory text must start with an empty contract")
	}
}

func TestMapWriterMatrix(t *testing.T) {
	docs := evidence.ClassifyEffect(evidence.EffectInput{
		ToolName: "edit_file", Args: json.RawMessage(`{"path":"README.md"}`),
	})
	if got := MapWriter(docs, 1, "", false); len(got.Preconditions) != 0 || len(got.PostSuccess) != 1 ||
		got.PostSuccess[0].Kind != ObligationTargetedVerify || got.PostSuccess[0].Enforcement != EnforcementAdvisory {
		t.Fatalf("docs mapping = %+v", got)
	}

	prod := evidence.ClassifyEffect(evidence.EffectInput{
		ToolName: "edit_file", Args: json.RawMessage(`{"path":"internal/agent/agent.go"}`),
	})
	got := MapWriter(prod, 2, "", false)
	if len(got.Preconditions) != 0 || !hasKind(got.PostSuccess, ObligationTargetedVerify, EnforcementRecoverable) ||
		!hasKind(got.PostSuccess, ObligationDiffReview, EnforcementRecoverable) {
		t.Fatalf("single prod mapping = %+v", got)
	}

	auth := evidence.ClassifyEffect(evidence.EffectInput{
		ToolName: "edit_file", Args: json.RawMessage(`{"path":"internal/auth/session.go"}`),
	})
	got = MapWriter(auth, 3, "", false)
	if !hasKind(got.Preconditions, ObligationTodo, EnforcementRecoverable) ||
		!hasKind(got.PostSuccess, ObligationFullVerify, EnforcementStrict) ||
		!hasKind(got.PostSuccess, ObligationSecurityReview, EnforcementStrict) {
		t.Fatalf("auth mapping = %+v", got)
	}

	author := evidence.ClassifyEffect(evidence.EffectInput{
		ToolName: "edit_file", Args: json.RawMessage(`{"path":"author.go"}`),
	})
	got = MapWriter(author, 4, "", false)
	if hasKind(got.PostSuccess, ObligationSecurityReview, EnforcementStrict) {
		t.Fatalf("author.go must not create security obligations: %+v", got)
	}

	forbid := MapWriter(prod, 5, "", true)
	if !hasKind(forbid.PostSuccess, ObligationTargetedVerify, EnforcementAdvisory) {
		t.Fatalf("user-forbid-tests must drop verification to advisory: %+v", forbid)
	}

	unknownBash := evidence.ClassifyEffect(evidence.EffectInput{
		ToolName: "bash", Args: json.RawMessage(`{"command":"go build ./... && go test ./..."}`),
	})
	got = MapWriter(unknownBash, 6, "", false)
	if len(got.Preconditions) != 0 {
		t.Fatalf("unknown bash must not require todo/criteria: %+v", got)
	}

	opaque := evidence.ClassifyEffect(evidence.EffectInput{ToolName: "mcp__srv__write", Args: json.RawMessage(`{}`)})
	got = MapWriter(opaque, 7, "", false)
	if !hasKind(got.Preconditions, ObligationTodo, EnforcementRecoverable) {
		t.Fatalf("opaque MCP writer must require todo/criteria: %+v", got)
	}
	if hasKind(got.PostSuccess, ObligationIndependentReview, EnforcementStrict) {
		t.Fatalf("pathless opaque writer must not auto-demand architecture review: %+v", got)
	}

	multi := evidence.ClassifyEffect(evidence.EffectInput{
		ToolName: "edit_file", ActualPaths: []string{"internal/agent/agent.go", "internal/agent/run_loop.go"},
	})
	got = MapWriter(multi, 8, "", false)
	if !hasKind(got.Preconditions, ObligationTodo, EnforcementRecoverable) ||
		!hasKind(got.Preconditions, ObligationCriteria, EnforcementRecoverable) ||
		!hasKind(got.PostSuccess, ObligationTargetedVerify, EnforcementRecoverable) {
		t.Fatalf("multi-file mapping = %+v", got)
	}
	if hasKind(got.PostSuccess, ObligationIndependentReview, EnforcementRecoverable) ||
		hasKind(got.PostSuccess, ObligationIndependentReview, EnforcementStrict) {
		t.Fatalf("same-package multi-file must not auto-demand architecture review: %+v", got)
	}

	schema := evidence.ClassifyEffect(evidence.EffectInput{
		ToolName: "edit_file", Args: json.RawMessage(`{"path":"schema/user.proto"}`),
	})
	got = MapWriter(schema, 9, "", false)
	if !hasKind(got.Preconditions, ObligationTodo, EnforcementRecoverable) ||
		!hasKind(got.PostSuccess, ObligationFullVerify, EnforcementStrict) ||
		!hasKind(got.PostSuccess, ObligationIndependentReview, EnforcementStrict) ||
		!hasKind(got.PostSuccess, ObligationSignoff, EnforcementStrict) {
		t.Fatalf("schema mapping = %+v", got)
	}

	migration := evidence.ClassifyEffect(evidence.EffectInput{
		ToolName: "edit_file", Args: json.RawMessage(`{"path":"internal/db/migrations/001_init.sql"}`),
	})
	got = MapWriter(migration, 10, "", false)
	if !hasKind(got.PostSuccess, ObligationFullVerify, EnforcementStrict) ||
		!hasKind(got.PostSuccess, ObligationSignoff, EnforcementStrict) {
		t.Fatalf("migration mapping = %+v", got)
	}

	push := evidence.ClassifyEffect(evidence.EffectInput{
		ToolName: "bash", Args: json.RawMessage(`{"command":"git push origin main"}`),
	})
	got = MapWriter(push, 11, "", false)
	if !hasKind(got.Preconditions, ObligationTodo, EnforcementRecoverable) ||
		!hasKind(got.PostSuccess, ObligationActionReceipt, EnforcementStrict) {
		t.Fatalf("git push mapping = %+v", got)
	}

	scratch := evidence.ClassifyEffect(evidence.EffectInput{
		ToolName: "write_file", Args: mustJSONPath(filepath.Join(os.TempDir(), "btc_klines.py")),
	})
	got = MapWriter(scratch, 12, "", false)
	if len(got.Preconditions) != 0 || len(got.PostSuccess) != 0 {
		t.Fatalf("scratch write must not create duties: %+v", got)
	}
}

func mustJSONPath(path string) json.RawMessage {
	value, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		panic(err)
	}
	return value
}

func TestRebuildIsDeterministicAndInvalidates(t *testing.T) {
	write := evidence.Receipt{
		ToolName: "edit_file", Success: true, Write: true, Mutation: true,
		Args: json.RawMessage(`{"path":"internal/agent/agent.go"}`), Paths: []string{"internal/agent/agent.go"},
	}
	verify := evidence.Receipt{ToolName: "bash", Success: true, Command: "go test ./internal/agent", Verification: evidence.VerificationPassed}
	rewrite := write
	facts := RebuildFacts{Receipts: []evidence.Receipt{write, verify, rewrite}}
	a := Rebuild(facts)
	b := Rebuild(facts)
	if len(a.Obligations) != len(b.Obligations) {
		t.Fatalf("rebuild drifted: %+v vs %+v", a.Obligations, b.Obligations)
	}
	if Stop := a.Stop(StopOptions{}); Stop != StopContinue && Stop != StopReady {
		// After rewrite the verification is stale; recover once.
		if a.Stop(StopOptions{}) == StopReady {
			t.Fatal("later writer must invalidate earlier verification")
		}
	}
	unsat := a.Unsatisfied()
	if len(unsat) == 0 {
		t.Fatal("rewritten file must leave verification unsatisfied")
	}
}

func TestDeniedWriterCreatesNoSuccessObligation(t *testing.T) {
	c := Rebuild(RebuildFacts{Receipts: []evidence.Receipt{{
		ToolName: "edit_file", Success: false, Write: true, Mutation: true,
		Args: json.RawMessage(`{"path":"internal/agent/agent.go"}`), Paths: []string{"internal/agent/agent.go"},
	}}})
	for _, o := range c.Obligations {
		if o.Kind == ObligationTargetedVerify || o.Kind == ObligationDiffReview {
			t.Fatalf("denied writer created success obligation: %+v", o)
		}
	}
}

func TestReadOnlyRebuildStaysEmpty(t *testing.T) {
	c := Rebuild(RebuildFacts{Receipts: []evidence.Receipt{{
		ToolName: "read_file", Success: true, Read: true,
		Args: json.RawMessage(`{"path":"internal/auth/session.go"}`),
	}}})
	if !c.Empty() && len(c.Obligations) != 0 {
		t.Fatalf("read-only receipts must not create obligations: %+v", c.Obligations)
	}
}

func TestActiveGoalTodosBecomeStrictCriteriaUntilCompleted(t *testing.T) {
	c := Rebuild(RebuildFacts{
		HasActiveGoal: true,
		Todos:         []evidence.TodoItem{{Content: "finish the task", Status: "in_progress"}},
		Receipts: []evidence.Receipt{{
			ToolName: "todo_write", Success: true,
			Todos: []evidence.TodoItem{{Content: "finish the task", Status: "in_progress"}},
		}},
	})
	if !hasKind(c.Unsatisfied(), ObligationCriteria, EnforcementStrict) {
		t.Fatalf("active goal todo must remain a strict criterion: %+v", c.Obligations)
	}
	c.AbsorbReceipt(2, evidence.Receipt{
		ToolName: "todo_write", Success: true,
		Todos: []evidence.TodoItem{{Content: "finish the task", Status: "completed"}},
	}, "", false, false)
	if hasKind(c.Unsatisfied(), ObligationCriteria, EnforcementStrict) {
		t.Fatalf("completed goal todo must satisfy the strict criterion: %+v", c.Obligations)
	}
}

func TestTargetedVerificationDoesNotSatisfyFullVerification(t *testing.T) {
	write := evidence.Receipt{
		ToolName: "edit_file", Success: true, Write: true, Mutation: true,
		Args: json.RawMessage(`{"path":"internal/auth/session.go"}`), Paths: []string{"internal/auth/session.go"},
	}
	targeted := evidence.Receipt{
		ToolName: "bash", Success: true, Command: "go test ./internal/auth",
		Verification: evidence.VerificationPassed,
	}
	c := Rebuild(RebuildFacts{Receipts: []evidence.Receipt{write, targeted}})
	if !hasKind(c.Unsatisfied(), ObligationFullVerify, EnforcementStrict) {
		t.Fatalf("targeted command must not prove full verification: %+v", c.Obligations)
	}
	c.AbsorbReceipt(3, evidence.Receipt{
		ToolName: "bash", Success: true, Command: "go test ./...",
		Verification: evidence.VerificationPassed,
	}, "", false, false)
	if hasKind(c.Unsatisfied(), ObligationFullVerify, EnforcementStrict) {
		t.Fatalf("project-wide command must prove full verification: %+v", c.Obligations)
	}
}

func TestDeclaredChecksCollectivelySatisfyFullVerification(t *testing.T) {
	write := evidence.Receipt{
		ToolName: "edit_file", Success: true, Write: true, Mutation: true,
		Args: json.RawMessage(`{"path":"schema/user.proto"}`), Paths: []string{"schema/user.proto"},
	}
	first := evidence.Receipt{ToolName: "bash", Success: true, Command: "go test ./...", Verification: evidence.VerificationPassed}
	second := evidence.Receipt{ToolName: "bash", Success: true, Command: "go vet ./...", Verification: evidence.VerificationPassed}
	c := Rebuild(RebuildFacts{ProjectChecks: []string{first.Command, second.Command}, Receipts: []evidence.Receipt{write, first}})
	if !hasKind(c.Unsatisfied(), ObligationFullVerify, EnforcementStrict) {
		t.Fatalf("one of two declared checks must not prove full verification: %+v", c.Obligations)
	}
	c.AbsorbReceipt(3, second, "", false, false)
	if hasKind(c.Unsatisfied(), ObligationFullVerify, EnforcementStrict) {
		t.Fatalf("all declared checks must prove full verification: %+v", c.Obligations)
	}
}

func TestFailedOrUnscopedDeclaredCheckDoesNotProveFullVerification(t *testing.T) {
	write := evidence.Receipt{
		ToolName: "edit_file", Success: true, Write: true, Mutation: true,
		Args: json.RawMessage(`{"path":"schema/user.proto"}`), Paths: []string{"schema/user.proto"},
	}
	failed := evidence.Receipt{
		ToolName: "bash", Success: true, Command: "go test ./...",
		Verification: evidence.VerificationFailed,
	}
	c := Rebuild(RebuildFacts{ProjectChecks: []string{failed.Command}, Receipts: []evidence.Receipt{write, failed}})
	if c.Checks[0].Status != Failed || !hasKind(c.Unsatisfied(), ObligationFullVerify, EnforcementStrict) {
		t.Fatalf("failed declared check must leave full verification open: checks=%+v obligations=%+v", c.Checks, c.Obligations)
	}

	targeted := evidence.Receipt{
		ToolName: "bash", Success: true, Command: "go test ./internal/auth",
		Verification: evidence.VerificationPassed,
	}
	c = Rebuild(RebuildFacts{ProjectChecks: []string{""}, Receipts: []evidence.Receipt{write, targeted}})
	if !hasKind(c.Unsatisfied(), ObligationFullVerify, EnforcementStrict) {
		t.Fatalf("an unscoped declared check must not let a targeted command prove full verification: %+v", c.Obligations)
	}
}

func TestLaterFailedVerificationRevokesEarlierProof(t *testing.T) {
	write := evidence.Receipt{
		ToolName: "edit_file", Success: true, Write: true, Mutation: true,
		Args: json.RawMessage(`{"path":"schema/user.proto"}`), Paths: []string{"schema/user.proto"},
	}
	pass := evidence.Receipt{
		ToolName: "bash", Success: true, Command: "go test ./...",
		Verification: evidence.VerificationPassed,
	}
	fail := pass
	exitCode := 1
	fail.Verification = ""
	fail.ExitCode = &exitCode
	c := Rebuild(RebuildFacts{ProjectChecks: []string{pass.Command}, Receipts: []evidence.Receipt{write, pass, fail}})
	if c.Checks[0].Status != Failed || !hasKind(c.Unsatisfied(), ObligationFullVerify, EnforcementStrict) {
		t.Fatalf("later failed verification must revoke the earlier proof: checks=%+v obligations=%+v", c.Checks, c.Obligations)
	}
}

func TestReviewEvidenceMustMatchKindTargetAndVerdict(t *testing.T) {
	prodWrite := evidence.Receipt{
		ToolName: "edit_file", Success: true, Write: true, Mutation: true,
		Args: json.RawMessage(`{"path":"internal/agent/agent.go"}`), Paths: []string{"internal/agent/agent.go"},
	}
	unrelatedRead := evidence.Receipt{
		ToolName: "read_file", Success: true, Read: true,
		Args: json.RawMessage(`{"path":"README.md"}`), Paths: []string{"README.md"},
	}
	targetRead := evidence.Receipt{
		ToolName: "read_file", Success: true, Read: true,
		Args: json.RawMessage(`{"path":"internal/agent/agent.go"}`), Paths: []string{"internal/agent/agent.go"},
	}
	c := Rebuild(RebuildFacts{Receipts: []evidence.Receipt{prodWrite, unrelatedRead}})
	if !hasKind(c.Unsatisfied(), ObligationDiffReview, EnforcementRecoverable) {
		t.Fatalf("unrelated read must not review the changed target: %+v", c.Obligations)
	}
	c.AbsorbReceipt(3, targetRead, "", false, false)
	if hasKind(c.Unsatisfied(), ObligationDiffReview, EnforcementRecoverable) {
		t.Fatalf("target read must review the changed target: %+v", c.Obligations)
	}

	statusOnly := evidence.Receipt{
		ToolName: "bash", Success: true, Read: true, OutputBytes: 20,
		Command: "git status --short internal/agent/agent.go", Paths: []string{"internal/agent/agent.go"},
	}
	contentRead := evidence.Receipt{
		ToolName: "bash", Success: true, Read: true, OutputBytes: 20,
		Command: "git diff -- internal/agent/agent.go", Paths: []string{"internal/agent/agent.go"},
	}
	c = Rebuild(RebuildFacts{Receipts: []evidence.Receipt{prodWrite, statusOnly}})
	if !hasKind(c.Unsatisfied(), ObligationDiffReview, EnforcementRecoverable) {
		t.Fatalf("path mention without content must not prove review: %+v", c.Obligations)
	}
	c.AbsorbReceipt(3, contentRead, "", false, false)
	if hasKind(c.Unsatisfied(), ObligationDiffReview, EnforcementRecoverable) {
		t.Fatalf("content-producing targeted diff must prove review: %+v", c.Obligations)
	}

	authWrite := evidence.Receipt{
		ToolName: "edit_file", Success: true, Write: true, Mutation: true,
		Args: json.RawMessage(`{"path":"internal/auth/session.go"}`), Paths: []string{"internal/auth/session.go"},
	}
	review := evidence.Receipt{
		ToolName: "review_report", Success: true,
		Args: json.RawMessage(`{"kind":"review","verdict":"pass","reviewed_paths":["internal/auth/session.go"],"findings":[]}`),
	}
	security := review
	security.Args = json.RawMessage(`{"kind":"security","verdict":"pass","reviewed_paths":["internal/auth/session.go"],"findings":[]}`)
	blocked := security
	blocked.Args = json.RawMessage(`{"kind":"security","verdict":"block","reviewed_paths":["internal/auth/session.go"],"findings":[]}`)
	c = Rebuild(RebuildFacts{Receipts: []evidence.Receipt{authWrite, review, blocked}})
	if !hasKind(c.Unsatisfied(), ObligationSecurityReview, EnforcementStrict) {
		t.Fatalf("ordinary or blocking report must not prove security review: %+v", c.Obligations)
	}
	c.AbsorbReceipt(4, security, "", false, false)
	if hasKind(c.Unsatisfied(), ObligationSecurityReview, EnforcementStrict) {
		t.Fatalf("passing security report must prove security review: %+v", c.Obligations)
	}
	c.AbsorbReceipt(5, blocked, "", false, false)
	if !hasKind(c.Unsatisfied(), ObligationSecurityReview, EnforcementStrict) {
		t.Fatalf("later blocking security report must revoke the earlier proof: %+v", c.Obligations)
	}
}

func TestPathlessMCPReceiptIsNotAWriter(t *testing.T) {
	c := Rebuild(RebuildFacts{Receipts: []evidence.Receipt{{
		ToolName: "mcp__image__screenshot", Success: true, Mutation: true,
		Args: json.RawMessage(`{}`),
	}}})
	if len(c.Unsatisfied()) != 0 {
		t.Fatalf("pathless MCP must not create writer obligations: %+v", c.Unsatisfied())
	}
}

func TestLoginTimeoutStaysEmptyUntilAuthWrite(t *testing.T) {
	if !New("修复登录超时").Empty() {
		t.Fatal("prompt must not preclassify login wording")
	}
	c := Rebuild(RebuildFacts{Receipts: []evidence.Receipt{{
		ToolName: "edit_file", Success: true, Write: true, Mutation: true,
		Args: json.RawMessage(`{"path":"internal/auth/session.go"}`), Paths: []string{"internal/auth/session.go"},
	}}})
	if !hasKind(c.Unsatisfied(), ObligationFullVerify, EnforcementStrict) ||
		!hasKind(c.Unsatisfied(), ObligationSecurityReview, EnforcementStrict) {
		t.Fatalf("auth write must create strict obligations: %+v", c.Unsatisfied())
	}
}

func TestFailedVerificationDoesNotCoverRewrite(t *testing.T) {
	write := evidence.Receipt{
		ToolName: "edit_file", Success: true, Write: true, Mutation: true,
		Args: json.RawMessage(`{"path":"internal/agent/agent.go"}`), Paths: []string{"internal/agent/agent.go"},
	}
	pass := evidence.Receipt{ToolName: "bash", Success: true, Command: "go test ./internal/agent", Verification: evidence.VerificationPassed}
	fail := evidence.Receipt{ToolName: "bash", Success: true, Command: "go test ./internal/agent", Verification: evidence.VerificationFailed}
	c := Rebuild(RebuildFacts{Receipts: []evidence.Receipt{write, pass, write, fail}})
	if !hasKind(c.Unsatisfied(), ObligationTargetedVerify, EnforcementRecoverable) {
		t.Fatalf("failed test after rewrite must leave verification open: %+v", c.Unsatisfied())
	}
}

func TestEarlierPassingTestDoesNotSatisfyLaterWrite(t *testing.T) {
	pass := evidence.Receipt{ToolName: "bash", Success: true, Command: "go test ./internal/agent", Verification: evidence.VerificationPassed}
	write := evidence.Receipt{
		ToolName: "edit_file", Success: true, Write: true, Mutation: true,
		Args: json.RawMessage(`{"path":"internal/agent/agent.go"}`), Paths: []string{"internal/agent/agent.go"},
	}
	c := Rebuild(RebuildFacts{Receipts: []evidence.Receipt{pass, write}})
	if !hasKind(c.Unsatisfied(), ObligationTargetedVerify, EnforcementRecoverable) {
		t.Fatalf("old passing test must not cover a later write: %+v", c.Unsatisfied())
	}
}

func TestAnyLaterWorkspaceWriteInvalidatesFullVerification(t *testing.T) {
	authWrite := evidence.Receipt{
		ToolName: "edit_file", Success: true, Write: true, Mutation: true,
		Args: json.RawMessage(`{"path":"internal/auth/session.go"}`), Paths: []string{"internal/auth/session.go"},
	}
	full := evidence.Receipt{
		ToolName: "bash", Success: true, Command: "go test ./...",
		Verification: evidence.VerificationPassed,
	}
	docsWrite := evidence.Receipt{
		ToolName: "edit_file", Success: true, Write: true, Mutation: true,
		Args: json.RawMessage(`{"path":"README.md"}`), Paths: []string{"README.md"},
	}
	c := Rebuild(RebuildFacts{Receipts: []evidence.Receipt{authWrite, full, docsWrite}})
	if !hasKind(c.Unsatisfied(), ObligationFullVerify, EnforcementStrict) {
		t.Fatalf("an unrelated later write must stale project-wide verification: %+v", c.Obligations)
	}
}

func TestStopRules(t *testing.T) {
	c := New("")
	if c.Stop(StopOptions{}) != StopReady {
		t.Fatal("empty contract is ready")
	}
	c.addObligation(Obligation{Kind: ObligationTargetedVerify, Enforcement: EnforcementAdvisory})
	if c.Stop(StopOptions{}) != StopReady {
		t.Fatal("advisory gaps stay ready")
	}
	c.addObligation(Obligation{Kind: ObligationDiffReview, Enforcement: EnforcementRecoverable})
	if c.Stop(StopOptions{}) != StopContinue {
		t.Fatal("first recoverable miss continues")
	}
	c.NoteRecoveryAttempt()
	if c.Stop(StopOptions{}) != StopPartial {
		t.Fatal("spent recoverable miss is partial")
	}
	strict := New("")
	strict.addObligation(Obligation{Kind: ObligationFullVerify, Enforcement: EnforcementStrict})
	if strict.Stop(StopOptions{}) != StopContinue {
		t.Fatal("strict miss continues")
	}
	if strict.Stop(StopOptions{PermissionDenied: true}) != StopBlocked {
		t.Fatal("strict + permission deny is blocked")
	}
}

func hasKind(obs []Obligation, kind ObligationKind, enf Enforcement) bool {
	for _, o := range obs {
		if o.Kind == kind && o.Enforcement == enf {
			return true
		}
	}
	return false
}
