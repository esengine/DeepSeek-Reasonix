package testutil

import (
	"context"
	"errors"
	"testing"
	"time"

	"reasonix/internal/provider"
)

func TestMockProviderStreamHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mp := NewMock("mock", Turn{Text: "hello"})
	ch, err := mp.Stream(ctx, provider.Request{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Stream error = %v, want context.Canceled", err)
	}
	if ch != nil {
		t.Fatal("Stream returned a channel for canceled context")
	}
	if got := mp.CallCount(); got != 0 {
		t.Fatalf("CallCount = %d, want 0", got)
	}
}

func TestMockProviderStreamStopsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	mp := NewMock("mock", Turn{
		Text: "first",
		ToolCalls: []provider.ToolCall{
			{ID: "call-1", Name: "noop", Arguments: `{}`},
		},
	})

	ch, err := mp.Stream(ctx, provider.Request{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if got := (<-ch).Text; got != "first" {
		t.Fatalf("first chunk text = %q, want first", got)
	}
	cancel()

	// Cancellation can race a send that is already committed: at most one
	// data chunk may arrive after the cancel. The stream must then deliver a
	// cancellation error and close.
	inFlight := 0
	for {
		var chunk provider.Chunk
		var ok bool
		select {
		case chunk, ok = <-ch:
		case <-time.After(5 * time.Second):
			t.Fatal("stream did not terminate after cancellation")
		}
		if !ok {
			t.Fatal("stream closed without returning cancellation error")
		}
		if chunk.Type == provider.ChunkError {
			if !errors.Is(chunk.Err, context.Canceled) {
				t.Fatalf("cancellation error = %v, want context.Canceled", chunk.Err)
			}
			break
		}
		inFlight++
		if inFlight > 1 {
			t.Fatalf("stream kept sending data after cancellation (%d chunks)", inFlight)
		}
	}
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("stream stayed open after cancellation error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not close after the cancellation error")
	}
}
