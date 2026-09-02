package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/evidence"
)

func TestComposeChildTaskPromptUsesFactsPack(t *testing.T) {
	got := composeChildTaskPrompt(ProfileExecSpec{
		Task: TaskSpec{Objective: "review the gate"},
		Context: ContextRequest{
			Decisions:       []acceptedDecision{{ID: "dec-1", Question: "ship?", Answer: "yes"}},
			EvidenceSummary: "tests passed",
			FileAnchors:     []string{"internal/agent/ask.go"},
			OutputFormat:    "verdict only",
		},
	})
	for _, want := range []string{"## Task", "review the gate", "dec-1", "tests passed", "ask.go", "verdict only", "Do not copy"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestApplyReviewBudgetDefaults(t *testing.T) {
	spec := ProfileExecSpec{Worker: WorkerSpec{Profile: "review"}}
	applyReviewBudget(&spec)
	if spec.Sched.MaxSteps != defaultReviewMaxSteps || spec.Sched.MaxOutputTokens != defaultReviewOutputTokens {
		t.Fatalf("budget = %+v", spec.Sched)
	}
}

func TestFillChildFactsFromParentTurnAndLedger(t *testing.T) {
	turn := &turnRuntime{}
	turn.loop.rememberDecision("dec-9", "ship?", "yes")
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "edit_file", Success: true, Write: true, Mutation: true,
		Paths: []string{"internal/agent/ask.go"},
	})
	ctx := withTurnState(evidence.WithLedger(context.Background(), ledger), turn)
	spec := ProfileExecSpec{Task: TaskSpec{Objective: "review the gate"}}
	fillChildFacts(ctx, &spec)
	if len(spec.Context.Decisions) != 1 || spec.Context.Decisions[0].ID != "dec-9" {
		t.Fatalf("decisions = %+v", spec.Context.Decisions)
	}
	if !strings.Contains(spec.Context.EvidenceSummary, "1 successful") {
		t.Fatalf("summary = %q", spec.Context.EvidenceSummary)
	}
	if len(spec.Context.FileAnchors) != 1 || filepath.ToSlash(spec.Context.FileAnchors[0]) != "internal/agent/ask.go" {
		t.Fatalf("anchors = %v", spec.Context.FileAnchors)
	}
	prompt := composeChildTaskPrompt(spec)
	if strings.Contains(prompt, "parent session") && !strings.Contains(prompt, "Do not copy") {
		t.Fatalf("prompt missing isolation note:\n%s", prompt)
	}
}

func TestChildMaxStepsForSpecStampsReviewOutputBudget(t *testing.T) {
	task := &TaskTool{}
	spec := ProfileExecSpec{Worker: WorkerSpec{Profile: "review"}}
	ctx, steps := task.childMaxStepsForSpec(context.Background(), &spec)
	if steps != defaultReviewMaxSteps {
		t.Fatalf("steps = %d", steps)
	}
	if childOutputBudgetFrom(ctx) != defaultReviewOutputTokens {
		t.Fatalf("output budget = %d", childOutputBudgetFrom(ctx))
	}
	opts := task.subagentOptions(ctx, steps, nil, 0, 1, "", nil)
	if opts.MaxOutputTokens != defaultReviewOutputTokens {
		t.Fatalf("child options max output = %d", opts.MaxOutputTokens)
	}
}

func TestPrepareReviewSubagentContextAddsBoundedVerifiedFacts(t *testing.T) {
	ledger := evidence.NewLedger()
	exit := 0
	ledger.Record(evidence.Receipt{
		ToolName: "go_test", Success: true, Read: true, Paths: []string{"z.go", "a.go"},
		OutputBytes: 42, OutputDigest: "0123456789abcdef", ExitCode: &exit, Verification: evidence.VerificationPassed,
	})
	prompt, steps, tokens, ok := PrepareReviewSubagentContext(evidence.WithLedger(context.Background(), ledger), "review", "review change")
	if !ok || steps != defaultReviewMaxSteps || tokens != defaultReviewOutputTokens {
		t.Fatalf("review budget = ok:%v steps:%d tokens:%d", ok, steps, tokens)
	}
	for _, want := range []string{"tool=go_test", "output_bytes=42", "output_digest=0123456789ab", "verification=passed", "a.go", "verdict"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("review prompt missing %q:\n%s", want, prompt)
		}
	}
	if _, _, _, ok := PrepareReviewSubagentContext(context.Background(), "explore", "look"); ok {
		t.Fatal("non-review profile must retain its existing runner budget")
	}
}
