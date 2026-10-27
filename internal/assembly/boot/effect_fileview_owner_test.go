package boot

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

func TestEffectChildReadCannotRefreshParentFileView(t *testing.T) {
	t.Parallel()
	for _, task := range []string{"task", "read_only_task"} {
		for _, dynamic := range []bool{false, true} {
			name := "direct"
			if dynamic {
				name = "capability"
			}
			t.Run(task+"/"+name, func(t *testing.T) {
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
						return browserCall("delegate", "use_capability", map[string]any{
							"action": "call", "capability_id": "tool:" + task,
							"arguments": map[string]any{"prompt": "Read notes.md", "model": "worker"},
						})
					},
					func(string) *provider.ToolCall {
						return browserCall("parent-write", "write_file", map[string]any{"path": target, "content": "stale parent version\n"})
					},
				}}
				child := &browserScriptProvider{rounds: []func(string) *provider.ToolCall{
					func(string) *provider.ToolCall {
						if dynamic {
							return browserCall("child-read", "use_capability", map[string]any{
								"action": "call", "capability_id": "tool:read_file", "arguments": map[string]any{"path": target},
							})
						}
						return browserCall("child-read", "read_file", map[string]any{"path": target})
					},
				}}
				if task == "read_only_task" {
					child.rounds = append(child.rounds, func(string) *provider.ToolCall {
						return browserCall("child-write", "use_capability", map[string]any{
							"action": "call", "capability_id": "tool:write_file",
							"arguments": map[string]any{"path": target, "content": "forbidden child write\n"},
						})
					})
				}
				kind := "boot-fileview-owner-" + strings.ToLower(t.Name())
				provider.Register(kind, func(cfg provider.Config) (provider.Provider, error) {
					if cfg.Model == "worker" {
						return child, nil
					}
					return parent, nil
				})
				writeFile(t, dir, "reasonix.toml", `
default_model = "parent"

[codegraph]
enabled = false

[[providers]]
name = "parent"
kind = "`+kind+`"
model = "parent"

[[providers]]
name = "worker"
kind = "`+kind+`"
model = "worker"
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
				if task == "read_only_task" && (len(childResults) != 2 || !strings.Contains(childResults[1], "read-only agent cannot")) {
					t.Fatalf("read-only child could execute a dynamic writer: %q", childResults)
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
				for _, reqs := range [][]provider.Request{requests, childRequests} {
					first, _ := json.Marshal(reqs[0].Tools)
					for _, req := range reqs[1:] {
						tools, _ := json.Marshal(req.Tools)
						if !bytes.Equal(first, tools) || systemOf(req) != systemOf(reqs[0]) {
							t.Fatal("file-view ownership changed the stable provider prefix")
						}
					}
				}
			})
		}
	}
}
