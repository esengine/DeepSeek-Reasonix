package shellparse

import (
	"strings"
	"testing"
)

func TestPipelineForStatusReadsAPlainPipeline(t *testing.T) {
	stages, ok := PipelineForStatus("go test ./... 2>&1 | tail -5", 2)
	if !ok {
		t.Fatal("a plain pipeline must decompose")
	}
	if len(stages) != 2 || !strings.HasPrefix(stages[0], "go test ./...") || stages[1] != "tail -5" {
		t.Fatalf("stages = %q", stages)
	}

	if stages, ok = PipelineForStatus("go test ./... | grep -v '^ok' | head -20", 3); !ok || len(stages) != 3 {
		t.Fatalf("three-stage pipeline = %q, ok=%v", stages, ok)
	}
}

// The shape models actually write. Either side of `&&` can be the last thing to
// run, but they are one and two stages wide, so the status count says which.
func TestPipelineForStatusResolvesByWidth(t *testing.T) {
	const command = "go vet ./... && go test ./... 2>&1 | tail -5"

	stages, ok := PipelineForStatus(command, 2)
	if !ok || len(stages) != 2 || !strings.HasPrefix(stages[0], "go test ./...") {
		t.Fatalf("two statuses = %q, ok=%v; want the suite's pipeline", stages, ok)
	}

	// One status means `go vet` failed and the pipeline never ran.
	stages, ok = PipelineForStatus(command, 1)
	if !ok || len(stages) != 1 || !strings.HasPrefix(stages[0], "go vet") {
		t.Fatalf("one status = %q, ok=%v; want the short-circuited left side", stages, ok)
	}
}

// Only the last `;`-separated statement can be the last to run.
func TestPipelineForStatusTakesTheFinalStatement(t *testing.T) {
	stages, ok := PipelineForStatus("go build ./...; go test ./... | tail", 2)
	if !ok || len(stages) != 2 || !strings.HasPrefix(stages[0], "go test") {
		t.Fatalf("stages = %q, ok=%v", stages, ok)
	}
}

// Two pipelines of the same width could each have produced the statuses, and
// attributing them to the wrong one would credit a command that never ran.
func TestPipelineForStatusRefusesEqualWidths(t *testing.T) {
	if stages, ok := PipelineForStatus("cat a | tail && go test ./... | tail", 2); ok {
		t.Fatalf("ambiguous widths decomposed to %q, want rejected", stages)
	}
}

func TestPipelineForStatusRejectsShapesItCannotDecide(t *testing.T) {
	for _, tc := range []struct {
		command   string
		statusLen int
	}{
		{"go test ./... | tail -5 &", 2},
		{"! go test ./... | tail", 2},
		{"", 2},
		{"go test ./... | tail", 0},
		{"go test ./... | tail", 3}, // no candidate of that width
	} {
		if stages, ok := PipelineForStatus(tc.command, tc.statusLen); ok {
			t.Errorf("%q with %d statuses decomposed to %q, want rejected", tc.command, tc.statusLen, stages)
		}
	}
}
