// Package anthropic implements the Anthropic Messages API provider (POST
// /v1/messages, SSE streaming) with a hand-written net/http client — no SDK. It
// self-registers under the "anthropic" kind, so any Claude model is a config
// instance rather than code.
//
// Two notes, both rooted in the transport-agnostic provider.Message abstraction:
//
//   - Extended thinking is opt-in (provider config thinking="adaptive"). Anthropic
//     requires the *signed* thinking block be replayed on the next turn when a tool
//     call followed thinking, so Message carries ReasoningSignature alongside
//     ReasoningContent and this provider replays the signed block on the next
//     request. Some Anthropic-compatible gateways such as LongCat instead use
//     thinking.type enabled|disabled; those values are passed through without
//     Anthropic's display/output_config fields. Off by default because the field is
//     provider-specific. (redacted_thinking blocks are not yet captured/replayed.)
//   - No temperature/top_p. Current Claude models (Opus 4.8/4.7) reject sampling
//     parameters with a 400; Anthropic steers behavior via prompting instead.
package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/netclient"
	"reasonix/internal/provider"
)

// defaultStreamIdleTimeout caps how long a started SSE stream may go silent before
// it's treated as a dropped connection — a half-open TCP connection (proxy switched
// mid-stream) sends no RST, so scanner.Scan() would block forever. Generous on
// purpose; live streams emit far more often. Stored per-client (client.idleTimeout)
// so a test can shorten it without a shared global that races other watchdogs.
const defaultStreamIdleTimeout = 120 * time.Second

const (
	// anthropicVersion is the required API version header value.
	anthropicVersion = "2023-06-01"
	// defaultBaseURL is the first-party endpoint; config may override it (e.g. a
	// gateway). Bedrock/Vertex use a different request shape and are out of scope.
	defaultBaseURL = "https://api.anthropic.com"
	// defaultMaxTokens is the output ceiling used when the request leaves MaxTokens
	// unset. Anthropic *requires* max_tokens, and the agent currently doesn't set
	// it, so this is the de-facto cap. Generous (you only pay for tokens actually
	// produced) and within every catalog model's limit (Sonnet/Haiku 64K, Opus 128K).
	defaultMaxTokens = 32768
)

func init() {
	provider.Register("anthropic", New)
}

// New builds an Anthropic provider from a resolved config.
func New(cfg provider.Config) (provider.Provider, error) {
	if cfg.Model == "" {
		return nil, fmt.Errorf("anthropic: model is required for provider %q", cfg.Name)
	}
	name := cfg.Name
	if name == "" {
		name = "anthropic"
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	keyEnv, _ := cfg.Extra["api_key_env"].(string) // for actionable auth errors
	keySource, _ := cfg.Extra["api_key_source"].(string)
	thinking, _ := cfg.Extra["thinking"].(string)
	thinking = strings.ToLower(strings.TrimSpace(thinking))
	effort, _ := cfg.Extra["effort"].(string)
	effort = strings.ToLower(strings.TrimSpace(effort))
	vision, _ := cfg.Extra["vision"].(bool)
	headers, _ := cfg.Extra["headers"].(map[string]string)
	authHeader, _ := cfg.Extra["auth_header"].(bool)
	httpClient, err := newHTTPClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("anthropic: network: %w", err)
	}
	// Anthropic's API surface is at {root}/v1/messages, so c.baseURL stores
	// the *root* — without any trailing /v1. The setup wizard, however, lets
	// users paste a full OpenAI-compatible URL (e.g.
	// "https://proxy.example.com/v1") because that's what /models probes
	// expect. Stripping the trailing /v1 here makes both forms land on the
	// same endpoint without forcing users to remember Anthropic's quirky
	// root-vs-versioned split. Without this, a user pasting
	// "https://proxy.example.com/v1" would probe /v1/models successfully
	// but get the chat client concatenating onto
	// "https://proxy.example.com/v1/v1/messages" — a 404.
	root := strings.TrimRight(baseURL, "/")
	root = strings.TrimSuffix(root, "/v1")
	if root == "" {
		root = defaultBaseURL
	}
	return &client{
		name:        name,
		apiKey:      cfg.APIKey,
		keyEnv:      keyEnv,
		keySource:   keySource,
		baseURL:     root,
		model:       cfg.Model,
		thinking:    thinking,
		effort:      effort,
		vision:      vision,
		headers:     cleanCustomHeaders(headers),
		authHeader:  authHeader,
		http:        httpClient, // no overall timeout; lifecycle is ctx-driven
		idleTimeout: defaultStreamIdleTimeout,
	}, nil
}

