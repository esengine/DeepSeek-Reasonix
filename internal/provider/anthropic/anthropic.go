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
//     request. DeepSeek's Anthropic endpoint instead uses unsigned thinking blocks,
//     thinking.type enabled|disabled, and output_config.effort; requests carrying
//     tools must replay all provider reasoning. Some other compatible gateways such
//     as LongCat use the binary toggle without output_config. (redacted_thinking
//     blocks are not yet captured/replayed.)
//   - Native Anthropic requests omit temperature/top_p. Current Claude models
//     (Opus 4.8/4.7) reject sampling parameters with a 400; Anthropic steers
//     behavior via prompting instead. DeepSeek's compatible endpoint accepts the
//     caller's temperature, so that field is preserved only for DeepSeek.
package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/netclient"
	"reasonix/internal/provider"
	"reasonix/internal/provider/openai"
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
	// defaultMaxTokens is the mandatory Anthropic fallback when neither config
	// nor request supplies max_tokens. Ordinary turns use 16K; reasoning-capable
	// paths raise via AutoOutputBudget. 128K is never automatic.
	defaultMaxTokens = provider.DefaultOrdinaryOutputTokens
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
	// Anthropic's API surface is at {root}/v1/messages, so c.baseURL stores
	// the *root* -- without any trailing /v1. The setup wizard, however, lets
	// users paste a full OpenAI-compatible URL (e.g.
	// "https://proxy.example.com/v1") because that's what /models probes
	// expect. Stripping the trailing /v1 here makes both forms land on the
	// same endpoint without forcing users to remember Anthropic's quirky
	// root-vs-versioned split. Without this, a user pasting
	// "https://proxy.example.com/v1" would probe /v1/models successfully
	// but get the chat client concatenating onto
	// "https://proxy.example.com/v1/v1/messages" -- a 404.
	root := strings.TrimRight(baseURL, "/")
	root = strings.TrimSuffix(root, "/v1")
	if root == "" {
		root = defaultBaseURL
	}
	officialDeepSeek := openai.IsDeepSeek(root)
	keyEnv, _ := cfg.Extra["api_key_env"].(string) // for actionable auth errors
	keySource, _ := cfg.Extra["api_key_source"].(string)
	thinking, _ := cfg.Extra["thinking"].(string)
	thinking = strings.ToLower(strings.TrimSpace(thinking))
	effort, _ := cfg.Extra["effort"].(string)
	effort = strings.ToLower(strings.TrimSpace(effort))
	vision, _ := cfg.Extra["vision"].(bool)
	// DeepSeek's official Anthropic-compatible endpoint is text-only. Enforce
	// that wire constraint here as defense in depth, independent of config or
	// extension capability metadata.
	vision = vision && !officialDeepSeek
	webSearch, _ := cfg.Extra["web_search"].(bool)
	headers, _ := cfg.Extra["headers"].(map[string]string)
	authHeader, _ := cfg.Extra["auth_header"].(bool)
	maxOutputTokens, _ := cfg.Extra["max_output_tokens"].(int)
	if maxOutputTokens <= 0 {
		// Messages requires max_tokens. 0 = automatic; negative also falls back
		// because the wire field is mandatory.
		reasoningOn := officialDeepSeek &&
			!strings.EqualFold(thinking, "disabled") &&
			!strings.EqualFold(effort, "disabled") &&
			!strings.EqualFold(effort, "off") &&
			!strings.EqualFold(effort, "none")
		if officialDeepSeek {
			maxOutputTokens = provider.AutoOutputBudget(reasoningOn, effort)
		} else {
			// Native Anthropic and unknown gateways: conservative ordinary default.
			maxOutputTokens = defaultMaxTokens
			if strings.EqualFold(thinking, "adaptive") || strings.EqualFold(thinking, "enabled") {
				maxOutputTokens = provider.AutoOutputBudget(true, effort)
			}
		}
	}
	httpClient, err := newHTTPClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("anthropic: network: %w", err)
	}
	return &client{
		name:             name,
		apiKey:           cfg.APIKey,
		keyEnv:           keyEnv,
		keySource:        keySource,
		baseURL:          root,
		model:            cfg.Model,
		nativeAnthropic:  strings.EqualFold(root, defaultBaseURL),
		deepseek:         officialDeepSeek,
		thinking:         thinking,
		effort:           effort,
		vision:           vision,
		mimo:             provider.IsMiMoEndpoint(root),
		webSearch:        webSearch,
		headers:          cleanCustomHeaders(headers),
		authHeader:       authHeader,
		defaultMaxTokens: maxOutputTokens,
		http:             httpClient, // no overall timeout; lifecycle is ctx-driven
		idleTimeout:      defaultStreamIdleTimeout,
	}, nil
}

