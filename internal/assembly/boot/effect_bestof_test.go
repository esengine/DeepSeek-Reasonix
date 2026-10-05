package boot

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/platform/gitcmd"
)

const candidateTask = "CANDIDATE-TASK: create out.txt"

// bestOfScriptProvider plays every model in the run: the session asks for
// best_of_n, each attempt writes out.txt, and a request without tools is the
// judge, which picks attempt 1.
type bestOfScriptProvider struct {
	mu   sync.Mutex
	reqs []provider.Request
}

func (p *bestOfScriptProvider) Name() string { return "boot-bestof-script" }

func firstUser(req provider.Request) string {
	for _, m := range req.Messages {
		if m.Role == provider.RoleUser {
			return m.Content
		}
	}
	return ""
}

func hasToolResult(req provider.Request) bool {
	for _, m := range req.Messages {
		if m.Role == provider.RoleTool {
			return true
		}
	}
	return false
}

func (p *bestOfScriptProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	switch {
	case len(req.Tools) == 0:
		ch <- provider.Chunk{Type: provider.ChunkText, Text: `{"winner": 1, "reason": "both work; the first is fine"}`}
	case hasToolResult(req):
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	case strings.Contains(firstUser(req), candidateTask):
		args, _ := json.Marshal(map[string]string{"path": "out.txt", "content": "made by an attempt\n"})
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "w1", Name: "write_file", Arguments: string(args)}}
	default:
		args, _ := json.Marshal(map[string]any{"prompt": candidateTask, "n": 2})
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "b1", Name: "best_of_n", Arguments: string(args)}}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *bestOfScriptProvider) requests() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]provider.Request(nil), p.reqs...)
}

func gitInDir(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := gitcmd.Command(context.Background(), dir, args...)
	cmd.Env = append(cmd.Env, "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// best_of_n runs each attempt in its own kernel at its own worktree: the
// attempts' writes never reach the workspace, the judged winner does, the
// attempts cannot fan out again, and no worktree is left behind.
func TestEffectBestOfRunsAttemptsInWorktreesAndLandsTheWinner(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	rec := &bestOfScriptProvider{}
	provider.Register("boot-bestof", func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"
tool_approval = "yolo"

[agent]
system_prompt = "BASE"
best_of_n = true

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "boot-bestof"
model = "x"
`)
	gitInDir(t, dir, "init", "-q")
	// Attempts run in worktrees, each a folder of its own: the model is the
	// user's, so an approval of this folder is not what makes them run.
	mirrorToUserConfig(t, dir)
	gitInDir(t, dir, "add", "-A")
	gitInDir(t, dir, "commit", "-q", "-m", "init")

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := ctrl.Run(context.Background(), "make out.txt, trying a few ways"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	ctrl.Close()

	var session, attempts []provider.Request
	for _, req := range agentRequests(rec.requests()) {
		if strings.Contains(firstUser(req), candidateTask) {
			attempts = append(attempts, req)
		} else {
			session = append(session, req)
		}
	}
	if !toolNames(session[0])["best_of_n"] {
		t.Fatalf("best_of_n missing from the session's schema: %v", toolSchemaNames(session[0].Tools))
	}
	if len(attempts) != 4 {
		t.Fatalf("attempt requests = %d, want 2 attempts × 2 rounds", len(attempts))
	}
	for _, req := range attempts {
		if toolNames(req)["best_of_n"] {
			t.Fatal("an attempt was offered best_of_n")
		}
	}
	body, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	if err != nil || strings.ReplaceAll(string(body), "\r\n", "\n") != "made by an attempt\n" {
		t.Fatalf("out.txt = %q, %v; want the winner's file in the workspace", body, err)
	}
	results := effectToolResults(session[len(session)-1])
	if len(results) != 1 || !strings.Contains(results[0], "Attempt 1 won") {
		t.Fatalf("tool results = %q", results)
	}
	var judged []string
	for _, req := range rec.requests() {
		if len(req.Tools) == 0 {
			judged = append(judged, req.Messages[len(req.Messages)-1].Content)
		}
	}
	if len(judged) != 1 || strings.Count(judged[0], "## Host record\n\nverdict ") != 2 {
		t.Fatalf("judge input does not carry each attempt's completion summary: %q", judged)
	}
	if n := strings.Count(gitInDir(t, dir, "worktree", "list", "--porcelain"), "worktree "); n != 1 {
		t.Fatalf("%d worktrees left, want only the workspace", n)
	}
}
