package boot

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/checkpoint"
)

func TestEffectBackgroundWriterRegistrationHandoff(t *testing.T) {
	for _, name := range []string{"task", "fleet"} {
		for _, dynamic := range []bool{false, true} {
			mode := "direct"
			if dynamic {
				mode = "capability"
			}
			t.Run(name+"/"+mode, func(t *testing.T) {
				home, root := statedBootHome(t), t.TempDir()
				holder := &admissionHolderProvider{started: make(chan struct{}), release: make(chan struct{})}
				parent := &browserScriptProvider{rounds: []func(string) *provider.ToolCall{
					func(string) *provider.ToolCall {
						args := map[string]any{"prompt": "wait for host", "model": "child", "run_in_background": true}
						if name == "fleet" {
							args = map[string]any{"run_in_background": true, "tasks": []any{
								map[string]any{"prompt": "first", "model": "child", "write_paths": []string{"one.txt"}},
								map[string]any{"prompt": "second", "model": "child", "write_paths": []string{"two.txt"}},
							}}
						}
						if dynamic {
							return browserCall("background", "use_capability", map[string]any{
								"action": "call", "capability_id": "tool:" + name, "arguments": args,
							})
						}
						return browserCall("background", name, args)
					},
				}}
				kind := "writer-handoff-" + strings.ToLower(t.Name())
				provider.Register(kind, func(cfg provider.Config) (provider.Provider, error) {
					if cfg.Model == "child" {
						return holder, nil
					}
					return parent, nil
				})
				writeFile(t, root, "reasonix.toml", `default_model = "parent"
[agent]
subagent_claims = "optimistic"
[codegraph]
enabled = false
[[providers]]
name = "parent"
kind = "`+kind+`"
model = "parent"
[[providers]]
name = "child"
kind = "`+kind+`"
model = "child"
`)
				approveWorkspaceAt(t, home, root)
				ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: event.Discard})
				if err != nil {
					t.Fatal(err)
				}
				var once sync.Once
				unblock := func() { once.Do(func() { close(holder.release) }) }
				join := func() {
					done := make(chan struct{})
					ctrl.JobManager().AfterUnfinished(func() { close(done) })
					<-done
				}
				t.Cleanup(func() { unblock(); ctrl.Close(); join() })
				store := checkpoint.New("", root)
				observer := checkpoint.NewMutationObserver(checkpoint.ObserverOptions{Store: store})
				// Bind the shared observer through the public Agent setter so the
				// task and fleet implementations remain those assembled by Build.
				ctrl.Executor().SetMutationObserver(observer)
				ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if err := ctrl.Run(ctx, "Start background work and finish this turn"); err != nil {
					t.Fatal(err)
				}
				select {
				case <-holder.started:
				case <-time.After(10 * time.Second):
					t.Fatal("background child did not reach its provider")
				}
				cancel()
				if writers := observer.ActiveWriters(); len(writers) == 0 || !store.Barrier().HasWriters() {
					t.Fatalf("parent turn completion retired a live background writer: %+v", writers)
				}
				unblock()
				join()
				if writers := observer.ActiveWriters(); len(writers) != 0 || store.Barrier().HasWriters() {
					raw, _ := json.Marshal(writers)
					t.Fatalf("completed background job retained writer admission: %s", raw)
				}
			})
		}
	}
}
