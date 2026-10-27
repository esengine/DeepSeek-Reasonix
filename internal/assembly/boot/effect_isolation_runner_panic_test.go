package boot

import (
	"context"
	"encoding/json"
	"fmt"
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

type panicIsolationProvider struct {
	inner    restartIsolationProvider
	deferred bool
	mu       sync.Mutex
	result   string
}

func (*panicIsolationProvider) Name() string { return "boot-isolation-panic" }

func (p *panicIsolationProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if strings.Contains(firstUser(req), isolatedTask) && hasToolResult(req) {
		panic("provider panic after child write")
	}
	if !strings.Contains(firstUser(req), isolatedTask) && hasToolResult(req) {
		p.mu.Lock()
		p.result = lastToolResult(req)
		p.mu.Unlock()
	}
	if p.deferred && !hasToolResult(req) && !strings.Contains(firstUser(req), isolatedTask) {
		raw, err := json.Marshal(map[string]any{"action": "call", "capability_id": "task:subagent",
			"arguments": map[string]string{"prompt": isolatedTask, "isolation": "worktree"}})
		if err != nil {
			return nil, err
		}
		ch := make(chan provider.Chunk, 2)
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "panic-task", Name: "use_capability", Arguments: string(raw)}}
		ch <- provider.Chunk{Type: provider.ChunkDone}
		close(ch)
		return ch, nil
	}
	return p.inner.Stream(ctx, req)
}

func TestEffectIsolatedRunnerPanicRetainsResultAfterRebuild(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	for _, deferred := range []bool{false, true} {
		t.Run(fmt.Sprintf("deferred=%t", deferred), func(t *testing.T) {
			home, dir := statedBootHome(t), testenv.TempDir(t)
			kind := uniqueKind("isolation-panic")
			rec := &panicIsolationProvider{deferred: deferred}
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
			path := filepath.Join(dir, ".reasonix", "sessions", "runner-panic.jsonl")
			ctrl.SetSessionPath(path)
			if err := runPanicIsolationTurn(ctrl, t.Context()); err != nil {
				t.Fatal(err)
			}
			rec.mu.Lock()
			result := rec.result
			rec.mu.Unlock()
			id := isoIDPattern.FindString(result)
			if id == "" || !strings.Contains(result, "provider panic after child write") {
				t.Fatalf("model-visible task result lost panic recovery identity or cause: %q", result)
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
				t.Fatalf("same-ID apply after rebuild: %q", applied)
			}
			raw, err := os.ReadFile(filepath.Join(dir, "out.txt"))
			if err != nil || strings.ReplaceAll(string(raw), "\r\n", "\n") != "made in isolation\n" {
				t.Fatalf("recovered bytes = %q, %v", raw, err)
			}
		})
	}
}

func runPanicIsolationTurn(ctrl *control.Controller, ctx context.Context) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("runner panic escaped before result finalization: %v", p)
		}
	}()
	return ctrl.Run(ctx, "DELEGATE make out.txt in isolation")
}
