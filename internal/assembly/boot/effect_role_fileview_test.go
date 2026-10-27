package boot

import (
	"bytes"
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
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

type roleViewProvider struct {
	mu    sync.Mutex
	reqs  []provider.Request
	next  func(int) *provider.ToolCall
	final string
}

func (*roleViewProvider) Name() string { return "role-fileview" }

func (p *roleViewProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	n := len(p.reqs)
	p.reqs = append(p.reqs, req)
	p.mu.Unlock()
	out := make(chan provider.Chunk, 2)
	if call := p.next(n); call != nil {
		out <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: call}
	} else {
		out <- provider.Chunk{Type: provider.ChunkText, Text: p.final}
	}
	out <- provider.Chunk{Type: provider.ChunkDone}
	close(out)
	return out, nil
}

func (p *roleViewProvider) requests() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]provider.Request(nil), p.reqs...)
}

func TestEffectRoleReadCannotRefreshExecutorFileView(t *testing.T) {
	for _, role := range []string{"planner", "guardian"} {
		for _, dynamic := range []bool{false, true} {
			name := "direct"
			if dynamic {
				name = "capability"
			}
			t.Run(role+"/"+name, func(t *testing.T) {
				home, dir := statedBootHome(t), robustTempDir(t)
				target := filepath.Join(dir, "notes.md")
				writeFile(t, dir, "notes.md", "original\n")
				const current = "the user's new version\n"
				reviewer := &roleViewProvider{final: "Plan: no changes."}
				reviewer.next = func(n int) *provider.ToolCall {
					if n != 0 {
						return nil
					}
					if dynamic {
						return browserCall("role-read", "use_capability", map[string]any{
							"action": "call", "capability_id": "tool:read_file", "arguments": map[string]any{"path": target},
						})
					}
					return browserCall("role-read", "read_file", map[string]any{"path": target})
				}
				if role == "guardian" {
					reviewer.final = `{"risk_level":"low","user_authorization":"high","outcome":"allow","rationale":"requested write"}`
				}
				executor := &roleViewProvider{final: "done"}
				executor.next = func(n int) *provider.ToolCall {
					if n == 0 {
						return browserCall("executor-read", "read_file", map[string]any{"path": target})
					}
					if role == "guardian" && n == 1 {
						if err := os.WriteFile(target, []byte(current), 0600); err != nil {
							t.Error(err)
						}
						return browserCall("request-review", "write_file", map[string]any{"path": filepath.Join(dir, "out.txt"), "content": "new file\n"})
					}
					if n == 2 {
						return browserCall("stale-write", "write_file", map[string]any{"path": target, "content": "stale executor version\n"})
					}
					return nil
				}
				kind := uniqueKind("boot-role-fileview")
				provider.Register(kind, func(cfg provider.Config) (provider.Provider, error) {
					if cfg.Model == role {
						return reviewer, nil
					}
					return executor, nil
				})
				writeFile(t, dir, "reasonix.toml", `
default_model = "executor"
[agent]
`+role+`_model = "reviewer"
[codegraph]
enabled = false
[[providers]]
name = "executor"
kind = "`+kind+`"
model = "executor"
[[providers]]
name = "reviewer"
kind = "`+kind+`"
model = "`+role+`"
`)
				approveWorkspaceAt(t, home, dir)
				var ctrl *control.Controller
				var ready sync.WaitGroup
				ready.Add(1)
				sink := event.FuncSink(func(e event.Event) {
					if e.Kind == event.ApprovalRequest {
						go func() { ready.Wait(); ctrl.Approve(e.Approval.ID, true, false, false) }()
					}
				})
				var err error
				ctrl, err = Build(t.Context(), Options{Home: home, WorkspaceRoot: dir, Sink: sink})
				ready.Done()
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(ctrl.Close)
				ctrl.SetFreshSessionPath(sessionstore.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
				ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
				defer cancel()
				if role == "planner" {
					ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
					if err := ctrl.Run(ctx, "Read notes.md"); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(target, []byte(current), 0600); err != nil {
						t.Fatal(err)
					}
					if err := ctrl.Run(ctx, control.PlannerRouteMarker+" inspect notes.md without executing changes"); err != nil {
						t.Fatal(err)
					}
				} else {
					ctrl.SetToolApprovalMode(control.ToolApprovalAsk)
					ctrl.EnableInteractiveApproval()
				}
				if err := ctrl.Run(ctx, "Update notes.md"); err != nil {
					t.Fatal(err)
				}
				reqs := reviewer.requests()
				if len(reqs) < 2 {
					t.Fatalf("%s did not finish its read: %d requests", role, len(reqs))
				}
				if results := effectToolResults(reqs[1]); len(results) == 0 || !strings.Contains(results[0], "the user's new version") {
					t.Fatalf("%s did not receive current file bytes: %q", role, results)
				}
				got, err := os.ReadFile(target)
				if err != nil || string(got) != current {
					t.Fatalf("%s read let executor overwrite unseen content: %q; %v", role, got, err)
				}
				execReqs := executor.requests()
				if len(execReqs) < 2 || !strings.Contains(strings.Join(effectToolResults(execReqs[1]), "\n"), "original") {
					t.Fatal("executor did not receive the original file")
				}
				results := effectToolResults(execReqs[len(execReqs)-1])
				if len(results) == 0 || !strings.Contains(results[len(results)-1], "modified after you last read or wrote it") {
					t.Fatalf("executor did not receive stale-view refusal: %q", results)
				}
				for _, requests := range [][]provider.Request{reqs, execReqs} {
					first, _ := json.Marshal(requests[0].Tools)
					for _, req := range requests[1:] {
						tools, _ := json.Marshal(req.Tools)
						if !bytes.Equal(first, tools) || systemOf(req) != systemOf(requests[0]) {
							t.Fatal("role binding changed the stable provider prefix")
						}
					}
				}
			})
		}
	}
}