func newHTTPClient(cfg provider.Config) (*http.Client, error) {
	spec, _ := cfg.Extra["proxy_spec"].(netclient.ProxySpec)
	return netclient.NewHTTPClient(spec, netclient.TransportOptions{})
}

type client struct {
	name        string
	apiKey      string
	keyEnv      string // api_key_env name, surfaced in auth errors
	keySource   string // source of keyEnv, surfaced in auth errors
	baseURL     string
	model       string
	thinking    string // "adaptive" enables extended thinking; "" = off (config-driven)
	effort      string // output_config.effort: low|medium|high|xhigh|max; "" = provider default
	vision      bool   // model accepts image input — embed attached images as base64 image blocks
	headers     map[string]string
	authHeader  bool // send Authorization: Bearer instead of Anthropic's x-api-key header
	http        *http.Client
	idleTimeout time.Duration // SSE stall watchdog window; defaultStreamIdleTimeout unless a test overrides
	authed      atomic.Bool   // a request has succeeded — gate transient-401 retry
}

func (c *client) Name() string { return c.name }

func (c *client) sendOpts() provider.SendOptions {
	return provider.SendOptions{
		Provider:   c.name,
		KeyEnv:     c.keyEnv,
		KeySource:  c.keySource,
		KeyPresent: c.apiKey != "",
		RetryAuth:  c.authed.Load(),
	}
}

func cleanCustomHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for name, value := range in {
		name = strings.TrimSpace(name)
		if name == "" || reservedCustomHeader(name) {
			continue
		}
		out[name] = strings.TrimSpace(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func reservedCustomHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "content-type", "accept", "x-api-key", "authorization", "anthropic-version":
		return true
	default:
		return false
	}
}

func applyCustomHeaders(h http.Header, headers map[string]string) {
	for name, value := range cleanCustomHeaders(headers) {
		h.Set(name, value)
	}
}

// bufPool reuses byte buffers for JSON-marshalled request bodies, reducing GC
// churn from repeated alloc/free of ~10-100KB buffers per turn.
var bufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

func (c *client) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	if err := json.NewEncoder(buf).Encode(c.buildRequest(req)); err != nil {
		bufPool.Put(buf)
		return nil, fmt.Errorf("%s: marshal request: %w", c.name, err)
	}
	body := make([]byte, buf.Len())
	copy(body, buf.Bytes())
	bufPool.Put(buf)

	newReq := func(ctx context.Context) (*http.Request, error) {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")
		if c.authHeader {
			httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		} else {
			httpReq.Header.Set("x-api-key", c.apiKey)
		}
		httpReq.Header.Set("anthropic-version", anthropicVersion)
		applyCustomHeaders(httpReq.Header, c.headers)
		return httpReq, nil
	}
	resp, err := provider.SendWithRetry(ctx, c.http, c.sendOpts(), newReq)
	if err != nil {
		return nil, err
	}
	c.authed.Store(true)

	out := make(chan provider.Chunk)
	go c.readStream(ctx, resp, out)
	return out, nil
}

