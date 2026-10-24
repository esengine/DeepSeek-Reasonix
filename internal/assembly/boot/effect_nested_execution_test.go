package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/session/control"
)

type nestedExecutionProvider struct {
	mu        sync.Mutex
	results   map[string]string
	onLeaf    func()
	extraFile bool
}

func (*nestedExecutionProvider) Name() string { return "nested-execution" }

func (p *nestedExecutionProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	level := "root"
	if strings.Contains(firstUser(req), "NESTED-LEAF") {
		level = "leaf"
	} else if strings.Contains(firstUser(req), "NESTED-PARENT") {
		level = "parent"
	}
	ch := make(chan provider.Chunk, 3)
	if hasToolResult(req) {
		if level == "leaf" && p.onLeaf != nil {
			p.onLeaf()
		}
		p.mu.Lock()
		p.results[level] = lastToolResult(req)
		p.mu.Unlock()
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "retained " + level}
	} else {
		name, args := "task", map[string]any{"prompt": "NESTED-PARENT", "isolation": "worktree"}
		switch level {
		case "parent":
			args["prompt"] = "NESTED-LEAF"
			args["write_paths"] = []string{"child.txt"}
			if p.extraFile {
				args["write_paths"] = []string{"child.txt", "other.txt"}
			}
		case "leaf":
			name, args = "write_file", map[string]any{"path": "child.txt", "content": "nested candidate\n"}
		}
		raw, _ := json.Marshal(args)
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: level + "-call", Name: name, Arguments: string(raw)}}
		if level == "leaf" && p.extraFile {
			raw, _ := json.Marshal(map[string]string{"path": "other.txt", "content": "other candidate\n"})
			ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "leaf-extra", Name: "write_file", Arguments: string(raw)}}
		}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectNestedExecutionRetainsRegisteredDescendantAtItsDirectSource(t *testing.T) {
	p := &nestedExecutionProvider{results: map[string]string{}}
	ctrl, root := nestedExecutionFixture(t, p, "", "", event.Discard)
	if err := ctrl.Run(t.Context(), "run the nested isolation fixture"); err != nil {
		t.Fatal(err)
	}
	catalog, err := ctrl.IsolationResults(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var child, parent *isolation.ResultSummary
	for index := range catalog.Results {
		row := &catalog.Results[index]
		if row.Kind != "result" {
			continue
		}
		if row.ParentID != "" {
			child = row
		} else {
			parent = row
		}
	}
	if child == nil || parent == nil || child.ParentID != parent.ID || child.TargetWorkspace != parent.CandidateRoot {
		p.mu.Lock()
		defer p.mu.Unlock()
		t.Fatalf("real nested task lost its registered direct source: %+v; results=%v", catalog, p.results)
	}
	if parent.State != isolation.Interrupted || parent.Activity != "inactive" {
		t.Fatalf("returned parent still appears to execute: %+v", parent)
	}
	if raw, err := os.ReadFile(filepath.Join(child.CandidateRoot, "child.txt")); err != nil || string(raw) != "nested candidate\n" {
		p.mu.Lock()
		defer p.mu.Unlock()
		t.Fatalf("nested candidate write: %q %v; results=%v catalog=%+v", raw, err, p.results, catalog)
	}
	for _, source := range []string{root, parent.CandidateRoot} {
		if _, err := os.Stat(filepath.Join(source, "child.txt")); !os.IsNotExist(err) {
			t.Fatalf("unverified nested candidate wrote a source: %v", err)
		}
	}
}

func nestedExecutionFixture(t *testing.T, p provider.Provider, runtime, checks string, sink event.Sink) (*control.Controller, string) {
	t.Helper()
	opts := nestedExecutionOptions(t, p, runtime, checks, sink)
	ctrl, err := Build(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ctrl.Close)
	bindPresenceSession(t, ctrl)
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	return ctrl, opts.WorkspaceRoot
}

func nestedExecutionOptions(t *testing.T, p provider.Provider, runtime, checks string, sink event.Sink) Options {
	t.Helper()
	home, root := statedBootHome(t), t.TempDir()
	kind := "nested-execution-" + strings.ToLower(t.Name())
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return p, nil })
	writeFile(t, root, "reasonix.toml", fmt.Sprintf(`default_model="fixture"
[agent]
collaboration="shared"
max_subagent_concurrency=4
max_parallel_writers=4
max_subagent_depth=4
verification_runtime=%q
[codegraph]
enabled=false
[[providers]]
name="fixture"
kind=%q
model="fixture"
`, runtime, kind))
	if checks != "" {
		writeFile(t, root, "REASONIX.md", "## Reasonix host checks\n\n- verify: "+checks+"\n")
	}
	mirrorToUserConfigAt(t, home, root)
	gitInDir(t, root, "init", "-q")
	gitInDir(t, root, "config", "core.autocrlf", "false")
	gitInDir(t, root, "add", "-A")
	gitInDir(t, root, "commit", "-qm", "fixture")
	return Options{Home: home, WorkspaceRoot: root, Sink: sink}
}
