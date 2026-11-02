package boot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/provider"
)

// writingProvider asks to write one file, then ends once it has seen the result.
type writingProvider struct {
	mu   sync.Mutex
	reqs []provider.Request
}

func (p *writingProvider) Name() string { return "boot-writing" }

func (p *writingProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	wrote := len(effectToolResults(req)) > 0
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 3)
	if wrote {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	} else {
		args, _ := json.Marshal(map[string]string{"path": "out.txt", "content": "hello"})
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "w-1", Name: "write_file", Arguments: string(args)}}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

var (
	writingOnce sync.Once
	writingCur  *writingProvider
	writingMu   sync.Mutex
)

func registerWriting(rec *writingProvider) {
	writingMu.Lock()
	writingCur = rec
	writingMu.Unlock()
	writingOnce.Do(func() {
		provider.Register("boot-writing", func(provider.Config) (provider.Provider, error) {
			writingMu.Lock()
			defer writingMu.Unlock()
			return writingCur, nil
		})
	})
}

func defaultModeBuild(t *testing.T, userConfig string, opts Options) (written bool, result string) {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	rec := &writingProvider{}
	registerWriting(rec)
	writeUserConfig(t, userConfig)
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "boot-writing"
model = "x"
`)
	approveWorkspace(t, dir)
	ctrl, err := Build(context.Background(), opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "write the file"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	_, statErr := os.Stat(filepath.Join(dir, "out.txt"))
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if results := effectToolResults(rec.reqs[len(rec.reqs)-1]); len(results) > 0 {
		result = results[0]
	}
	return statErr == nil, result
}

// A new conversation starts in the plan mode the user chose, through the real
// assembly: the model's write is refused and nothing reaches the disk. The same
// build without the opt-in, a resumed conversation or a headless run, writes.
func TestEffectDefaultPlanModeStartsANewConversationInPlanMode(t *testing.T) {
	const plan = "[agent]\ndefault_mode = \"plan\"\n"
	written, result := defaultModeBuild(t, plan, Options{NewConversation: true})
	if written {
		t.Fatalf("a new conversation with default_mode = plan wrote a file; the model saw %q", result)
	}
	if !strings.Contains(strings.ToLower(result), "plan") {
		t.Fatalf("the model's result does not say plan mode refused the write: %q", result)
	}

	if written, result := defaultModeBuild(t, plan, Options{}); !written {
		t.Fatalf("a build that is not a new conversation (resume, headless run) started in plan mode: %q", result)
	}
	if written, result := defaultModeBuild(t, "", Options{NewConversation: true}); !written {
		t.Fatalf("without the setting a new conversation started in plan mode: %q", result)
	}
}
