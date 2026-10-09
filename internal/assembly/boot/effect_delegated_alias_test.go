package boot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

type delegatedAliasProvider struct {
	mu      sync.Mutex
	calls   int
	path    string
	written chan struct{}
}

func (*delegatedAliasProvider) Name() string { return "delegated-alias" }

func (p *delegatedAliasProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	call := p.calls
	p.calls++
	p.mu.Unlock()
	if call == 2 {
		close(p.written)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ch := make(chan provider.Chunk, 2)
	switch call {
	case 0:
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "delegate", Name: "task", Arguments: `{"prompt":"Write the fixture","write_paths":["."]}`}}
	case 1:
		args, _ := json.Marshal(map[string]string{"path": p.path, "content": "fixture"})
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "write", Name: "write_file", Arguments: string(args)}}
	default:
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "Fixture complete"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectDelegatedWorkspaceAliasWrite(t *testing.T) {
	for _, kind := range []string{"trailing-dot", "leaf-symlink"} {
		t.Run(kind, func(t *testing.T) {
			if kind == "trailing-dot" && runtime.GOOS != "windows" {
				t.Skip("Windows alias")
			}
			root, build := revisionLeaseWorkspace(t)
			target := filepath.Join(root, "fixture.txt")
			path := target + "."
			if kind == "leaf-symlink" {
				if err := os.WriteFile(target, []byte("prior"), 0o600); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(root, "link.txt")
				if err := os.Symlink(target, path); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			p := &delegatedAliasProvider{path: path, written: make(chan struct{})}
			c := build(p, event.Discard)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			done := make(chan struct{})
			go func() { defer close(done); _ = c.Run(ctx, "Delegate the fixture write") }()
			defer func() { cancel(); <-done }()
			select {
			case <-p.written:
			case <-ctx.Done():
				t.Fatal("delegated writer did not return")
			}
			if data, err := os.ReadFile(target); err != nil || string(data) != "fixture" {
				t.Fatalf("delegated effect: %q %v", data, err)
			}
		})
	}
}
