package anthropic

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
)

const overloadedEvent = "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"overloaded\"}}\n\n"

func streamErrorOf(t *testing.T, sse string) error {
	t.Helper()
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(sse))}
	ch := make(chan provider.Chunk)
	go (&client{name: "anthropic"}).readStream(context.Background(), resp, ch)
	var got error
	for ck := range ch {
		if ck.Type == provider.ChunkError {
			got = ck.Err
		}
	}
	if got == nil {
		t.Fatal("stream ended without an error chunk")
	}
	return got
}

// An error event before any output is an attempt the Agent may make again.
func TestStreamErrorBeforeOutputIsUpstreamInterrupt(t *testing.T) {
	sse := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":3}}}\n\n" + overloadedEvent
	err := streamErrorOf(t, sse)
	if got := provider.StreamInterruptReason(err); got != provider.StreamInterruptUpstreamError {
		t.Fatalf("interrupt reason = %q, want %q (err %v)", got, provider.StreamInterruptUpstreamError, err)
	}
	var payload *provider.StreamPayloadError
	if !errors.As(err, &payload) || payload.Type != "overloaded_error" || payload.Message != "overloaded" {
		t.Fatalf("payload identity lost: %#v", payload)
	}
}

// After output a replay would show it twice, so the error is not recoverable.
func TestStreamErrorAfterOutputStaysPayloadError(t *testing.T) {
	sse := "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n" + overloadedEvent
	err := streamErrorOf(t, sse)
	if provider.IsStreamInterrupted(err) {
		t.Fatalf("error after output must not be a stream interruption: %v", err)
	}
	var payload *provider.StreamPayloadError
	if !errors.As(err, &payload) || payload.Type != "overloaded_error" {
		t.Fatalf("want a StreamPayloadError, got %v", err)
	}
}
