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
)

type admissionHolderProvider struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (*admissionHolderProvider) Name() string { return "boot-admission-holder" }

func (p *admissionHolderProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.once.Do(func() { close(p.started) })
	select {
	case <-p.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "finished without changes"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// A real background task owns an explicit path while its first model request
// is blocked. The optimistic sibling must honor that live scheduler claim,
// through either a direct file tool or a dynamically resolved capability.
func TestEffectOptimisticSiblingHonorsNamedWriteAdmission(t *testing.T) {
	for _, dispatch := range []string{"task", "fleet"} {
		for _, dynamic := range []bool{false, true} {
			for _, overlap := range []bool{false, true} {
				mode, scope := "direct", "disjoint"
				if dynamic {
					mode = "capability"
				}
				if overlap {
					scope = "overlap"
				}
				t.Run(dispatch+"/"+mode+"/"+scope, func(t *testing.T) {
					home, dir := statedBootHome(t), robustTempDir(t)
					ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
					defer cancel()
					holder := &admissionHolderProvider{started: make(chan struct{}), release: make(chan struct{})}
					observer := &browserScriptProvider{}
					target := filepath.Join(dir, "other.txt")
					if overlap {
						target = filepath.Join(dir, "claimed.txt")
					}
					worker := &browserScriptProvider{rounds: []func(string) *provider.ToolCall{
						func(string) *provider.ToolCall {
							args := map[string]any{"path": target, "content": "optimistic write\n"}
							if dynamic {
								return browserCall("writer", "use_capability", map[string]any{
									"action": "call", "capability_id": "tool:write_file", "arguments": args,
								})
							}
							return browserCall("writer", "write_file", args)
						},
					}}
					parent := &browserScriptProvider{rounds: []func(string) *provider.ToolCall{
						func(string) *provider.ToolCall {
							return browserCall("holder", "task", map[string]any{
								"prompt": "Hold claimed.txt until released", "model": "holder",
								"write_paths": []string{"claimed.txt"}, "run_in_background": true,
							})
						},
						func(string) *provider.ToolCall {
							select {
							case <-holder.started:
							case <-ctx.Done():
								t.Error("background task never entered its model request")
								return nil
							}
							if dispatch == "fleet" {
								return browserCall("optimistic", "fleet", map[string]any{"tasks": []any{
									map[string]any{"id": "writer", "prompt": "Write the requested file", "model": "worker"},
									map[string]any{"id": "observer", "prompt": "Observe without writing", "model": "observer", "read_only": true},
								}})
							}
							return browserCall("optimistic", dispatch, map[string]any{
								"prompt": "Write the requested file", "model": "worker",
							})
						},
					}}
					kind := "boot-optimistic-admission-" + strings.ToLower(t.Name())
					provider.Register(kind, func(cfg provider.Config) (provider.Provider, error) {
						switch cfg.Model {
						case "holder":
							return holder, nil
						case "worker":
							return worker, nil
						case "observer":
							return observer, nil
						default:
							return parent, nil
						}
					})
					writeFile(t, dir, "reasonix.toml", `
default_model = "parent"

[agent]
subagent_claims = "optimistic"

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

[[providers]]
name = "worker"
kind = "`+kind+`"
model = "worker"

[[providers]]
name = "observer"
kind = "`+kind+`"
model = "observer"
`)
					approveWorkspaceAt(t, home, dir)
					ctrl, err := Build(ctx, Options{Home: home, WorkspaceRoot: dir, Sink: event.Discard})
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() {
						close(holder.release)
						ctrl.Close()
						if manager := ctrl.JobManager(); manager != nil {
							done := make(chan struct{})
							manager.AfterUnfinished(func() { close(done) })
							<-done
						}
					})
					ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
					if err := ctrl.Run(ctx, "Start a background holder, then run the optimistic writer"); err != nil {
						t.Fatal(err)
					}
					reqs := worker.requests()
					if len(reqs) < 2 {
						t.Fatalf("optimistic writer never returned a tool result: %d requests", len(reqs))
					}
					results := effectToolResults(reqs[len(reqs)-1])
					if len(results) != 1 {
						t.Fatalf("writer results = %q", results)
					}
					content, readErr := os.ReadFile(target)
					if overlap {
						if !os.IsNotExist(readErr) || !strings.Contains(results[0], "blocked:") {
							t.Fatalf("optimistic writer bypassed the live claim: file=%q, read=%v, result=%q", content, readErr, results[0])
						}
					} else if readErr != nil || string(content) != "optimistic write\n" {
						t.Fatalf("disjoint writer was blocked: file=%q, read=%v, result=%q", content, readErr, results[0])
					}
					for _, requests := range [][]provider.Request{parent.requests(), reqs} {
						first, _ := json.Marshal(requests[0].Tools)
						for _, request := range requests[1:] {
							tools, _ := json.Marshal(request.Tools)
							if !bytes.Equal(first, tools) || systemOf(request) != systemOf(requests[0]) {
								t.Fatal("admission changed the stable provider prefix")
							}
						}
					}
				})
			}
		}
	}
}