// buildRequest converts the transport-agnostic Request into the Messages API shape:
// RoleSystem messages lift to the top-level `system` field; assistant tool calls
// become `tool_use` blocks; RoleTool results become `tool_result` blocks in a user
// turn. Consecutive same-role messages are coalesced because the API requires
// alternating user/assistant turns (tool results are user turns).
func (c *client) buildRequest(req provider.Request) anthRequest {
	var system []textBlock
	var msgs []anthMessage

	// appendBlocks adds blocks under role, merging into the previous message when
	// it shares the role (keeps user/assistant strictly alternating).
	appendBlocks := func(role string, blocks ...contentBlock) {
		if len(blocks) == 0 {
			return
		}
		if n := len(msgs); n > 0 && msgs[n-1].Role == role {
			msgs[n-1].Content = append(msgs[n-1].Content, blocks...)
			return
		}
		msgs = append(msgs, anthMessage{Role: role, Content: blocks})
	}

	for _, m := range provider.SanitizeToolPairing(req.Messages) {
		switch m.Role {
		case provider.RoleSystem:
			if m.Content != "" {
				system = append(system, textBlock{Type: "text", Text: m.Content})
			}
		case provider.RoleUser:
			if m.Content != "" {
				appendBlocks("user", contentBlock{Type: "text", Text: m.Content})
			}
			if c.vision {
				for _, url := range m.Images {
					if mt, data, ok := provider.ParseImageDataURL(url); ok {
						appendBlocks("user", contentBlock{Type: "image", Source: &imageSource{Type: "base64", MediaType: mt, Data: data}})
					}
				}
			}
		case provider.RoleTool:
			content := m.Content
			if content == "" {
				content = "(no output)" // tool_result content must be non-empty
			}
			appendBlocks("user", contentBlock{Type: "tool_result", ToolUseID: m.ToolCallID, Content: content})
		case provider.RoleAssistant:
			var blocks []contentBlock
			// Replay the signed thinking block first (Anthropic requires it precede
			// the tool_use it led to). Only when thinking is on and we have both the
			// text and its signature — reasoning without a signature (e.g. from an
			// openai-compatible provider) can't be replayed as a thinking block.
			if c.thinking == "adaptive" && m.ReasoningContent != "" && m.ReasoningSignature != "" {
				blocks = append(blocks, contentBlock{Type: "thinking", Thinking: m.ReasoningContent, Signature: m.ReasoningSignature})
			}
			if m.Content != "" {
				blocks = append(blocks, contentBlock{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				input := json.RawMessage(tc.Arguments)
				if len(input) == 0 {
					input = json.RawMessage("{}") // input is required, even when empty
				}
				blocks = append(blocks, contentBlock{Type: "tool_use", ID: tc.ID, Name: tc.Name, Input: input})
			}
			appendBlocks("assistant", blocks...)
		}
	}

	var tools []anthTool
	for _, t := range req.Tools {
		schema := t.Parameters
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		tools = append(tools, anthTool{Name: t.Name, Description: t.Description, InputSchema: schema})
	}

	// Prompt-cache breakpoints (ephemeral, prefix-match). Render order is
	// tools → system → messages, so a marker on the last system block caches
	// tools+system together; with no system, mark the last tool. A marker on the
	// last block of the last message caches the conversation prefix, accruing hits
	// incrementally as turns are appended. Max 4 breakpoints; we use ≤2.
	if n := len(system); n > 0 {
		system[n-1].CacheControl = ephemeral()
	} else if n := len(tools); n > 0 {
		tools[n-1].CacheControl = ephemeral()
	}
	if n := len(msgs); n > 0 {
		if k := len(msgs[n-1].Content); k > 0 {
			msgs[n-1].Content[k-1].CacheControl = ephemeral()
		}
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	r := anthRequest{
		Model:     c.model,
		MaxTokens: maxTokens,
		System:    system,
		Messages:  msgs,
		Tools:     tools,
		Stream:    true,
	}
	// Extended thinking is opt-in and provider-specific. Anthropic proper uses
	// type=adaptive plus display/output_config. LongCat-style compatible gateways
	// use the simpler enabled|disabled knob and do not accept output_config.
	switch c.thinking {
	case "adaptive":
		r.Thinking = &thinkingConfig{Type: "adaptive", Display: "summarized"}
		if c.effort != "" {
			r.OutputConfig = &outputConfig{Effort: c.effort}
		}
	case "enabled", "disabled":
		t := c.thinking
		if c.effort == "enabled" || c.effort == "disabled" {
			t = c.effort
		}
		r.Thinking = &thinkingConfig{Type: t}
	}
	return r
}

// readStream parses the Messages API SSE stream into Chunks. Text deltas emit live;
// each tool_use content block emits a ChunkToolCallStart when its id+name are known
// and a complete ChunkToolCall when the block closes; usage is assembled from
// message_start (input/cache) + message_delta (output + stop_reason) and emitted
// once before ChunkDone.
func (c *client) readStream(ctx context.Context, resp *http.Response, out chan<- provider.Chunk) {
	defer resp.Body.Close()
	defer close(out)

	// Close the body if the stream stalls past c.idleTimeout so scanner.Scan()
	// unblocks instead of hanging on a half-open connection. The watchdog owns the
	// timer; the read loop only pings the buffered activity channel (no Timer.Reset
	// race). A context cancel already unblocks the scan via the transport.
	idleTimeout := c.idleTimeout
	if idleTimeout <= 0 { // zero-value client (constructed without New)
		idleTimeout = defaultStreamIdleTimeout
	}
	done := make(chan struct{})
	defer close(done)
	activity := make(chan struct{}, 1)
	var stalled atomic.Bool
	go func() {
		idle := time.NewTimer(idleTimeout)
		defer idle.Stop()
		for {
			select {
			case <-ctx.Done():
				resp.Body.Close()
				return
			case <-idle.C:
				stalled.Store(true)
				resp.Body.Close()
				return
			case <-activity:
				if !idle.Stop() {
					select {
					case <-idle.C:
					default:
					}
				}
				idle.Reset(idleTimeout)
			case <-done:
				return
			}
		}
	}()

	send := func(chunk provider.Chunk) bool {
		return sendChunk(ctx, out, chunk)
	}

	tools := map[int]*provider.ToolCall{} // tool_use blocks, keyed by content index
	var inTok, outTok, cacheCreate, cacheRead int
	var stopReason string
	haveUsage := false

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		select { // ping the idle watchdog; non-blocking so a full buffer is fine
		case activity <- struct{}{}:
		default:
		}
		line := strings.TrimSpace(scanner.Text())
		// SSE carries `event:` and `data:` lines; the data JSON's own `type` field
		// is authoritative, so we only need the data payloads.
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}

		var ev streamEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			send(provider.Chunk{Type: provider.ChunkError, Err: fmt.Errorf("%s: decode stream: %w", c.name, err)})
			return
		}

		switch ev.Type {
		case "message_start":
			if ev.Message != nil && ev.Message.Usage != nil {
				inTok = ev.Message.Usage.InputTokens
				cacheCreate = ev.Message.Usage.CacheCreationInputTokens
				cacheRead = ev.Message.Usage.CacheReadInputTokens
				haveUsage = true
			}
		case "content_block_start":
			if ev.ContentBlock != nil && ev.ContentBlock.Type == "tool_use" {
				tc := &provider.ToolCall{ID: ev.ContentBlock.ID, Name: ev.ContentBlock.Name}
				tools[ev.Index] = tc
				if !send(provider.Chunk{Type: provider.ChunkToolCallStart, ToolCall: &provider.ToolCall{ID: tc.ID, Name: tc.Name}}) {
					return
				}
			}
		case "content_block_delta":
			if ev.Delta == nil {
				continue
			}
			switch ev.Delta.Type {
			case "text_delta":
				if ev.Delta.Text != "" {
					if !send(provider.Chunk{Type: provider.ChunkText, Text: ev.Delta.Text}) {
						return
					}
				}
			case "thinking_delta":
				if ev.Delta.Thinking != "" {
					if !send(provider.Chunk{Type: provider.ChunkReasoning, Text: ev.Delta.Thinking}) {
						return
					}
				}
			case "signature_delta":
				if ev.Delta.Signature != "" {
					if !send(provider.Chunk{Type: provider.ChunkReasoning, Signature: ev.Delta.Signature}) {
						return
					}
				}
			case "input_json_delta":
				if tc := tools[ev.Index]; tc != nil {
					tc.Arguments += ev.Delta.PartialJSON
				}
			}
		case "content_block_stop":
			if tc := tools[ev.Index]; tc != nil {
				if !send(provider.Chunk{Type: provider.ChunkToolCall, ToolCall: tc}) {
					return
				}
				delete(tools, ev.Index)
			}
		case "message_delta":
			if ev.Delta != nil && ev.Delta.StopReason != "" {
				stopReason = ev.Delta.StopReason
			}
			if ev.Usage != nil {
				outTok = ev.Usage.OutputTokens
				haveUsage = true
			}
		case "message_stop":
			// Stream complete; fall through to finalize below.
		case "error":
			msg := "stream error"
			if ev.Error != nil && ev.Error.Message != "" {
				msg = ev.Error.Message
			}
			send(provider.Chunk{Type: provider.ChunkError, Err: fmt.Errorf("%s: %s", c.name, msg)})
			return
		}
	}

	if ctx.Err() != nil {
		return
	}
	if stalled.Load() {
		send(provider.Chunk{Type: provider.ChunkError, Err: fmt.Errorf("%s: stream stalled — no data for %s, connection likely dropped", c.name, idleTimeout)})
		return
	}
	if err := scanner.Err(); err != nil {
		send(provider.Chunk{Type: provider.ChunkError, Err: fmt.Errorf("%s: read stream: %w", c.name, err)})
		return
	}

	if haveUsage {
		if !send(provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{
			PromptTokens:     inTok + cacheCreate + cacheRead,
			CompletionTokens: outTok,
			TotalTokens:      inTok + cacheCreate + cacheRead + outTok,
			CacheHitTokens:   cacheRead,
			CacheMissTokens:  inTok + cacheCreate, // uncached input + cache writes (billed ≥1×)
			FinishReason:     mapStopReason(stopReason),
		}}) {
			return
		}
	}
	send(provider.Chunk{Type: provider.ChunkDone})
}

