package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/hook"
)

const reviewHookProviderKind = "cli-review-hook-test"

const reviewHookSecret = "review-hook-secret-contents"

// reviewHookProvider reads secret.txt on its first request and finishes the
// review on the next, recording every request it was sent.
type reviewHookProvider struct {
	mu   sync.Mutex
	reqs []provider.Request
}

func (p *reviewHookProvider) Name() string { return reviewHookProviderKind }

func (p *reviewHookProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	n := len(p.reqs)
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	if n == 1 {
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "review-read", Name: "read_file", Arguments: `{"path":"secret.txt"}`,
		}}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "No issues found."}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

var (
	reviewHookRegister sync.Once
	reviewHookMu       sync.Mutex
	reviewHookCurrent  *reviewHookProvider
)

// TestReviewCommandRunsOnlyUserHooks holds the review command's hook scope: the
// user's global PreToolUse deny covers the review's reads, while a hook the
// reviewed checkout configures never runs. Each run fires under its own id.
func TestReviewCommandRunsOnlyUserHooks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the hooks under test are POSIX shell scripts")
	}
	home := isolateCLIConfigHome(t)
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	reviewHookRegister.Do(func() {
		provider.Register(reviewHookProviderKind, func(provider.Config) (provider.Provider, error) {
			reviewHookMu.Lock()
			defer reviewHookMu.Unlock()
			return reviewHookCurrent, nil
		})
	})

	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write(filepath.Join(dir, "secret.txt"), reviewHookSecret+"\n")
	write(filepath.Join(dir, "main.go"), "package main\n")
	git("init", "-q")
	git("add", "main.go")
	git("commit", "-q", "-m", "init")
	write(filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")
	write(filepath.Join(dir, "reasonix.toml"), `
default_model = "reviewer"

[[providers]]
name = "reviewer"
kind = "`+reviewHookProviderKind+`"
model = "x"
base_url = "http://127.0.0.1:1"
`)
	globalLog := filepath.Join(home, "global-hook.log")
	globalScript := filepath.Join(home, "deny-read.sh")
	write(globalScript, "#!/bin/sh\ncat >> '"+globalLog+"'\necho >> '"+globalLog+"'\necho 'reading secrets is not allowed' >&2\nexit 2\n")
	if err := hook.Save(hook.ScopeGlobal, "", hook.Settings{Hooks: map[hook.Event][]hook.HookConfig{
		hook.PreToolUse: {{Match: "read_file", Command: globalScript}},
	}}); err != nil {
		t.Fatal(err)
	}
	projectLog := filepath.Join(home, "project-hook.log")
	projectScript := filepath.Join(dir, "checkout-hook.sh")
	write(projectScript, "#!/bin/sh\ncat >> '"+projectLog+"'\nexit 0\n")
	projectHooks := hook.Settings{Hooks: map[hook.Event][]hook.HookConfig{
		hook.PreToolUse: {{Match: "*", Command: projectScript}},
	}}
	if err := hook.Save(hook.ScopeProject, dir, projectHooks); err != nil {
		t.Fatal(err)
	}
	// Approved, so what keeps it out of the review is the review, not the gate.
	approveWorkspace(t, dir)
	if err := hook.ApproveSavedProjectHooks(hook.LoadOptions{ProjectRoot: dir}, projectHooks); err != nil {
		t.Fatal(err)
	}

	var sessions []string
	for range 2 {
		prov := &reviewHookProvider{}
		reviewHookMu.Lock()
		reviewHookCurrent = prov
		reviewHookMu.Unlock()
		var rc int
		captureStdout(t, func() { rc = reviewCommand(nil) })
		if rc != 0 {
			t.Fatalf("reviewCommand rc = %d, want 0", rc)
		}
		assertReviewReadBlocked(t, prov)
		sessions = reviewHookSessions(t, globalLog)
	}
	if _, err := os.Stat(projectLog); !os.IsNotExist(err) {
		t.Fatalf("the reviewed checkout's hook ran (stat err = %v)", err)
	}
	if len(sessions) != 2 || sessions[0] == sessions[1] {
		t.Fatalf("review hook sessions = %q, want one distinct id per run", sessions)
	}
}

func assertReviewReadBlocked(t *testing.T, prov *reviewHookProvider) {
	t.Helper()
	prov.mu.Lock()
	reqs := append([]provider.Request(nil), prov.reqs...)
	prov.mu.Unlock()
	if len(reqs) < 2 {
		t.Fatalf("review made %d request(s), want the read and its follow-up", len(reqs))
	}
	var results []string
	for _, m := range reqs[1].Messages {
		if m.Role == provider.RoleTool {
			results = append(results, m.Content)
		}
	}
	if len(results) != 1 || !strings.HasPrefix(results[0], "blocked:") {
		t.Fatalf("review read_file result = %q, want a PreToolUse block", results)
	}
	for _, req := range reqs {
		for _, m := range req.Messages {
			if strings.Contains(m.Content, reviewHookSecret) {
				t.Fatal("the review model received the file a PreToolUse hook denied")
			}
		}
	}
}

func reviewHookSessions(t *testing.T, logPath string) []string {
	t.Helper()
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("the global PreToolUse hook never ran for the review's read_file: %v", err)
	}
	var out []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var p hook.Payload
		if err := json.Unmarshal([]byte(line), &p); err != nil {
			t.Fatalf("decode hook payload %q: %v", line, err)
		}
		if p.ToolName != "read_file" || p.SessionID == "" {
			t.Fatalf("hook payload = %+v, want read_file under a session id", p)
		}
		out = append(out, p.SessionID)
	}
	return out
}
