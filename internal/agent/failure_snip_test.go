package agent

import (
	"fmt"
	"strings"
	"testing"
)

func passingLog(n int) []string {
	out := make([]string, 0, n*2)
	for i := range n {
		out = append(out,
			fmt.Sprintf("=== RUN   TestConfigCase%03d", i),
			fmt.Sprintf("--- PASS: TestConfigCase%03d (0.00s)", i))
	}
	return out
}

func TestSnipFailureKeepsTheAssertionAndDropsThePasses(t *testing.T) {
	lines := passingLog(60)
	lines = append(lines,
		"=== RUN   TestQuotedValues",
		"    format_test.go:412: assertion failed: expected beta-7d21, got gamma-4a88",
		"--- FAIL: TestQuotedValues (0.01s)")
	lines = append(lines, passingLog(60)...)
	content := strings.Join(lines, "\n")

	got := snipFailureResult(content)
	if got == content {
		t.Fatal("nothing was elided from a mostly-passing log")
	}
	for _, want := range []string{"beta-7d21", "gamma-4a88", "format_test.go:412"} {
		if !strings.Contains(got, want) {
			t.Errorf("failure detail %q did not survive", want)
		}
	}
	if !strings.Contains(got, "lines omitted") {
		t.Error("no elision marker: the reader cannot tell content was dropped")
	}
	if len(got) >= len(content) {
		t.Errorf("result grew: %d -> %d bytes", len(content), len(got))
	}
	if strings.Count(got, "PASS") > 2*failureContextLines+2 {
		t.Errorf("kept too many passing lines:\n%s", got)
	}
}

// Small results are left alone: there is nothing to win and the elision marker
// would cost more than it saves.
func TestSnipFailureLeavesShortResultsWhole(t *testing.T) {
	content := "error: build failed\n./main.go:12:5: undefined: foo\nexit status 1"
	if got := snipFailureResult(content); got != content {
		t.Errorf("short result rewritten:\n%s", got)
	}
}

// A long result with no recognisable failure line is returned untouched rather
// than emptied — the caller decides what to do with it.
func TestSnipFailureKeepsUnrecognisedOutputWhole(t *testing.T) {
	content := strings.Join(passingLog(40), "\n")
	if got := snipFailureResult(content); got != content {
		t.Errorf("output with no failure markers was rewritten:\n%.200s", got)
	}
}

// keptForProjection runs on every fold, so shortening must reach a fixed point
// on the first pass. A second pass that swallowed the elision marker and
// restated the count would make a twice-folded projection differ byte for byte
// from a once-folded one, breaking the prompt cache for nothing.
func TestSnipFailureIsIdempotent(t *testing.T) {
	allFailures := make([]string, 200)
	for i := range allFailures {
		allFailures[i] = fmt.Sprintf("--- FAIL: TestCase%03d (0.00s)", i)
	}
	mixed := passingLog(60)
	mixed = append(mixed, "    format_test.go:412: assertion failed: expected beta-7d21, got gamma-4a88")
	mixed = append(mixed, passingLog(60)...)

	for name, lines := range map[string][]string{"all failures": allFailures, "one failure": mixed} {
		t.Run(name, func(t *testing.T) {
			once := snipFailureResult(strings.Join(lines, "\n"))
			twice := snipFailureResult(once)
			if once != twice {
				t.Errorf("not a fixed point:\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
			}
		})
	}
}

// A log that is failures throughout must still shrink, or the ceiling does not
// bound anything.
func TestSnipFailureBoundsAnAllFailureLog(t *testing.T) {
	lines := make([]string, 0, 400)
	for i := range 400 {
		lines = append(lines, fmt.Sprintf("--- FAIL: TestCase%03d (0.00s)", i))
	}
	content := strings.Join(lines, "\n")
	got := snipFailureResult(content)
	if got == content {
		t.Fatal("an all-failure log was not bounded")
	}
	if n := strings.Count(got, "FAIL"); n > failureMaxKeptLines {
		t.Errorf("kept %d failure lines, over the %d ceiling", n, failureMaxKeptLines)
	}
}