func sendChunk(ctx context.Context, out chan<- provider.Chunk, chunk provider.Chunk) bool {
	select {
	case out <- chunk:
		return true
	default:
	}
	select {
	case <-ctx.Done():
		return false
	case out <- chunk:
		return true
	}
}

// mapStopReason translates Anthropic stop reasons to the OpenAI-style finish
// reasons the agent already recognises (it surfaces abnormal ones like "length").
func mapStopReason(s string) string {
	switch s {
	case "end_turn", "stop_sequence":
		return "stop"
	case "tool_use":
		return "tool_calls"
	case "max_tokens":
		return "length"
	default:
		return s // "refusal", "pause_turn", "" — pass through
	}
}

// --- Messages API wire protocol ---

func ephemeral() *cacheControl { return &cacheControl{Type: "ephemeral"} }

type cacheControl struct {
	Type string `json:"type"`
}

type anthRequest struct {
	Model        string          `json:"model"`
	MaxTokens    int             `json:"max_tokens"`
	System       []textBlock     `json:"system,omitempty"`
	Messages     []anthMessage   `json:"messages"`
	Tools        []anthTool      `json:"tools,omitempty"`
	Thinking     *thinkingConfig `json:"thinking,omitempty"`
	OutputConfig *outputConfig   `json:"output_config,omitempty"`
	Stream       bool            `json:"stream"`
}

