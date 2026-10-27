package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
	"reflect"
	"strings"
	"sync"
	"testing"
)

type continuationIsolationProvider struct {
	failAfterWrite bool
	mu             sync.Mutex
	ref            string
	children       []provider.Request
	results        []string
}

func (*continuationIsolationProvider) Name() string { return "continuation-isolation" }
func (p *continuationIsolationProvider) Stream(_ context.Context, r provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	call := func(id, name string, args any) {
		raw, _ := json.Marshal(args)
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: id, Name: name, Arguments: string(raw)}}
	}
	child := strings.Contains(firstUser(r), "ISOLATED-CONTINUATION-CHILD")
	if child {
		p.children = append(p.children, r)
		if toolResultAfterLastUser(r) {
			if p.failAfterWrite && p.ref == "" {
				return nil, fmt.Errorf("fixture failure after writing")
			}
			ch <- provider.Chunk{Type: provider.ChunkText, Text: "child complete"}
		} else {
			content := "first\n"
			if p.ref != "" {
				content = "second\n"
			}
			call("write", "write_file", map[string]string{"path": "out.txt", "content": content})
		}
	} else if toolResultAfterLastUser(r) {
		p.results = append(p.results, lastToolResult(r))
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "parent complete"}
	} else if p.ref == "" {
		call("initial", "task", map[string]any{"prompt": "ISOLATED-CONTINUATION-CHILD", "isolation": "worktree", "tools": []string{"write_file"}, "write_paths": []string{"out.txt"}})
	} else {
		call("continue", "task", map[string]any{"prompt": "continue and write second", "continue_from": p.ref, "tools": []string{"write_file"}, "write_paths": []string{"out.txt"}})
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectIsolatedContinuationAfterRestartUsesOriginalWorktreeAndPrefix(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprintf("failed=%v", failed), func(t *testing.T) { testIsolatedContinuationAfterRestart(t, failed) })
	}
}

func testIsolatedContinuationAfterRestart(t *testing.T, failed bool) {
	home, dir := statedBootHome(t), robustTempDir(t)
	p := &continuationIsolationProvider{failAfterWrite: failed}
	kind := uniqueKind("continuation-isolation")
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return p, nil })
	writeFile(t, dir, "reasonix.toml", `default_model = "probe"
[agent]
worktree_isolation = true
[codegraph]
enabled = false
[[providers]]
name = "probe"
kind = "`+kind+`"
model = "probe"
`)
	mirrorToUserConfigAt(t, home, dir)
	gitInDir(t, dir, "init", "-q")
	gitInDir(t, dir, "add", "-A")
	gitInDir(t, dir, "commit", "-qm", "fixture")
	build := func() *control.Controller {
		c, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, Sink: event.Discard})
		if err != nil {
			t.Fatal(err)
		}
		c.SetToolApprovalMode(control.ToolApprovalYolo)
		return c
	}
	c := build()
	c.EnsureSessionPath()
	if err := c.Run(context.Background(), "Delegate isolated work"); err != nil {
		t.Fatal(err)
	}
	runs, err := c.SubagentRuns()
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs=%v %v results=%v", runs, err, p.results)
	}
	p.ref = runs[0].Ref
	managed := filepath.Join(config.RootsForHome(home).DeliveryWorktreeDir(), "isolated")
	before, err := isolatedWorktreeRoots(managed)
	if err != nil || len(before) != 1 {
		t.Fatalf("worktrees=%v %v", before, err)
	}
	path := c.SessionPath()
	c.Close()
	sess, err := sessionstore.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	c = build()
	defer c.Close()
	if err := c.Resume(sess, path); err != nil {
		t.Fatal(err)
	}
	if err := c.Run(context.Background(), "Continue isolated work"); err != nil {
		t.Fatal(err)
	}
	after, err := isolatedWorktreeRoots(managed)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("worktree replaced: %v -> %v (%v)", before, after, err)
	}
	if data, err := os.ReadFile(filepath.Join(after[0], "out.txt")); err != nil || string(data) != "second\n" {
		t.Fatalf("retained content=%q %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "out.txt")); !os.IsNotExist(err) {
		t.Fatalf("isolated write reached source: %v", err)
	}
	runs, err = c.SubagentRuns()
	if err != nil || len(runs) != 1 || runs[0].Ref != p.ref {
		t.Fatalf("transcript replaced: %v %v", runs, err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.children) != 4 {
		t.Fatalf("child requests=%d parent results=%v", len(p.children), p.results)
	}
	for _, result := range p.results {
		if strings.Contains(result, "OUTSIDE DECLARED") {
			t.Fatal(result)
		}
	}
	if !reflect.DeepEqual(p.children[0].Tools, p.children[2].Tools) || p.children[0].Messages[0].Content != p.children[2].Messages[0].Content {
		t.Fatal("continuation changed stable prefix")
	}
	if !requestContains(p.children[2], "ISOLATED-CONTINUATION-CHILD") || (!failed && !requestContains(p.children[2], "child complete")) {
		t.Fatal("prior transcript missing")
	}
}
