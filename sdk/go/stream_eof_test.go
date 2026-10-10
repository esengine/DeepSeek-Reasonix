package extension

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"strings"
	"testing"
	"time"
)

func TestHostEOFEndsActiveProviderStream(t *testing.T) {
	chunks := make(chan StreamChunk, 1)
	chunks <- TextChunk("one")
	provider := &scriptProvider{makeChannel: func(StreamRequest) <-chan StreamChunk { return chunks }}
	observerStarted := make(chan struct{})
	observerDone := make(chan struct{})
	handler := providerHandler()
	handler.result.Subscriptions = []string{string(EventSessionStart)}
	host, waiter := startFakeHost(t, handler, Options{
		Provider: provider,
		Observer: func(ctx context.Context, _ string, _ json.RawMessage) {
			close(observerStarted)
			<-ctx.Done()
			// Callback cleanup can outlive the notification queue's closure.
			time.Sleep(100 * time.Millisecond)
			close(observerDone)
		},
	})
	host.handshake(t)
	host.notify(MethodExtensionEvent, EventParams{Event: EventSessionStart, Payload: json.RawMessage(`{"phase":"start"}`)})
	select {
	case <-observerStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("observer did not start")
	}
	if resp := host.request(MethodExtensionProviderStreamOpen, openStreamRequest("eof-stream")); resp.Err != nil {
		t.Fatalf("stream open: %+v", resp.Err)
	}
	host.nextNotification(MethodExtensionProviderStreamChunk)
	if err := host.toSDK.Close(); err != nil {
		t.Fatal(err)
	}
	if err, ok := waiter.wait(5 * time.Second); !ok || err != nil {
		t.Fatalf("Serve after host EOF: err=%v completed=%v", err, ok)
	}
	select {
	case <-observerDone:
	default:
		t.Fatal("Serve returned before observer cleanup")
	}
}

func TestNotificationQueueRetainsFIFOAndOverflowFailure(t *testing.T) {
	c := newConn(strings.NewReader(""), io.Discard, log.New(io.Discard, "", 0))
	for seq := int64(1); seq <= maxQueuedNotifications; seq++ {
		if err := c.notify(MethodExtensionProviderStreamChunk, StreamChunkParams{StreamID: "queued", Seq: seq, Chunk: TextChunk("part")}); err != nil {
			t.Fatalf("enqueue %d: %v", seq, err)
		}
	}
	if err := c.notify(MethodExtensionProviderStreamChunk, StreamChunkParams{StreamID: "queued", Seq: maxQueuedNotifications + 1, Chunk: TextChunk("overflow")}); err == nil || c.recordedCloseError() != err {
		t.Fatalf("overflow error = %v, connection error = %v", err, c.recordedCloseError())
	}
	select {
	case <-c.closed:
	default:
		t.Fatal("queue overflow did not fail the connection")
	}
	for seq := int64(1); seq <= maxQueuedNotifications; seq++ {
		var frame hostFrame
		if err := json.Unmarshal(<-c.notifyQueue, &frame); err != nil {
			t.Fatal(err)
		}
		var chunk StreamChunkParams
		if err := json.Unmarshal(frame.Params, &chunk); err != nil || chunk.Seq != seq || chunk.Chunk.Text != "part" {
			t.Fatalf("queued chunk = %+v, want seq=%d, err=%v", chunk, seq, err)
		}
	}
}
