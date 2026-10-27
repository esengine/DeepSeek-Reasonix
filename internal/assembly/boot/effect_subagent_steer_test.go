package boot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/delegation"
	"reasonix/internal/state/sessionstore"
)

// steerChildProvider scripts a parent that starts one background sub-agent and a
// child that is held in its first model call, then makes one tool call, then
// answers. Guidance sent while it is held is read at the tool boundary.
type steerChildProvider struct {
	mu          sync.Mutex
	parentTools int
	parent      int
	child       int
	childReqs   [][]provider.Message
	childTools  [][]provider.ToolSchema
	started     chan struct{}
	release     chan struct{}
	startedOnce sync.Once
}

func (p *steerChildProvider) Name() string { return "boot-effect-steer-child" }

func (p *steerChildProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	if p.parentTools == 0 {
		p.parentTools = len(req.Tools)
	}
	isParent := len(req.Tools) == p.parentTools
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 3)
	if len(req.Tools) == 0 {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "title"}
		ch <- provider.Chunk{Type: provider.ChunkDone}
		close(ch)
		return ch, nil
	}
	if !isParent {
		p.mu.Lock()
		p.child++
		n := p.child
		p.childReqs = append(p.childReqs, append([]provider.Message(nil), req.Messages...))
		p.childTools = append(p.childTools, append([]provider.ToolSchema(nil), req.Tools...))
		p.mu.Unlock()
		if n == 1 {
			p.startedOnce.Do(func() { close(p.started) })
			select {
			case <-p.release:
			case <-ctx.Done():
			}
			args, _ := json.Marshal(map[string]string{"path": "."})
			ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "ls1", Name: "list_dir", Arguments: string(args)}}
		} else {
			ch <- provider.Chunk{Type: provider.ChunkText, Text: "child done"}
		}
		ch <- provider.Chunk{Type: provider.ChunkDone}
		close(ch)
		return ch, nil
	}
	p.mu.Lock()
	p.parent++
	n := p.parent
	p.mu.Unlock()
	if n == 1 {
		args, _ := json.Marshal(map[string]any{"prompt": "survey the repo", "run_in_background": true, "description": "survey"})
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "t1", Name: "task", Arguments: string(args)}}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

var (
	steerChildCurrent = struct {
		sync.Mutex
		p *steerChildProvider
	}{}
	steerChildRegistered sync.Once
)

func TestEffectGuidanceSentToARunningSubagentIsReadAtItsNextToolCall(t *testing.T) {
	home := isolateConfigHome(t)
	_ = os.MkdirAll(filepath.Join(home, ".reasonix", "sessions"), 0o755)
	dir := robustTempDir(t)
	t.Chdir(dir)
	prov := &steerChildProvider{started: make(chan struct{}), release: make(chan struct{})}
	steerChildCurrent.Lock()
	steerChildCurrent.p = prov
	steerChildCurrent.Unlock()
	steerChildRegistered.Do(func() {
		provider.Register("boot-effect-steer-child", func(provider.Config) (provider.Provider, error) {
			steerChildCurrent.Lock()
			defer steerChildCurrent.Unlock()
			return steerChildCurrent.p, nil
		})
	})
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-effect-steer-child"
model = "x"
`)
	approveWorkspace(t, dir)
	sink := &childReceiptSink{done: make(chan struct{})}
	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	_ = os.WriteFile(filepath.Join(home, ".reasonix", "sessions", "o.execution.jsonl"), nil, 0o600)
	ctrl.SetSessionPath(filepath.Join(dir, ".reasonix", "sessions", "o.jsonl"))
	ctrl.SetBypass(true)
	ctrl.Submit("go")
	select {
	case <-sink.done:
	case <-time.After(30 * time.Second):
		t.Fatal("turn did not finish")
	}
	select {
	case <-prov.started:
	case <-time.After(20 * time.Second):
		t.Fatal("the child never started")
	}
	runs, err := ctrl.SubagentRuns()
	if err != nil || len(runs) != 1 || runs[0].State != delegation.RunRunning {
		t.Fatalf("runs = %+v err=%v, want one running", runs, err)
	}

	if err := ctrl.SteerSubagent(runs[0].Ref, "prefer the shallow listing", delegation.GuidanceContext{Summary: "Reuse the completed audit; keep existing permissions."}); err != nil {
		t.Fatalf("SteerSubagent: %v", err)
	}
	close(prov.release)
	deadline := time.Now().Add(30 * time.Second)
	for {
		got, _ := ctrl.SubagentRuns()
		if len(got) == 1 && got[0].State == delegation.RunCompleted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the child never completed: %+v", got)
		}
		time.Sleep(50 * time.Millisecond)
	}

	prov.mu.Lock()
	reqs := prov.childReqs
	tools := prov.childTools
	prov.mu.Unlock()
	if len(reqs) < 2 {
		t.Fatalf("the child made %d requests, want at least 2", len(reqs))
	}
	if !reflect.DeepEqual(tools[0], tools[1]) || len(reqs[1]) < len(reqs[0]) || !reflect.DeepEqual(reqs[0], reqs[1][:len(reqs[0])]) {
		t.Fatal("handoff changed the provider-visible prefix")
	}
	var second strings.Builder
	for _, m := range reqs[1] {
		second.WriteString(m.Content + "\n")
	}
	if !strings.Contains(second.String(), sessionstore.MidTurnSteerPrefix) || !strings.Contains(second.String(), "prefer the shallow listing") {
		t.Fatalf("the child's request after its tool call did not carry the guidance:\n%s", second.String())
	}
	if strings.Contains(strings.Join(messagesText(reqs[0]), "\n"), "prefer the shallow listing") {
		t.Fatal("guidance appeared in a request made before it was sent")
	}
	if !strings.Contains(second.String(), "Reuse the completed audit") || strings.Contains(strings.Join(messagesText(reqs[0]), "\n"), "Reuse the completed audit") {
		t.Fatal("selected context was not appended at the turn tail")
	}
	if err := ctrl.SteerSubagent(runs[0].Ref, "too late"); err == nil {
		t.Fatal("a finished sub-agent accepted guidance")
	}
}

func messagesText(msgs []provider.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Content
	}
	return out
}
