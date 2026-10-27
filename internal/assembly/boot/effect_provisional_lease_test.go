package boot

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/workspacelease"
)

func TestEffectRefusedWriteDoesNotRetainANewWorkspacePath(t *testing.T) {
	for _, dynamic := range []bool{false, true} {
		name := "direct"
		if dynamic {
			name = "capability"
		}
		t.Run(name, func(t *testing.T) {
			home, root := statedBootHome(t), robustTempDir(t)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			holding, paused, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			holder := &browserScriptProvider{rounds: []func(string) *provider.ToolCall{
				func(string) *provider.ToolCall { close(holding); <-release; return nil },
			}}
			parent := &browserScriptProvider{rounds: []func(string) *provider.ToolCall{
				func(string) *provider.ToolCall {
					return browserCall("written", "write_file", map[string]any{"path": filepath.Join(root, "written.txt"), "content": "retained\n"})
				},
				func(string) *provider.ToolCall {
					return browserCall("holder", "task", map[string]any{"prompt": "Hold refused.txt", "model": "holder", "write_paths": []string{"refused.txt"}, "run_in_background": true})
				},
				func(string) *provider.ToolCall {
					select {
					case <-holding:
					case <-ctx.Done():
						return nil
					}
					args := map[string]any{"path": filepath.Join(root, "refused.txt"), "content": "must not run\n"}
					if dynamic {
						return browserCall("refused", "use_capability", map[string]any{"action": "call", "capability_id": "tool:write_file", "arguments": args})
					}
					return browserCall("refused", "write_file", args)
				},
				func(string) *provider.ToolCall { close(paused); <-release; return nil },
			}}
			kind := "boot-provisional-lease-" + strings.ToLower(t.Name())
			provider.Register(kind, func(cfg provider.Config) (provider.Provider, error) {
				if cfg.Model == "holder" {
					return holder, nil
				}
				return parent, nil
			})
			writeFile(t, root, "reasonix.toml", `
default_model = "parent"
[codegraph]
enabled = false
[[providers]]
name = "parent"
kind = "`+kind+`"
model = "parent"
[[providers]]
name = "holder"
kind = "`+kind+`"
model = "holder"
`)
			approveWorkspaceAt(t, home, root)
			ctrl, err := Build(ctx, Options{Home: home, WorkspaceRoot: root, Sink: event.Discard})
			if err != nil {
				t.Fatal(err)
			}
			ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
			done := make(chan error, 1)
			go func() { done <- ctrl.Run(ctx, "Write written.txt, start a holder, then attempt refused.txt") }()
			t.Cleanup(func() {
				close(release)
				cancel()
				<-done
				ctrl.Close()
				if manager := ctrl.JobManager(); manager != nil {
					joined := make(chan struct{})
					manager.AfterUnfinished(func() { close(joined) })
					<-joined
				}
			})
			select {
			case <-paused:
			case <-ctx.Done():
				t.Fatal("parent never reached its next model request")
			}
			requests := parent.requests()
			first, _ := json.Marshal(requests[0].Tools)
			for _, request := range requests[1:] {
				definitions, _ := json.Marshal(request.Tools)
				if !bytes.Equal(first, definitions) || systemOf(request) != systemOf(requests[0]) {
					t.Fatal("provisional cleanup changed the stable provider prefix")
				}
			}
			if _, err := os.Stat(filepath.Join(root, "refused.txt")); !os.IsNotExist(err) {
				t.Fatalf("refused writer executed: %v", err)
			}
			if content, err := os.ReadFile(filepath.Join(root, "written.txt")); err != nil || string(content) != "retained\n" {
				t.Fatalf("earlier write did not execute: %q, %v", content, err)
			}
			next, err := workspacelease.New(root, config.WorkspaceLeaseDir(), nil)
			if err != nil {
				t.Fatal(err)
			}
			next.BeginRun()
			defer next.EndRun()
			if waits(t, func(ctx context.Context) error { return next.AcquirePaths(ctx, []string{"refused.txt"}) }) {
				t.Error("never-executed path stayed held during the parent's next model request")
			}
			if !waits(t, func(ctx context.Context) error { return next.AcquirePaths(ctx, []string{"written.txt"}) }) {
				t.Error("cleanup released the earlier genuine write")
			}
		})
	}
}
