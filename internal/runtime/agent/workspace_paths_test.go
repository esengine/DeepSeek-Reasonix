package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/workspacelease"
)

type namedLeaseWriter struct{ workspaceLeaseTestTool }

func (*namedLeaseWriter) WritesNamedPaths() bool { return true }

func (w *namedLeaseWriter) WritePaths(args json.RawMessage) ([]string, error) {
	var p struct {
		Path        string `json:"path"`
		Source      string `json:"source_path"`
		Destination string `json:"destination_path"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if w.Name() == "move_file" {
		if p.Source == "" || p.Destination == "" {
			return nil, nil
		}
		return []string{p.Source, p.Destination}, nil
	}
	if p.Path == "" {
		return nil, nil
	}
	return []string{p.Path}, nil
}

func TestWorkspaceLeaseExecutionScope(t *testing.T) {
	for _, tc := range []struct {
		name, toolName, args string
		hooks, blocked       bool
	}{
		{"disjoint", "write_file", `{"path":"b.go"}`, false, false},
		{"same", "write_file", `{"path":"a.go"}`, false, true},
		{"move source", "move_file", `{"source_path":"a.go","destination_path":"b.go"}`, false, true},
		{"move destination", "move_file", `{"source_path":"b.go","destination_path":"a.go"}`, false, true},
		{"unknown bash", "bash", `{"command":"opaque-program"}`, false, true},
		{"unstated effects", "computer_act", `{}`, false, true},
		{"mutating hook", "write_file", `{"path":"b.go"}`, true, true},
		{"incomplete move", "move_file", `{"source_path":"b.go"}`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, locks := testenv.TempDir(t), testenv.TempDir(t)
			if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
				t.Fatal(err)
			}
			first, err := workspacelease.New(root, locks, nil)
			if err != nil {
				t.Fatal(err)
			}
			second, err := workspacelease.New(root, locks, nil)
			if err != nil {
				t.Fatal(err)
			}
			first.BeginRun()
			second.BeginRun()
			defer first.EndRun()
			defer second.EndRun()
			if err := first.AcquirePaths(context.Background(), []string{"a.go"}); err != nil {
				t.Fatal(err)
			}
			writer := &namedLeaseWriter{workspaceLeaseTestTool: workspaceLeaseTestTool{name: tc.toolName}}
			a := deliveryLeaseTestAgent(t, second, writer)
			a.writeWorkspaceRoot = root
			if tc.hooks {
				a.svc.hooks = &workspaceLeaseTestHooks{}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			out := a.executeOne(ctx, &a.turn, provider.ToolCall{ID: "writer", Name: tc.toolName, Arguments: tc.args})
			if out.blocked != tc.blocked {
				t.Fatalf("blocked=%v, want %v: %+v", out.blocked, tc.blocked, out)
			}
			wantCalls := int32(1)
			if tc.blocked {
				wantCalls = 0
			}
			if writer.calls.Load() != wantCalls {
				t.Fatalf("writer calls=%d, want %d", writer.calls.Load(), wantCalls)
			}
		})
	}
}

func TestWorkspaceLeaseDoesNotTrustAToolNameAlone(t *testing.T) {
	a := &Agent{}
	plan := &toolCallPlan{runTool: &workspaceLeaseTestTool{name: "write_file"}, runArgs: json.RawMessage(`{"path":"a.go"}`)}
	if paths := a.workspaceWritePaths(plan); len(paths) != 0 {
		t.Fatalf("opaque name narrowed scope: %v", paths)
	}
}
