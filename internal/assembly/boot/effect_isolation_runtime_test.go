package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

const isolationRuntimePrompt = "ISOLATED-RUNTIME: inspect a.txt"

type isolationRuntimeRequest struct {
	model, effort string
	request       provider.Request
}

type isolationRuntimeRecorder struct {
	mu       sync.Mutex
	requests []isolationRuntimeRequest
	args     map[string]any
	deferred bool
	result   string
}

type isolationRuntimeProvider struct {
	recorder      *isolationRuntimeRecorder
	model, effort string
}

func (*isolationRuntimeProvider) Name() string { return "boot-isolation-runtime" }

func (p *isolationRuntimeProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	r := p.recorder
	r.mu.Lock()
	defer r.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	call := func(id, name string, args any) {
		raw, _ := json.Marshal(args)
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: id, Name: name, Arguments: string(raw)}}
	}
	if strings.Contains(firstUser(req), isolationRuntimePrompt) {
		r.requests = append(r.requests, isolationRuntimeRequest{model: p.model, effort: p.effort, request: req})
		// A finite scripted ceiling bounds the negative control even without a cap.
		if len(r.requests) >= 10 {
			ch <- provider.Chunk{Type: provider.ChunkText, Text: "inspection finished"}
		} else {
			call(fmt.Sprintf("read-%d", len(r.requests)), "read_file", map[string]string{"path": fmt.Sprintf("a-%d.txt", len(r.requests))})
		}
	} else if hasToolResult(req) {
		r.result = lastToolResult(req)
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "parent done"}
	} else if r.deferred {
		call("delegate", "use_capability", map[string]any{"action": "call", "capability_id": "task:subagent", "arguments": r.args})
	} else {
		call("delegate", "task", r.args)
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectIsolatedTaskHonorsRuntimeSelectionAndBudget(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	cases := []struct {
		name, configuredModel, inheritedEffort, callModel, callEffort, parentEffort string
		wantModel, wantEffort                                                       string
		unsupported, deriveBudget, noParentCap, shared                              bool
		wantRequests                                                                int
	}{
		{name: "explicit", configuredModel: "child-model", callModel: "alternate-model", callEffort: "low", wantModel: "alternate", wantEffort: "low"},
		{name: "configured-model", configuredModel: "child-model", callEffort: "low", wantModel: "child", wantEffort: "low"},
		{name: "inherited-effort", configuredModel: "child-model", inheritedEffort: "low", wantModel: "child", wantEffort: "low"},
		{name: "parent-runtime-effort", parentEffort: "low", wantModel: "parent", wantEffort: "low"},
		{name: "unsupported-inherited-effort", callModel: "child-model", inheritedEffort: "max", wantModel: "child", wantEffort: "high"},
		{name: "unsupported-explicit-effort", configuredModel: "child-model", callEffort: "max", unsupported: true},
		{name: "unsupported-configured-pair", configuredModel: "child-model", inheritedEffort: "max", unsupported: true},
		{name: "derived-budget", configuredModel: "child-model", wantModel: "child", wantEffort: "high", deriveBudget: true, wantRequests: 7},
		{name: "automatic-budget", wantModel: "parent", wantEffort: "high", deriveBudget: true, noParentCap: true, wantRequests: 10},
		{name: "shared-parent-alias", callModel: "parent-model", parentEffort: "low", wantModel: "parent", wantEffort: "low", shared: true},
	}
	for _, tc := range cases {
		for _, deferred := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/deferred=%t", tc.name, deferred), func(t *testing.T) {
				home, root := statedBootHome(t), testenv.TempDir(t)
				kind := uniqueKind("isolation-runtime")
				rec := &isolationRuntimeRecorder{deferred: deferred, args: map[string]any{
					"prompt": isolationRuntimePrompt, "isolation": "worktree", "max_steps": 1,
				}}
				if tc.deriveBudget {
					delete(rec.args, "max_steps")
				}
				if tc.shared {
					delete(rec.args, "isolation")
				}
				if tc.callModel != "" {
					rec.args["model"] = tc.callModel
				}
				if tc.callEffort != "" {
					rec.args["effort"] = tc.callEffort
				}
				provider.Register(kind, func(cfg provider.Config) (provider.Provider, error) {
					effort, _ := cfg.Extra["effort"].(string)
					return &isolationRuntimeProvider{recorder: rec, model: cfg.Model, effort: effort}, nil
				})
				var config strings.Builder
				fmt.Fprintf(&config, `
default_model = "parent-model"
[agent]
system_prompt = "BASE"
worktree_isolation = true
subagent_model = %q
subagent_effort = %q
[codegraph]
enabled = false
`, tc.configuredModel, tc.inheritedEffort)
				for _, model := range []string{"parent", "child", "alternate"} {
					fmt.Fprintf(&config, `
[[providers]]
name = %q
kind = %q
model = %q
supported_efforts = ["low", "high"]
default_effort = "high"
`, model+"-model", kind, model)
				}
				writeFile(t, root, "reasonix.toml", config.String())
				writeFile(t, root, "a.txt", "read me\n")
				for i := 1; i <= 10; i++ {
					writeFile(t, root, fmt.Sprintf("a-%d.txt", i), fmt.Sprintf("round %d\n", i))
				}
				gitInDir(t, root, "init", "-q")
				mirrorToUserConfigAt(t, home, root)
				gitInDir(t, root, "add", "-A")
				gitInDir(t, root, "commit", "-q", "-m", "init")
				opts := Options{Home: home, WorkspaceRoot: root, MaxSteps: 12, Sink: event.Discard}
				if tc.noParentCap {
					opts.MaxSteps = 0
				}
				if tc.parentEffort != "" {
					opts.EffortOverride = &tc.parentEffort
				}
				ctrl, err := Build(t.Context(), opts)
				if err != nil {
					t.Fatal(err)
				}
				defer ctrl.Close()
				ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
				if err := ctrl.Run(t.Context(), "delegate the inspection"); err != nil {
					t.Fatal(err)
				}
				rec.mu.Lock()
				defer rec.mu.Unlock()
				if tc.unsupported {
					if len(rec.requests) != 0 || !strings.Contains(rec.result, "max") {
						t.Fatalf("unsupported effort silently executed: requests=%d result=%q", len(rec.requests), rec.result)
					}
					return
				}
				wantRequests := tc.wantRequests
				if wantRequests == 0 {
					wantRequests = 2
				}
				if len(rec.requests) != wantRequests {
					t.Errorf("child request count = %d, want %d; result=%q", len(rec.requests), wantRequests, rec.result)
				}
				for _, got := range rec.requests {
					if got.model != tc.wantModel || got.effort != tc.wantEffort {
						t.Errorf("child runtime = model %q effort %q; want %q/%q", got.model, got.effort, tc.wantModel, tc.wantEffort)
					}
				}
				if len(rec.requests) > 1 {
					first, _ := json.Marshal(rec.requests[0].request.Tools)
					last, _ := json.Marshal(rec.requests[len(rec.requests)-1].request.Tools)
					if string(first) != string(last) || len(rec.requests[0].request.Tools) == 0 {
						t.Fatal("child tool definitions changed at finalization")
					}
					initial := rec.requests[0].request.Messages[0]
					final := rec.requests[len(rec.requests)-1].request.Messages[0]
					if initial.Role != provider.RoleSystem || final.Role != provider.RoleSystem || initial.Content != final.Content {
						t.Fatal("child system prefix changed during the run")
					}
				}
			})
		}
	}
}
