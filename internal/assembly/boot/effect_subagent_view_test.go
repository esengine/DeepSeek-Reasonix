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
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/delegation"
)

type slowChildProvider struct {
	mu          sync.Mutex
	parent      int
	parentTools int
	started     chan struct{}
	release     chan struct{}
	startedO    sync.Once
}

func (p *slowChildProvider) Name() string { return "boot-effect-slow-child" }

func (p *slowChildProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
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
		p.startedO.Do(func() { close(p.started) })
		select {
		case <-p.release:
		case <-ctx.Done():
		}
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "child done"}
		ch <- provider.Chunk{Type: provider.ChunkDone}
		close(ch)
		return ch, nil
	}
	p.mu.Lock()
	p.parent++
	n := p.parent
	p.mu.Unlock()
	if n == 1 {
		args, _ := json.Marshal(map[string]any{"tasks": []map[string]string{{"title": "slow", "task": "slow edits"}}})
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "t1", Name: "spawn_agents", Arguments: string(args)}}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

var (
	slowChildCurrent    atomic.Pointer[slowChildProvider]
	slowChildRegistered sync.Once
)

func registerSlowChild() {
	provider.Register("boot-effect-slow-child", func(provider.Config) (provider.Provider, error) { return slowChildCurrent.Load(), nil })
}

func TestEffectARunningSubagentIsReadableThroughTheController(t *testing.T) {
	home := isolateConfigHome(t)
	_ = os.MkdirAll(filepath.Join(home, ".reasonix", "sessions"), 0o755)
	dir := robustTempDir(t)
	t.Chdir(dir)
	prov := &slowChildProvider{started: make(chan struct{}), release: make(chan struct{})}
	slowChildCurrent.Store(prov)
	slowChildRegistered.Do(registerSlowChild)
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-effect-slow-child"
model = "x"
`)
	approveWorkspace(t, dir)
	done := make(chan struct{})
	var once sync.Once
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.TurnDone {
			once.Do(func() { close(done) })
		}
	})
	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	ctrl.SetSessionPath(filepath.Join(dir, ".reasonix", "sessions", "o.jsonl"))
	ctrl.SetBypass(true)
	ctrl.Submit("go")
	select {
	case <-prov.started:
	case <-time.After(30 * time.Second):
		t.Fatal("the child never started")
	}

	runs, err := ctrl.SubagentRuns()
	if err != nil || len(runs) != 1 || runs[0].State != delegation.RunRunning {
		t.Fatalf("runs while the child is held = %+v err=%v, want one running run", runs, err)
	}
	live, err := ctrl.SubagentTranscript(runs[0].Ref, 0, 0)
	if err != nil || !live.Live || !transcriptSays(live, "slow edits") {
		t.Fatalf("live transcript = %+v err=%v, want the child's task in a live read", live, err)
	}

	close(prov.release)
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("turn did not finish")
	}
	got, _ := ctrl.SubagentRuns()
	if len(got) != 1 || got[0].State != delegation.RunCompleted {
		t.Fatalf("the child never reached completed: %+v", got)
	}
	finished, err := ctrl.SubagentTranscript(runs[0].Ref, 0, 0)
	if err != nil || finished.Live || !transcriptSays(finished, "child done") {
		t.Fatalf("finished transcript = %+v err=%v, want the stored answer and not live", finished, err)
	}
}

func transcriptSays(tr delegation.RunTranscript, text string) bool {
	for _, m := range tr.Messages {
		if strings.Contains(m.Content, text) {
			return true
		}
	}
	return false
}
