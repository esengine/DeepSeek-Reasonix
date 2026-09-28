package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/hook"
	"reasonix/internal/provider"
)

const reviewHookProbeKind = "cli-review-hook-probe"

var reviewHookProbe = &reviewHookProbeProvider{}

func init() {
	provider.Register(reviewHookProbeKind, func(provider.Config) (provider.Provider, error) {
		return reviewHookProbe, nil
	})
}

// A review runs in a checkout under review, so only the user's own hooks may
// fire: the global deny governs the review subagent, the project hook never runs.
func TestReviewCommandRunsOnlyUserHooks(t *testing.T) {
	isolateCLIConfigHome(t)
	dir := setupReviewCheckout(t, nil)
	scripts := t.TempDir()
	globalLog := filepath.Join(scripts, "global.log")
	projectLog := filepath.Join(scripts, "project.log")
	globalHook := writeReviewHookScript(t, scripts, "global-deny.sh", globalLog, 2)
	projectHook := writeReviewHookScript(t, scripts, "project-log.sh", projectLog, 0)
	writeReviewHookSettings(t, hook.GlobalSettingsPath(""), globalHook)
	writeReviewHookSettings(t, hook.ProjectSettingsPath(dir), projectHook)

	var sessions []string
	for run := range 2 {
		reviewHookProbe.reset()
		captureStdout(t, func() {
			if rc := reviewCommand(nil); rc != 0 {
				t.Fatalf("reviewCommand rc = %d, want 0", rc)
			}
		})
		if !reviewHookProbe.ran() {
			t.Fatal("review subagent never reached the provider")
		}
		if _, err := os.Stat(projectLog); !os.IsNotExist(err) {
			t.Fatalf("the reviewed checkout's project hook ran (stat err = %v)", err)
		}
		if reviewHookProbe.readLeaked() {
			t.Fatal("review subagent received marker.txt contents past the global PreToolUse deny")
		}
		if !reviewHookProbe.readBlocked() {
			t.Fatal("review subagent's read_file result was not blocked by the global PreToolUse hook")
		}
		payloads := readReviewHookLog(t, globalLog)
		if len(payloads) != run+1 {
			t.Fatalf("global hook ran %d times after %d reviews, want one per review", len(payloads), run+1)
		}
		p := payloads[run]
		if p.ToolName != "read_file" || !strings.HasPrefix(p.SessionID, "review:") || p.SessionID == "review:" {
			t.Fatalf("hook payload = %+v, want read_file under a per-run review session", p)
		}
		sessions = append(sessions, p.SessionID)
	}
	if sessions[0] == sessions[1] {
		t.Fatalf("two review runs shared hook session %q", sessions[0])
	}
}

// The reviewed checkout's reasonix.toml must not choose the interpreter hooks
// run under: on Windows resolving a configured bash probes it, then spawns
// every bash hook with it, outside the sandbox.
func TestReviewHookRunnerTakesShellFromUserConfigOnly(t *testing.T) {
	isolateCLIConfigHome(t)
	userShell := filepath.Join(t.TempDir(), "user-shell")
	writeReviewTestFile(t, filepath.Dir(config.UserConfigPath()), filepath.Base(config.UserConfigPath()),
		fmt.Sprintf("[tools.shell]\nprefer = \"powershell\"\npath = %q\n", userShell))
	var evilBash string
	setupReviewCheckout(t, func(dir string) string {
		evilBash = filepath.Join(dir, "evil-bash")
		return fmt.Sprintf("\n[tools.shell]\nprefer = \"bash\"\npath = %q\n", evilBash)
	})

	var got []config.ShellConfig
	var loads []hook.LoadOptions
	prev := newCommandHookRunner
	t.Cleanup(func() { newCommandHookRunner = prev })
	newCommandHookRunner = func(shell config.ShellConfig, load hook.LoadOptions, warn io.Writer) *hook.Runner {
		got = append(got, shell)
		loads = append(loads, load)
		return prev(shell, load, warn)
	}

	reviewHookProbe.reset()
	captureStdout(t, func() {
		if rc := reviewCommand(nil); rc != 0 {
			t.Fatalf("reviewCommand rc = %d, want 0", rc)
		}
	})
	if len(got) != 1 {
		t.Fatalf("hook runner built %d times, want 1", len(got))
	}
	want := config.ShellConfig{Prefer: "powershell", Path: userShell}
	if got[0] != want {
		t.Fatalf("review hook runner shell = %+v, want the user's %+v (checkout configured %q)", got[0], want, evilBash)
	}
	if !loads[0].SkipProject {
		t.Fatalf("review hook runner load = %+v, want SkipProject", loads[0])
	}
}

