package boot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

type guardianBoundaryProvider struct {
	mu    sync.Mutex
	calls []*provider.ToolCall
	reqs  []provider.Request
}

func (*guardianBoundaryProvider) Name() string { return "guardian-boundary" }
func (p *guardianBoundaryProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	n := len(p.reqs)
	p.reqs = append(p.reqs, req)
	p.mu.Unlock()
	out := make(chan provider.Chunk, 2)
	if n < len(p.calls) {
		out <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: p.calls[n]}
	} else {
		out <- provider.Chunk{Type: provider.ChunkText, Text: `{"risk_level":"low","user_authorization":"high","outcome":"allow","rationale":"requested executor write"}`}
	}
	out <- provider.Chunk{Type: provider.ChunkDone}
	close(out)
	return out, nil
}

func TestEffectGuardianCannotInvokeDeferredFileWriter(t *testing.T) {
	home, dir := statedBootHome(t), robustTempDir(t)
	target := filepath.Join(dir, "notes.md")
	const original = "user-owned notes\n"
	writeFile(t, dir, "notes.md", original)
	guardian := &guardianBoundaryProvider{calls: []*provider.ToolCall{
		browserCall("review-read", "read_file", map[string]any{"path": target}),
		browserCall("review-write", "use_capability", map[string]any{"action": "call", "capability_id": "tool:write_file", "arguments": map[string]any{"path": target, "content": "unexpected reviewer write\n"}}),
	}}
	executor := &browserScriptProvider{rounds: []func(string) *provider.ToolCall{
		func(string) *provider.ToolCall {
			return browserCall("requested-write", "write_file", map[string]any{"path": filepath.Join(dir, "out.txt"), "content": "requested executor write\n"})
		},
	}}
	kind := uniqueKind("boot-guardian-boundary")
	provider.Register(kind, func(cfg provider.Config) (provider.Provider, error) {
		if cfg.Model == "guardian" {
			return guardian, nil
		}
		return executor, nil
	})
	writeFile(t, dir, "reasonix.toml", `
default_model = "executor"
[agent]
guardian_model = "guardian"
[codegraph]
enabled = false
[[providers]]
name = "executor"
kind = "`+kind+`"
model = "executor"
[[providers]]
name = "guardian"
kind = "`+kind+`"
model = "guardian"
`)
	approveWorkspaceAt(t, home, dir)
	ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: dir, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ctrl.Close)
	ctrl.SetFreshSessionPath(sessionstore.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
	ctrl.SetToolApprovalMode(control.ToolApprovalAsk)
	ctrl.EnableInteractiveApproval()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	if err := ctrl.Run(ctx, "Write out.txt"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != original {
		t.Fatalf("guardian changed user content: %q; %v", got, err)
	}
	got, err = os.ReadFile(filepath.Join(dir, "out.txt"))
	if err != nil || string(got) != "requested executor write\n" {
		t.Fatalf("authorized executor write failed: %q; %v", got, err)
	}
	guardian.mu.Lock()
	reqs := append([]provider.Request(nil), guardian.reqs...)
	guardian.mu.Unlock()
	if len(reqs) != 3 {
		t.Fatalf("guardian requests=%d, want 3", len(reqs))
	}
	if results := effectToolResults(reqs[1]); len(results) != 1 || !strings.Contains(results[0], "user-owned notes") {
		t.Fatalf("guardian could not read: %q", results)
	}
	if results := effectToolResults(reqs[2]); len(results) != 2 || !strings.Contains(results[1], "read-only agent cannot") {
		t.Fatalf("guardian writer did not receive boundary refusal: %q", results)
	}
}
