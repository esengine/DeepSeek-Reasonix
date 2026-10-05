package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

// requireShellStub skips where a #!/usr/bin/env bash stub cannot be executed.
// Every test that writes its own stub must call it: exec fails silently enough
// on Windows that the assertion which follows blames the code instead.
func requireShellStub(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stub agent is a shell script")
	}
}

// fakeAgent stands in for the reasonix binary: it records every invocation's
// argv and writes the metrics file it was told to, making segment accounting
// verifiable without a provider. The failure it guards — a later leg replacing
// an earlier leg's numbers — is invisible in any single run.
func fakeAgent(t *testing.T, promptTokens, completionTokens int) (bin, argvLog string) {
	t.Helper()
	requireShellStub(t)
	dir := testenv.TempDir(t)
	argvLog = filepath.Join(dir, "argv.log")
	bin = filepath.Join(dir, "fake-agent")
	script := `#!/usr/bin/env bash
printf '%s\n' "$*" >> ` + argvLog + `
metrics=""
while [ $# -gt 0 ]; do
  case "$1" in
    --metrics) metrics="$2"; shift 2;;
    *) shift;;
  esac
done
[ -n "$metrics" ] && cat > "$metrics" <<JSON
{"prompt_tokens":` + strconv.Itoa(promptTokens) + `,"completion_tokens":` + strconv.Itoa(completionTokens) + `,"steps":2,"cost":0.5,"currency":"USD","complete":true,"tool_calls":3}
JSON
exit 0
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, argvLog
}

func TestSegmentedRunAddsEveryLegsSpend(t *testing.T) {
	bin, argvLog := fakeAgent(t, 100, 20)
	cfg := suiteConfig{bin: bin, segments: 3}
	task := task{ID: "seg", Prompt: "fix it", MaxSteps: 9, TimeoutSec: 60}
	var r result
	if err := runSegments(t.Context(), cfg, task, testenv.TempDir(t), "", nil, &r); err != nil {
		t.Fatalf("runSegments: %v", err)
	}

	if r.Segments != 3 {
		t.Fatalf("segments = %d, want 3", r.Segments)
	}
	if r.PromptTokens != 300 || r.CompletionTokens != 60 {
		t.Fatalf("tokens = %d/%d, want 300/60 — a later leg replaced an earlier one instead of adding",
			r.PromptTokens, r.CompletionTokens)
	}
	if r.Steps != 6 || r.ToolCalls != 9 {
		t.Fatalf("steps=%d tools=%d, want 6/9 summed across legs", r.Steps, r.ToolCalls)
	}
	if r.Cost != 1.5 {
		t.Fatalf("cost = %v, want 1.5", r.Cost)
	}
	if r.Currency != "USD" || !r.Complete {
		t.Fatalf("non-additive fields lost: %+v", r.runMetrics)
	}

	argv, err := os.ReadFile(argvLog)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(argv)), "\n")
	if len(lines) != 3 {
		t.Fatalf("invocations = %d, want 3", len(lines))
	}
	if strings.Contains(lines[0], "--continue") {
		t.Fatalf("leg 1 must start a fresh session: %s", lines[0])
	}
	for i, line := range lines[1:] {
		if !strings.Contains(line, "--continue") {
			t.Fatalf("leg %d must resume: %s", i+2, line)
		}
		if strings.Contains(line, "fix it") {
			t.Fatalf("leg %d restated the task: %s", i+2, line)
		}
	}
}

func TestSegmentedRunKeepsPerLegMetricsApart(t *testing.T) {
	bin, _ := fakeAgent(t, 10, 1)
	work := testenv.TempDir(t)
	var r result
	if err := runSegments(t.Context(), suiteConfig{bin: bin, segments: 2}, task{ID: "x", Prompt: "p", MaxSteps: 4, TimeoutSec: 60}, work, "", nil, &r); err != nil {
		t.Fatalf("runSegments: %v", err)
	}
	// Both legs' spend has to survive. One shared path would let the last leg
	// overwrite the first, and the tokens it spent would simply vanish.
	if r.PromptTokens != 20 || r.CompletionTokens != 2 {
		t.Fatalf("folded %d prompt / %d completion tokens, want 20 / 2 from two legs",
			r.PromptTokens, r.CompletionTokens)
	}
}

// The agent lists and reads its own working directory, so the harness may not
// leave its bookkeeping there: one observed run spent a tool call reading the
// benchmark's token counts back out of the work dir.
func TestHarnessMetricsStayOutOfTheWorkDir(t *testing.T) {
	bin, _ := fakeAgent(t, 10, 1)
	work := testenv.TempDir(t)
	var r result
	if err := runSegments(t.Context(), suiteConfig{bin: bin, segments: 2}, task{ID: "x", Prompt: "p", MaxSteps: 4, TimeoutSec: 60}, work, "", nil, &r); err != nil {
		t.Fatalf("runSegments: %v", err)
	}
	entries, err := os.ReadDir(work)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if isHarnessArtifact(e.Name()) {
			t.Errorf("the agent would have seen %s in its own directory", e.Name())
		}
	}
}

func TestSegmentedRunStopsAtTheFirstFailedLeg(t *testing.T) {
	requireShellStub(t)
	dir := testenv.TempDir(t)
	bin := filepath.Join(dir, "failing-agent")
	log := filepath.Join(dir, "calls.log")
	script := "#!/usr/bin/env bash\necho x >> " + log + "\nexit 3\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	var r result
	err := runSegments(t.Context(), suiteConfig{bin: bin, segments: 3}, task{ID: "x", Prompt: "p", MaxSteps: 6, TimeoutSec: 60}, testenv.TempDir(t), "", nil, &r)
	if err == nil {
		t.Fatal("a failed leg must surface as the run's error")
	}
	calls, _ := os.ReadFile(log)
	if got := len(strings.Fields(string(calls))); got != 1 {
		t.Fatalf("invocations = %d, want 1 — resuming a session the child never finished is a different experiment", got)
	}
}

func TestUnsegmentedRunKeepsTheOriginalTrajectoryPath(t *testing.T) {
	if got := lastSegmentTrajectory("t", "task-a", 1); got != filepath.Join("t", "task-a.trajectory.jsonl") {
		t.Fatalf("path = %q, want the unsegmented name", got)
	}
	if got := lastSegmentTrajectory("t", "task-a", 3); got != filepath.Join("t", "task-a.seg3.trajectory.jsonl") {
		t.Fatalf("path = %q, want the last leg's file", got)
	}
	if got := lastSegmentTrajectory("", "task-a", 3); got != "" {
		t.Fatalf("path = %q, want empty when trajectories are off", got)
	}
}
