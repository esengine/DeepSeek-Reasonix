package boot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/hook"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

func TestHookWorkspaceMutationHelper(t *testing.T) {
	for i, arg := range os.Args {
		if arg != "--hook-workspace-mutation" {
			continue
		}
		if len(os.Args) != i+3 {
			os.Exit(3)
		}
		if err := os.WriteFile(os.Args[i+1], []byte("hook changed bytes"), 0o600); err != nil {
			os.Exit(4)
		}
		if os.Args[i+2] == "deny" {
			os.Exit(2)
		}
		os.Exit(0)
	}
}

type hookWorkspaceSink struct {
	mu        sync.Mutex
	mutations []event.WorkspaceMutation
	results   []event.Tool
}

func (s *hookWorkspaceSink) Emit(e event.Event) {
	if e.Kind != event.ToolResult {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results = append(s.results, e.Tool)
}
func (s *hookWorkspaceSink) RecordWorkspaceMutation(m event.WorkspaceMutation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mutations = append(s.mutations, m)
}

func TestEffectHookWritesInvalidateWorkspaceWithoutExecutingDeniedTool(t *testing.T) {
	for _, tc := range []struct {
		name, tool, args string
		stage            hook.Event
		deny             bool
	}{
		{"denied reader", "read_file", `{"path":"note.txt"}`, hook.PreToolUse, true},
		{"denied writer", "write_file", `{"path":"new.txt","content":"tool write"}`, hook.PreToolUse, true},
		{"post reader", "read_file", `{"path":"note.txt"}`, hook.PostToolUse, false},
		{"failed writer", "edit_file", `{"path":"note.txt","old_string":"absent","new_string":"new"}`, hook.PostToolUseFailure, false},
	} {
		for _, proxy := range []bool{false, true} {
			name := tc.name + "/direct"
			if proxy {
				name = tc.name + "/capability"
			}
			t.Run(name, func(t *testing.T) {
				home := statedBootHome(t)
				root := t.TempDir()
				marker := filepath.Join(root, "hook-side.txt")
				writeFile(t, root, "note.txt", "original")
				kind := "hook-workspace-" + strings.ReplaceAll(t.Name(), "/", "-")
				call := scriptedCall{name: tc.tool, args: tc.args}
				if proxy {
					args, _ := json.Marshal(map[string]any{"action": "call", "capability_id": "tool:" + tc.tool, "arguments": json.RawMessage(tc.args)})
					call = scriptedCall{name: "use_capability", args: string(args)}
				}
				p := &scriptedCallProvider{calls: []scriptedCall{call}}
				provider.Register(kind, func(provider.Config) (provider.Provider, error) { return p, nil })
				writeFile(t, root, "reasonix.toml", `default_model = "fixture"
[agent]
system_prompt = "BASE"
[codegraph]
enabled = false
[[providers]]
name = "fixture"
kind = "`+kind+`"
model = "fixture"
`)
				approveWorkspaceAt(t, home, root)
				exe, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				mode := "pass"
				if tc.deny {
					mode = "deny"
				}
				command := `"` + filepath.ToSlash(exe) + `" -test.run=^TestHookWorkspaceMutationHelper$ -- --hook-workspace-mutation "` + filepath.ToSlash(marker) + `" ` + mode
				settings := hook.Settings{Hooks: map[hook.Event][]hook.HookConfig{tc.stage: {{Match: tc.tool, Command: command}}}}
				body, err := json.Marshal(settings)
				if err != nil {
					t.Fatal(err)
				}
				settingsPath := filepath.Join(home, hook.SettingsFilename)
				if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(settingsPath, body, 0o600); err != nil {
					t.Fatal(err)
				}
				sink := &hookWorkspaceSink{}
				ctrl, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: root, Sink: sink})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { ctrl.Close() })
				ctrl.SetFreshSessionPath(sessionstore.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
				ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				if err := ctrl.Run(ctx, "exercise the configured hook"); err != nil {
					t.Fatal(err)
				}
				if data, err := os.ReadFile(marker); err != nil || string(data) != "hook changed bytes" {
					t.Fatalf("hook write absent: %q %v", data, err)
				}
				if tc.deny && tc.tool == "write_file" {
					if _, err := os.Stat(filepath.Join(root, "new.txt")); !os.IsNotExist(err) {
						t.Fatalf("denied tool changed target: %v", err)
					}
				}
				sink.mu.Lock()
				defer sink.mu.Unlock()
				var mutations []event.WorkspaceMutation
				for _, m := range sink.mutations {
					if m.ToolID == "path-0" {
						mutations = append(mutations, m)
					}
				}
				if len(mutations) != 1 || !mutations[0].AllPaths || !mutations[0].GitMeta || !mutations[0].Content || !mutations[0].Tree || !mutations[0].WorkingTree || len(mutations[0].Paths) != 0 {
					t.Fatalf("hook bytes did not invalidate sink: %+v", mutations)
				}
				var results []event.Tool
				for _, r := range sink.results {
					if r.ID == "path-0" {
						results = append(results, r)
					}
				}
				if len(results) != 1 || !results[0].WorkspaceMutation || !results[0].WorkspaceAllPaths || results[0].Executed == tc.deny {
					t.Fatalf("tool execution/effects conflated: %+v", results)
				}
			})
		}
	}
}
