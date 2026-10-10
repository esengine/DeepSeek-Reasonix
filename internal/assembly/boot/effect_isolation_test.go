package boot

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

const isolatedTask = "ISOLATED-TASK: create out.txt"

var isoIDPattern = regexp.MustCompile(`iso_[0-9a-f]+`)

// isolationScriptProvider plays every model in the run: the session delegates
// an isolated task, the child writes out.txt in its worktree, and the session
// applies the result it was handed. It notes whether out.txt was in the
// workspace at the moment the session first saw the task's result.
type isolationScriptProvider struct {
	dir             string
	mu              sync.Mutex
	reqs            []provider.Request
	landedBeforeApp bool
	childSaw        string
	managed         string // the stated home's isolated-worktree root
	childWorktree   string // a worktree found there while the child ran
}

func (p *isolationScriptProvider) Name() string { return "boot-isolation-script" }

func lastToolResult(req provider.Request) string {
	for _, m := range slices.Backward(req.Messages) {
		if m.Role == provider.RoleTool {
			return m.Content
		}
	}
	return ""
}

func (p *isolationScriptProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	call := func(id, name string, args any) {
		raw, _ := json.Marshal(args)
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: id, Name: name, Arguments: string(raw)}}
	}
	last := lastToolResult(req)
	switch {
	case strings.Contains(firstUser(req), isolatedTask):
		if hasToolResult(req) {
			p.mu.Lock()
			p.childSaw = last
			p.mu.Unlock()
			ch <- provider.Chunk{Type: provider.ChunkText, Text: "wrote out.txt"}
		} else {
			found, _ := filepath.Glob(filepath.Join(p.managed, "*", "*"))
			p.mu.Lock()
			if len(found) > 0 {
				p.childWorktree = found[0]
			}
			p.mu.Unlock()
			call("w1", "write_file", map[string]string{"path": "out.txt", "content": "made in isolation\n"})
		}
	case strings.HasPrefix(last, "applied "), hasToolResult(req) && !isoIDPattern.MatchString(last):
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	case isoIDPattern.MatchString(last):
		_, err := os.Stat(filepath.Join(p.dir, "out.txt"))
		p.mu.Lock()
		p.landedBeforeApp = err == nil
		p.mu.Unlock()
		call("a1", "apply_isolated", map[string]string{"id": isoIDPattern.FindString(last)})
	default:
		call("t1", "task", map[string]string{"prompt": isolatedTask, "isolation": "worktree"})
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *isolationScriptProvider) requests() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]provider.Request(nil), p.reqs...)
}

// runIsolatedTask runs one session that delegates an isolated task and applies
// it, bound to its own stated home, and returns that home with the provider.
func runIsolatedTask(t *testing.T, kind string) (*isolationScriptProvider, string, string) {
	t.Helper()
	home := statedBootHome(t)
	dir := testenv.TempDir(t)
	rec := &isolationScriptProvider{dir: dir, managed: filepath.Join(config.RootsForHome(home).DeliveryWorktreeDir(), "isolated")}
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"
worktree_isolation = true

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
	gitInDir(t, dir, "init", "-q")
	// Attempts run in worktrees, each a folder of its own: the model is the
	// user's, so an approval of this folder is not what makes them run.
	mirrorToUserConfigAt(t, home, dir)
	gitInDir(t, dir, "add", "-A")
	gitInDir(t, dir, "commit", "-q", "-m", "init")

	ctrl, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// The posture a frontend sets on a window; the isolated child runs under it.
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	if err := ctrl.Run(context.Background(), "make out.txt without touching my tree until I say"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	ctrl.Close()
	return rec, home, dir
}

// An isolated task runs in its own kernel at its own worktree: its write stays
// out of the workspace until the session applies it, the child cannot isolate
// or apply again, and closing the session leaves no worktree behind.
func TestEffectIsolatedTaskIsHeldUntilApplied(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Parallel()
	rec, home, dir := runIsolatedTask(t, "boot-isolation")

	var session, child []provider.Request
	for _, req := range agentRequests(rec.requests()) {
		if strings.Contains(firstUser(req), isolatedTask) {
			child = append(child, req)
		} else {
			session = append(session, req)
		}
	}
	if len(session) == 0 || len(child) == 0 {
		t.Fatalf("session requests = %d, child requests = %d; want both", len(session), len(child))
	}
	if rec.landedBeforeApp {
		t.Fatal("the child's write reached the workspace before it was applied")
	}
	if b, err := os.ReadFile(filepath.Join(dir, "out.txt")); err != nil || strings.ReplaceAll(string(b), "\r\n", "\n") != "made in isolation\n" {
		t.Fatalf("out.txt after apply = %q, %v", b, err)
	}
	left, _ := filepath.Glob(filepath.Join(config.RootsForHome(home).DeliveryWorktreeDir(), "isolated", "*", "*"))
	if len(left) != 0 {
		t.Fatalf("worktrees left after Close: %v", left)
	}
}

// Two sessions in one process, each bound to its own home, keep their isolated
// worktrees under that home: the worktree root is part of the binding Build
// was handed, not of the environment the process happens to have.
func TestEffectIsolatedWorktreesLiveUnderTheStatedHome(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Parallel()
	for _, name := range []string{"alpha", "beta"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rec, _, _ := runIsolatedTask(t, "boot-isolation-home-"+name)
			if rec.childWorktree == "" {
				t.Fatalf("the isolated child ran with no worktree under its stated home %s", rec.managed)
			}
		})
	}
}
