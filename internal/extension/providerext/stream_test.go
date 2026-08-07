package providerext

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/extension/protocol"
	"reasonix/internal/provider"
)

// openTestStream resolves the demo ref and opens a stream, returning the
// chunk channel and the stream ID the sidecar would address.
func openTestStream(t *testing.T, r *Resolver, fc *fakeClient, effort *string) (<-chan provider.Chunk, string) {
	t.Helper()
	p, err := r.Resolve(provider.Selection{Ref: "plugin/demo/fake/x", Effort: effort})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	out, err := p.Stream(context.Background(), provider.Request{
		Messages:  []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		MaxTokens: 16,
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	return out, fc.openedParams(t).StreamID
}

func textChunk(text string) protocol.ProviderChunk {
	return protocol.ProviderChunk{Type: protocol.ChunkText, Text: text}
}

// collectChunks drains the channel until it closes, failing on a wedge.
func collectChunks(t *testing.T, out <-chan provider.Chunk) []provider.Chunk {
	t.Helper()
	var chunks []provider.Chunk
	for {
		select {
		case chunk, ok := <-out:
			if !ok {
				return chunks
			}
			chunks = append(chunks, chunk)
		case <-time.After(testBudget):
			t.Fatal("stream channel did not close")
		}
	}
}

func texts(chunks []provider.Chunk) []string {
	var out []string
	for _, c := range chunks {
		out = append(out, c.Text)
	}
	return out
}

func TestStreamDeliversOutOfOrderChunksInOrder(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	r := testResolver(t, baseCatalog(), nil, fc)
	out, id := openTestStream(t, r, fc, nil)

	r.RouteStreamChunk(protocol.StreamChunkParams{StreamID: id, Seq: 2, Chunk: textChunk("b")})
	r.RouteStreamChunk(protocol.StreamChunkParams{StreamID: id, Seq: 3, Chunk: textChunk("c")})
	select {
	case chunk := <-out:
		t.Fatalf("received chunk %q before the missing seq 1 arrived", chunk.Text)
	case <-time.After(50 * time.Millisecond):
	}
	r.RouteStreamChunk(protocol.StreamChunkParams{StreamID: id, Seq: 1, Chunk: textChunk("a")})
	r.RouteStreamEnd(protocol.StreamEndParams{StreamID: id, LastSeq: 3})

	chunks := collectChunks(t, out)
	if got := texts(chunks); fmt.Sprint(got) != "[a b c]" {
		t.Fatalf("delivered texts = %v, want in-order [a b c]", got)
	}
	for _, chunk := range chunks {
		if chunk.Type != provider.ChunkText {
			t.Fatalf("chunk type = %v", chunk.Type)
		}
	}
}

func TestStreamDropsDuplicateAndStaleChunks(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	r := testResolver(t, baseCatalog(), nil, fc)
	out, id := openTestStream(t, r, fc, nil)

	r.RouteStreamChunk(protocol.StreamChunkParams{StreamID: id, Seq: 1, Chunk: textChunk("first")})
	r.RouteStreamChunk(protocol.StreamChunkParams{StreamID: id, Seq: 1, Chunk: textChunk("duplicate")})
	r.RouteStreamEnd(protocol.StreamEndParams{StreamID: id, LastSeq: 2})
	r.RouteStreamChunk(protocol.StreamChunkParams{StreamID: id, Seq: 2, Chunk: textChunk("second")})
	// A stale replay of seq 1 after delivery must not resurrect it.
	r.RouteStreamChunk(protocol.StreamChunkParams{StreamID: id, Seq: 1, Chunk: textChunk("stale")})

	chunks := collectChunks(t, out)
	if got := texts(chunks); fmt.Sprint(got) != "[first second]" {
		t.Fatalf("delivered texts = %v, want [first second]", got)
	}
}

func TestStreamCleanEndClosesChannel(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	r := testResolver(t, baseCatalog(), nil, fc)
	out, id := openTestStream(t, r, fc, nil)

	// The zero-chunk sentinel: end with LastSeq 0 closes immediately.
	r.RouteStreamEnd(protocol.StreamEndParams{StreamID: id, LastSeq: 0})
	chunks := collectChunks(t, out)
	if len(chunks) != 0 {
		t.Fatalf("chunks = %v, want none", chunks)
	}
}

func TestStreamMissingChunkAtEndInterrupts(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	r := testResolver(t, baseCatalog(), nil, fc)
	out, id := openTestStream(t, r, fc, nil)

	r.RouteStreamChunk(protocol.StreamChunkParams{StreamID: id, Seq: 1, Chunk: textChunk("a")})
	// seq 2 never arrives; the frozen boundary demands it.
	r.RouteStreamEnd(protocol.StreamEndParams{StreamID: id, LastSeq: 3})

	chunks := collectChunks(t, out)
	if len(chunks) != 2 {
		t.Fatalf("chunks = %v, want the delivered text plus the gap error", texts(chunks))
	}
	terminal := chunks[1]
	if terminal.Type != provider.ChunkError || !provider.IsStreamInterrupted(terminal.Err) {
		t.Fatalf("terminal = %+v, want interrupted ChunkError", terminal)
	}
	if !strings.Contains(terminal.Err.Error(), "missing chunk 2 of 3") {
		t.Fatalf("gap error = %q, want the missing seq named", terminal.Err)
	}
}

func TestStreamLateChunksAfterEndDropped(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	r := testResolver(t, baseCatalog(), nil, fc)
	out, id := openTestStream(t, r, fc, nil)

	r.RouteStreamChunk(protocol.StreamChunkParams{StreamID: id, Seq: 1, Chunk: textChunk("a")})
	r.RouteStreamEnd(protocol.StreamEndParams{StreamID: id, LastSeq: 1})
	chunks := collectChunks(t, out)
	if got := texts(chunks); fmt.Sprint(got) != "[a]" {
		t.Fatalf("chunks = %v", got)
	}

	// Late traffic for a completed stream is dropped, never resurrected: the
	// channel stays closed and nothing new arrives.
	r.RouteStreamChunk(protocol.StreamChunkParams{StreamID: id, Seq: 2, Chunk: textChunk("late")})
	r.RouteStreamEnd(protocol.StreamEndParams{StreamID: id, LastSeq: 2})
	select {
	case chunk, ok := <-out:
		if ok {
			t.Fatalf("late delivery %q after the stream closed", chunk.Text)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("stream channel should already be closed")
	}
}

func TestStreamRejectsBufferedChunkBeyondFrozenEnd(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	r := testResolver(t, baseCatalog(), nil, fc)
	out, id := openTestStream(t, r, fc, nil)

	r.RouteStreamChunk(protocol.StreamChunkParams{StreamID: id, Seq: 2, Chunk: textChunk("beyond")})
	r.RouteStreamEnd(protocol.StreamEndParams{StreamID: id, LastSeq: 1})

	chunks := collectChunks(t, out)
	if len(chunks) != 1 || chunks[0].Type != provider.ChunkError || !provider.IsStreamInterrupted(chunks[0].Err) {
		t.Fatalf("chunks = %+v, want interrupted protocol error", chunks)
	}
	if !strings.Contains(chunks[0].Err.Error(), "exceeds frozen LastSeq 1") {
		t.Fatalf("error = %q, want frozen boundary detail", chunks[0].Err)
	}
}

func TestStreamRejectsLateChunkBeyondFrozenEnd(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	r := testResolver(t, baseCatalog(), nil, fc)
	out, id := openTestStream(t, r, fc, nil)

	r.RouteStreamEnd(protocol.StreamEndParams{StreamID: id, LastSeq: 2})
	r.RouteStreamChunk(protocol.StreamChunkParams{StreamID: id, Seq: 3, Chunk: textChunk("late")})

	chunks := collectChunks(t, out)
	if len(chunks) != 1 || chunks[0].Type != provider.ChunkError || !provider.IsStreamInterrupted(chunks[0].Err) {
		t.Fatalf("chunks = %+v, want interrupted protocol error", chunks)
	}
}

func TestStreamRejectsConflictingDuplicateEnd(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	r := testResolver(t, baseCatalog(), nil, fc)
	out, id := openTestStream(t, r, fc, nil)

	r.RouteStreamEnd(protocol.StreamEndParams{StreamID: id, LastSeq: 2})
	r.RouteStreamEnd(protocol.StreamEndParams{StreamID: id, LastSeq: 3})

	chunks := collectChunks(t, out)
	if len(chunks) != 1 || chunks[0].Type != provider.ChunkError || !provider.IsStreamInterrupted(chunks[0].Err) {
		t.Fatalf("chunks = %+v, want interrupted protocol error", chunks)
	}
}

func TestStreamCancelSendsCancelAndCloses(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	r := testResolver(t, baseCatalog(), nil, fc)

	p, err := r.Resolve(provider.Selection{Ref: "plugin/demo/fake/x"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	out, err := p.Stream(ctx, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser}}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	id := fc.openedParams(t).StreamID

	cancel()
	fc.waitCancel(t, id)
	chunks := collectChunks(t, out)
	// Cancellation aborts delivery (the consumer is gone): any error chunk
	// that does beat the abort must be the interruption, never a hard failure.
	for _, chunk := range chunks {
		if chunk.Type == provider.ChunkError && !provider.IsStreamInterrupted(chunk.Err) {
			t.Fatalf("post-cancel chunk = %+v, want interruption only", chunk)
		}
	}
}

func TestStreamErrorChunkIsDefensivelyRedacted(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	r := testResolver(t, baseCatalog(), nil, fc)
	out, id := openTestStream(t, r, fc, nil)
	const secret = "sk-abcdef1234567890SECRETKEY"

	r.RouteStreamChunk(protocol.StreamChunkParams{StreamID: id, Seq: 1, Chunk: protocol.ProviderChunk{
		Type:  protocol.ChunkError,
		Error: &protocol.ProviderError{Code: protocol.ProviderFailed, Message: "provider rejected api_key=" + secret},
	}})
	r.RouteStreamEnd(protocol.StreamEndParams{StreamID: id, LastSeq: 1})

	chunks := collectChunks(t, out)
	if len(chunks) != 1 || chunks[0].Type != provider.ChunkError {
		t.Fatalf("chunks = %+v", chunks)
	}
	if chunks[0].Err == nil || strings.Contains(chunks[0].Err.Error(), secret) {
		t.Fatalf("error leaked credential: %v", chunks[0].Err)
	}
	if !strings.Contains(chunks[0].Err.Error(), "provider rejected api_key=") {
		t.Fatalf("error lost diagnostic context: %v", chunks[0].Err)
	}
	if provider.IsStreamInterrupted(chunks[0].Err) {
		t.Fatal("provider_failed mapped to an interruption")
	}
}

func TestStreamInterruptedErrorChunkMapsToStreamInterrupted(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	r := testResolver(t, baseCatalog(), nil, fc)
	out, id := openTestStream(t, r, fc, nil)

	r.RouteStreamChunk(protocol.StreamChunkParams{StreamID: id, Seq: 1, Chunk: protocol.ProviderChunk{
		Type:  protocol.ChunkError,
		Error: &protocol.ProviderError{Code: protocol.ProviderInterrupted, Message: "The extension provider stream was interrupted."},
	}})
	r.RouteStreamEnd(protocol.StreamEndParams{StreamID: id, LastSeq: 1})

	chunks := collectChunks(t, out)
	if len(chunks) != 1 || !provider.IsStreamInterrupted(chunks[0].Err) {
		t.Fatalf("chunks = %+v, want StreamInterruptedError", chunks)
	}
}

func TestStreamEndErrorBecomesTerminalChunkError(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	r := testResolver(t, baseCatalog(), nil, fc)
	out, id := openTestStream(t, r, fc, nil)
	const secret = "sk-abcdef1234567890SECRETKEY"

	r.RouteStreamChunk(protocol.StreamChunkParams{StreamID: id, Seq: 1, Chunk: textChunk("partial")})
	r.RouteStreamEnd(protocol.StreamEndParams{StreamID: id, LastSeq: 1, Error: "provider rejected token=" + secret})

	chunks := collectChunks(t, out)
	if len(chunks) != 2 {
		t.Fatalf("chunks = %v", texts(chunks))
	}
	terminal := chunks[1]
	if terminal.Type != provider.ChunkError || terminal.Err == nil || strings.Contains(terminal.Err.Error(), secret) {
		t.Fatalf("terminal = %+v, want the host-redacted end error", terminal)
	}
	if !strings.Contains(terminal.Err.Error(), "provider rejected token=") {
		t.Fatalf("terminal error lost diagnostic context: %q", terminal.Err)
	}
	if provider.IsStreamInterrupted(terminal.Err) {
		t.Fatal("a clean failure must not read as an interruption")
	}
}

func TestStreamEndInterruptedBecomesStreamInterrupted(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	r := testResolver(t, baseCatalog(), nil, fc)
	out, id := openTestStream(t, r, fc, nil)

	r.RouteStreamEnd(protocol.StreamEndParams{StreamID: id, LastSeq: 0, Interrupted: true})
	chunks := collectChunks(t, out)
	if len(chunks) != 1 || !provider.IsStreamInterrupted(chunks[0].Err) {
		t.Fatalf("chunks = %+v, want StreamInterruptedError", chunks)
	}
}

func TestStreamChunkTypesRoundTripThroughDTO(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	r := testResolver(t, baseCatalog(), nil, fc)
	out, id := openTestStream(t, r, fc, nil)

	r.RouteStreamChunk(protocol.StreamChunkParams{StreamID: id, Seq: 1, Chunk: protocol.ProviderChunk{
		Type: protocol.ChunkReasoning, Text: "thinking", Signature: "sig-123",
	}})
	r.RouteStreamChunk(protocol.StreamChunkParams{StreamID: id, Seq: 2, Chunk: protocol.ProviderChunk{
		Type: protocol.ChunkToolCallStart, ToolCall: &protocol.ProviderToolCall{ID: "call-1", Name: "bash"},
	}})
	r.RouteStreamChunk(protocol.StreamChunkParams{StreamID: id, Seq: 3, Chunk: protocol.ProviderChunk{
		Type: protocol.ChunkToolCallDelta, ToolCall: &protocol.ProviderToolCall{ID: "call-1", Name: "bash"}, ArgChars: 42,
	}})
	r.RouteStreamChunk(protocol.StreamChunkParams{StreamID: id, Seq: 4, Chunk: protocol.ProviderChunk{
		Type: protocol.ChunkToolCall,
		ToolCall: &protocol.ProviderToolCall{
			ID: "call-1", Name: "bash", Arguments: `{"cmd":"ls"}`, ThoughtSignature: "gemini-sig",
		},
	}})
	r.RouteStreamChunk(protocol.StreamChunkParams{StreamID: id, Seq: 5, Chunk: protocol.ProviderChunk{
		Type: protocol.ChunkUsage,
		Usage: &protocol.ProviderUsage{
			PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30,
			CacheHitTokens: 4, CacheMissTokens: 6, ReasoningTokens: 8, FinishReason: "tool_calls",
		},
	}})
	r.RouteStreamEnd(protocol.StreamEndParams{StreamID: id, LastSeq: 5})

	chunks := collectChunks(t, out)
	if len(chunks) != 5 {
		t.Fatalf("chunks = %d, want 5", len(chunks))
	}
	if chunks[0].Type != provider.ChunkReasoning || chunks[0].Text != "thinking" || chunks[0].Signature != "sig-123" {
		t.Fatalf("reasoning chunk = %+v", chunks[0])
	}
	if chunks[1].Type != provider.ChunkToolCallStart || chunks[1].ToolCall == nil || chunks[1].ToolCall.ID != "call-1" {
		t.Fatalf("tool-call-start chunk = %+v", chunks[1])
	}
	if chunks[2].Type != provider.ChunkToolCallArgsDelta || chunks[2].ArgChars != 42 {
		t.Fatalf("args-delta chunk = %+v", chunks[2])
	}
	if chunks[3].Type != provider.ChunkToolCall || chunks[3].ToolCall.Arguments != `{"cmd":"ls"}` || chunks[3].ToolCall.ThoughtSignature != "gemini-sig" {
		t.Fatalf("tool-call chunk = %+v", chunks[3])
	}
	usage := chunks[4].Usage
	if chunks[4].Type != provider.ChunkUsage || usage == nil ||
		usage.PromptTokens != 10 || usage.CompletionTokens != 20 || usage.TotalTokens != 30 ||
		usage.CacheHitTokens != 4 || usage.CacheMissTokens != 6 || usage.ReasoningTokens != 8 ||
		usage.FinishReason != "tool_calls" {
		t.Fatalf("usage chunk = %+v", chunks[4])
	}
}

func TestStreamDisconnectMidStreamInterrupts(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	r := testResolver(t, baseCatalog(), nil, fc)
	out, id := openTestStream(t, r, fc, nil)

	r.RouteStreamChunk(protocol.StreamChunkParams{StreamID: id, Seq: 1, Chunk: textChunk("a")})
	fc.kill() // mid-stream crash: no end, no more chunks, ever

	chunks := collectChunks(t, out)
	if len(chunks) != 2 {
		t.Fatalf("chunks = %v, want delivered text plus the interruption", texts(chunks))
	}
	terminal := chunks[1]
	if terminal.Type != provider.ChunkError || !provider.IsStreamInterrupted(terminal.Err) {
		t.Fatalf("terminal = %+v, want StreamInterruptedError", terminal)
	}
	if !strings.Contains(terminal.Err.Error(), "demo") {
		t.Fatalf("interruption = %q, want the plugin named", terminal.Err)
	}
}

func TestStreamFailsFastAfterCrash(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	r := testResolver(t, baseCatalog(), nil, fc)
	fc.kill()

	p, err := r.Resolve(provider.Selection{Ref: "plugin/demo/fake/x"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	_, err = p.Stream(context.Background(), provider.Request{})
	if !provider.IsStreamInterrupted(err) {
		t.Fatalf("Stream error = %v, want fail-fast StreamInterruptedError", err)
	}
	if opens := len(fc.opened); opens != 0 {
		t.Fatalf("stream opens = %d, want none after the crash", opens)
	}
}

func TestStreamOpenDeclined(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	fc.accept = false
	r := testResolver(t, baseCatalog(), nil, fc)

	p, err := r.Resolve(provider.Selection{Ref: "plugin/demo/fake/x"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	_, err = p.Stream(context.Background(), provider.Request{})
	if err == nil || !strings.Contains(err.Error(), "declined") {
		t.Fatalf("Stream error = %v, want declined", err)
	}
}

func TestStreamOpenInterruptedErrorMapsToStreamInterrupted(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	fc.openErr = &protocol.ProtocolError{Reason: protocol.ErrProviderInterrupted, Message: "extension sidecar demo crashed"}
	r := testResolver(t, baseCatalog(), nil, fc)

	p, err := r.Resolve(provider.Selection{Ref: "plugin/demo/fake/x"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	_, err = p.Stream(context.Background(), provider.Request{})
	if !provider.IsStreamInterrupted(err) {
		t.Fatalf("Stream error = %v, want StreamInterruptedError", err)
	}
}

func TestStreamOpenGenericErrorPassesThrough(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	fc.openErr = errors.New("transport wedged")
	r := testResolver(t, baseCatalog(), nil, fc)

	p, err := r.Resolve(provider.Selection{Ref: "plugin/demo/fake/x"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	_, err = p.Stream(context.Background(), provider.Request{})
	if err == nil || !strings.Contains(err.Error(), "transport wedged") {
		t.Fatalf("Stream error = %v", err)
	}
	if provider.IsStreamInterrupted(err) {
		t.Fatal("generic open failure mapped to an interruption")
	}
}

func TestStreamOpenCarriesRequestEffortAndSeqBase(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	r := testResolver(t, baseCatalog(), nil, fc)

	effort := "high"
	p, err := r.Resolve(provider.Selection{Ref: "plugin/demo/fake/x", Effort: &effort})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	temperature := 0.5
	out, err := p.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: "sys"},
			{Role: provider.RoleUser, Content: "hi", Images: []string{"data:image/png;base64,AA=="}},
			{Role: provider.RoleAssistant, Content: "prev", ReasoningContent: "because", ReasoningSignature: "rs"},
		},
		Tools:       []provider.ToolSchema{{Name: "bash", Description: "run", Parameters: []byte(`{"type":"object"}`)}},
		Temperature: &temperature,
		MaxTokens:   128,
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	params := fc.openedParams(t)
	if params.ProviderRef != "plugin/demo/fake/x" || params.Model != "x" || params.Effort != "high" {
		t.Fatalf("open params = %+v", params)
	}
	if params.SeqBase != 1 {
		t.Fatalf("SeqBase = %d, want 1-based chunk numbering", params.SeqBase)
	}
	if !strings.HasPrefix(params.StreamID, "es_") {
		t.Fatalf("StreamID = %q, want the es_ prefix", params.StreamID)
	}
	req := params.Request
	if len(req.Messages) != 3 || len(req.Tools) != 1 {
		t.Fatalf("request = %+v", req)
	}
	if req.Messages[1].Images[0] != "data:image/png;base64,AA==" || req.Messages[2].ReasoningSignature != "rs" {
		t.Fatalf("request messages did not convert: %+v", req.Messages)
	}
	if req.Tools[0].Name != "bash" || string(req.Tools[0].Parameters) != `{"type":"object"}` {
		t.Fatalf("request tools did not convert: %+v", req.Tools)
	}
	if req.Temperature == nil || *req.Temperature != 0.5 || req.MaxTokens != 128 {
		t.Fatalf("request scalars = %+v", req)
	}

	// Finish the stream cleanly so its watcher cannot outlive the test.
	r.RouteStreamEnd(protocol.StreamEndParams{StreamID: params.StreamID, LastSeq: 0})
	collectChunks(t, out)
}

func TestProviderReasoningPoliciesComeFromDescriptor(t *testing.T) {
	descriptor := demoDescriptor()
	descriptor.ToolCallReasoning = true
	descriptor.ReasoningRoundTrip = true
	descriptor.WarnOnMissingToolCallReasoning = true
	fc := newFakeClient("demo", descriptor)
	r := testResolver(t, baseCatalog(), nil, fc)

	p, err := r.Resolve(provider.Selection{Ref: "plugin/demo/fake/x"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !provider.RequiresToolCallReasoning(p) || !provider.RequiresReasoningRoundTrip(p) || !provider.WarnOnMissingToolCallReasoning(p) {
		t.Fatal("descriptor reasoning policies did not propagate")
	}
	if identity := p.(interface{ MissingToolCallReasoningWarningIdentity() string }).MissingToolCallReasoningWarningIdentity(); !strings.Contains(identity, "demo") || !strings.Contains(identity, "plugin/demo/fake/x") {
		t.Fatalf("warning identity = %q", identity)
	}
}

func TestRouteUnknownStreamDropped(t *testing.T) {
	r := testResolver(t, baseCatalog(), nil)
	// No stream registered: routing must not panic or create state.
	r.RouteStreamChunk(protocol.StreamChunkParams{StreamID: "es_nope", Seq: 1, Chunk: textChunk("x")})
	r.RouteStreamEnd(protocol.StreamEndParams{StreamID: "es_nope", LastSeq: 1})
	r.mu.Lock()
	registered := len(r.streams)
	r.mu.Unlock()
	if registered != 0 {
		t.Fatalf("unknown routing created %d streams", registered)
	}
}

func TestStreamDeliveryOverflowTerminates(t *testing.T) {
	r := testResolver(t, baseCatalog(), nil)
	stream := &extensionStream{
		out:          make(chan provider.Chunk, 1),
		done:         make(chan struct{}),
		deliveryWake: make(chan struct{}, 1),
		nextSeq:      1,
		pending:      map[int64]provider.Chunk{},
		delivery:     make([]provider.Chunk, deliveryQueueLimit-1),
	}
	r.mu.Lock()
	r.streams["overflow"] = stream
	stream.pending[1] = provider.Chunk{Type: provider.ChunkText, Text: "overflow"}
	r.flushLocked("overflow", stream)
	_, stillRegistered := r.streams["overflow"]
	final := stream.deliveryFinal
	queued := append([]provider.Chunk(nil), stream.delivery...)
	r.mu.Unlock()

	if stillRegistered || !final {
		t.Fatal("overflowing stream was not terminated")
	}
	if len(queued) != deliveryQueueLimit || queued[len(queued)-1].Err == nil ||
		!provider.IsStreamInterrupted(queued[len(queued)-1].Err) {
		t.Fatalf("overflow queue = %d chunks, terminal %v", len(queued), queued[len(queued)-1].Err)
	}
}

func TestStreamDisconnectDoesNotBlockOnBackpressure(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	r := testResolver(t, baseCatalog(), nil, fc)
	stream := &extensionStream{
		client:        fc,
		out:           make(chan provider.Chunk, 1),
		done:          make(chan struct{}),
		abortDelivery: make(chan struct{}),
		deliveryWake:  make(chan struct{}, 1),
		nextSeq:       1,
		pending: map[int64]provider.Chunk{
			1: {Type: provider.ChunkText, Text: "one"},
			2: {Type: provider.ChunkText, Text: "two"},
		},
	}
	r.mu.Lock()
	r.streams["backpressure"] = stream
	r.mu.Unlock()
	go r.deliverStream(stream)

	r.mu.Lock()
	r.flushLocked("backpressure", stream)
	r.mu.Unlock()

	deadline := time.Now().Add(time.Second)
	for len(stream.out) != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(stream.out) != 1 {
		t.Fatal("stream never filled its output buffer")
	}

	fc.kill()
	// The watchStream goroutine only exists for streams opened through
	// Resolver.open; this hand-built stream finishes the way the broker's
	// Detach does, directly.
	r.mu.Lock()
	r.finishLocked("backpressure", stream, provider.Chunk{Type: provider.ChunkError, Err: &provider.StreamInterruptedError{
		Err: errors.New("extension sidecar demo disconnected"),
	}})
	r.mu.Unlock()
	var chunks []provider.Chunk
	for chunk := range stream.out {
		chunks = append(chunks, chunk)
	}
	// The disconnect finishes the stream without aborting delivery: buffered
	// chunks drain ahead of the terminal interruption.
	if len(chunks) != 3 || chunks[0].Text != "one" || chunks[1].Text != "two" ||
		!provider.IsStreamInterrupted(chunks[2].Err) {
		t.Fatalf("delivered chunks = %#v, want ordered text followed by interruption", chunks)
	}
}

func TestStreamAbandonedConsumerDoesNotLeakDelivery(t *testing.T) {
	r := testResolver(t, baseCatalog(), nil)
	stream := &extensionStream{
		out:           make(chan provider.Chunk, 1),
		abortDelivery: make(chan struct{}),
		deliveryWake:  make(chan struct{}, 1),
		delivery: []provider.Chunk{
			{Type: provider.ChunkText, Text: "one"},
			{Type: provider.ChunkText, Text: "two"},
		},
	}
	exited := make(chan struct{})
	go func() {
		r.deliverStream(stream)
		close(exited)
	}()
	deadline := time.Now().Add(time.Second)
	for len(stream.out) != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(stream.out) != 1 {
		t.Fatal("delivery did not fill the abandoned consumer buffer")
	}
	r.mu.Lock()
	r.abortDeliveryLocked(stream)
	r.mu.Unlock()
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("delivery goroutine remained blocked after abort")
	}
}

func TestConcurrentStreamsOnOneSidecar(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	r := testResolver(t, baseCatalog(), nil, fc)

	const streamCount = 8
	const chunkCount = 20
	type handle struct {
		out <-chan provider.Chunk
		id  string
	}
	handles := make([]handle, 0, streamCount)
	for i := range streamCount {
		p, err := r.Resolve(provider.Selection{Ref: "plugin/demo/fake/x"})
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		out, err := p.Stream(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser}}})
		if err != nil {
			t.Fatalf("Stream %d: %v", i, err)
		}
		fc.mu.Lock()
		id := fc.opened[len(fc.opened)-1].StreamID
		fc.mu.Unlock()
		handles = append(handles, handle{out: out, id: id})
	}

	// Interleave chunk routing for every stream from separate goroutines.
	var wg sync.WaitGroup
	for i, h := range handles {
		wg.Add(1)
		go func(i int, h handle) {
			defer wg.Done()
			for seq := int64(1); seq <= chunkCount; seq++ {
				r.RouteStreamChunk(protocol.StreamChunkParams{
					StreamID: h.id, Seq: seq,
					Chunk: textChunk(fmt.Sprintf("s%d-c%d", i, seq)),
				})
			}
			r.RouteStreamEnd(protocol.StreamEndParams{StreamID: h.id, LastSeq: chunkCount})
		}(i, h)
	}
	wg.Wait()

	for i, h := range handles {
		chunks := collectChunks(t, h.out)
		if len(chunks) != chunkCount {
			t.Fatalf("stream %d delivered %d chunks, want %d", i, len(chunks), chunkCount)
		}
		for seq := 1; seq <= chunkCount; seq++ {
			want := fmt.Sprintf("s%d-c%d", i, seq)
			if chunks[seq-1].Text != want {
				t.Fatalf("stream %d chunk %d = %q, want %q", i, seq, chunks[seq-1].Text, want)
			}
		}
	}
}

// TestStreamPendingWindowOverflowInterrupts: a sidecar emitting ever-higher
// sequences without the missing next chunk must not grow the pending buffer
// without bound — the stream fails interrupted once the sequence window is
// exceeded.
func TestStreamPendingWindowOverflowInterrupts(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	r := testResolver(t, baseCatalog(), nil, fc)
	out, id := openTestStream(t, r, fc, nil)

	r.RouteStreamChunk(protocol.StreamChunkParams{StreamID: id, Seq: 1, Chunk: textChunk("first")})
	// Seqs 2..256 sit inside the pending window; none is delivered while seq
	// 2 is missing... feed a gap first: seq 3 skips 2, so nextSeq stalls.
	for seq := int64(3); seq <= pendingWindowLimit+1; seq++ {
		r.RouteStreamChunk(protocol.StreamChunkParams{StreamID: id, Seq: seq, Chunk: textChunk("gap")})
	}
	select {
	case chunk := <-out:
		if chunk.Type != provider.ChunkText {
			t.Fatalf("unexpected early terminal chunk: %+v", chunk)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("seq 1 should have been delivered immediately")
	}
	// The first chunk beyond the window terminates the stream.
	r.RouteStreamChunk(protocol.StreamChunkParams{StreamID: id, Seq: pendingWindowLimit + 2, Chunk: textChunk("overflow")})

	chunks := collectChunks(t, out)
	last := chunks[len(chunks)-1]
	if last.Type != provider.ChunkError || !provider.IsStreamInterrupted(last.Err) {
		t.Fatalf("terminal chunk = %+v, want interrupted error", last)
	}
}