// Review hooks run with the checkout as cwd, and cmd.exe resolves a bare
// `python` against the cwd first, so the checkout could ship the interpreter.
func TestReviewHooksNeverResolveCommandsAgainstTheCheckout(t *testing.T) {
	isolateCLIConfigHome(t)
	dir := setupReviewCheckout(t, nil)
	writeReviewHookSettings(t, hook.GlobalSettingsPath(""), "python guard.py")

	var mu sync.Mutex
	var spawned []hook.SpawnInput
	record := func(_ context.Context, in hook.SpawnInput) hook.SpawnResult {
		mu.Lock()
		defer mu.Unlock()
		spawned = append(spawned, in)
		return hook.SpawnResult{}
	}
	prev := newCommandHookRunner
	t.Cleanup(func() { newCommandHookRunner = prev })
	newCommandHookRunner = func(_ config.ShellConfig, load hook.LoadOptions, _ io.Writer) *hook.Runner {
		return hook.NewRunner(hook.Load(load), load.ProjectRoot, record, nil)
	}

	reviewHookProbe.reset()
	captureStdout(t, func() {
		if rc := reviewCommand(nil); rc != 0 {
			t.Fatalf("reviewCommand rc = %d, want 0", rc)
		}
	})
	mu.Lock()
	defer mu.Unlock()
	if len(spawned) == 0 {
		t.Fatal("the user's PreToolUse hook never spawned for the review subagent")
	}
	for _, in := range spawned {
		if got := in.Env[hook.NoCwdCommandSearchEnv]; got != "1" {
			t.Fatalf("review hook %q spawned in %s with %s=%q, want 1", in.Command, in.Cwd, hook.NoCwdCommandSearchEnv, got)
		}
		if !sameReviewPath(in.Cwd, dir) {
			t.Fatalf("review hook cwd = %q, want the checkout %q", in.Cwd, dir)
		}
	}
}

func sameReviewPath(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

func setupReviewCheckout(t *testing.T, extraTOML func(dir string) string) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
	} {
		runReviewTestGit(t, dir, args...)
	}
	writeReviewTestFile(t, dir, "marker.txt", "review hook probe\n")
	runReviewTestGit(t, dir, "add", "marker.txt")
	runReviewTestGit(t, dir, "commit", "-q", "-m", "init")
	writeReviewTestFile(t, dir, "marker.txt", "review hook probe\nchanged\n")
	extra := ""
	if extraTOML != nil {
		extra = extraTOML(dir)
	}
	writeReviewTestFile(t, dir, "reasonix.toml", `
default_model = "reviewer"

[[providers]]
name = "reviewer"
kind = "`+reviewHookProbeKind+`"
model = "review-model"
base_url = "http://127.0.0.1:1"
`+extra)
	approveWorkspace(t, dir)
	return dir
}

type reviewHookPayload struct{ ToolName, SessionID string }

func writeReviewHookScript(t *testing.T, dir, name, logPath string, exit int) string {
	t.Helper()
	body := fmt.Sprintf("#!/bin/sh\ncat >> '%s'\nexit %d\n", strings.ReplaceAll(logPath, "'", `'\''`), exit)
	writeReviewTestFile(t, dir, name, body)
	path := filepath.Join(dir, name)
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeReviewHookSettings(t *testing.T, path, command string) {
	t.Helper()
	settings, err := json.Marshal(map[string]any{"hooks": map[string]any{"PreToolUse": []any{map[string]string{"match": "read_file", "command": command}}}})
	if err != nil {
		t.Fatal(err)
	}
	writeReviewTestFile(t, filepath.Dir(path), filepath.Base(path), string(settings))
}

func readReviewHookLog(t *testing.T, path string) []reviewHookPayload {
	t.Helper()
	log, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("global PreToolUse hook never ran for the review subagent's read_file: %v", err)
	}
	var out []reviewHookPayload
	for line := range strings.SplitSeq(strings.TrimSpace(string(log)), "\n") {
		var p reviewHookPayload
		if err := json.Unmarshal([]byte(line), &p); err != nil {
			t.Fatalf("decode hook payload %q: %v", line, err)
		}
		out = append(out, p)
	}
	return out
}

func runReviewTestGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeReviewTestFile(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

type reviewHookProbeProvider struct {
	mu      sync.Mutex
	calls   int
	blocked bool
	leaked  bool
}

func (p *reviewHookProbeProvider) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls, p.blocked, p.leaked = 0, false, false
}

func (p *reviewHookProbeProvider) ran() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls > 0
}

func (p *reviewHookProbeProvider) readBlocked() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.blocked
}

func (p *reviewHookProbeProvider) readLeaked() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.leaked
}

func (p *reviewHookProbeProvider) Name() string { return reviewHookProbeKind }

func (p *reviewHookProbeProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	call := p.calls
	p.calls++
	for _, msg := range req.Messages {
		if msg.Role != provider.RoleTool || msg.Name != "read_file" {
			continue
		}
		if strings.Contains(msg.Content, "blocked:") {
			p.blocked = true
		}
		if strings.Contains(msg.Content, "review hook probe") {
			p.leaked = true
		}
	}
	p.mu.Unlock()

	chunks := []provider.Chunk{{Type: provider.ChunkText, Text: "no findings"}, {Type: provider.ChunkDone}}
	if call == 0 {
		chunks = []provider.Chunk{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "review-read", Name: "read_file", Arguments: `{"path":"marker.txt"}`}}}
	}
	ch := make(chan provider.Chunk, len(chunks))
	for _, chunk := range chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}
