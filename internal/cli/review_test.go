package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/skill"
)

func TestBuildReviewTask(t *testing.T) {
	// Small diff.
	diff := "diff --git a/foo.go b/foo.go\n+added line"
	got := buildReviewTask(diff, "")
	if !strings.Contains(got, "Review the following changes.") {
		t.Error("missing review prompt prefix")
	}
	if !strings.Contains(got, diff) {
		t.Errorf("diff content missing:\n%s", got)
	}

	// With extra instructions.
	got = buildReviewTask(diff, "focus on error handling")
	if !strings.Contains(got, "focus on error handling") {
		t.Error("extra instructions missing")
	}
	if !strings.Contains(got, "The diff is:") {
		t.Error("missing diff separator")
	}

	// Truncation.
	hugeDiff := strings.Repeat("x", 20000)
	got = buildReviewTask(hugeDiff, "")
	if !strings.Contains(got, "truncated at 16000") {
		t.Error("large diff should be truncated")
	}
	if len(got) > 16500 {
		t.Errorf("truncated output too long: %d", len(got))
	}
}

func TestBuildReviewSubagentRegistryUsesForegroundOnlyBash(t *testing.T) {
	reg := buildReviewSubagentRegistry(skill.Skill{AllowedTools: []string{
		"bash",
		"wait",
		"bash_output",
		"kill_shell",
		"task",
	}}, config.Default(), t.TempDir())

	for _, hidden := range []string{"wait", "bash_output", "kill_shell", "task"} {
		if _, ok := reg.Get(hidden); ok {
			t.Fatalf("review subagent registry should hide %q; got %v", hidden, reg.Names())
		}
	}
	bash, ok := reg.Get("bash")
	if !ok {
		t.Fatalf("review subagent registry should keep bash; got %v", reg.Names())
	}
	if strings.Contains(string(bash.Schema()), "run_in_background") {
		t.Fatalf("review subagent bash schema should not include run_in_background: %s", bash.Schema())
	}
	if _, err := bash.Execute(context.Background(), json.RawMessage(`{"command":"sleep 1","run_in_background":true}`)); err == nil || !strings.Contains(err.Error(), "background bash is unavailable in subagents") {
		t.Fatalf("review subagent background bash should return a clear error, got %v", err)
	}
}

// TestBuildReviewSubagentRegistryConfinesReaders pins the sandbox contract:
// the CLI review registry must honor the user's [sandbox] forbid_read config
// exactly like an in-session registry. The zero-value readers registered at
// init are unconfined, so before this the review subagent could read paths a
// normal session refuses.
func TestBuildReviewSubagentRegistryConfinesReaders(t *testing.T) {
	root := t.TempDir()
	secret := filepath.Join(root, "secrets")
	if err := os.MkdirAll(secret, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secret, "token.txt"), []byte("hunter2"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Sandbox.ForbidRead = []string{secret}

	reg := buildReviewSubagentRegistry(skill.Skill{
		ReadOnly:     true,
		AllowedTools: []string{"read_file"},
	}, cfg, root)

	rf, ok := reg.Get("read_file")
	if !ok {
		t.Fatalf("read_file missing; got %v", reg.Names())
	}
	out, err := rf.Execute(context.Background(), json.RawMessage(`{"path":`+strconv.Quote(filepath.Join(secret, "token.txt"))+`}`))
	if err == nil && strings.Contains(out, "hunter2") {
		t.Fatalf("forbid_read path was readable through the review registry: %s", out)
	}
	if err == nil {
		t.Fatalf("expected a not-exist style refusal, got output: %s", out)
	}
}

// TestBuildReviewSubagentRegistryEnforcesReadOnlySkill pins the CLI path of the
// review read-only contract: `reasonix review` runs the same builtin skill as
// the in-session review tool, so its bash must enforce the read-only
// policy instead of trusting the prompt's "stay read-only" promise.
func TestBuildReviewSubagentRegistryEnforcesReadOnlySkill(t *testing.T) {
	reg := buildReviewSubagentRegistry(skill.Skill{
		ReadOnly:     true,
		AllowedTools: []string{"bash", "read_file", "task"},
	}, config.Default(), t.TempDir())

	if _, ok := reg.Get("task"); ok {
		t.Fatalf("read-only review registry should hide task; got %v", reg.Names())
	}
	bash, ok := reg.Get("bash")
	if !ok {
		t.Fatalf("read-only review registry should keep bash; got %v", reg.Names())
	}
	if !bash.ReadOnly() {
		t.Fatal("read-only review bash wrapper must report ReadOnly")
	}
	out, err := bash.Execute(context.Background(), json.RawMessage(`{"command":"rm -rf tmp"}`))
	if err != nil || !strings.HasPrefix(out, "blocked:") {
		t.Fatalf("write-capable command should be blocked as tool output, got %q, %v", out, err)
	}
}
