package providerext

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"reasonix/internal/extension/protocol"
	"reasonix/internal/provider"
)

func TestFinishedStreamUnregistersDrainCancel(t *testing.T) {
	r := testResolver(t, baseCatalog(), nil)
	var unregistered atomic.Int32
	stream := &extensionStream{
		done: make(chan struct{}),
		unregisterDrainCancel: func() {
			unregistered.Add(1)
		},
	}
	r.mu.Lock()
	r.streams["finished"] = stream
	r.finishLocked("finished", stream, provider.Chunk{})
	r.finishLocked("finished", stream, provider.Chunk{})
	r.mu.Unlock()
	if got := unregistered.Load(); got != 1 {
		t.Fatalf("drain cancel unregister count = %d, want 1", got)
	}
}

func TestDrainCancelInstallUnregistersWhenStreamAlreadyFinished(t *testing.T) {
	r := testResolver(t, baseCatalog(), nil)
	stream := &extensionStream{done: make(chan struct{})}
	r.mu.Lock()
	r.streams["finished-before-install"] = stream
	r.finishLocked("finished-before-install", stream, provider.Chunk{})
	r.mu.Unlock()

	var unregistered atomic.Int32
	r.installDrainCancel("finished-before-install", stream, func() {
		unregistered.Add(1)
	})
	if got := unregistered.Load(); got != 1 {
		t.Fatalf("late drain cancel unregister count = %d, want 1", got)
	}
	if stream.unregisterDrainCancel != nil {
		t.Fatal("completed stream retained a drain cancel unregister callback")
	}
}

func TestConcurrentStreamsReuseProviderHandleWithoutMutation(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	r := testResolver(t, baseCatalog(), nil, fc)
	p, err := r.Resolve(provider.Selection{Ref: "plugin/demo/fake/x"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	type result struct {
		out <-chan provider.Chunk
		err error
	}
	const streamCount = 8
	results := make(chan result, streamCount)
	var wg sync.WaitGroup
	for range streamCount {
		wg.Go(func() {
			out, streamErr := p.Stream(context.Background(), provider.Request{
				Messages: []provider.Message{{Role: provider.RoleUser}},
			})
			results <- result{out: out, err: streamErr}
		})
	}
	wg.Wait()
	close(results)

	var outputs []<-chan provider.Chunk
	for item := range results {
		if item.err != nil {
			t.Fatalf("Stream: %v", item.err)
		}
		outputs = append(outputs, item.out)
	}
	fc.mu.Lock()
	opened := append([]protocol.StreamOpenParams(nil), fc.opened...)
	fc.mu.Unlock()
	if len(opened) != streamCount {
		t.Fatalf("opened streams = %d, want %d", len(opened), streamCount)
	}
	for _, params := range opened {
		r.RouteStreamEnd(protocol.StreamEndParams{StreamID: params.StreamID, LastSeq: 0})
	}
	for _, out := range outputs {
		if chunks := collectChunks(t, out); len(chunks) != 0 {
			t.Fatalf("clean empty stream delivered %d chunks", len(chunks))
		}
	}
}
