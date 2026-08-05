package extension

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

// scriptProvider is a test Provider with scripted catalog and streams.
type scriptProvider struct {
	catalog    []ProviderDescriptor
	catalogErr error

	streamErr error
	// makeChannel builds the chunk channel for one Stream call; the test owns
	// the channel lifecycle.
	makeChannel func(req StreamRequest) <-chan StreamChunk

	mu       sync.Mutex
	requests []StreamRequest
}

func (p *scriptProvider) Catalog(context.Context) ([]ProviderDescriptor, error) {
	if p.catalogErr != nil {
		return nil, p.catalogErr
	}
	return p.catalog, nil
}

func (p *scriptProvider) Stream(_ context.Context, req StreamRequest) (<-chan StreamChunk, error) {
	p.mu.Lock()
	p.requests = append(p.requests, req)
	p.mu.Unlock()
	if p.streamErr != nil {
		return nil, p.streamErr
	}
	return p.makeChannel(req), nil
}

func providerHandler() *testHandler {
	return &testHandler{result: &InitializeResult{
		Name: "provider-ext", Version: "1.0.0",
		Providers: []ProviderDescriptor{{Ref: "plugin/provider-ext/echo", DisplayName: "Echo", Model: "echo-1"}},
	}}
}

func openStreamRequest(streamID string) StreamOpenParams {
	return StreamOpenParams{
		StreamID:    streamID,
		ProviderRef: "plugin/provider-ext/echo",
		Model:       "echo-1",
		Request: ProviderRequest{
			Messages: []ProviderMessage{{Role: ProviderRoleUser, Content: "hi"}},
			Tools:    []ProviderToolSchema{},
		},
		SeqBase: 1,
	}
}

// TestProviderCatalog serves extension/provider/catalog.
func TestProviderCatalog(t *testing.T) {
	provider := &scriptProvider{catalog: []ProviderDescriptor{
		{Ref: "plugin/provider-ext/echo", DisplayName: "Echo", Model: "echo-1", ContextWindow: 8192, Tools: true},
	}}
	host, _ := startFakeHost(t, providerHandler(), Options{Provider: provider})
	host.handshake(t)
	resp := host.request(MethodExtensionProviderCatalog, ProviderCatalogParams{})
	if resp.Err != nil {
		t.Fatalf("catalog failed: %+v", resp.Err)
	}
	var result ProviderCatalogResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if len(result.Providers) != 1 || result.Providers[0].Ref != "plugin/provider-ext/echo" || !result.Providers[0].Tools {
		t.Fatalf("catalog = %+v", result.Providers)
	}
}