func newHTTPClient(cfg provider.Config) (*http.Client, error) {
	spec, _ := cfg.Extra["proxy_spec"].(netclient.ProxySpec)
	return netclient.NewHTTPClient(spec, netclient.TransportOptions{})
}

type client struct {
	name             string
	apiKey           string
	keyEnv           string // api_key_env name, surfaced in auth errors
	keySource        string // source of keyEnv, surfaced in auth errors
	baseURL          string
	model            string
	nativeAnthropic  bool   // first-party endpoint: documented default-5m cache-write pricing applies
	deepseek         bool   // official DeepSeek Anthropic endpoint: unsigned reasoning replay + automatic cache
	thinking         string // "adaptive" enables extended thinking; "" = off (config-driven)
	effort           string // output_config.effort: low|medium|high|xhigh|max; "" = provider default
	vision           bool   // model accepts image input — embed attached images as base64 image blocks
	mimo             bool   // true for MiMo — upgrades legacy tuple schemas to Draft 2020-12
	webSearch        bool   // enable server-side web_search tool (DeepSeek Anthropic API)
	headers          map[string]string
	authHeader       bool // send Authorization: Bearer instead of Anthropic's x-api-key header
	defaultMaxTokens int
	http             *http.Client
	idleTimeout      time.Duration // SSE stall watchdog window; defaultStreamIdleTimeout unless a test overrides
	authed           atomic.Bool   // a request has succeeded — gate transient-401 retry
}

func (c *client) Name() string { return c.name }

func (c *client) deepSeekThinkingEnabled() bool {
	return c != nil && c.deepseek && c.thinking != "disabled" && c.effort != "disabled"
}

// deepSeekAnthropicUsesProEffortMapping mirrors DeepSeek's model routing for the
// Anthropic endpoint. Opus aliases route to V4 Pro; Sonnet/Haiku aliases and
// unsupported model names route to V4 Flash.
func deepSeekAnthropicUsesProEffortMapping(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return model == "deepseek-v4-pro" || strings.HasPrefix(model, "claude-opus")
}

func normalizeDeepSeekAnthropicEffort(model, effort string) string {
	switch effort {
	case "low":
		if deepSeekAnthropicUsesProEffortMapping(model) {
			return "high"
		}
		return "low"
	case "medium":
		return "high"
	case "xhigh":
		if deepSeekAnthropicUsesProEffortMapping(model) {
			return "max"
		}
		return "high"
	case "high", "max":
		return effort
	default:
		return ""
	}
}

func (c *client) RequiresToolCallReasoning() bool {
	return c.deepSeekThinkingEnabled()
}

func (c *client) MissingToolCallReasoningWarningIdentity() string {
	if c == nil {
		return ""
	}
	protocol := "anthropic"
	if c.deepseek {
		protocol = "deepseek-anthropic"
	}
	return strings.Join([]string{
		"anthropic", strings.TrimSpace(c.name), strings.TrimSpace(c.baseURL),
		strings.TrimSpace(c.model), protocol, strings.TrimSpace(c.thinking), strings.TrimSpace(c.effort),
	}, "\x00")
}

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
	requestCtx := provider.WithRequestAttemptCounter(ctx)
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	if err := json.NewEncoder(buf).Encode(c.buildRequest(requestCtx, req)); err != nil {
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
	resp, err := provider.SendWithRetry(requestCtx, c.http, c.sendOpts(), newReq)
	if err != nil {
		return nil, provider.AnnotateToolSchemaError(err, req.Tools)
	}
	c.authed.Store(true)

	out := make(chan provider.Chunk)
	go c.readStream(requestCtx, resp, out)
	return out, nil
}