type thinkingConfig struct {
	Type    string `json:"type"`              // "adaptive"
	Display string `json:"display,omitempty"` // "summarized" to stream the reasoning text
}

type outputConfig struct {
	Effort string `json:"effort,omitempty"` // low | medium | high | xhigh | max
}

type textBlock struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

type anthMessage struct {
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
}

// contentBlock is the union of the block kinds we emit in a request: text,
// tool_use (echoing a prior assistant call), and tool_result. Unused fields are
// omitted so each block serialises to its canonical shape.
type contentBlock struct {
	Type         string          `json:"type"`
	Text         string          `json:"text,omitempty"`        // text
	Thinking     string          `json:"thinking,omitempty"`    // thinking
	Signature    string          `json:"signature,omitempty"`   // thinking
	ID           string          `json:"id,omitempty"`          // tool_use
	Name         string          `json:"name,omitempty"`        // tool_use
	Input        json.RawMessage `json:"input,omitempty"`       // tool_use
	ToolUseID    string          `json:"tool_use_id,omitempty"` // tool_result
	Content      string          `json:"content,omitempty"`     // tool_result
	Source       *imageSource    `json:"source,omitempty"`      // image
	CacheControl *cacheControl   `json:"cache_control,omitempty"`
}

type imageSource struct {
	Type      string `json:"type"` // "base64"
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type anthTool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"input_schema"`
	CacheControl *cacheControl   `json:"cache_control,omitempty"`
}

// streamEvent is the discriminated SSE event; read the fields matching Type.
type streamEvent struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message *struct {
		Usage *wireUsage `json:"usage"`
	} `json:"message"`
	ContentBlock *struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
	Delta *struct {
		Type        string `json:"type"`         // text_delta | thinking_delta | signature_delta | input_json_delta
		Text        string `json:"text"`         // text_delta
		Thinking    string `json:"thinking"`     // thinking_delta
		Signature   string `json:"signature"`    // signature_delta
		PartialJSON string `json:"partial_json"` // input_json_delta
		StopReason  string `json:"stop_reason"`  // message_delta
	} `json:"delta"`
	Usage *wireUsage `json:"usage"` // message_delta (cumulative output_tokens)
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

type wireUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}
