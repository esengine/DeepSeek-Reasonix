package boot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/session/control"
)

type isolatedIntegrityProvider struct {
	mu      sync.Mutex
	command string
	result  string
}

func (*isolatedIntegrityProvider) Name() string { return "isolated-integrity" }

func (p *isolatedIntegrityProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	if hasToolResult(req) {
		if strings.Contains(firstUser(req), "INTEGRITY-CHILD") {
			p.mu.Lock()
			p.result = lastToolResult(req)
			p.mu.Unlock()
		}
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	} else {
		name, args := "task", map[string]any{"prompt": "INTEGRITY-CHILD", "isolation": "worktree", "tools": []string{"bash"}}
		if strings.Contains(firstUser(req), "INTEGRITY-CHILD") {
			name, args = "bash", map[string]any{"command": p.command}
		}
		raw, _ := json.Marshal(args)
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "integrity", Name: name, Arguments: string(raw)}}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectIsolatedUnixShellProtectsSourceAndUsesPrivateCache(t *testing.T) {
	if runtime.GOOS == "windows" || !sandbox.Available() {
		t.Skip("requires native Unix sandbox")
	}
	home, root := statedBootHome(t), testenv.TempDir(t)
	hostCache := filepath.Join(testenv.TempDir(t), "host-cache")
	t.Setenv("GOCACHE", hostCache)
	quote := func(path string) string { return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'" }
	p := &isolatedIntegrityProvider{command: "printf candidate > local.txt; printf escaped > " + quote(filepath.Join(root, "unexpected.txt")) + "; rm " + quote(filepath.Join(root, "victim.txt")) + "; mv " + quote(filepath.Join(root, "rename.txt")) + " " + quote(filepath.Join(root, "renamed.txt")) + "; mkdir -p \"$GOCACHE\"; printf cache > \"$GOCACHE/probe\"; test -f \"$GOCACHE/probe\" && test \"$GOCACHE\" != " + quote(hostCache) + " && echo PRIVATE-CACHE-OK"}
	provider.Register("boot-isolated-unix-integrity", func(provider.Config) (provider.Provider, error) { return p, nil })
	writeFile(t, root, "reasonix.toml", `
default_model = "test-model"
[agent]
system_prompt = "BASE"
collaboration = "shared"
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "boot-isolated-unix-integrity"
model = "x"
`)
	writeFile(t, root, "victim.txt", "source survives\n")
	writeFile(t, root, "rename.txt", "source survives\n")
	mirrorToUserConfigAt(t, home, root)
	gitInDir(t, root, "init", "-q")
	gitInDir(t, root, "add", "-A")
	gitInDir(t, root, "commit", "-qm", "source")
	ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ctrl.Close)
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	if err := ctrl.Run(t.Context(), "delegate an isolated Unix integrity fixture"); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	result := p.result
	p.mu.Unlock()
	for _, name := range []string{"victim.txt", "rename.txt"} {
		body, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(body) != "source survives\n" {
			t.Fatalf("source %s changed: %q %v", name, body, err)
		}
	}
	for _, name := range []string{"unexpected.txt", "renamed.txt", "local.txt"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("candidate touched source %s: %v", name, err)
		}
	}
	if !strings.Contains(result, "PRIVATE-CACHE-OK") {
		t.Fatalf("candidate shell/cache did not execute: %s", result)
	}
	catalog, err := ctrl.IsolationResults(t.Context())
	if err != nil || len(catalog.Results) != 1 {
		t.Fatalf("candidate result missing: %+v %v", catalog, err)
	}
	preview, err := ctrl.PreviewIsolatedDiscard(t.Context(), catalog.Results[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(filepath.Join(preview.CandidateRoot, "local.txt")); err != nil || string(body) != "candidate" {
		t.Fatalf("candidate cwd was not confined: %q %v", body, err)
	}
}