// TestProviderCatalogNil ensures the array shape survives an empty catalog:
// the wire requires "providers":[], never null.
func TestProviderCatalogNil(t *testing.T) {
	provider := &scriptProvider{}
	host, _ := startFakeHost(t, providerHandler(), Options{Provider: provider})
	host.handshake(t)
	resp := host.request(MethodExtensionProviderCatalog, ProviderCatalogParams{})
	var raw struct {
		Providers json.RawMessage `json:"providers"`
	}
	if err := json.Unmarshal(resp.Result, &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(raw.Providers) != "[]" {
		t.Fatalf("providers = %s, want []", raw.Providers)
	}
}

// TestProviderCatalogWithoutProvider answers unknown_method when no Provider
// is configured.
func TestProviderCatalogWithoutProvider(t *testing.T) {
	host, _ := startFakeHost(t, basicHandler(), Options{})
	host.handshake(t)
	resp := host.request(MethodExtensionProviderCatalog, ProviderCatalogParams{})
	if resp.Err == nil || resp.Err.Code != CodeMethodNotFound {
		t.Fatalf("expected unknown_method, got %+v", resp.Err)
	}
}

// TestProviderStreamPump verifies contiguous 1-based seqs and the terminal
// stream/end lastSeq.
func TestProviderStreamPump(t *testing.T) {
	chunks := make(chan StreamChunk, 4)
	chunks <- TextChunk("Hello")
	chunks <- ReasoningChunk("thinking", "sig-1")
	chunks <- UsageChunk(ProviderUsage{PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5, FinishReason: "stop"})
	close(chunks)
	provider := &scriptProvider{makeChannel: func(StreamRequest) <-chan StreamChunk { return chunks }}
	host, _ := startFakeHost(t, providerHandler(), Options{Provider: provider})
	host.handshake(t)

	resp := host.request(MethodExtensionProviderStreamOpen, openStreamRequest("stream-1"))
	if resp.Err != nil {
		t.Fatalf("stream open failed: %+v", resp.Err)
	}
	var opened StreamOpenResult
	if err := json.Unmarshal(resp.Result, &opened); err != nil || !opened.Accepted {
		t.Fatalf("open result = %+v", opened)
	}

	endParams := host.waitStreamEnd()
	sentChunks, ends := host.streamNotifications()
	if len(ends) != 1 {
		t.Fatalf("stream/end count = %d, want exactly 1", len(ends))
	}
	if endParams.StreamID != "stream-1" || endParams.LastSeq != 3 || endParams.Error != "" || endParams.Interrupted {
		t.Fatalf("end = %+v, want lastSeq 3 clean", endParams)
	}
	for i, chunk := range sentChunks {
		if chunk.Seq != int64(i+1) {
			t.Fatalf("chunk %d seq = %d, want contiguous 1-based", i, chunk.Seq)
		}
		if chunk.StreamID != "stream-1" {
			t.Fatalf("chunk %d streamId = %q", i, chunk.StreamID)
		}
	}
	if sentChunks[0].Chunk.Type != ChunkText || sentChunks[0].Chunk.Text != "Hello" {
		t.Fatalf("chunk 0 = %+v", sentChunks[0].Chunk)
	}
	if sentChunks[1].Chunk.Type != ChunkReasoning || sentChunks[1].Chunk.Signature != "sig-1" {
		t.Fatalf("chunk 1 = %+v", sentChunks[1].Chunk)
	}
	if sentChunks[2].Chunk.Usage == nil || sentChunks[2].Chunk.Usage.TotalTokens != 5 {
		t.Fatalf("chunk 2 = %+v", sentChunks[2].Chunk)
	}
}

// TestProviderStreamCancel asserts a processed cancel stops chunk production:
// no chunk may be sent after the cancel response, and the stream ends
// interrupted.
func TestProviderStreamCancel(t *testing.T) {
	chunks := make(chan StreamChunk) // unbuffered: every send is visible
	provider := &scriptProvider{makeChannel: func(StreamRequest) <-chan StreamChunk { return chunks }}
	host, _ := startFakeHost(t, providerHandler(), Options{Provider: provider})
	host.handshake(t)
	resp := host.request(MethodExtensionProviderStreamOpen, openStreamRequest("stream-c"))
	var opened StreamOpenResult
	if err := json.Unmarshal(resp.Result, &opened); err != nil || !opened.Accepted {
		t.Fatalf("open result = %+v", opened)
	}

	// Feed one chunk, wait for it on the wire.
	go func() { chunks <- TextChunk("one") }()
	first := host.nextNotification(MethodExtensionProviderStreamChunk)

	// Cancel; the response means the SDK processed it.
	resp = host.request(MethodExtensionProviderStreamCancel, StreamCancelParams{StreamID: "stream-c"})
	var cancelled StreamCancelResult
	if err := json.Unmarshal(resp.Result, &cancelled); err != nil || !cancelled.Cancelled {
		t.Fatalf("cancel result = %+v respErr=%+v", cancelled, resp.Err)
	}

	// Keep producing: none of these may reach the wire.
	go func() {
		for i := 0; i < 5; i++ {
			chunks <- TextChunk("late")
		}
	}()
	endParams := host.waitStreamEnd()
	if !endParams.Interrupted || endParams.LastSeq != 1 {
		t.Fatalf("end = %+v, want interrupted lastSeq 1", endParams)
	}
	sentChunks, _ := host.streamNotifications()
	for _, chunk := range sentChunks {
		if chunk.Seq > 1 {
			t.Fatalf("chunk seq %d sent after the cancel was processed", chunk.Seq)
		}
	}
	var firstParams StreamChunkParams
	if err := json.Unmarshal(first.Params, &firstParams); err != nil || firstParams.Seq != 1 {
		t.Fatalf("first chunk = %+v", firstParams)
	}
}

// TestProviderStreamErrorChunk maps a provider error chunk to stream/end's
// error field without forwarding the chunk.
func TestProviderStreamErrorChunk(t *testing.T) {
	chunks := make(chan StreamChunk, 2)
	chunks <- TextChunk("partial")
	chunks <- ErrorChunk("provider upstream unavailable")
	close(chunks)
	provider := &scriptProvider{makeChannel: func(StreamRequest) <-chan StreamChunk { return chunks }}
	host, _ := startFakeHost(t, providerHandler(), Options{Provider: provider})
	host.handshake(t)
	host.request(MethodExtensionProviderStreamOpen, openStreamRequest("stream-e"))

	endParams := host.waitStreamEnd()
	if endParams.Error != "provider upstream unavailable" {
		t.Fatalf("end.error = %q", endParams.Error)
	}
	if endParams.LastSeq != 1 || endParams.Interrupted {
		t.Fatalf("end = %+v, want lastSeq 1 not interrupted", endParams)
	}
	sentChunks, _ := host.streamNotifications()
	if len(sentChunks) != 1 || sentChunks[0].Chunk.Type != ChunkText {
		t.Fatalf("chunks = %+v, want only the text chunk forwarded", sentChunks)
	}
}

// TestProviderStreamOpenError answers provider_failed when Stream refuses to
// open.
func TestProviderStreamOpenError(t *testing.T) {
	provider := &scriptProvider{streamErr: errors.New("quota exhausted")}
	host, _ := startFakeHost(t, providerHandler(), Options{Provider: provider})
	host.handshake(t)
	resp := host.request(MethodExtensionProviderStreamOpen, openStreamRequest("stream-f"))
	if resp.Err == nil {
		t.Fatal("expected provider_failed")
	}
	data, _ := resp.Err.Data.(ProtocolErrorData)
	if data.Reason != ErrProviderFailed {
		t.Fatalf("reason = %q, want provider_failed", data.Reason)
	}
}

// TestProviderStreamOpenInvalidEnvelope rejects malformed opens before they
// reach the Provider.
func TestProviderStreamOpenInvalidEnvelope(t *testing.T) {
	var calls atomic.Int64
	provider := &scriptProvider{makeChannel: func(StreamRequest) <-chan StreamChunk {
		calls.Add(1)
		return make(chan StreamChunk)
	}}
	host, _ := startFakeHost(t, providerHandler(), Options{Provider: provider})
	host.handshake(t)
	frames := []string{
		`{"providerRef":"x","request":{"messages":[],"tools":[]},"seqBase":1,"streamId":""}`,
		`{"providerRef":"x","request":{"messages":null,"tools":[]},"seqBase":1,"streamId":"s"}`,
		`{"providerRef":"x","request":{"messages":[],"tools":[{"name":"t","parameters":[1]}]},"seqBase":1,"streamId":"s"}`,
		`{"providerRef":"x","request":{"messages":[],"tools":[]},"seqBase":-1,"streamId":"s"}`,
	}
	for _, params := range frames {
		resp := host.request(MethodExtensionProviderStreamOpen, json.RawMessage(params))
		if resp.Err == nil || resp.Err.Code != CodeInvalidParams {
			t.Fatalf("params %s: expected invalid_params, got %+v", params, resp.Err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("Stream called %d times on invalid envelopes", calls.Load())
	}
}

// TestStreamRequestPassedThrough checks the helper-level StreamRequest maps
// the wire params faithfully.
func TestStreamRequestPassedThrough(t *testing.T) {
	chunks := make(chan StreamChunk)
	close(chunks)
	provider := &scriptProvider{makeChannel: func(StreamRequest) <-chan StreamChunk { return chunks }}
	host, _ := startFakeHost(t, providerHandler(), Options{Provider: provider})
	host.handshake(t)
	open := openStreamRequest("stream-req")
	open.Effort = "high"
	open.Request.MaxTokens = 128
	temp := 0.5
	open.Request.Temperature = &temp
	host.request(MethodExtensionProviderStreamOpen, open)
	host.waitStreamEnd()
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.requests) != 1 {
		t.Fatalf("Stream calls = %d", len(provider.requests))
	}
	req := provider.requests[0]
	if req.StreamID != "stream-req" || req.ProviderRef != "plugin/provider-ext/echo" || req.Model != "echo-1" || req.Effort != "high" {
		t.Fatalf("request = %+v", req)
	}
	if req.Request.MaxTokens != 128 || req.Request.Temperature == nil || *req.Request.Temperature != 0.5 {
		t.Fatalf("provider request = %+v", req.Request)
	}
	if len(req.Request.Messages) != 1 || req.Request.Messages[0].Content != "hi" {
		t.Fatalf("messages = %+v", req.Request.Messages)
	}
}
