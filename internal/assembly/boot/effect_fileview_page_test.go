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

func TestEffectPagedReadDoesNotRefreshUnseenFileContent(t *testing.T) {
	t.Parallel()
	home, dir := statedBootHome(t), robustTempDir(t)
	target := filepath.Join(dir, "notes.md")
	writeFile(t, dir, "notes.md", "header\noriginal tail\n")
	const current = "header\nthe user's new tail\n"
	rec := &browserScriptProvider{rounds: []func(string) *provider.ToolCall{
		func(string) *provider.ToolCall {
			return browserCall("whole", "read_file", map[string]any{"path": target})
		},
		func(last string) *provider.ToolCall {
			if !strings.Contains(last, "original tail") {
				t.Errorf("whole read did not reach the provider: %q", last)
			}
			if err := os.WriteFile(target, []byte(current), 0600); err != nil {
				t.Error(err)
			}
			return browserCall("page", "read_file", map[string]any{"path": target, "limit": 1})
		},
		func(last string) *provider.ToolCall {
			if !strings.Contains(last, "header") || strings.Contains(last, "new tail") {
				t.Errorf("page did not reach the provider as a partial read: %q", last)
			}
			return browserCall("overwrite", "write_file", map[string]any{"path": target, "content": "changed header\noriginal tail\n"})
		},
	}}
	kind := "boot-fileview-" + strings.ToLower(t.Name())
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
	approveWorkspaceAt(t, home, dir)
	ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: dir, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	runErr := ctrl.Run(context.Background(), "update the header in notes.md")
	got, err := os.ReadFile(target)
	if err != nil || string(got) != current {
		t.Fatalf("unseen user edit was overwritten: %q; read=%v; run=%v", got, err, runErr)
	}
	if runErr != nil {
		t.Fatal(runErr)
	}
	reqs := rec.requests()
	if len(reqs) < 4 {
		t.Fatalf("missing requests: %d", len(reqs))
	}
	results := effectToolResults(reqs[len(reqs)-1])
	if len(results) != 3 || !strings.Contains(results[2], "modified after you last read or wrote it") {
		t.Fatalf("stale overwrite refusal did not reach the provider: %q", results)
	}
}
