package boot

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

type firstFinishIsolationProvider struct {
	inner restartIsolationProvider
	t     *testing.T
	path  string
	empty string
	mu    sync.Mutex
	err   string
}

func (*firstFinishIsolationProvider) Name() string { return "boot-first-finish-isolation" }

func (p *firstFinishIsolationProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	child := strings.Contains(firstUser(req), isolatedTask)
	if child && hasToolResult(req) {
		// The final child model response precedes Finish; no runner is cancelled.
		p.t.Setenv("PATH", p.empty)
	} else if !child && hasToolResult(req) {
		p.t.Setenv("PATH", p.path)
		last := lastToolResult(req)
		if strings.Contains(last, "inspection is incomplete") {
			p.mu.Lock()
			p.err = last
			p.mu.Unlock()
		}
	}
	return p.inner.Stream(ctx, req)
}

func TestEffectFirstFinishFailureSurvivesRestartAndApplies(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	home := statedBootHome(t)
	dir := testenv.TempDir(t)
	kind := uniqueKind("boot-first-finish")
	rec := &firstFinishIsolationProvider{t: t, path: os.Getenv("PATH"), empty: testenv.TempDir(t)}
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
	gitInDir(t, dir, "config", "core.autocrlf", "true")
	mirrorToUserConfigAt(t, home, dir)
	gitInDir(t, dir, "add", "-A")
	gitInDir(t, dir, "commit", "-q", "-m", "init")
	build := func() *control.Controller {
		ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: dir, Sink: event.Discard})
		if err != nil {
			t.Fatal(err)
		}
		ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
		t.Cleanup(ctrl.Close)
		return ctrl
	}
	ctrl := build()
	path := filepath.Join(dir, ".reasonix", "sessions", "first-finish.jsonl")
	ctrl.SetSessionPath(path)
	if err := ctrl.Run(t.Context(), "DELEGATE make out.txt in isolation"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", rec.path)
	rec.mu.Lock()
	result := rec.err
	rec.mu.Unlock()
	id := isoIDPattern.FindString(result)
	if id == "" {
		for _, message := range ctrl.History() {
			if message.Role == provider.RoleTool {
				t.Logf("tool %s: %s", message.Name, message.Content)
			}
		}
		t.Fatalf("task result did not disclose unfinished inspection and recovery identity: %q", result)
	}
	if _, err := os.Stat(filepath.Join(dir, "out.txt")); !os.IsNotExist(err) {
		t.Fatalf("candidate reached source before apply: %v", err)
	}
	ctrl.Close()
	loaded, err := sessionstore.LoadSession(path)
	if err != nil || loaded == nil {
		t.Fatalf("load session: %v", err)
	}
	ctrl = build()
	if err := ctrl.Resume(loaded, path); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Run(t.Context(), "APPLY the pending isolated result"); err != nil {
		t.Fatal(err)
	}
	rec.inner.mu.Lock()
	applied := rec.inner.applyResult
	rec.inner.mu.Unlock()
	if !strings.Contains(applied, "applied "+id) {
		t.Fatalf("same-ID apply after controller reconstruction: %q", applied)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	if err != nil || strings.ReplaceAll(string(raw), "\r\n", "\n") != "made in isolation\n" {
		t.Fatalf("retained work after restart = %q, %v", raw, err)
	}
}
