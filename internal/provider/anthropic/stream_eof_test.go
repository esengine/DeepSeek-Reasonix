package anthropic

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"reasonix/internal/provider"
)

// Compatible Anthropic gateways often omit message_stop after message_delta
// the same way OpenAI-compatible gateways omit [DONE] after finish_reason.
// A stop_reason is the provider's commit signal; EOF after that must finalize.

func TestReadStreamAcceptsStopReasonWithoutMessageStop(t *testing.T) {
	sse := `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":5}}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}
`
	c := &client{name: "cc"}
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(sse))}
	ch := make(chan provider.Chunk)
	go c.readStream(context.Background(), resp, ch)

	var text strings.Builder
	var sawDone, sawErr bool
	var usage *provider.Usage
	for ck := range ch {
		switch ck.Type {
		case provider.ChunkText:
			text.WriteString(ck.Text)
		case provider.ChunkUsage:
			usage = ck.Usage
		case provider.ChunkDone:
			sawDone = true
		case provider.ChunkError:
			sawErr = true
			t.Fatalf("stop_reason without message_stop should complete cleanly: %v", ck.Err)
		}
	}
	if text.String() != "hello" || !sawDone || sawErr {
		t.Fatalf("text=%q done=%v err=%v", text.String(), sawDone, sawErr)
	}
	if usage == nil || usage.FinishReason != "stop" {
		t.Fatalf("usage = %+v, want finish_reason=stop", usage)
	}
}

func TestReadStreamAcceptsToolUseStopReasonWithoutMessageStop(t *testing.T) {
	sse := `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":10}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"bash"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"cmd\":\"ls\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":8}}
`
	c := &client{name: "cc"}
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(sse))}
	ch := make(chan provider.Chunk)
	go c.readStream(context.Background(), resp, ch)

	var sawDone, sawErr bool
	var call *provider.ToolCall
	var usage *provider.Usage
	for ck := range ch {
		switch ck.Type {
		case provider.ChunkToolCall:
			call = ck.ToolCall
		case provider.ChunkUsage:
			usage = ck.Usage
		case provider.ChunkDone:
			sawDone = true
		case provider.ChunkError:
			sawErr = true
			t.Fatalf("tool_use stop_reason without message_stop should complete: %v", ck.Err)
		}
	}
	if sawErr || !sawDone {
		t.Fatalf("done=%v err=%v", sawDone, sawErr)
	}
	if call == nil || call.ID != "toolu_1" || call.Name != "bash" || call.Arguments != `{"cmd":"ls"}` {
		t.Fatalf("tool call = %+v", call)
	}
	if usage == nil || usage.FinishReason != "tool_calls" {
		t.Fatalf("usage = %+v, want finish_reason=tool_calls", usage)
	}
}

func TestStreamScanEndErrorClassifiesTerminal(t *testing.T) {
	if err := streamScanEndError("cc", time.Second, false, nil, "end_turn"); err != nil {
		t.Fatalf("stop_reason must commit: %v", err)
	}
	err := streamScanEndError("cc", time.Second, false, nil, "")
	if !provider.IsStreamInterrupted(err) {
		t.Fatalf("empty stop_reason = %v, want interrupt", err)
	}
	if !strings.Contains(err.Error(), "stream ended before message_stop") {
		t.Fatalf("interrupt = %v", err)
	}
	if err := streamScanEndError("cc", time.Second, true, nil, ""); !provider.IsStreamInterrupted(err) ||
		provider.StreamInterruptReason(err) != provider.StreamInterruptIdleTimeout {
		t.Fatalf("stall = %v", err)
	}
}

func TestReadStreamStillInterruptsWithoutStopReasonOrMessageStop(t *testing.T) {
	sse := `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":5}}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}
`
	c := &client{name: "cc"}
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(sse))}
	ch := make(chan provider.Chunk)
	go c.readStream(context.Background(), resp, ch)

	var gotInterrupted, sawDone bool
	for ck := range ch {
		switch ck.Type {
		case provider.ChunkDone:
			sawDone = true
		case provider.ChunkError:
			var interrupted *provider.StreamInterruptedError
			gotInterrupted = errors.As(ck.Err, &interrupted)
		}
	}
	if sawDone {
		t.Fatal("must not emit ChunkDone without message_stop or stop_reason")
	}
	if !gotInterrupted {
		t.Fatal("EOF without stop_reason must stay StreamInterruptedError")
	}
}
