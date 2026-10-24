package boot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/session/control"
	"reasonix/internal/state/mutation"
)

type fleetTransactionProvider struct {
	scenario   fleetTransactionScenario
	written    chan string
	finish     chan struct{}
	read       chan string
	edit       chan struct{}
	mu         sync.Mutex
	rootResult string
	prefix     []byte
}

type fleetTransactionScenario struct{ background, sameFile bool }

func (*fleetTransactionProvider) Name() string { return "fleet-transactions" }

func (p *fleetTransactionProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	call := func(id, name string, args any) {
		raw, _ := json.Marshal(args)
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: id, Name: name, Arguments: string(raw)}}
	}
	text := func() { ch <- provider.Chunk{Type: provider.ChunkText, Text: "fleet result retained"} }
	id := ""
	for _, item := range []string{"a", "b"} {
		if strings.Contains(firstUser(req), "FLEET-ITEM-"+item) {
			id = item
		}
	}
	if id == "" {
		if err := p.checkPrefix(req); err != nil {
			return nil, err
		}
	}
	if id != "" {
		if err := p.child(ctx, req, id, call, text); err != nil {
			return nil, err
		}
	} else if !hasToolResult(req) {
		tasks := []map[string]any{}
		for _, item := range []string{"a", "b"} {
			paths, tools := []string{"a.md", "b.md"}, []string{"write_file"}
			if p.scenario.sameFile {
				paths, tools = []string{"shared.md"}, []string{"read_file", "edit_file"}
			}
			tasks = append(tasks, map[string]any{"prompt": "FLEET-ITEM-" + item, "write_paths": paths, "tools": tools})
		}
		call("fleet-run", "fleet", map[string]any{"tasks": tasks, "run_in_background": p.scenario.background})
	} else {
		result := lastToolResult(req)
		p.mu.Lock()
		p.rootResult = result
		p.mu.Unlock()
		if _, waited := callResult(req, "fleet-wait"); p.scenario.background && !waited {
			var job string
			if _, err := fmt.Sscanf(result, "Started background fleet %q", &job); err != nil || job == "" {
				return nil, fmt.Errorf("fleet background job missing: %s", result)
			}
			call("fleet-wait", "wait", map[string]any{"job_ids": []string{job}})
		} else {
			text()
		}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *fleetTransactionProvider) checkPrefix(req provider.Request) error {
	if len(req.Messages) == 0 {
		return fmt.Errorf("root request has no system prefix")
	}
	current, err := json.Marshal(struct {
		System provider.Message
		Tools  any
	}{req.Messages[0], req.Tools})
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.prefix) == 0 {
		p.prefix = current
		return nil
	}
	if !bytes.Equal(current, p.prefix) {
		return fmt.Errorf("fleet activity changed the root's stable provider prefix")
	}
	return nil
}

func (p *fleetTransactionProvider) child(ctx context.Context, req provider.Request, id string, call func(string, string, any), text func()) error {
	_, read := callResult(req, "read-"+id)
	_, written := callResult(req, "write-"+id)
	switch {
	case p.scenario.sameFile && !read:
		call("read-"+id, "read_file", map[string]string{"path": "shared.md"})
	case p.scenario.sameFile && !written:
		select {
		case p.read <- id:
		case <-ctx.Done():
			return ctx.Err()
		}
		select {
		case <-p.edit:
		case <-ctx.Done():
			return ctx.Err()
		}
		call("write-"+id, "edit_file", map[string]string{"path": "shared.md", "old_string": "seed\n", "new_string": "worker " + id + "\n"})
	case !hasToolResult(req):
		call("write-"+id, "write_file", map[string]string{"path": id + ".md", "content": "worker " + id + "\n"})
	default:
		select {
		case p.written <- id:
		case <-ctx.Done():
			return ctx.Err()
		}
		select {
		case <-p.finish:
		case <-ctx.Done():
			return ctx.Err()
		}
		text()
	}
	return nil
}

