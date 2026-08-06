package cosplay

import (
	"context"
	"strings"
	"testing"
)

// --- stubs ---

type stubGen struct {
	tests []TestCase
}

func (g stubGen) Generate(ctx context.Context, task Task, code string, n int) []TestCase {
	if len(g.tests) > 0 {
		return g.tests
	}
	// fall back to the offline template generator
	return TemplateGenerator{}.Generate(ctx, task, code, n)
}

// stubRunner passes a test iff the candidate code contains "OK".
type stubRunner struct{}

func (stubRunner) Run(ctx context.Context, cand Candidate, test TestCase) (bool, string, string, error) {
	pass := strings.Contains(cand.Code, "OK")
	return pass, "", "", nil
}

// stubRepair rewrites the candidate to contain "OK" (always fixes).
type stubRepair struct{}

func (stubRepair) Repair(ctx context.Context, task Task, code string, failures []Failure, language string) string {
	return "// FIXED: " + code + "\n// OK"
}

var testTask = Task{Description: "double a number", Language: "python"}

var testTests = []TestCase{
	{ID: "t1", Language: "python", Body: "assert f(1)==2", Source: "generated", Input: "1", Expected: "2"},
	{ID: "t2", Language: "python", Body: "assert f(2)==4", Source: "generated", Input: "2", Expected: "4"},
	{ID: "t3", Language: "python", Body: "assert f(3)==6", Source: "generated", Input: "3", Expected: "6"},
}

// TestVerifyCleanCandidate: a candidate that passes every test is the
// consensus best with no repair rounds.
func TestVerifyCleanCandidate(t *testing.T) {
	v := NewVerifier(stubGen{tests: testTests}, stubRunner{}, stubRepair{})
	rep, err := v.Verify(context.Background(), testTask, Candidate{ID: "c0", Code: "def f(x): return x*2 # OK", Language: "python"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Best.ID != "c0" {
		t.Errorf("best should be the original clean candidate, got %q", rep.Best.ID)
	}
	if rep.PassRate != 1.0 {
		t.Errorf("expected pass rate 1.0, got %v", rep.PassRate)
	}
	if rep.Rounds != 1 {
		t.Errorf("expected 1 round, got %d", rep.Rounds)
	}
	if len(rep.Corrections) != 0 {
		t.Errorf("clean candidate should need no corrections, got %v", rep.Corrections)
	}
}

// TestVerifyRepairLoop: a failing candidate is repaired over rounds and the
// consensus picks the repaired candidate.
func TestVerifyRepairLoop(t *testing.T) {
	v := NewVerifier(stubGen{tests: testTests}, stubRunner{}, stubRepair{})
	rep, err := v.Verify(context.Background(), testTask, Candidate{ID: "c0", Code: "def f(x): return x # broken", Language: "python"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Best.ID == "c0" {
		t.Error("consensus should prefer the repaired candidate over the broken original")
	}
	if !strings.Contains(rep.Best.Code, "OK") {
		t.Error("best candidate should carry the repaired code")
	}
	if len(rep.Corrections) == 0 {
		t.Error("expected at least one repair correction")
	}
	if rep.PassRate != 1.0 {
		t.Errorf("repaired candidate should pass everything, got %v", rep.PassRate)
	}
}

// TestVerifyWithoutRepairer: no Repairer → failures are reported, best stays
// the original, no corrections.
func TestVerifyWithoutRepairer(t *testing.T) {
	v := NewVerifier(stubGen{tests: testTests}, stubRunner{}, nil)
	rep, err := v.Verify(context.Background(), testTask, Candidate{ID: "c0", Code: "def f(x): return x # broken", Language: "python"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Best.ID != "c0" {
		t.Errorf("without a repairer the original candidate stays best, got %q", rep.Best.ID)
	}
	if rep.PassRate != 0 {
		t.Errorf("expected pass rate 0 for a fully failing candidate, got %v", rep.PassRate)
	}
	if len(rep.Corrections) != 0 {
		t.Errorf("no repairer means no corrections, got %v", rep.Corrections)
	}
}

// TestTemplateGeneratorProducesAssertions: the offline generator emits
// example-derived discriminating tests plus smoke fillers.
func TestTemplateGeneratorProducesAssertions(t *testing.T) {
	gen := TemplateGenerator{}
	tests := gen.Generate(context.Background(), Task{
		Description: "double",
		Language:    "python",
		Function:    "f",
		Examples:    []Example{{Input: "21", Expected: "42"}},
	}, "def f(x): return x*2", 4)
	if len(tests) == 0 {
		t.Fatal("expected generated tests")
	}
	first := tests[0]
	if first.ID != "ex-0" {
		t.Errorf("first test should be the example assertion, got %q", first.ID)
	}
	if !strings.Contains(first.Body, "42") {
		t.Errorf("example assertion must embed the expected value: %s", first.Body)
	}
	if len(tests) < 2 {
		t.Error("expected smoke fillers after the example assertion")
	}
}

// TestConsensusRanksAndClusters: pass rate ordering wins; near-equal scores
// collapse into one representative per cluster.
func TestConsensusRanksAndClusters(t *testing.T) {
	m := NewMatrix()
	m.Record("bad", "t1", false)
	m.Record("bad", "t2", false)
	m.Record("good", "t1", true)
	m.Record("good", "t2", true)
	m.Record("great", "t1", true)
	m.Record("great", "t2", true)

	scored := Consensus(m, []Candidate{{ID: "bad"}, {ID: "good"}, {ID: "great"}}, []TestCase{{ID: "t1"}, {ID: "t2"}})
	if len(scored) == 0 {
		t.Fatal("consensus returned nothing")
	}
	if scored[0].Candidate.ID != "good" && scored[0].Candidate.ID != "great" {
		t.Errorf("top cluster representative should be good or great, got %q", scored[0].Candidate.ID)
	}
	if scored[0].PassRate != 1.0 {
		t.Errorf("top representative pass rate should be 1.0, got %v", scored[0].PassRate)
	}
	// bad (0.0) and the good/great cluster (1.0) must not be merged.
	if len(scored) != 2 {
		t.Errorf("expected 2 clusters (bad + good/great), got %d: %+v", len(scored), scored)
	}
}

// TestMatrixBasics: record/lookup/pass-rate/passed-by-any.
func TestMatrixBasics(t *testing.T) {
	m := NewMatrix()
	m.Record("a", "t1", true)
	m.Record("a", "t2", false)
	m.Record("b", "t1", true)
	if !m.PassFor("a", "t1") {
		t.Error("a/t1 should pass")
	}
	if m.PassFor("a", "t2") {
		t.Error("a/t2 should fail")
	}
	if m.PassRate("a") != 0.5 {
		t.Errorf("a pass rate should be 0.5, got %v", m.PassRate("a"))
	}
	if !m.PassedByAny("t1") {
		t.Error("t1 passed by someone")
	}
	if m.PassedByAny("t2") {
		t.Error("t2 passed by nobody")
	}
	if _, ok := m.Lookup("nobody", "t1"); ok {
		t.Error("unknown candidate must not have cells")
	}
}
