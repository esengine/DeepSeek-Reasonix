package cosplay

import (
	"context"
	"strings"
	"testing"
)

// fakeBackend returns canned text; useful to exercise parsing paths.
type fakeBackend struct {
	text string
	err  error
}

func (f fakeBackend) Complete(ctx context.Context, system, user string, maxTokens int) (string, error) {
	return f.text, f.err
}

func TestModelGeneratorParsesJSON(t *testing.T) {
	g := ModelGenerator{Backend: fakeBackend{text: `here you go:
[
  {"input": "1, 2", "expected": "3"},
  {"input": "-1, 1", "expected": "0"},
]
hope this helps`}}
	tests := g.Generate(context.Background(), Task{Description: "add", Language: "go"}, "package add\nfunc Add(a,b int) int{return a+b}", 2)
	if len(tests) != 2 {
		t.Fatalf("want 2 tests, got %d: %+v", len(tests), tests)
	}
	if tests[0].Input != "1, 2" || tests[0].Expected != "3" {
		t.Errorf("test[0] wrong: %+v", tests[0])
	}
	if tests[0].Source != "model" || tests[0].Language != "go" {
		t.Errorf("metadata wrong: %+v", tests[0])
	}
}

func TestModelGeneratorFallsBackOnGarbage(t *testing.T) {
	g := ModelGenerator{Backend: fakeBackend{text: "I refuse to output JSON."}}
	tests := g.Generate(context.Background(), Task{Description: "x", Language: "go"}, "code", 3)
	// Fallback to template generator — must still produce tests.
	if len(tests) == 0 {
		t.Fatal("expected fallback tests, got none")
	}
	if tests[0].Source == "model" {
		t.Errorf("expected template fallback, got %+v", tests[0])
	}
}

func TestModelGeneratorNilBackend(t *testing.T) {
	g := ModelGenerator{}
	tests := g.Generate(context.Background(), Task{Description: "x", Language: "go"}, "code", 2)
	if len(tests) == 0 {
		t.Fatal("nil backend must fall back to template generator")
	}
}

func TestModelRepairerExtractsFencedCode(t *testing.T) {
	r := ModelRepairer{Backend: fakeBackend{text: "Here is the fix:\n```go\npackage add\n\nfunc Add(a, b int) int { return a + b }\n```\nDone."}}
	fixed := r.Repair(context.Background(), Task{Description: "add", Language: "go"},
		"package add\nfunc Add(a,b int) int {return a-b}",
		[]Failure{{TestID: "model-0", Detail: "got -1 want 3"}}, "go")
	if !strings.Contains(fixed, "a + b") {
		t.Errorf("repair did not extract fixed code: %q", fixed)
	}
}

func TestModelRepairerNoFence(t *testing.T) {
	r := ModelRepairer{Backend: fakeBackend{text: "sure, just use a+b instead."}}
	fixed := r.Repair(context.Background(), Task{Description: "add", Language: "go"}, "code", []Failure{{TestID: "t", Detail: "e"}}, "go")
	if fixed != "" {
		t.Errorf("expected empty repair without fenced block, got %q", fixed)
	}
}

func TestModelRepairerSameCode(t *testing.T) {
	r := ModelRepairer{Backend: fakeBackend{text: "```go\ncode\n```"}}
	fixed := r.Repair(context.Background(), Task{Description: "x", Language: "go"}, "code", []Failure{{TestID: "t", Detail: "e"}}, "go")
	if fixed != "" {
		t.Errorf("repair that returns identical code must be rejected, got %q", fixed)
	}
}

func TestExtractFencedCodeMultiline(t *testing.T) {
	raw := "intro\n```python\nx = 1\nprint(x)\n```\noutro"
	got := extractFencedCode(raw)
	if got != "x = 1\nprint(x)" {
		t.Errorf("extractFencedCode = %q", got)
	}
}
