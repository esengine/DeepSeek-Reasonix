package boot

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/checkpoint"
)

type noLaunchProvider struct {
	requests atomic.Int32
}

func (*noLaunchProvider) Name() string { return "no-launch-child" }
func (p *noLaunchProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	p.requests.Add(1)
	ch := make(chan provider.Chunk, 1)
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectBackgroundJobNoLaunchFinalizesResources(t *testing.T) {
	for _, name := range []string{"task", "fleet"} {
		for _, dynamic := range []bool{false, true} {
			mode := "direct"
			if dynamic {
				mode = "capability"
			}
			t.Run(name+"/"+mode, func(t *testing.T) {
				home, root := statedBootHome(t), t.TempDir()
				child := &noLaunchProvider{}
				var ctrl *control.Controller
				parent := &browserScriptProvider{rounds: []func(string) *provider.ToolCall{
					func(string) *provider.ToolCall {
						// Close only the public job manager after the turn is bound.
						// This does not claim a full Controller-close incident.
						ctrl.JobManager().Close()
						args := map[string]any{"prompt": "must not run", "model": "child", "run_in_background": true}
						if name == "fleet" {
							args = map[string]any{"run_in_background": true, "tasks": []any{
								map[string]any{"prompt": "first", "model": "child", "write_paths": []string{"one.txt"}},
								map[string]any{"prompt": "second", "model": "child", "write_paths": []string{"two.txt"}},
							}}
						}
						if dynamic {
							return browserCall("no-launch", "use_capability", map[string]any{
								"action": "call", "capability_id": "tool:" + name, "arguments": args,
							})
						}
						return browserCall("no-launch", name, args)
					},
				}}
				kind := "job-no-launch-" + strings.ToLower(t.Name())
				provider.Register(kind, func(cfg provider.Config) (provider.Provider, error) {
					if cfg.Model == "child" {
						return child, nil
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
				var err error
				ctrl, err = Build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: event.Discard})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { ctrl.Close() })
				store := checkpoint.New("", root)
				observer := checkpoint.NewMutationObserver(checkpoint.ObserverOptions{Store: store})
				ctrl.Executor().SetMutationObserver(observer)
				ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
				if err := ctrl.Run(t.Context(), "Try background work on this manager"); err != nil {
					t.Fatal(err)
				}
				writers := observer.ActiveWriters()
				for _, w := range writers {
					defer observer.UnregisterWriter(w.ID)
				}
				if len(writers) != 0 || store.Barrier().HasWriters() {
					t.Errorf("no-launch task/fleet stranded checkpoint exclusion: %+v", writers)
				}
				if child.requests.Load() != 0 {
					t.Errorf("no-launch child reached provider %d times", child.requests.Load())
				}
			})
		}
	}
}
