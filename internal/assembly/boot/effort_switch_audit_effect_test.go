package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

func TestEffectChildEffortSelection(t *testing.T) {
	for _, tc := range []struct {
		name, model, effort, want string
		rejected                  bool
	}{
		{"implicit", "", "", "high", false},
		{"same-model", "relay/parent", "", "high", false},
		{"same-bare-model", "parent", "", "high", false},
		{"different-model", "relay/child", "", "low", false},
		{"explicit-effort", "relay/parent", "medium", "medium", false},
		{"explicit-auto", "relay/parent", "auto", "low", false},
		{"unsupported-child-effort", "relay/child", "max", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateConfigHome(t)
			rememberHome(t)
			fenceBootTestHistoryCatalog(t)
			t.Setenv("AUDIT_FAKE_KEY", "fixture-key")
			dir := robustTempDir(t)
			t.Chdir(dir)
			var mu sync.Mutex
			var sent []any
			var profiles []event.Profile
			var failures int
			args, err := json.Marshal(map[string]any{
				"action": "call", "capability_id": "task:subagent",
				"arguments": map[string]any{"prompt": "reply ok", "description": "probe", "max_steps": 2, "model": tc.model, "effort": tc.effort},
			})
			if err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				mu.Lock()
				first := len(sent) == 0
				sent = append(sent, body["reasoning_effort"])
				mu.Unlock()
				w.Header().Set("Content-Type", "text/event-stream")
				if first {
					payload, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "delegate", "type": "function", "function": map[string]any{"name": "use_capability", "arguments": string(args)}}}}, "finish_reason": "tool_calls"}}})
					fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", payload)
				} else {
					fmt.Fprint(w, chatCompletionsSSE)
				}
			}))
			defer srv.Close()
			writeFile(t, dir, "reasonix.toml", fmt.Sprintf(`
default_model = "relay/parent"
[codegraph]
enabled = false
[[providers]]
name = "relay"
kind = "openai"
base_url = "%s"
api_key_env = "AUDIT_FAKE_KEY"
models = ["parent", "child"]
supported_efforts = ["low", "medium", "high"]
default_effort = "low"
`, srv.URL))
			approveWorkspace(t, dir)
			high := "high"
			ctrl, err := Build(context.Background(), Options{Model: "relay/parent", WorkspaceRoot: dir, EffortOverride: &high, MaxSteps: 3, Sink: event.FuncSink(func(e event.Event) {
				if e.Kind == event.ToolResult && e.Tool.Err != "" {
					mu.Lock()
					failures++
					mu.Unlock()
				}
				if e.Kind == event.ToolDispatch && e.Tool.Profile != nil {
					mu.Lock()
					profiles = append(profiles, *e.Tool.Profile)
					mu.Unlock()
				}
			})})
			if err != nil {
				t.Fatal(err)
			}
			defer ctrl.Close()
			ctrl.EnsureSessionPath()
			if err := ctrl.Run(context.Background(), "delegate a neutral probe"); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if tc.rejected {
				if len(sent) != 2 || sent[0] != "high" || sent[1] != "high" || failures == 0 {
					t.Errorf("rejected child sent %v, failures %d; want parent-only requests and a tool error", sent, failures)
				}
			} else if len(sent) < 3 || sent[0] != "high" || sent[1] != tc.want || sent[len(sent)-1] != "high" {
				t.Errorf("parent/child/parent efforts = %v, want high/%s/high", sent, tc.want)
			}
			if len(profiles) == 0 || profiles[0].Effort != tc.effort || profiles[0].Model != tc.model {
				t.Errorf("dispatch profiles = %+v, want selection %s/%s", profiles, tc.model, tc.effort)
			}
			parent := strings.TrimSuffix(filepath.Base(ctrl.SessionPath()), ".jsonl")
			children, err := sessionstore.ListSubagentsByParent(ctrl.SessionDir(), parent)
			if err != nil {
				t.Fatal(err)
			}
			if tc.rejected {
				if len(children) != 0 {
					t.Errorf("rejected child persisted %+v", children)
				}
			} else if len(children) != 1 || children[0].Meta.Effort != tc.want {
				t.Errorf("persisted children = %+v, want effort %s", children, tc.want)
			}
		})
	}
}

func TestEffectModelEffortSwitchMatrix(t *testing.T) {
	isolateConfigHome(t)
	rememberHome(t)
	fenceBootTestHistoryCatalog(t)
	t.Setenv("AUDIT_FAKE_KEY", "fixture-key")
	dir := robustTempDir(t)
	t.Chdir(dir)
	var mu sync.Mutex
	var sent []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		mu.Lock()
		sent = append(sent, body)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, chatCompletionsSSE)
	}))
	defer srv.Close()
	writeFile(t, dir, "reasonix.toml", fmt.Sprintf(`
default_model = "relay/parent"
[codegraph]
enabled = false
[[providers]]
name = "relay"
kind = "openai"
base_url = "%s"
api_key_env = "AUDIT_FAKE_KEY"
models = ["parent", "child", "plain"]
supported_efforts = ["low", "medium", "high"]
default_effort = "low"
model_overrides = { child = { supported_efforts = ["low", "high"], default_effort = "high" }, plain = { reasoning_protocol = "none" } }
`, srv.URL))
	approveWorkspace(t, dir)
	var current *control.Controller
	defer func() {
		if current != nil {
			current.Close()
		}
	}()
	for _, tc := range []struct {
		name, model string
		effort      *string
		want        any
		resume      bool
	}{
		{"initial", "parent", nil, "low", false},
		{"model-only", "child", nil, "high", false},
		{"effort-only", "child", new(("low")), "low", false},
		{"both", "parent", new(("medium")), "medium", false},
		{"without-effort", "plain", new(("medium")), nil, false},
		{"switch-back", "parent", new(("medium")), "medium", false},
		{"unsupported", "child", new(("medium")), "high", false},
		{"explicit-default", "child", new(("high")), "high", false},
		{"auto", "child", new(("")), "high", false},
		{"resume", "child", new(("low")), "low", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output string
			next, err := Build(context.Background(), Options{Model: "relay/" + tc.model, WorkspaceRoot: dir, EffortOverride: tc.effort, Sink: event.FuncSink(func(e event.Event) {
				if e.Kind == event.Text {
					output += e.Text
				}
			})})
			if err != nil {
				t.Fatal(err)
			}
			if current != nil {
				if err := current.Snapshot(); err != nil {
					t.Fatal(err)
				}
				if tc.resume {
					loaded, err := sessionstore.LoadSession(current.SessionPath())
					if err != nil {
						t.Fatal(err)
					}
					if err := next.Resume(loaded, current.SessionPath()); err != nil {
						t.Fatal(err)
					}
				} else if err := ApplyRuntimeMigration(next, current, CaptureRuntimeMigration(current)); err != nil {
					t.Fatal(err)
				}
				current.Close()
			} else {
				next.EnsureSessionPath()
			}
			current = next
			if got := current.RuntimeSelection().ModelRef; got != "relay/"+tc.model {
				t.Errorf("model = %s", got)
			}
			if err := current.Run(context.Background(), "neutral probe "+tc.name); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			body := sent[len(sent)-1]
			mu.Unlock()
			got := body["reasoning_effort"]
			if body["model"] != tc.model {
				t.Errorf("request model = %v, want %s", body["model"], tc.model)
			}
			if got != tc.want || output != "ok" {
				t.Errorf("request effort = %v, sink = %q; want %v / ok", got, output, tc.want)
			}
		})
	}
}