// buildRequest converts the transport-agnostic Request into the Messages API shape:
// RoleSystem messages lift to the top-level `system` field; assistant tool calls
// become `tool_use` blocks; RoleTool results become `tool_result` blocks in a user
// turn. Consecutive same-role messages are coalesced because the API requires
// alternating user/assistant turns (tool results are user turns).
func (c *client) buildRequest(_ context.Context, req provider.Request) anthRequest {
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
			block := contentBlock{Type: "tool_result", ToolUseID: m.ToolCallID, Content: content}
			if c.vision {
				if blocks := toolResultBlocks(content, m.Images); blocks != nil {
					block.Content = blocks
				}
			}
			appendBlocks("user", block)
		case provider.RoleAssistant:
			var blocks []contentBlock
			// Replay provider reasoning before the tool_use it led to. DeepSeek uses
			// unsigned thinking blocks and requires the reasoning from a tool-call
			// turn in every subsequent request, even if the current request no longer
			// declares tools or has since disabled thinking. Anthropic proper requires
			// a signature, so reasoning without one cannot be replayed on that endpoint.
			if c.deepseek && len(m.ToolCalls) > 0 && m.ReasoningContent != "" {
				blocks = append(blocks, contentBlock{Type: "thinking", Thinking: m.ReasoningContent})
			} else if c.thinking == "adaptive" && m.ReasoningContent != "" && m.ReasoningSignature != "" {
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
	if c.webSearch {
		tools = append(tools, anthTool{Type: "web_search_20250305", Name: "web_search"})
	}
	for _, t := range req.Tools {
		schema := t.Parameters
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		if c.mimo {
			schema = provider.NormalizeLegacyTupleItemsForDraft202012(schema)
		}
		tools = append(tools, anthTool{Name: t.Name, Description: t.Description, InputSchema: schema})
	}

	// Prompt-cache breakpoints (ephemeral, prefix-match). DeepSeek ignores
	// cache_control and manages prefix caching automatically, so keep those fields
	// off its wire entirely. Render order for native Anthropic is
	// tools → system → messages, so a marker on the last system block caches
	// tools+system together; with no system, mark the last tool. A marker on the
	// last block of the last message caches the conversation prefix, accruing hits
	// incrementally as turns are appended. Max 4 breakpoints; we use ≤2. Keep
	// Anthropic's default 5m TTL by omitting the ttl field. Besides being cheaper
	// than the opt-in 1h write, this keeps provider-visible request bytes stable
	// across turns, retries, and wall-clock timing.
	if !c.deepseek {
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
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = c.defaultMaxTokens
		if maxTokens <= 0 {
			maxTokens = defaultMaxTokens
		}
	}
	r := anthRequest{
		Model:     c.model,
		MaxTokens: maxTokens,
		System:    system,
		Messages:  msgs,
		Tools:     tools,
		Stream:    true,
	}
	// Extended thinking is provider-specific. DeepSeek defaults to enabled and
	// accepts output_config.effort alongside its binary toggle. Anthropic proper
	// uses type=adaptive plus display/output_config. LongCat-style compatible
	// gateways use the simpler enabled|disabled knob and reject output_config.
	if c.deepseek {
		r.Temperature = req.Temperature
		t := c.thinking
		if t != "disabled" {
			t = "enabled"
		}
		if c.effort == "disabled" {
			t = "disabled"
		}
		r.Thinking = &thinkingConfig{Type: t}
		if t != "disabled" {
			effort := normalizeDeepSeekAnthropicEffort(c.model, c.effort)
			switch effort {
			case "low", "high", "max":
				r.OutputConfig = &outputConfig{Effort: effort}
			}
		}
	} else {
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
	}
	return r
}

// readStream parses the Messages API SSE stream into Chunks. Text deltas emit live;
// each tool_use content block emits a ChunkToolCallStart when its id+name are known
// and a complete ChunkToolCall when the block closes; usage is assembled from
// message_start/message_delta usage (compatible gateways may put every counter
// in the final delta) and emitted once before ChunkDone.
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
	argBuckets := map[int]int{}           // last emitted 2KB progress bucket per block
	var inTok, outTok, cacheCreate, cacheRead int
	var stopReason string
	haveUsage := false
	mergeUsage := func(usage *wireUsage) {
		if usage == nil {
			return
		}
		// The native Anthropic stream reports input/cache counters in
		// message_start and output_tokens in message_delta. Compatible gateways
		// such as LongCat report all counters in message_delta instead. Counters
		// are cumulative and non-negative, so retaining the largest value also
		// tolerates gateways that repeat partial usage in both events.
		inTok = max(inTok, usage.InputTokens)
		outTok = max(outTok, usage.OutputTokens)
		cacheCreate = max(cacheCreate, usage.CacheCreationInputTokens)
		cacheRead = max(cacheRead, usage.CacheReadInputTokens)
		haveUsage = true
	}

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
			send(provider.Chunk{Type: provider.ChunkError, Err: provider.StreamDecodeError(c.name, data, err)})
			return
		}

		switch ev.Type {
		case "message_start":
			if ev.Message != nil && ev.Message.Usage != nil {
				mergeUsage(ev.Message.Usage)
			}
		case "content_block_start":
			if ev.ContentBlock != nil {
				switch ev.ContentBlock.Type {
				case "tool_use":
					tc := &provider.ToolCall{ID: ev.ContentBlock.ID, Name: ev.ContentBlock.Name}
					tools[ev.Index] = tc
					if !send(provider.Chunk{Type: provider.ChunkToolCallStart, ToolCall: &provider.ToolCall{ID: tc.ID, Name: tc.Name}}) {
						return
					}
				case "web_search_tool_result":
					// Search results are delivered inline in content_block.content as a
					// JSON array of result objects (title, url, encrypted_content).
					// Only the model sees the plain text; we surface titles and URLs.
					// server_tool_use blocks (the model initiating the search) are
					// intentionally skipped — the API executes them server-side and
					// the results appear here.
					formatted := formatWebSearchResults(ev.ContentBlock.Content)
					if formatted != "" {
						if !send(provider.Chunk{Type: provider.ChunkText, Text: formatted}) {
							return
						}
					}
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
					// Progress ticks for large streaming argument payloads, one
					// per 2KB bucket (see the openai provider for rationale).
					if bucket := len(tc.Arguments) / 2048; bucket > argBuckets[ev.Index] {
						argBuckets[ev.Index] = bucket
						if !send(provider.Chunk{Type: provider.ChunkToolCallArgsDelta, ToolCall: &provider.ToolCall{ID: tc.ID, Name: tc.Name}, ArgChars: len(tc.Arguments)}) {
							return
						}
					}
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
			mergeUsage(ev.Usage)
		case "message_stop":
			// Anthropic's terminal event. Tool blocks may already have closed;
			// without this, the attempt stays speculative and is not committed.
			// Stop reading immediately so a post-terminal connection reset cannot
			// reclassify a complete response as interrupted.
			goto finalize
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
		err := fmt.Errorf("%s: stream stalled — no data for %s, connection likely dropped", c.name, idleTimeout)
		send(provider.Chunk{Type: provider.ChunkError, Err: provider.StreamInterrupt(err, provider.StreamInterruptIdleTimeout)})
		return
	}
	if err := scanner.Err(); err != nil {
		wrapped := fmt.Errorf("%s: read stream: %w", c.name, err)
		if provider.IsConnReset(err) {
			send(provider.Chunk{Type: provider.ChunkError, Err: provider.StreamInterrupt(wrapped, provider.ClassifyStreamInterrupt(err))})
			return
		}
		send(provider.Chunk{Type: provider.ChunkError, Err: wrapped})
		return
	}
	// EOF / clean close before message_stop is an uncommitted attempt. Complete
	// ChunkToolCall blocks that arrived earlier remain speculative.
	send(provider.Chunk{Type: provider.ChunkError, Err: provider.StreamInterrupt(
		fmt.Errorf("%s: stream ended before message_stop: %w", c.name, io.ErrUnexpectedEOF),
		provider.StreamInterruptPrematureEOF,
	)})
	return

finalize:
	if haveUsage {
		cacheWriteBilledTokens := 0.0
		if cacheCreate > 0 && c.nativeAnthropic {
			cacheWriteBilledTokens = float64(cacheCreate) * cacheWrite5MinuteInputMultiplier
		}
		usage := &provider.Usage{
			PromptTokens:           inTok + cacheCreate + cacheRead,
			CompletionTokens:       outTok,
			TotalTokens:            inTok + cacheCreate + cacheRead + outTok,
			CacheHitTokens:         cacheRead,
			CacheMissTokens:        inTok + cacheCreate,
			CacheWriteTokens:       cacheCreate,
			CacheWriteBilledTokens: cacheWriteBilledTokens,
			FinishReason:           mapStopReason(stopReason),
		}
		provider.ApplyRequestAttemptCount(ctx, usage)
		if !send(provider.Chunk{Type: provider.ChunkUsage, Usage: usage}) {
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

// webSearchResult is a single result from a web_search_tool_result block.
type webSearchResult struct {
	URL      string `json:"url"`
	Title    string `json:"title"`
	Text     string `json:"text"`
	SiteName string `json:"site_name"`
}

// formatWebSearchResults parses a web_search_tool_result content array
// and formats titles and URLs as human-readable text. DeepSeek returns
// encrypted_content rather than plain text at the transport layer; the
// model still sees the original content.
func formatWebSearchResults(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var results []webSearchResult
	if err := json.Unmarshal(raw, &results); err != nil {
		return ""
	}
	var b strings.Builder
	for _, r := range results {
		if r.Title == "" && r.URL == "" {
			continue
		}
		fmt.Fprintf(&b, "\n- **%s**", r.Title)
		if r.URL != "" {
			fmt.Fprintf(&b, "\n  <%s>", r.URL)
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "\n" + b.String() + "\n"
}

// Messages API wire protocol

const cacheWrite5MinuteInputMultiplier = 1.25

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
	Temperature  *float64        `json:"temperature,omitempty"`
	Thinking     *thinkingConfig `json:"thinking,omitempty"`
	OutputConfig *outputConfig   `json:"output_config,omitempty"`
	Stream       bool            `json:"stream"`
}

type thinkingConfig struct {
	Type    string `json:"type"`              // "adaptive"
	Display string `json:"display,omitempty"` // "summarized" to stream the reasoning text
}

type outputConfig struct {
	Effort string `json:"effort,omitempty"` // low | high | max
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
	Content      any             `json:"content,omitempty"`     // tool_result: string, or []contentBlock when the result carries images
	Source       *imageSource    `json:"source,omitempty"`      // image
	CacheControl *cacheControl   `json:"cache_control,omitempty"`
}

type imageSource struct {
	Type      string `json:"type"` // "base64"
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

// toolResultBlocks builds array content for a tool_result whose message carries
// images: the text first, then one image block per parseable data URL. It
// returns nil when nothing parses, so text-only results keep plain string
// content — byte-identical serialization to previous releases.
func toolResultBlocks(text string, images []string) []contentBlock {
	var imgs []contentBlock
	for _, url := range images {
		if mt, data, ok := provider.ParseImageDataURL(url); ok {
			imgs = append(imgs, contentBlock{Type: "image", Source: &imageSource{Type: "base64", MediaType: mt, Data: data}})
		}
	}
	if imgs == nil {
		return nil
	}
	return append([]contentBlock{{Type: "text", Text: text}}, imgs...)
}

type anthTool struct {
	Type         string          `json:"type,omitempty"` // "web_search" for server-side search; empty for named tools
	Name         string          `json:"name,omitempty"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"input_schema,omitempty"`
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
		Type      string          `json:"type"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		ToolUseID string          `json:"tool_use_id"` // web_search_tool_result
		Content   json.RawMessage `json:"content"`     // web_search_tool_result: array of result objects
	} `json:"content_block"`
	Delta *struct {
		Type             string          `json:"type"`         // text_delta | thinking_delta | signature_delta | input_json_delta | web_search_tool_result_delta
		Text             string          `json:"text"`         // text_delta
		Thinking         string          `json:"thinking"`     // thinking_delta
		Signature        string          `json:"signature"`    // signature_delta
		PartialJSON      string          `json:"partial_json"` // input_json_delta
		StopReason       string          `json:"stop_reason"`  // message_delta
		WebSearchResults json.RawMessage `json:"results"`      // web_search_tool_result_delta
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