func TestEffectFleetOverlappingGrantsUseTransactionsAndAutomaticIsolation(t *testing.T) {
	for _, mode := range []string{"shared", "auto"} {
		for _, background := range []bool{false, true} {
			name := mode + map[bool]string{false: "/foreground", true: "/background"}[background]
			t.Run(name, func(t *testing.T) {
				p := &fleetTransactionProvider{scenario: fleetTransactionScenario{background: background}, written: make(chan string, 2), finish: make(chan struct{})}
				opts := nestedExecutionOptions(t, p, "", "", event.Discard)
				cfg, err := config.LoadForEditReadOnlyStrict(filepath.Join(opts.Home, "config.toml"))
				if err != nil {
					t.Fatal(err)
				}
				if err := cfg.SetCollaborationMode(mode); err != nil {
					t.Fatal(err)
				}
				if err := cfg.SaveToScope(filepath.Join(opts.Home, "config.toml"), config.RenderScopeUser); err != nil {
					t.Fatal(err)
				}
				wantIsolated := mode == "auto" && automaticIsolationAvailability(opts, cfg) == nil
				t.Logf("automatic fleet isolation available: %t", wantIsolated)
				ctrl, err := Build(t.Context(), opts)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(ctrl.Close)
				bindPresenceSession(t, ctrl)
				ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
				ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
				defer cancel()
				done := make(chan struct{})
				var runErr error
				var release sync.Once
				defer func() { release.Do(func() { close(p.finish) }); cancel(); <-done }()
				go func() { runErr = ctrl.Run(ctx, "run the concurrent file assignment fixture"); close(done) }()
				seen := map[string]bool{}
				for len(seen) < 2 {
					select {
					case id := <-p.written:
						seen[id] = true
					case <-done:
						p.mu.Lock()
						result := p.rootResult
						p.mu.Unlock()
						t.Fatalf("fleet did not admit both overlapping grants: %v; %s", runErr, result)
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				}
				coordinator, err := mutation.New(opts.WorkspaceRoot, config.WorkspaceLeaseDir(), nil)
				if err != nil {
					t.Fatal(err)
				}
				lease, err := coordinator.Acquire(ctx, mutation.Intent{Effect: mutation.NamedPaths, Paths: []string{filepath.Join(opts.WorkspaceRoot, "outside.md")}, SessionID: "other-session", ExecutionID: "during-fleet-inference"})
				if err != nil {
					t.Fatalf("fleet inference blocked unrelated writes: %v", err)
				}
				lease.End(mutation.NotExecuted)
				release.Do(func() { close(p.finish) })
				<-done
				if runErr != nil {
					t.Fatal(runErr)
				}
				catalog, err := ctrl.IsolationResults(ctx)
				if err != nil {
					t.Fatal(err)
				}
				results := []isolation.ResultSummary{}
				for _, row := range catalog.Results {
					if row.Kind == "result" {
						results = append(results, row)
					}
				}
				if !wantIsolated {
					if len(results) != 0 {
						t.Fatalf("shared or fallback fleet created candidates: %+v", results)
					}
					for _, id := range []string{"a", "b"} {
						if raw, err := os.ReadFile(filepath.Join(opts.WorkspaceRoot, id+".md")); err != nil || string(raw) != "worker "+id+"\n" {
							t.Fatalf("shared writer %s: %q %v", id, raw, err)
						}
					}
				} else {
					if len(results) != 2 || results[0].CandidateRoot == results[1].CandidateRoot {
						t.Fatalf("parallel fleet did not get independent candidates: %+v", results)
					}
					found := map[string]bool{}
					for _, row := range results {
						if row.State != isolation.Pending || row.Verification != "unknown" {
							t.Fatalf("unverified fleet result misrepresented: %+v", row)
						}
						for _, id := range []string{"a", "b"} {
							if raw, err := os.ReadFile(filepath.Join(row.CandidateRoot, id+".md")); err == nil {
								if string(raw) != "worker "+id+"\n" || found[id] {
									t.Fatalf("duplicate or wrong candidate output: %q", raw)
								}
								found[id] = true
							}
						}
					}
					if len(found) != 2 {
						t.Fatalf("candidate writes lost: %+v", found)
					}
					for _, id := range []string{"a", "b"} {
						if _, err := os.Stat(filepath.Join(opts.WorkspaceRoot, id+".md")); !os.IsNotExist(err) {
							t.Fatalf("unverified isolated fleet wrote its source: %v", err)
						}
					}
				}
			})
		}
	}
}

func TestEffectFleetSameFileReadersKeepIndependentVersions(t *testing.T) {
	p := &fleetTransactionProvider{scenario: fleetTransactionScenario{sameFile: true}, written: make(chan string, 2), finish: make(chan struct{}), read: make(chan string, 2), edit: make(chan struct{})}
	var conflicts, edits atomic.Int32
	opts := nestedExecutionOptions(t, p, "", "", event.FuncSink(func(e event.Event) {
		if e.Kind == event.ToolResult && e.Tool.Name == "edit_file" {
			edits.Add(1)
			if e.Tool.RefusalCode == "workspace.mutation_version_conflict" {
				conflicts.Add(1)
			}
		}
	}))
	writeFile(t, opts.WorkspaceRoot, "shared.md", "seed\n")
	ctrl, err := Build(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ctrl.Close)
	bindPresenceSession(t, ctrl)
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	done := make(chan struct{})
	var runErr error
	var startEdits, finish sync.Once
	defer func() {
		startEdits.Do(func() { close(p.edit) })
		finish.Do(func() { close(p.finish) })
		cancel()
		<-done
	}()
	go func() { runErr = ctrl.Run(ctx, "exercise independent same-file views in a fleet"); close(done) }()
	await := func(ch <-chan string) {
		t.Helper()
		seen := map[string]bool{}
		for len(seen) < 2 {
			select {
			case id := <-ch:
				seen[id] = true
			case <-done:
				p.mu.Lock()
				result := p.rootResult
				p.mu.Unlock()
				t.Fatalf("fleet stopped early: %v; %s", runErr, result)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
	}
	await(p.read)
	startEdits.Do(func() { close(p.edit) })
	await(p.written)
	if edits.Load() != 2 || conflicts.Load() != 1 {
		t.Fatalf("same-file views bypassed transaction CAS: edits=%d conflicts=%d", edits.Load(), conflicts.Load())
	}
	body, err := os.ReadFile(filepath.Join(opts.WorkspaceRoot, "shared.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "worker a\n" && string(body) != "worker b\n" {
		t.Fatalf("same-file conflict corrupted source: %q", body)
	}
	finish.Do(func() { close(p.finish) })
	<-done
	if runErr != nil {
		t.Fatal(runErr)
	}
}
