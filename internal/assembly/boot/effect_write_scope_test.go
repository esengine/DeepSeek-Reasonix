package boot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/tools/builtin"
)

// writeScopeProvider asks for one write_file outside the workspace, then stops.
type writeScopeProvider struct {
	mu     sync.Mutex
	target string
	turn   int
	reqs   []provider.Request
}

func (p *writeScopeProvider) Name() string { return "boot-write-scope" }

func (p *writeScopeProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.turn++
	turn := p.turn
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	if turn == 1 {
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "call-write", Name: "write_file",
			Arguments: `{"path":` + jsonString(p.target) + `,"content":"x\n"}`,
		}}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

var (
	writeScopeRegister sync.Once
	writeScopeMu       sync.Mutex
	writeScopeCurrent  *writeScopeProvider
)

// A write outside the workspace is refused by the file tools' write scope, on
// every platform. With bash unconfined — the only mode Windows has — nothing
// about the refusal may be attributed to an OS sandbox: the model and the
// frontend both receive the workspace cause, the path, and the setting.
func TestEffectWriteOutsideWorkspaceNamesTheWriteScope(t *testing.T) {
	if got := config.Default().BashModeForGOOS("windows"); got != "off" {
		t.Fatalf("the Windows posture this test stands in for is bash %q, want off", got)
	}
	for _, bash := range []string{"off", "enforce"} {
		t.Run("bash="+bash, func(t *testing.T) {
			isolateConfigHome(t)
			dir := robustTempDir(t)
			outside := robustTempDir(t)
			t.Chdir(dir)
			target := filepath.Join(outside, "notes.md")
			rec := &writeScopeProvider{target: target}
			writeScopeRegister.Do(func() {
				provider.Register("boot-write-scope", func(provider.Config) (provider.Provider, error) {
					writeScopeMu.Lock()
					defer writeScopeMu.Unlock()
					return writeScopeCurrent, nil
				})
			})
			writeScopeMu.Lock()
			writeScopeCurrent = rec
			writeScopeMu.Unlock()
			writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[sandbox]
bash = "`+bash+`"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "boot-write-scope"
model = "x"
`)
			var mu sync.Mutex
			var results []event.Tool
			sink := event.FuncSink(func(e event.Event) {
				if e.Kind == event.ToolResult && e.Tool.Name == "write_file" {
					mu.Lock()
					results = append(results, e.Tool)
					mu.Unlock()
				}
			})
			ctrl, err := Build(context.Background(), Options{Sink: sink})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			defer ctrl.Close()
			ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
			_ = ctrl.Run(context.Background(), "write the notes")

			if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Fatalf("a write outside the workspace landed (stat err %v); the scope must not loosen", err)
			}
			rec.mu.Lock()
			last := rec.reqs[len(rec.reqs)-1]
			rec.mu.Unlock()
			got := effectToolResults(last)
			if len(got) == 0 {
				t.Fatal("no tool result reached the provider")
			}
			model := got[len(got)-1]
			for _, want := range []string{
				"(refusal: " + builtin.CodeWriteOutsideScope + ")",
				"workspace write scope",
				"not an OS sandbox",
				target,
				"allow_write",
			} {
				if !strings.Contains(model, want) {
					t.Errorf("model-visible refusal lacks %q:\n%s", want, model)
				}
			}
			if strings.Contains(model, "confined") || strings.Contains(model, "widen [sandbox]") {
				t.Errorf("model-visible refusal still reads as a sandbox confinement:\n%s", model)
			}

			mu.Lock()
			defer mu.Unlock()
			if len(results) != 1 {
				t.Fatalf("want one write_file result at the frontend sink, got %d", len(results))
			}
			if code := results[0].RefusalCode; code != builtin.CodeWriteOutsideScope {
				t.Fatalf("frontend refusal code = %q, want %q", code, builtin.CodeWriteOutsideScope)
			}
		})
	}
}
