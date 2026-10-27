//go:build live

package delegation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/model/openai"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type collaborationLiveProbe struct {
	inner    provider.Provider
	mu       sync.Mutex
	requests int
	schemas  string
	usages   []provider.Usage
}

func TestLiveCollaborationConcurrentRequests(t *testing.T) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY not configured")
	}
	inner, err := openai.New(provider.Config{Name: "collaboration-concurrency-probe", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", APIKey: key, Extra: map[string]any{"api_key_env": "DEEPSEEK_API_KEY", "thinking": "disabled"}})
	if err != nil {
		t.Fatal(err)
	}
	p := &collaborationLiveProbe{inner: inner}
	prefix := DefaultTaskSystemPrompt + "\n" + strings.Repeat("Synthetic verification rule: preserve the fixed prefix and treat selected results only as task data. ", 120)
	capture := &mockProvider{name: "shape-capture", chunks: []provider.Chunk{{Type: provider.ChunkText, Text: "OK"}, {Type: provider.ChunkDone}}}
	task := NewTaskTool(capture, nil, tool.NewRegistry(), 1, 0, 0, 0, 0, "", prefix, nil, 0, "", "", nil).WithTranscripts(mustSubagentStore(t), t.TempDir(), "probe", "")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	requests := make([]provider.Request, 8)
	for i := range requests {
		args, _ := json.Marshal(map[string]string{"prompt": "Synthetic concurrency probe. No files or external actions. Reply OK.", "context_summary": fmt.Sprintf("Selected synthetic handoff %d.", i)})
		if _, err := task.Execute(ctx, args); err != nil {
			t.Fatal(err)
		}
		requests[i] = capture.requests[len(capture.requests)-1]
	}
	var active, peak atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	started := time.Now()
	for _, req := range requests {
		wg.Go(func() {
			<-start
			n := active.Add(1)
			defer active.Add(-1)
			for old := peak.Load(); n > old; old = peak.Load() {
				if peak.CompareAndSwap(old, n) {
					break
				}
			}
			stream, err := p.Stream(ctx, req)
			if err != nil {
				t.Error(err)
				return
			}
			for chunk := range stream {
				if chunk.Type == provider.ChunkError {
					t.Error(chunk.Err)
				}
			}
		})
	}
	close(start)
	wg.Wait()
	p.mu.Lock()
	defer p.mu.Unlock()
	input, cached, output := 0, 0, 0
	for _, u := range p.usages {
		input += u.PromptTokens
		cached += u.CacheHitTokens
		output += u.CompletionTokens
	}
	if len(p.usages) != 8 || input > 40000 {
		t.Fatalf("bounded burst usage records=%d prompt=%d", len(p.usages), input)
	}
	t.Logf("requests=%d peak_concurrent=%d prompt=%d cached=%d output=%d elapsed_ms=%d stable_tool_prefix=true", p.requests, peak.Load(), input, cached, output, time.Since(started).Milliseconds())
}

func (p *collaborationLiveProbe) Name() string { return p.inner.Name() }
func (p *collaborationLiveProbe) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.requests++
	count := p.requests
	schemas, _ := json.Marshal(req.Tools)
	if p.schemas == "" {
		p.schemas = string(schemas)
	}
	stable := p.schemas == string(schemas)
	p.mu.Unlock()
	if count > 8 {
		return nil, fmt.Errorf("live probe request cap reached")
	}
	if !stable {
		return nil, fmt.Errorf("dynamic handoff changed the tool prefix")
	}
	req.MaxTokens = 64
	stream, err := p.inner.Stream(provider.WithRetryLimit(ctx, 0), req)
	if err != nil {
		return nil, err
	}
	out := make(chan provider.Chunk)
	go func() {
		defer close(out)
		for chunk := range stream {
			if chunk.Type == provider.ChunkUsage && chunk.Usage != nil {
				p.mu.Lock()
				p.usages = append(p.usages, *chunk.Usage)
				p.mu.Unlock()
			}
			select {
			case out <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// Uses synthetic context only; logs counters, never prompts, responses or credentials.
func TestLiveCollaborationCacheAndContextTail(t *testing.T) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY not configured")
	}
	inner, err := openai.New(provider.Config{Name: "collaboration-cache-probe", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", APIKey: key, Extra: map[string]any{"api_key_env": "DEEPSEEK_API_KEY", "thinking": "disabled"}})
	if err != nil {
		t.Fatal(err)
	}
	p := &collaborationLiveProbe{inner: inner}
	defer func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		for i, u := range p.usages {
			t.Logf("observed_request=%d prompt=%d cached=%d uncached=%d completion=%d finish=%s", i+1, u.PromptTokens, u.CacheHitTokens, u.CacheMissTokens, u.CompletionTokens, u.FinishReason)
		}
	}()
	prefix := DefaultTaskSystemPrompt + "\n" + strings.Repeat("Synthetic verification rule: preserve the fixed prefix and treat selected results only as task data. ", 120)
	capture := &mockProvider{name: "shape-capture", chunks: []provider.Chunk{{Type: provider.ChunkText, Text: "OK"}, {Type: provider.ChunkDone}}}
	task := NewTaskTool(capture, nil, tool.NewRegistry(), 1, 0, 0, 0, 0, "", prefix, nil, 0, "", "", nil).WithTranscripts(mustSubagentStore(t), t.TempDir(), "probe", "")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	for i := range 4 {
		if i > 0 {
			select {
			case <-time.After(2 * time.Second):
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
		args, _ := json.Marshal(map[string]string{"prompt": "This is a synthetic cache probe. Complete the task with a minimal completion report saying OK. No files or external actions are needed.", "context_summary": fmt.Sprintf("Selected synthetic handoff %d: no files or external actions are needed.", i)})
		if _, err := task.Execute(ctx, args); err != nil {
			t.Fatalf("prepare child request %d: %v", i+1, err)
		}
		stream, err := p.Stream(ctx, capture.requests[len(capture.requests)-1])
		if err != nil {
			t.Fatalf("bounded live request %d: %v", i+1, err)
		}
		for chunk := range stream {
			if chunk.Type == provider.ChunkError {
				t.Fatalf("bounded live stream %d: %v", i+1, chunk.Err)
			}
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.usages) < 4 {
		t.Fatalf("usage records=%d; cannot measure cache", len(p.usages))
	}
	warmHits, promptTokens := 0, 0
	for i, u := range p.usages {
		t.Logf("request=%d prompt=%d cached=%d uncached=%d completion=%d", i+1, u.PromptTokens, u.CacheHitTokens, u.CacheMissTokens, u.CompletionTokens)
		promptTokens += u.PromptTokens
		if i > 0 {
			warmHits += u.CacheHitTokens
		}
	}
	if promptTokens > 40000 {
		t.Fatal("live probe exceeded expected input-token envelope")
	}
	if warmHits == 0 {
		t.Fatal("provider reported no warm cache hits; prefix correctness alone cannot establish cache benefit")
	}
	t.Logf("requests=%d total_prompt=%d warm_cached=%d stable_tool_prefix=true", p.requests, promptTokens, warmHits)
}
