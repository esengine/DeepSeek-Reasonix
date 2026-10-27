package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

type deniedCapabilityProvider struct {
	mu    sync.Mutex
	calls int
	roots string
	names [][]string
}

func (p *deniedCapabilityProvider) Name() string { return "boot-effect-denied-capability" }

func (p *deniedCapabilityProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.calls++
	n := p.calls
	var names []string
	for _, t := range req.Tools {
		names = append(names, t.Name)
	}
	p.names = append(p.names, names)
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 3)
	switch {
	case n == 1:
		args, _ := json.Marshal(map[string]any{"prompt": "run the build with bash", "write_paths": []string{p.roots}})
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "t1", Name: "task", Arguments: string(args)}}
	case n >= 2 && n <= 4:
		args, _ := json.Marshal(map[string]any{"action": "call", "capability_id": "tool:bash", "arguments": map[string]string{"command": "echo hi"}})
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: fmt.Sprintf("u%d", n), Name: "use_capability", Arguments: string(args)}}
	default:
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectDeniedCapabilityCallRefusesBeforeExecutionAndSaysWhatToDo(t *testing.T) {
	home := isolateConfigHome(t)
	_ = os.MkdirAll(filepath.Join(home, ".reasonix", "sessions"), 0o755)
	dir := robustTempDir(t)
	t.Chdir(dir)
	prov := &deniedCapabilityProvider{roots: filepath.Join(dir, "out.txt")}
	provider.Register("boot-effect-denied-capability", func(provider.Config) (provider.Provider, error) { return prov, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-effect-denied-capability"
model = "x"
`)
	approveWorkspace(t, dir)
	sink := &childReceiptSink{done: make(chan struct{})}
	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	_ = os.MkdirAll(filepath.Join(dir, ".reasonix", "sessions"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, ".reasonix", "sessions", "p.execution.jsonl"), nil, 0o600)
	ctrl.SetSessionPath(filepath.Join(dir, ".reasonix", "sessions", "p.jsonl"))
	ctrl.SetBypass(true)
	ctrl.Submit("go")
	select {
	case <-sink.done:
	case <-time.After(60 * time.Second):
		t.Fatal("turn did not finish")
	}
	prov.mu.Lock()
	tools := prov.names
	prov.mu.Unlock()
	child := 0
	for _, names := range tools {
		if !slices.Contains(names, "task") {
			child++
		}
	}
	if child < 4 {
		t.Fatalf("the child made %d model requests, want its three denied calls and the answer after them", child)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	denied := 0
	for _, e := range sink.events {
		if e.Kind != event.ToolResult || e.Tool.Name != "use_capability" {
			continue
		}
		denied++
		if e.Tool.Err == "" || e.Tool.Executed || e.Tool.Execution != nil {
			t.Fatalf("capability refusal carried execution evidence: %+v", e.Tool)
		}
		for _, want := range []string{"not proven read-only", "write_paths", "Do not retry"} {
			if !strings.Contains(e.Tool.Output, want) {
				t.Fatalf("the refusal %q does not say %q", e.Tool.Output, want)
			}
		}
	}
	if denied != 3 {
		t.Fatalf("saw %d refused calls, want 3", denied)
	}
}
