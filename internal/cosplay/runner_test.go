package cosplay

import (
	"context"
	"os/exec"
	"testing"
)

// TestProcessRunnerGoEndToEnd runs the real Go toolchain through the
// template-generated assertion test: a correct candidate passes, a broken
// one fails with the mismatch surfaced.
func TestProcessRunnerGoEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	ctx := context.Background()
	runner := &ProcessRunner{}

	gen := TemplateGenerator{}
	task := Task{
		Description: "double a number",
		Language:    "go",
		Function:    "double",
		Examples:    []Example{{Input: "21", Expected: "42"}},
	}
	tests := gen.Generate(ctx, task, "", 2)

	// Correct candidate: pass.
	okCand := Candidate{ID: "ok", Language: "go", Code: "func double(x int) int { return x * 2 }"}
	pass, got, detail, err := runner.Run(ctx, okCand, tests[0])
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}
	if !pass {
		t.Errorf("correct candidate should pass; got=%q detail=%q", got, detail)
	}

	// Broken candidate: fail with the mismatch visible.
	badCand := Candidate{ID: "bad", Language: "go", Code: "func double(x int) int { return x }"}
	pass, _, _, err = runner.Run(ctx, badCand, tests[0])
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}
	if pass {
		t.Error("broken candidate must fail")
	}
}

// TestProcessRunnerPythonEndToEnd exercises the python path when available.
func TestProcessRunnerPythonEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("python"); err != nil {
		t.Skip("python not on PATH")
	}
	ctx := context.Background()
	runner := &ProcessRunner{}

	gen := TemplateGenerator{}
	task := Task{
		Description: "double a number",
		Language:    "python",
		Function:    "double",
		Examples:    []Example{{Input: "21", Expected: "42"}},
	}
	tests := gen.Generate(ctx, task, "", 2)

	okCand := Candidate{ID: "ok", Language: "python", Code: "def double(x): return x * 2"}
	pass, _, _, err := runner.Run(ctx, okCand, tests[0])
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}
	if !pass {
		t.Error("correct python candidate should pass")
	}

	badCand := Candidate{ID: "bad", Language: "python", Code: "def double(x): return x"}
	pass, _, _, _ = runner.Run(ctx, badCand, tests[0])
	if pass {
		t.Error("broken python candidate must fail")
	}
}

// TestJudgeParsesMarkers covers the output-parsing helper directly.
func TestJudgeParsesMarkers(t *testing.T) {
	cases := []struct {
		out  string
		want bool
	}{
		{"GOT: 42\nPASS\n", true},
		{"GOT: 21\nEXPECTED: 42\n", false},
		{"hello world\n", true}, // no markers → smoke pass
	}
	for _, c := range cases {
		got, _, _, _ := judge(c.out)
		if got != c.want {
			t.Errorf("judge(%q) = %v, want %v", c.out, got, c.want)
		}
	}
}
