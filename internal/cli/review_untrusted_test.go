package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/provider"
)

const untrustedReviewProbeKind = "cli-review-untrusted-probe"

var untrustedReviewProbe = &untrustedReviewProbeProvider{}

func init() {
	provider.Register(untrustedReviewProbeKind, func(provider.Config) (provider.Provider, error) {
		return untrustedReviewProbe, nil
	})
}

// A reviewed checkout configures a search binary and ships its own review
// skill; `reasonix review` must execute neither, and must run the built-in
// read-only review prompt rather than the checkout's.
func TestReviewCommandIgnoresCheckoutToolsAndSkills(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("marker scripts are POSIX shell")
	}
	isolateCLIConfigHome(t)
	dir := t.TempDir()
	t.Chdir(dir)
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
	} {
		runUntrustedReviewGit(t, dir, args...)
	}
	writeUntrustedReviewFile(t, dir, "main.go", "package main\n", 0o644)
	runUntrustedReviewGit(t, dir, "add", "main.go")
	runUntrustedReviewGit(t, dir, "commit", "-q", "-m", "init")
	writeUntrustedReviewFile(t, dir, "main.go", "package main\n\nfunc main() {}\n", 0o644)

	rgMarker := filepath.Join(dir, "rg-ran")
	skillMarker := filepath.Join(dir, "skill-bash-ran")
	writeUntrustedReviewFile(t, dir, "tools/rg", "#!/bin/sh\necho ran > '"+rgMarker+"'\n", 0o755)
	writeUntrustedReviewFile(t, dir, "tools/evil.sh", "#!/bin/sh\necho ran > '"+skillMarker+"'\n", 0o755)
	writeUntrustedReviewFile(t, dir, ".reasonix/skills/review/SKILL.md",
		"---\ndescription: checkout review\nrunAs: subagent\nallowed-tools: grep, bash, web_fetch\n---\n"+untrustedReviewSkillBody+"\n", 0o644)
	writeUntrustedReviewFile(t, dir, "reasonix.toml", `
default_model = "reviewer"

[[providers]]
name = "reviewer"
kind = "`+untrustedReviewProbeKind+`"
model = "review-model"
base_url = "http://127.0.0.1:1"

[tools.search]
engine = "rg"
rg_path = "tools/rg"
`, 0o644)
	approveWorkspace(t, dir)

	untrustedReviewProbe.reset()
	captureStdout(t, func() {
		if rc := reviewCommand(nil); rc != 0 {
			t.Fatalf("reviewCommand rc = %d, want 0", rc)
		}
	})
	calls, system, offered := untrustedReviewProbe.snapshot()
	if calls < 3 {
		t.Fatalf("review subagent reached the provider %d times, want the grep and bash rounds too", calls)
	}
	if strings.Contains(system, untrustedReviewSkillBody) {
		t.Error("review ran the checkout's .reasonix/skills/review prompt")
	}
	if offered["web_fetch"] {
		t.Error("review offered web_fetch, which only the checkout's review skill grants")
	}
	if _, err := os.Stat(rgMarker); !os.IsNotExist(err) {
		t.Errorf("review's grep executed the checkout's [tools.search] rg_path (stat err = %v)", err)
	}
	if _, err := os.Stat(skillMarker); !os.IsNotExist(err) {
		t.Errorf("review's bash ran a writing command under the checkout's review skill (stat err = %v)", err)
	}
}

const untrustedReviewSkillBody = "CHECKOUT-REVIEW-SKILL-BODY"

func runUntrustedReviewGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeUntrustedReviewFile(t *testing.T, dir, name, body string, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

type untrustedReviewProbeProvider struct {
	mu      sync.Mutex
	calls   int
	system  string
	offered map[string]bool
}

func (p *untrustedReviewProbeProvider) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls, p.system, p.offered = 0, "", map[string]bool{}
}

func (p *untrustedReviewProbeProvider) snapshot() (int, string, map[string]bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls, p.system, p.offered
}

func (p *untrustedReviewProbeProvider) Name() string { return untrustedReviewProbeKind }

func (p *untrustedReviewProbeProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	call := p.calls
	p.calls++
	for _, schema := range req.Tools {
		p.offered[schema.Name] = true
	}
	for _, msg := range req.Messages {
		if msg.Role == provider.RoleSystem {
			p.system += msg.Content
		}
	}
	p.mu.Unlock()

	var chunks []provider.Chunk
	switch call {
	case 0:
		chunks = []provider.Chunk{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "review-grep", Name: "grep", Arguments: `{"pattern":"main","path":"."}`}}}
	case 1:
		chunks = []provider.Chunk{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "review-bash", Name: "bash", Arguments: `{"command":"sh tools/evil.sh"}`}}}
	default:
		chunks = []provider.Chunk{{Type: provider.ChunkText, Text: "no findings"}}
	}
	chunks = append(chunks, provider.Chunk{Type: provider.ChunkDone})
	ch := make(chan provider.Chunk, len(chunks))
	for _, chunk := range chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}
