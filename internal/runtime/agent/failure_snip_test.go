package agent

import (
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
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
	// A real failing run ends with its verdict, which is what the position
	// floor is there to keep.
	lines = append(lines, "FAIL", "FAIL\tprobe/config\t0.42s", "FAIL")
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
	if !strings.Contains(got, "FAIL\tprobe/config") {
		t.Errorf("the run's verdict did not survive:\n%s", got)
	}
	// The passes that survive are the ones the head/tail floor pays for, not a
	// window that grew with the log: 240 passing lines still collapse.
	if maxPasses := failureHeadLines + failureTailLines + 2*failureContextLines; strings.Count(got, "PASS") > maxPasses {
		t.Errorf("kept too many passing lines (>%d):\n%s", maxPasses, got)
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

// Output carrying no recognised word is still shortened to its ends. Returning
// it whole would make the word list the switch that decides whether a failure
// is compacted at all — a log whose wording is not on the list would ride into
// every later fold in full.
func TestSnipFailureShortensUnrecognisedOutputToItsEnds(t *testing.T) {
	lines := passingLog(40)
	content := strings.Join(lines, "\n")
	got := snipFailureResult(content)
	if got == content {
		t.Fatal("an unrecognised long result was kept whole")
	}
	if !strings.Contains(got, "lines omitted") {
		t.Error("no elision marker: the reader cannot tell content was dropped")
	}
	if !strings.Contains(got, lines[0]) {
		t.Error("the head did not survive")
	}
	if last := lines[len(lines)-1]; !strings.Contains(got, last) {
		t.Errorf("the tail did not survive: %q", last)
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

// The word list is a supplement, not the gate. One unrelated line carrying a
// listed word ("the error handling code runs after…") is enough to switch
// snipping on; before the position floor, that stray hit was kept while the
// run's actual verdict — worded in a shape no list carries — was elided.
func TestSnipFailureKeepsTheVerdictWhenOnlyProseMatchedTheWordList(t *testing.T) {
	lines := padded("[INFO] checked file ", 20)
	lines = append(lines, "note: the error handling code runs after the retry loop")
	lines = append(lines, padded("[INFO] checked file ", 20)...)
	lines = append(lines, "[ERR] handlers.go:88 unchecked return value")
	lines = append(lines, padded("[INFO] checked file ", 10)...)
	content := strings.Join(lines, "\n")

	got := snipFailureResult(content)
	if got == content {
		t.Fatal("nothing was elided")
	}
	if !strings.Contains(got, "[ERR] handlers.go:88") {
		t.Errorf("the real failure was elided while the prose hit survived:\n%s", got)
	}
}

// The tail floor follows what the runner recorded as the diagnosis rather than
// a fixed guess: OutputTail is written by the code that watched the process
// exit, and only on failure.
func TestSnipFailureTailFollowsTheRecordedDiagnosis(t *testing.T) {
	code := 1
	long := strings.Join(padded("stack frame ", 30), "\n")
	cases := []struct {
		name string
		ex   *provider.ToolExecution
		want int
	}{
		{"no execution record", nil, failureTailLines},
		{"exited zero, verification failed", &provider.ToolExecution{Verification: tool.ShellVerificationFailed}, failureTailLines},
		{"short tail keeps the floor", &provider.ToolExecution{OutputTail: "FAIL\tpkg\t0.4s"}, failureTailLines},
		{"long tail widens the floor", &provider.ToolExecution{OutputTail: long}, 30},
		{"tail beyond the ceiling is bounded", &provider.ToolExecution{OutputTail: strings.Join(padded("f ", 500), "\n")}, failureMaxKeptLines},
	}
	for _, tc := range cases {
		if tc.ex != nil {
			tc.ex.ExitCode = &code
		}
		if got := recordedTailLines(tc.ex); got != tc.want {
			t.Errorf("%s: tail lines = %d, want %d", tc.name, got, tc.want)
		}
	}
}
