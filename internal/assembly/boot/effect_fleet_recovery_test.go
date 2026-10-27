package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

type fleetRecoveryProvider struct {
	mu            sync.Mutex
	id            string
	first, second int
	result        string
	parents       []provider.Request
}

func (*fleetRecoveryProvider) Name() string { return "fleet-recovery" }
func (p *fleetRecoveryProvider) Stream(_ context.Context, r provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ch := make(chan provider.Chunk, 3)
	call := func(id, name string, args any) {
		raw, _ := json.Marshal(args)
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: id, Name: name, Arguments: string(raw)}}
	}
	first := firstUser(r)
	if strings.Contains(first, "SECOND-FLEET-CHILD") {
		p.second++
		if p.second == 1 {
			return nil, fmt.Errorf("fixture failure before completion")
		}
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "second complete"}
	} else if strings.Contains(first, "FIRST-FLEET-CHILD") {
		p.first++
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "first complete"}
	} else {
		p.parents = append(p.parents, r)
		if toolResultAfterLastUser(r) {
			p.result = lastToolResult(r)
			ch <- provider.Chunk{Type: provider.ChunkText, Text: "parent complete"}
		} else if p.id != "" {
			call("resume", "fleet", map[string]any{"recovery": map[string]string{"action": "resume", "id": p.id}})
		} else {
			call("initial", "fleet", map[string]any{"tasks": []map[string]any{
				{"id": "one", "prompt": "FIRST-FLEET-CHILD", "read_only": true, "tools": []string{"read_file"}},
				{"id": "two", "prompt": "SECOND-FLEET-CHILD", "depends_on": []string{"one"}, "read_only": true, "tools": []string{"read_file"}},
			}})
		}
	}
	ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 5}}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectFleetRecoveryAfterRestartReusesCompletedAnswer(t *testing.T) {
	home, dir := statedBootHome(t), robustTempDir(t)
	p := &fleetRecoveryProvider{}
	kind := uniqueKind("fleet-recovery")
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return p, nil })
	writeFile(t, dir, "reasonix.toml", `default_model = "probe"
[agent]
task_token_budget = 100000
[codegraph]
enabled = false
[[providers]]
name = "probe"
kind = "`+kind+`"
model = "probe"
`)
	mirrorToUserConfigAt(t, home, dir)
	done := make(chan struct{}, 4)
	build := func() *control.Controller {
		c, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone && e.Source == "" {
				select {
				case done <- struct{}{}:
				default:
				}
			}
		})})
		if err != nil {
			t.Fatal(err)
		}
		c.SetToolApprovalMode(control.ToolApprovalYolo)
		return c
	}
	c := build()
	c.EnsureSessionPath()
	if err := c.Run(context.Background(), "Run the fleet"); err != nil {
		t.Fatal(err)
	}
	p.id = regexp.MustCompile(`fr_[A-Za-z0-9]+`).FindString(p.result)
	if p.id == "" || p.first != 1 || p.second != 1 {
		t.Fatalf("before restart: first=%d second=%d result=%s", p.first, p.second, p.result)
	}
	path := c.SessionPath()
	runs, err := c.SubagentRuns()
	if err != nil || len(runs) != 2 {
		t.Fatalf("runs=%+v %v", runs, err)
	}
	ledgers, err := filepath.Glob(filepath.Join(store.SessionBudgetDir(path), "*.json"))
	if err != nil || len(ledgers) != 1 {
		t.Fatalf("budget persistence=%v %v", ledgers, err)
	}
	spent := func() int {
		data, err := os.ReadFile(ledgers[0])
		if err != nil {
			t.Fatal(err)
		}
		var r struct {
			Tokens int `json:"tokens"`
		}
		if err := json.Unmarshal(data, &r); err != nil {
			t.Fatal(err)
		}
		return r.Tokens
	}
	before := spent()
	c.Close()
	for len(done) > 0 {
		<-done
	}
	sess, err := sessionstore.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	c = build()
	defer c.Close()
	if err := c.Resume(sess, path); err != nil {
		t.Fatal(err)
	}
	if p.first != 1 || p.second != 1 {
		t.Fatal("startup replayed work")
	}
	plans, err := c.FleetRecoveryPlans(runs[0].Ref)
	if err != nil || len(plans) != 1 || !plans[0].Ready {
		t.Fatalf("preview=%+v %v", plans, err)
	}
	if err := c.SetInboxPaused(true); err != nil {
		t.Fatal(err)
	}
	if err := c.RequestFleetRecovery(runs[0].Ref, plans[0].ID, plans[0].Snapshot); err != nil {
		t.Fatal(err)
	}
	if p.second != 1 {
		t.Fatal("queued recovery bypassed turn admission")
	}
	if err := c.SetInboxPaused(false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("recovery turn did not finish")
	}
	if after := spent(); after != before+15 {
		t.Fatalf("reopened fleet budget lost its owner: before=%d after=%d", before, after)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.first != 1 || p.second != 2 || !strings.Contains(p.result, "Completed fleet") {
		t.Fatalf("after recovery: first=%d second=%d result=%s", p.first, p.second, p.result)
	}
	if len(p.parents) != 4 || !reflect.DeepEqual(p.parents[0].Tools, p.parents[2].Tools) {
		t.Fatal("recovery state changed tool prefix")
	}
}
