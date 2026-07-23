package broker

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"reasonix/internal/provider"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/rpcwire"
)

func TestHostIgnoresNotificationsFromSupersededGeneration(t *testing.T) {
	h := NewHost()
	h.generation = 2
	stream := &hostStream{
		generation: 2, out: make(chan provider.Chunk, 1), done: make(chan struct{}),
		deliveryWake: make(chan struct{}, 1), nextSeq: 1, pending: make(map[int64]provider.Chunk),
	}
	h.streams["reused-stream"] = stream
	raw, err := json.Marshal(protocol.BrokerStreamChunkParams{
		StreamID: "reused-stream", Seq: 1,
		Chunk: protocol.BrokerProviderChunk{Type: protocol.BrokerChunkText, Text: "stale"},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.handleChunk(1, raw)
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(stream.pending) != 0 || len(stream.delivery) != 0 || stream.nextSeq != 1 {
		t.Fatalf("superseded generation mutated current stream: %+v", stream)
	}
}

func TestHostOutputBackpressureDoesNotBlockDetach(t *testing.T) {
	h := NewHost()
	h.generation = 1
	stream := &hostStream{
		generation:   1,
		out:          make(chan provider.Chunk, 1),
		done:         make(chan struct{}),
		deliveryWake: make(chan struct{}, 1),
		nextSeq:      1,
		pending: map[int64]provider.Chunk{
			1: {Type: provider.ChunkText, Text: "one"},
			2: {Type: provider.ChunkText, Text: "two"},
		},
	}
	h.streams["backpressure"] = stream
	go h.deliverStream(stream)

	h.mu.Lock()
	h.flushLocked("backpressure", stream)
	h.mu.Unlock()

	deadline := time.Now().Add(time.Second)
	for len(stream.out) != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(stream.out) != 1 {
		t.Fatal("stream never filled its output buffer")
	}

	detached := make(chan struct{})
	go func() {
		h.Detach(1)
		close(detached)
	}()
	select {
	case <-detached:
	case <-time.After(time.Second):
		t.Fatal("Detach blocked behind a slow stream consumer")
	}

	var chunks []provider.Chunk
	for chunk := range stream.out {
		chunks = append(chunks, chunk)
	}
	if len(chunks) != 3 || chunks[0].Text != "one" || chunks[1].Text != "two" || chunks[2].Err == nil {
		t.Fatalf("delivered chunks = %#v, want ordered text followed by disconnect error", chunks)
	}
}

func TestHostDeliveryQueueOverflowTerminatesOnlyThatStream(t *testing.T) {
	h := NewHost()
	h.generation = 1
	stream := &hostStream{
		generation:   1,
		out:          make(chan provider.Chunk, 1),
		done:         make(chan struct{}),
		deliveryWake: make(chan struct{}, 1),
		nextSeq:      1,
		pending:      make(map[int64]provider.Chunk),
		delivery:     make([]provider.Chunk, hostDeliveryQueueLimit-1),
	}
	h.streams["overflow"] = stream

	h.mu.Lock()
	stream.pending[1] = provider.Chunk{Type: provider.ChunkText, Text: "overflow"}
	h.flushLocked("overflow", stream)
	_, stillRegistered := h.streams["overflow"]
	final := stream.deliveryFinal
	queued := append([]provider.Chunk(nil), stream.delivery...)
	h.mu.Unlock()

	if stillRegistered || !final {
		t.Fatal("overflowing stream was not terminated")
	}
	if len(queued) != hostDeliveryQueueLimit || queued[len(queued)-1].Err == nil {
		t.Fatalf("overflow queue length/error = %d/%v", len(queued), queued[len(queued)-1].Err)
	}
}

func TestHostCancelBeforeOpenResponseCancelsDesktopProvider(t *testing.T) {
	pipes := newPipePair()
	desktopConn := rpcwire.NewConn(pipes.clientR, pipes.clientW, rpcwire.Options{
		Name: "desktop", StrictJSONRPC: true, MaxInboundBytes: 1 << 20, MaxOutboundBytes: 1 << 20,
	})
	hostConn := rpcwire.NewConn(pipes.hostR, pipes.hostW, rpcwire.Options{
		Name: "host", StrictJSONRPC: true, MaxInboundBytes: 1 << 20, MaxOutboundBytes: 1 << 20,
	})
	started := make(chan struct{})
	cancelled := make(chan struct{})
	desktop, err := Attach(desktopConn, Options{
		Catalog: func(context.Context, map[string]struct{}) ([]protocol.BrokerProviderDescriptor, error) {
			return nil, nil
		},
		Open: func(ctx context.Context, _ string, _ string, _ provider.Request) (<-chan provider.Chunk, error) {
			close(started)
			<-ctx.Done()
			close(cancelled)
			return nil, ctx.Err()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := desktop.Activate(); err != nil {
		t.Fatal(err)
	}
	defer desktop.Close()

	host := NewHost()
	if err := host.Attach(hostConn, 1); err != nil {
		t.Fatal(err)
	}
	serveCtx, stopServe := context.WithCancel(context.Background())
	defer stopServe()
	go desktopConn.Serve(serveCtx)
	go hostConn.Serve(serveCtx)

	streamCtx, cancelStream := context.WithCancel(context.Background())
	openErr := make(chan error, 1)
	go func() {
		_, err := host.open(streamCtx, "local/model", nil, provider.Request{})
		openErr <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("Desktop provider did not start")
	}
	cancelStream()
	select {
	case err := <-openErr:
		if err == nil {
			t.Fatal("Host open succeeded after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("Host open did not return after cancellation")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("Desktop provider context was not cancelled")
	}
}

func TestHostAbandonedConsumerDoesNotLeakDelivery(t *testing.T) {
	h := NewHost()
	stream := &hostStream{
		out: make(chan provider.Chunk, 1), abortDelivery: make(chan struct{}),
		deliveryWake: make(chan struct{}, 1),
		delivery: []provider.Chunk{
			{Type: provider.ChunkText, Text: "one"},
			{Type: provider.ChunkText, Text: "two"},
		},
	}
	exited := make(chan struct{})
	go func() {
		h.deliverStream(stream)
		close(exited)
	}()
	deadline := time.Now().Add(time.Second)
	for len(stream.out) != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(stream.out) != 1 {
		t.Fatal("delivery did not fill the abandoned consumer buffer")
	}
	h.mu.Lock()
	h.abortDeliveryLocked(stream)
	h.mu.Unlock()
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("delivery goroutine remained blocked after abort")
	}
}
