package taskcontract

import (
	"encoding/json"
	"testing"

	"reasonix/internal/evidence"
)

func TestMapWriterArchitectureWorthyCases(t *testing.T) {
	cross := evidence.ClassifyEffect(evidence.EffectInput{
		ToolName: "edit_file", ActualPaths: []string{"internal/agent/agent.go", "internal/plugin/plugin.go"},
	})
	got := MapWriter(cross, 1, "", false)
	if !hasKind(got.PostSuccess, ObligationIndependentReview, EnforcementRecoverable) {
		t.Fatalf("cross-module write must demand independent review: %+v", got)
	}

	var largePaths []string
	for i := range 8 {
		largePaths = append(largePaths, "internal/agent/file"+string(rune('a'+i))+".go")
	}
	large := evidence.ClassifyEffect(evidence.EffectInput{ToolName: "edit_file", ActualPaths: largePaths})
	got = MapWriter(large, 2, "", false)
	if !hasKind(got.PostSuccess, ObligationIndependentReview, EnforcementRecoverable) {
		t.Fatalf("large-scope write must demand independent review: %+v", got)
	}

	conc := evidence.ClassifyEffect(evidence.EffectInput{
		ToolName: "edit_file", Args: json.RawMessage(`{"path":"internal/agent/mutex.go"}`),
	})
	got = MapWriter(conc, 3, "", false)
	if !hasKind(got.PostSuccess, ObligationIndependentReview, EnforcementRecoverable) {
		t.Fatalf("concurrency path must demand independent review: %+v", got)
	}

	opaqueArch := evidence.ClassifyEffect(evidence.EffectInput{
		ToolName: "mcp__srv__write", ActualPaths: []string{"internal/agent/agent.go", "internal/plugin/host.go"},
	})
	got = MapWriter(opaqueArch, 4, "", false)
	if !hasKind(got.PostSuccess, ObligationIndependentReview, EnforcementRecoverable) &&
		!hasKind(got.PostSuccess, ObligationIndependentReview, EnforcementStrict) {
		t.Fatalf("architecture-worthy opaque write must keep independent review: %+v", got)
	}
}

func TestIndependentReviewCapStopsThirdAutoDemand(t *testing.T) {
	write := evidence.Receipt{
		ToolName: "edit_file", Success: true, Write: true, Mutation: true,
		Args: json.RawMessage(`{"path":"schema/user.proto"}`), Paths: []string{"schema/user.proto"},
	}
	review := func() evidence.Receipt {
		return evidence.Receipt{ToolName: "review_report", Success: true, Args: json.RawMessage(`{
			"kind":"review","verdict":"pass","reviewed_paths":["schema/user.proto"],"findings":[]
		}`)}
	}
	c := Rebuild(RebuildFacts{Receipts: []evidence.Receipt{write, review(), write, review(), write}})
	unsat := 0
	for _, o := range c.Unsatisfied() {
		if o.Kind == ObligationIndependentReview {
			unsat++
		}
	}
	if unsat != 0 {
		t.Fatalf("third architecture write after two successful reviews still demanded independent review: %+v", c.Unsatisfied())
	}
	if c.independentReviews != 2 {
		t.Fatalf("independent review count = %d, want 2", c.independentReviews)
	}
}
