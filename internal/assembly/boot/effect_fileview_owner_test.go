package boot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

type routedByTools struct{ parent, child *browserScriptProvider }

func (*routedByTools) Name() string { return "boot-fileview-routed" }

func (p *routedByTools) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if len(req.Tools) == 0 {
		ch := make(chan provider.Chunk, 2)
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "title"}
		ch <- provider.Chunk{Type: provider.ChunkDone}
		close(ch)
		return ch, nil
	}
	if len(req.Tools) > 3 {
		return p.parent.Stream(ctx, req)
	}
	return p.child.Stream(ctx, req)
}

func TestEffectChildReadCannotRefreshParentFileView(t *testing.T) {
	home, dir := statedBootHome(t), robustTempDir(t)
	target := filepath.Join(dir, "notes.md")
	writeFile(t, dir, "notes.md", "original\n")
	const current = "the user's new version\n"
	parent := &browserScriptProvider{rounds: []func(string) *provider.ToolCall{
		func(string) *provider.ToolCall {
			return browserCall("parent-read", "read_file", map[string]any{"path": target})
		},
		func(last string) *provider.ToolCall {
			if !strings.Contains(last, "original") {
				t.Errorf("parent did not receive original content: %q", last)
			}
			if err := os.WriteFile(target, []byte(current), 0600); err != nil {
				t.Error(err)
			}
			return browserCall("delegate", "spawn_agents", map[string]any{
				"tasks": []map[string]any{{"title": "read", "task": "Read notes.md"}},
			})
		},
		func(string) *provider.ToolCall {
			return browserCall("parent-write", "write_file", map[string]any{"path": target, "content": "stale parent version\n"})
		},
	}}
	child := &browserScriptProvider{rounds: []func(string) *provider.ToolCall{
		func(string) *provider.ToolCall {
			return browserCall("child-read", "read_file", map[string]any{"path": target})
		},
	}}
	kind := "boot-fileview-owner"
	provider.Register(kind, func(provider.Config) (provider.Provider, error) {
		return &routedByTools{parent: parent, child: child}, nil
	})
	writeFile(t, dir, "reasonix.toml", `
default_model = "parent"

[codegraph]
enabled = false

[[providers]]
name = "parent"
kind = "`+kind+`"
model = "parent"
`)
	approveWorkspaceAt(t, home, dir)
	ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: dir, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	runErr := ctrl.Run(context.Background(), "Delegate a read and then update notes.md")
	childRequests := child.requests()
	if len(childRequests) < 2 {
		t.Fatalf("child never completed its read: %d requests; run=%v", len(childRequests), runErr)
	}
	childResults := effectToolResults(childRequests[len(childRequests)-1])
	if len(childResults) == 0 || !strings.Contains(childResults[0], "the user's new version") {
		t.Fatalf("child did not receive new content: %q", childResults)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != current {
		t.Fatalf("child read let parent overwrite unseen content: %q; read=%v; run=%v", got, err, runErr)
	}
	if runErr != nil {
		t.Fatal(runErr)
	}
	requests := parent.requests()
	results := effectToolResults(requests[len(requests)-1])
	if len(results) != 3 || !strings.Contains(results[2], "modified after you last read or wrote it") {
		t.Fatalf("parent did not receive the stale-view refusal: %q", results)
	}
}
