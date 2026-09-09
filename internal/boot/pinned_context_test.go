package boot

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

const bootPinnedContextProviderKind = "boot-pinned-context-test"

var (
	bootPinnedContextProviderOnce sync.Once
	bootPinnedContextProviderMu   sync.Mutex
	bootPinnedContextProviderLive *bootPinnedContextProvider
)

type bootPinnedContextProvider struct {
	mu       sync.Mutex
	requests []provider.Request
}

func (p *bootPinnedContextProvider) Name() string { return bootPinnedContextProviderKind }

func (p *bootPinnedContextProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.requests = append(p.requests, req)
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func registerBootPinnedContextProvider() {
	bootPinnedContextProviderOnce.Do(func() {
		provider.Register(bootPinnedContextProviderKind, func(provider.Config) (provider.Provider, error) {
			bootPinnedContextProviderMu.Lock()
			defer bootPinnedContextProviderMu.Unlock()
			if bootPinnedContextProviderLive == nil {
				return nil, errors.New("boot pinned-context provider is not installed")
			}
			return bootPinnedContextProviderLive, nil
		})
	})
}

func useBootPinnedContextProvider(t *testing.T, p *bootPinnedContextProvider) {
	t.Helper()
	bootPinnedContextProviderMu.Lock()
	bootPinnedContextProviderLive = p
	bootPinnedContextProviderMu.Unlock()
	t.Cleanup(func() {
		bootPinnedContextProviderMu.Lock()
		if bootPinnedContextProviderLive == p {
			bootPinnedContextProviderLive = nil
		}
		bootPinnedContextProviderMu.Unlock()
	})
}

func TestBuildInjectsPinnedContextOnceWithStablePrefix(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	registerBootPinnedContextProvider()
	recorder := &bootPinnedContextProvider{}
	useBootPinnedContextProvider(t, recorder)
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-pinned-context-test"
model = "x"
`)

	loaderCalls := 0
	ctrl, err := Build(context.Background(), Options{
		Sink: event.Discard,
		PinnedContextLoader: func(context.Context, string) (agent.PinnedContextSnapshot, error) {
			loaderCalls++
			return agent.PinnedContextSnapshot{Files: []agent.PinnedContextFile{{Path: "a.md", Content: "A"}}}, nil
		},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	ctrl.EnsureSessionPath()
	if err := ctrl.Run(context.Background(), "first"); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := ctrl.Run(context.Background(), "second"); err != nil {
		t.Fatalf("second run: %v", err)
	}

	recorder.mu.Lock()
	requests := append([]provider.Request(nil), recorder.requests...)
	recorder.mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("provider requests = %d, want 2", len(requests))
	}
	if len(requests[0].Messages) == 0 {
		t.Fatal("first provider request has no messages")
	}
	if first := requests[0].Messages[0].Content; !strings.HasPrefix(first, "BASE") || strings.Contains(first, "<pinned_context_revision") {
		t.Fatalf("leading system was rewritten with pinned context: %q", first)
	}
	if loaderCalls != 2 {
		t.Fatalf("loader calls = %d", loaderCalls)
	}
	if len(requests[1].Messages) < len(requests[0].Messages) ||
		!reflect.DeepEqual(requests[1].Messages[:len(requests[0].Messages)], requests[0].Messages) {
		t.Fatal("second provider request did not preserve the first request as an exact prefix")
	}
	revisions := 0
	for _, message := range ctrl.History() {
		if agent.IsPinnedContextRevision(message) {
			revisions++
		}
	}
	if revisions != 1 {
		t.Fatalf("revision messages = %d, want 1", revisions)
	}
}
