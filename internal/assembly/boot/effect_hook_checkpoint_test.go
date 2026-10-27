package boot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/hook"
	"reasonix/internal/session/control"
	"reasonix/internal/state/checkpoint"
	"reasonix/internal/state/sessionstore"
)

func TestEffectReadHooksLeaveCheckpointCoverage(t *testing.T) {
	for _, stage := range []hook.Event{hook.PreToolUse, hook.PostToolUse} {
		for _, proxy := range []bool{false, true} {
			name := string(stage) + "/direct"
			if proxy {
				name = string(stage) + "/capability"
			}
			t.Run(name, func(t *testing.T) {
				home := statedBootHome(t)
				root := t.TempDir()
				marker := filepath.Join(root, "hook.txt")
				writeFile(t, root, "note.txt", "original")
				kind := "hook-checkpoint-" + strings.ReplaceAll(t.Name(), "/", "-")
				call := scriptedCall{name: "read_file", args: `{"path":"note.txt"}`}
				if proxy {
					call = scriptedCall{name: "use_capability", args: `{"action":"call","capability_id":"tool:read_file","arguments":{"path":"note.txt"}}`}
				}
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
				settings := hook.Settings{Hooks: map[hook.Event][]hook.HookConfig{stage: {{Match: "read_file", Command: markerCommand(t, marker)}}}}
				raw, err := json.Marshal(settings)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(home, hook.SettingsFilename), raw, 0o600); err != nil {
					t.Fatal(err)
				}
				ctrl, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: root, Sink: event.Discard})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { ctrl.Close() })
				ctrl.SetFreshSessionPath(sessionstore.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
				ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				if err := ctrl.Run(ctx, "read the fixture"); err != nil {
					t.Fatal(err)
				}
				if result := p.resultOf(0); !strings.Contains(result, "original") {
					t.Fatalf("reader did not retain its normal result: %q", result)
				}
				if data, err := os.ReadFile(marker); err != nil || string(data) != "ran" {
					t.Fatalf("real hook did not write: %q %v", data, err)
				}
				metas := ctrl.Checkpoints()
				found := false
				for _, meta := range metas {
					for _, gap := range meta.CoverageGaps {
						if gap.Reason == checkpoint.GapHookWrite {
							found = true
						}
					}
				}
				if !found {
					t.Fatalf("real hook write omitted from checkpoint coverage: %+v", metas)
				}
			})
		}
	}
}
