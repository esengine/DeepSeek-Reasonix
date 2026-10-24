package boot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/session/control"
)

type hostCheckScriptProvider struct {
	isolationScriptProvider
	allowShell bool
}

func (p *hostCheckScriptProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if strings.Contains(firstUser(req), isolatedTask) {
		return p.isolationScriptProvider.Stream(ctx, req)
	}
	ch := make(chan provider.Chunk, 2)
	if hasToolResult(req) {
		p.mu.Lock()
		p.childSaw = lastToolResult(req)
		p.mu.Unlock()
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "candidate retained"}
	} else {
		names := []string{"write_file"}
		if p.allowShell {
			names = append(names, "bash")
		}
		args, _ := json.Marshal(map[string]any{"prompt": isolatedTask, "isolation": "worktree", "tools": names, "max_steps": 4})
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "task", Name: "task", Arguments: string(args)}}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectHostChecksKeepOriginalToolGrantAndCandidateAttribution(t *testing.T) {
	for _, name := range []string{"allowed", "restricted", "cancel", "cancel-integration", "panic"} {
		t.Run(name, func(t *testing.T) {
			home, root := statedBootHome(t), t.TempDir()
			managed := filepath.Join(config.RootsForHome(home).DeliveryWorktreeDir(), "isolated")
			p := &hostCheckScriptProvider{isolationScriptProvider: isolationScriptProvider{dir: root, managed: managed}, allowShell: name == "allowed" || name == "cancel" || name == "cancel-integration"}
			kind := "host-check-effect-" + name
			provider.Register(kind, func(provider.Config) (provider.Provider, error) { return p, nil })
			writeFile(t, root, "reasonix.toml", `default_model="fixture"
[agent]
collaboration="shared"
[codegraph]
enabled=false
[[providers]]
name="fixture"
kind="`+kind+`"
model="fixture"
`)
			writeFile(t, root, "REASONIX.md", "## Reasonix host checks\n\n- verify: git diff --check\n")
			mirrorToUserConfigAt(t, home, root)
			gitInDir(t, root, "init", "-q")
			gitInDir(t, root, "add", "-A")
			gitInDir(t, root, "commit", "-qm", "fixture")
			var mu sync.Mutex
			var results []event.Event
			var panicArmed atomic.Bool
			var hostDispatches atomic.Int32
			runCtx, cancel := context.WithCancel(t.Context())
			defer cancel()
			c, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: event.FuncSink(func(e event.Event) {
				if e.Kind == event.ToolDispatch && e.Tool.Issuer == event.IssuedByHost && e.Tool.Name == "bash" {
					check := hostDispatches.Add(1)
					if (name == "cancel" && check == 1) || (name == "cancel-integration" && check == 2) {
						cancel()
					}
				}
				if e.Kind == event.ToolResult && e.Tool.Issuer == event.IssuedByHost && e.Tool.Name == "bash" {
					mu.Lock()
					results = append(results, e)
					mu.Unlock()
					panicArmed.Store(name == "panic")
				}
				if e.Kind == event.Notice && e.Level == event.LevelWarn && panicArmed.CompareAndSwap(true, false) {
					panic("fixture verification notice failure")
				}
			})})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			cfg, err := config.RootsForHome(home).LoadForRootReadOnly(root)
			if err != nil {
				t.Fatal(err)
			}
			canExecute := sandbox.AvailableFor(sandbox.Spec{Mode: "enforce", RequiredIntegrity: sandbox.CompleteFileIntegrity, Network: cfg.Sandbox.Network})
			t.Logf("candidate program execution available: %t", canExecute)
			c.EnsureSessionPath()
			c.SetToolApprovalMode(control.ToolApprovalYolo)
			if err := c.Run(runCtx, "delegate an isolated writer"); err != nil && name != "cancel" && name != "cancel-integration" {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			wantChecks := 1
			if canExecute && (name == "allowed" || name == "cancel-integration") {
				wantChecks = 2 // original result, then the fresh integration candidate
			}
			if len(results) != wantChecks {
				raw, _ := json.Marshal(results)
				t.Fatalf("host check executions = %d, want %d; checks=%s; result=%s", len(results), wantChecks, raw, p.childSaw)
			}
			if results[0].Tool.Executed != (name == "allowed" || name == "cancel-integration") {
				t.Fatalf("host check changed original grant: %+v", results[0].Tool)
			}
			if canExecute && name == "allowed" && (results[0].Tool.Err != "" || results[0].Tool.Execution == nil || results[0].Tool.Execution.ExitCode == nil || *results[0].Tool.Execution.ExitCode != 0) {
				t.Fatalf("host check did not execute successfully: %+v", results[0].Tool)
			}
			if _, err := os.Stat(filepath.Join(root, "out.txt")); !os.IsNotExist(err) {
				t.Fatalf("candidate wrote to source: %v", err)
			}
			marks, err := filepath.Glob(filepath.Join(managed, "*", "entry-v2.json"))
			if err != nil || len(marks) != 1 {
				t.Fatalf("candidate records: %v %v", marks, err)
			}
			raw, err := os.ReadFile(marks[0])
			if err != nil {
				t.Fatal(err)
			}
			var saved struct {
				Evidence *isolation.CandidateEvidence `json:"evidence"`
				State    isolation.State              `json:"state"`
				ID       string                       `json:"id"`
			}
			if err := json.Unmarshal(raw, &saved); err != nil || saved.Evidence == nil || saved.Evidence.Verification != "unknown" {
				t.Fatalf("candidate check promoted source verification: %s %v", raw, err)
			}
			if !canExecute && p.allowShell && name != "cancel" {
				execution := results[0].Tool.Execution
				if len(saved.Evidence.Checks) != 1 || saved.Evidence.Checks[0].Success || results[0].Tool.RefusalCode == "" ||
					execution == nil || execution.State != tool.ShellStateNotRun || execution.ExitCode != nil {
					t.Fatalf("unsupported execution invented success or lost refusal: %s %+v", raw, results[0].Tool)
				}
				attempts := isolation.NewStore(managed, root, c.WorkspaceRepo()).IntegrationAttempts()
				if len(attempts) != 0 {
					t.Fatal("unsupported check started integration")
				}
			}
			if canExecute && name == "allowed" {
				if len(saved.Evidence.Checks) != 1 || !saved.Evidence.Checks[0].Success || len(saved.Evidence.Checks[0].Inputs) != 1 {
					t.Fatalf("host launch did not reach durable check evidence: %s", raw)
				}
				input := saved.Evidence.Checks[0].Inputs[0]
				if !input.Completed || !input.Stable || input.Directory == root || len(input.Gaps) == 0 || input.EnvironmentHash == "" {
					t.Fatalf("candidate launch identity or coverage gaps lost: %+v", input)
				}
				attempts, err := filepath.Glob(filepath.Join(managed, ".integration-v1", "int_*.json"))
				if err != nil || len(attempts) != 1 {
					t.Fatalf("integration attempt was not persisted: %v %v", attempts, err)
				}
				attemptRaw, err := os.ReadFile(attempts[0])
				if err != nil {
					t.Fatal(err)
				}
				var attempt isolation.IntegrationAttempt
				if err := json.Unmarshal(attemptRaw, &attempt); err != nil || attempt.ResultID != saved.ID ||
					attempt.Verification != "unknown" || len(attempt.Checks) != 1 || attempt.Candidate.WorkspaceRoot == input.Directory {
					t.Fatalf("integration reused original environment or invented verification: %s %v", attemptRaw, err)
				}
			}
			if canExecute && name == "cancel-integration" {
				attempts := isolation.NewStore(managed, root, c.WorkspaceRepo()).IntegrationAttempts()
				if len(attempts) != 1 || attempts[0].Attempt == nil || attempts[0].Attempt.State != isolation.Cancelled || attempts[0].Attempt.Verification != "unknown" || attempts[0].Active {
					t.Fatalf("second-round cancellation was swallowed or retained ownership: %+v", attempts)
				}
			}
			want := isolation.Pending
			switch name {
			case "cancel":
				want = isolation.Cancelled
			case "panic":
				want = isolation.Failed
			}
			if saved.State != want {
				t.Fatalf("check phase termination lost: state=%s want=%s", saved.State, want)
			}
			manager := isolation.NewStore(managed, root, c.WorkspaceRepo())
			preview, err := manager.PreviewDiscard(t.Context(), saved.ID)
			if err != nil {
				t.Fatalf("finished candidate retained active ownership: %v", err)
			}
			if _, err := manager.DiscardExpected(t.Context(), saved.ID, preview.Revision); err != nil {
				t.Fatalf("finished candidate retained active ownership: %v", err)
			}
		})
	}
}
