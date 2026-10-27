package boot

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/hook"
	"reasonix/internal/session/control"
	"reasonix/internal/state/checkpoint"
	"reasonix/internal/state/sessionstore"
)

type checkpointCancelGate struct {
	cancel  context.CancelFunc
	checked chan struct{}
	once    sync.Once
}

func (g *checkpointCancelGate) Check(context.Context, string, json.RawMessage, bool) (bool, string, error) {
	g.once.Do(func() {
		g.cancel()
		close(g.checked)
	})
	return true, "", nil
}

func TestEffectCancelledCheckpointAdmissionStopsUserCode(t *testing.T) {
	for _, name := range []string{"read_file", "write_file"} {
		for _, proxy := range []bool{false, true} {
			scenario := name + "/direct"
			if proxy {
				scenario = name + "/capability"
			}
			t.Run(scenario, func(t *testing.T) {
				home, root := statedBootHome(t), t.TempDir()
				writeFile(t, root, "note.txt", "user bytes")
				marker := filepath.Join(root, "hook.txt")
				args := `{"path":"note.txt"}`
				if name == "write_file" {
					args = `{"path":"note.txt","content":"tool bytes"}`
				}
				call := scriptedCall{name: name, args: args}
				if proxy {
					raw, err := json.Marshal(map[string]any{"action": "call", "capability_id": "tool:" + name, "arguments": json.RawMessage(args)})
					if err != nil {
						t.Fatal(err)
					}
					call = scriptedCall{name: "use_capability", args: string(raw)}
				}
				kind := "checkpoint-cancel-" + strings.ReplaceAll(t.Name(), "/", "-")
				p := &scriptedCallProvider{calls: []scriptedCall{call}}
				provider.Register(kind, func(provider.Config) (provider.Provider, error) { return p, nil })
				writeFile(t, root, "reasonix.toml", `default_model = "fixture"
[codegraph]
enabled = false
[[providers]]
name = "fixture"
kind = "`+kind+`"
model = "fixture"
`)
				approveWorkspaceAt(t, home, root)
				if name == "read_file" {
					settings := hook.Settings{Hooks: map[hook.Event][]hook.HookConfig{hook.PreToolUse: {{Match: name, Command: markerCommand(t, marker)}}}}
					raw, err := json.Marshal(settings)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(home, hook.SettingsFilename), raw, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				sink := &hookWorkspaceSink{}
				ctrl, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: root, Sink: sink})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { ctrl.Close() })
				ctrl.SetFreshSessionPath(sessionstore.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
				ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
				// The fixture supplies the checkpoint owner through the public Agent
				// binding; tools, proxy resolution and hooks still come from Build.
				store := checkpoint.New("", root)
				ctrl.Executor().SetMutationObserver(checkpoint.NewMutationObserver(checkpoint.ObserverOptions{Store: store}))
				if err := store.Barrier().EnterExclusive(); err != nil {
					t.Fatal(err)
				}
				var release sync.Once
				unlock := func() { release.Do(store.Barrier().ExitExclusive) }
				defer unlock()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				gate := &checkpointCancelGate{cancel: cancel, checked: make(chan struct{})}
				ctrl.Executor().SetGate(gate)
				done := make(chan error, 1)
				go func() { done <- ctrl.Run(ctx, "exercise checkpoint cancellation") }()
				select {
				case <-gate.checked:
				case <-time.After(10 * time.Second):
					unlock()
					cancel()
					<-done
					t.Fatal("script did not reach the permission boundary")
				}
				select {
				case err = <-done:
				case <-time.After(10 * time.Second):
					t.Error("cancelled invocation still waits for the exclusive checkpoint owner")
					unlock()
					err = <-done
				}
				if err != nil && !errors.Is(err, context.Canceled) {
					t.Errorf("Run returned an unrelated error: %v", err)
				}
				if store.Barrier().HasWriters() {
					t.Error("cancelled invocation retained writer admission")
				}
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Errorf("cancelled reader entered its hook: %v", err)
				}
				if data, err := os.ReadFile(filepath.Join(root, "note.txt")); err != nil || string(data) != "user bytes" {
					t.Errorf("cancelled invocation changed user bytes: %q %v", data, err)
				}
				sink.mu.Lock()
				defer sink.mu.Unlock()
				if len(sink.results) != 1 || sink.results[0].Executed || sink.results[0].WorkspaceMutation {
					t.Errorf("cancelled invocation's execution/effect result = %+v", sink.results)
				}
			})
		}
	}
}
