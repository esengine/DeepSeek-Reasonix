// Package openai implements the OpenAI-compatible /chat/completions provider.
// It self-registers under the "openai" kind, so DeepSeek, MiMo, MiniMax-M3, and
// any other OpenAI-compatible endpoint are just config instances rather than
// code. Each instance picks the wire shape from its base URL:
//   - api.deepseek.com → emits thinking.type=enabled (DeepSeek-flavor CoT) plus
//     reasoning_effort as a depth hint.
//   - api.minimaxi.com → emits thinking.type=adaptive|disabled (M3's binary
//     knob) instead of reasoning_effort, since M3 has no level scale.
//   - open.bigmodel.cn / api.z.ai (Zhipu GLM) → emits thinking.type=enabled|
//     disabled instead of reasoning_effort, which Zhipu silently ignores.
//   - api.longcat.chat → emits thinking.type=enabled|disabled and omits
//     reasoning_effort, matching LongCat's OpenAI-compatible API.
//   - ollama.com → accepts hosted Ollama Cloud's reasoning_effort scale,
//     including max, and omits the field for none/disabled.
//   - everything else (MiMo and other OpenAI-compatible gateways) uses the
//     vanilla reasoning_effort scale (low/medium/high).
//
// See docs/REASONING_PROVIDERS.md for the per-backend protocol reference.
package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/netclient"
	"reasonix/internal/provider"
)

// defaultStreamIdleTimeout caps how long a started SSE stream may go without any
// bytes before it's treated as a dropped connection. A half-open TCP connection
// (e.g. a proxy switched mid-stream) sends no RST, so scanner.Scan() would block
// forever; this turns that hang into a recoverable error. Generous on purpose —
// live streams emit tokens/keepalives far more often. Stored per-client
// (client.idleTimeout) so a test can shorten it without a shared global that
// would race other streams' watchdogs.
const defaultStreamIdleTimeout = 120 * time.Second

func init() {
	provider.Register("openai", New)
}

// New builds an OpenAI-compatible provider from a resolved config.
func New(cfg provider.Config) (provider.Provider, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("openai: base_url is required for provider %q", cfg.Name)
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("openai: model is required for provider %q", cfg.Name)
	}
	name := cfg.Name
	if name == "" {
		name = "openai"
	}
	keyEnv, _ := cfg.Extra["api_key_env"].(string) // for actionable auth errors
	keySource, _ := cfg.Extra["api_key_source"].(string)
	effort, _ := cfg.Extra["effort"].(string)
	effort = strings.ToLower(strings.TrimSpace(effort))
	if effort == "auto" {
		effort = ""
	}
	protocol, _ := cfg.Extra["reasoning_protocol"].(string)
	protocol = normalizeReasoningProtocol(protocol)
	chatURL, _ := cfg.Extra["chat_url"].(string)
	chatURL = normalizeChatURL(cfg.BaseURL, chatURL)
	headers, _ := cfg.Extra["headers"].(map[string]string)
	extraBody, _ := cfg.Extra["extra_body"].(map[string]any)
	vision, _ := cfg.Extra["vision"].(bool)
	visionDetail, _ := cfg.Extra["vision_detail"].(string)
	visionDetail = strings.ToLower(strings.TrimSpace(visionDetail))
	if visionDetail != "low" && visionDetail != "high" {
		visionDetail = "" // auto — omit the field
	}
	deepseek := protocol == "deepseek" || (protocol == "" && IsDeepSeek(cfg.BaseURL))
	minimax := protocol == "" && IsMiniMax(cfg.BaseURL)
	zhipu := protocol == "" && IsZhipu(cfg.BaseURL)
	longcat := protocol == "" && IsLongCat(cfg.BaseURL)
	ollamaCloud := protocol == "" && IsOllamaCloud(cfg.BaseURL)
	// Optional explicit `thinking` config field — a vendor-agnostic escape hatch
	// (credit @eghrhegpe, #5063) for OpenAI-compatible providers we don't
	// auto-detect (e.g. opencode.ai). "enabled"/"disabled" drive thinking.type;
	// anything else is ignored so an unknown value never breaks a request.
	thinkingType, _ := cfg.Extra["thinking"].(string)
	thinkingType = strings.ToLower(strings.TrimSpace(thinkingType))
	if thinkingType != "enabled" && thinkingType != "disabled" {
		thinkingType = ""
	}
	switch {
	case protocol == "none":
		effort = ""
	case deepseek:
		if thinkingType == "disabled" {
			effort = ""
			break
		}
		switch effort {
		case "", "off": // "off" is a retired level (disabled thinking); fall back to the default depth
			effort = "high"
		case "disabled":
			// DeepSeek can turn thinking off too; route through thinking.type and
			// drop the depth hint so the wire carries thinking.type=disabled only.
			effort = ""
			thinkingType = "disabled"
		case "high", "max":
		default:
			return nil, fmt.Errorf("openai: provider %q uses DeepSeek thinking; effort must be high, max, or disabled", name)
		}
	case minimax:
		// M3's knob is binary. The config effort layer normalises user input
		// to "adaptive", "disabled", or "" (== auto). We keep "high"/"max"
		// (legacy DeepSeek) and "low"/"medium" (Anthropic) out — config-level
		// NormalizeEffort remaps them to "adaptive" already, so anything
		// reaching here is expected to be one of: "", "adaptive", "disabled".
		effort = strings.ToLower(strings.TrimSpace(effort))
		switch effort {
		case "": // auto — leave empty so the wire emits thinking.type=adaptive
		case "adaptive", "disabled":
		default:
			return nil, fmt.Errorf("openai: provider %q uses MiniMax thinking; effort must be adaptive or disabled", name)
		}
	case zhipu:
		// Zhipu GLM gates chain-of-thought through `thinking.type`
		// (enabled|disabled) and silently ignores reasoning_effort, so /effort
		// mirrors that binary knob. The config effort layer normalises depth
		// levels onto one of these; "" means auto == the GLM default (thinking on).
		switch effort {
		case "", "enabled", "disabled":
		default:
			return nil, fmt.Errorf("openai: provider %q uses Zhipu thinking; effort must be enabled or disabled", name)
		}
	case longcat:
		// LongCat exposes a binary thinking knob on its OpenAI-compatible endpoint:
		// thinking.type=enabled|disabled. It documents reasoning text via
		// reasoning_content, but not the generic reasoning_effort scale.
		switch effort {
		case "", "enabled", "disabled":
		default:
			return nil, fmt.Errorf("openai: provider %q uses LongCat thinking; effort must be enabled or disabled", name)
		}
	case ollamaCloud:
		// Hosted Ollama Cloud uses top-level reasoning_effort. "none" and the
		// legacy/off aliases intentionally omit the field, which lets the model
		// run without thinking. Local Ollama is not auto-detected because its
		// model/version support varies.
		switch effort {
		case "", "none", "disabled", "off":
			effort = ""
		case "xhigh", "max":
			effort = "max"
		case "low", "medium", "high":
		default:
			return nil, fmt.Errorf("openai: provider %q uses Ollama Cloud thinking; effort must be none, low, medium, high, or max", name)
		}
	case effort != "":
		// Non-DeepSeek backends use OpenAI's reasoning_effort scale (low/medium/
		// high); "max" is a DeepSeek-ism MiMo et al. reject with 400, so clamp it
		// to the OpenAI ceiling and reject other values at boot, not at request time.
		switch effort {
		case "max":
			effort = "high"
		case "low", "medium", "high":
		default:
			return nil, fmt.Errorf("openai: provider %q: effort must be low, medium, or high", name)
		}
	}
	httpClient, err := newHTTPClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("openai: network: %w", err)
	}
	return &client{
		name:         name,
		apiKey:       cfg.APIKey,
		keyEnv:       keyEnv,
		keySource:    keySource,
		baseURL:      strings.TrimRight(cfg.BaseURL, "/"),
		chatURL:      chatURL,
		headers:      cleanCustomHeaders(headers),
		extraBody:    cleanExtraBody(extraBody),
		model:        cfg.Model,
		deepseek:     deepseek,
		minimax:      minimax,
		zhipu:        zhipu,
		longcat:      longcat,
		thinkingType: thinkingType,
		vision:       vision,
		visionDetail: visionDetail,
		effort:       effort,
		http:         httpClient,
		idleTimeout:  defaultStreamIdleTimeout,
	}, nil
}

func newHTTPClient(cfg provider.Config) (*http.Client, error) {
	spec, _ := cfg.Extra["proxy_spec"].(netclient.ProxySpec)
	return netclient.NewHTTPClient(spec, netclient.TransportOptions{
		DialTimeout:           30 * time.Second,
		KeepAlive:             30 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 120 * time.Second, // models can think for a while before the first token
	})
}

type client struct {
	name         string
	apiKey       string
	keyEnv       string // api_key_env name, surfaced in auth errors
	keySource    string // source of keyEnv, surfaced in auth errors
	baseURL      string
	chatURL      string
	headers      map[string]string
	extraBody    map[string]any
	model        string
	http         *http.Client
	deepseek     bool
	minimax      bool          // true for api.minimaxi.com — emits MiniMax-M3's thinking knob instead of reasoning_effort
	zhipu        bool          // true for Zhipu GLM (bigmodel.cn / z.ai) — gates thinking via thinking.type, ignores reasoning_effort
	longcat      bool          // true for LongCat — gates thinking via thinking.type, ignores reasoning_effort
	thinkingType string        // explicit `thinking` config override (enabled|disabled); "" = no override
	vision       bool          // model accepts image input — embed attached images as image_url parts
	visionDetail string        // image_url detail hint (low|high); "" = auto/omit
	effort       string        // reasoning_effort for OpenAI; thinking.type for MiniMax; "" = auto/provider default
	idleTimeout  time.Duration // SSE stall watchdog window; defaultStreamIdleTimeout unless a test overrides
	authed       atomic.Bool   // a request has succeeded — gate transient-401 retry
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

func normalizeReasoningProtocol(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "deepseek", "openai", "none":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return ""
	}
}

func normalizeChatURL(baseURL, chatURL string) string {
	if trimmed := strings.TrimRight(strings.TrimSpace(chatURL), "/"); trimmed != "" {
		return trimmed
	}
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/chat/completions"
}

func cleanCustomHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for rawName, rawValue := range in {
		name := strings.TrimSpace(rawName)
		value := strings.TrimSpace(rawValue)
		if name == "" || value == "" || reservedCustomHeader(name) {
			continue
		}
		out[name] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func applyCustomHeaders(h http.Header, headers map[string]string) {
	for name, value := range cleanCustomHeaders(headers) {
		h.Set(name, value)
	}
}

func applyAPIKeyHeader(h http.Header, baseURL, apiKey string) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return
	}
	if IsMiMo(baseURL) {
		h.Set("api-key", apiKey)
		return
	}
	h.Set("Authorization", "Bearer "+apiKey)
}

func cleanExtraBody(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for rawName, value := range in {
		name := strings.TrimSpace(rawName)
		if name == "" || reservedExtraBodyField(name) {
			continue
		}
		out[name] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func reservedExtraBodyField(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "model", "messages", "tools", "stream", "stream_options", "temperature", "max_tokens", "reasoning_effort", "thinking":
		return true
	default:
		return false
	}
}

func reservedCustomHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "authorization", "content-type", "accept", "host":
		return true
	default:
		return false
	}
}

// bufPool reuses byte buffers for JSON-marshalled request bodies. Each turn
// allocates a buffer, marshals the request, and sends it — pooling avoids the
// GC churn from repeated alloc/free of ~10-100KB buffers. The pool is
// provider-level (not global) so OpenAI and Anthropic don't compete.
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
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.chatURL, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		applyAPIKeyHeader(httpReq.Header, c.baseURL, c.apiKey)
		httpReq.Header.Set("Accept", "text/event-stream")
		applyCustomHeaders(httpReq.Header, c.headers)
		return httpReq, nil
	}
	resp, err := provider.SendWithRetry(ctx, c.http, c.sendOpts(), newReq)
	if err != nil {
		return nil, err
	}
	c.authed.Store(true)

	out := make(chan provider.Chunk)
	go c.streamWithReconnect(ctx, resp, newReq, out)
	return out, nil
}

// maxStreamReconnects bounds how many times a mid-stream connection drop is
// replayed from scratch before the error is surfaced — each replay re-runs the
// whole request (cheap under prompt caching, but not free).
const maxStreamReconnects = 3

// streamWithReconnect drives readStream and, when the connection is cut before
// any model output has been forwarded, replays the request rather than failing
// the turn. Once a token (reasoning/text/tool-call) has been emitted, a replay
// would duplicate output, so the error is surfaced instead.
func (c *client) streamWithReconnect(ctx context.Context, resp *http.Response, newReq func(context.Context) (*http.Request, error), out chan<- provider.Chunk) {
	defer close(out)
	for attempt := 0; ; attempt++ {
		emitted, err := c.readStream(ctx, resp, out)
		if err == nil {
			return
		}
		if !provider.IsConnReset(err) {
			sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkError, Err: err})
			return
		}
		if emitted {
			sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkError, Err: &provider.StreamInterruptedError{Err: err}})
			return
		}
		if attempt >= maxStreamReconnects {
			sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkError, Err: err})
			return
		}
		next, rerr := provider.SendWithRetry(ctx, c.http, c.sendOpts(), newReq)
		if rerr != nil {
			sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkError, Err: rerr})
			return
		}
		resp = next
	}
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

func (c *client) buildRequest(req provider.Request) chatRequest {
	// Repair tool-call pairing before sending: an interrupted/resumed history can
	// carry an assistant tool_calls turn whose results never landed, which DeepSeek
	// rejects with a 400 ("must be followed by tool messages …").
	src := provider.SanitizeToolPairing(req.Messages)
	msgs := make([]chatMessage, len(src))
	for i, m := range src {
		cm := chatMessage{
			Role:       string(m.Role),
			ToolCallID: m.ToolCallID,
			Name:       m.Name,
		}
		// DeepSeek thinking mode 400s a tool_calls turn whose reasoning_content was
		// dropped on a cache-miss replay ("reasoning_content … must be passed back"),
		// so round it back — but only on the turn that carries the tool calls.
		if c.deepseek && m.Role == provider.RoleAssistant && len(m.ToolCalls) > 0 {
			cm.ReasoningContent = m.ReasoningContent
		}
		for _, tc := range m.ToolCalls {
			wire := chatToolCall{ID: tc.ID, Type: "function"}
			wire.Function.Name = tc.Name
			wire.Function.Arguments = tc.Arguments
			cm.ToolCalls = append(cm.ToolCalls, wire)
		}
		switch {
		case c.vision && m.Role == provider.RoleUser && len(m.Images) > 0:
			cm.Content = imageContentParts(m.Content, m.Images, c.visionDetail)
		case m.Role != provider.RoleAssistant || len(cm.ToolCalls) == 0 || m.Content != "":
			cm.Content = m.Content
		}
		msgs[i] = cm
	}

	var tools []chatTool
	for _, t := range req.Tools {
		parameters := t.Parameters
		if len(parameters) == 0 {
			parameters = provider.CanonicalizeSchema(nil)
		}
		tools = append(tools, chatTool{
			Type:     "function",
			Function: chatFunction{Name: t.Name, Description: t.Description, Parameters: parameters},
		})
	}

	out := chatRequest{
		Model:           c.model,
		Messages:        msgs,
		Tools:           tools,
		Stream:          true,
		StreamOptions:   &streamOptions{IncludeUsage: true},
		Temperature:     req.Temperature,
		MaxTokens:       req.MaxTokens,
		ReasoningEffort: c.effort,
		ExtraBody:       c.extraBody,
	}
	switch {
	case c.deepseek:
		// DeepSeek's CoT is controlled by `thinking` plus `reasoning_effort` for
		// depth. Thinking is on by default but can be turned off via
		// effort=disabled / thinking=disabled (credit @eghrhegpe, #5063).
		if c.thinkingType == "disabled" {
			out.Thinking = &thinkingMode{Type: "disabled"}
		} else {
			out.Thinking = &thinkingMode{Type: "enabled"}
		}
	case c.minimax:
		// M3 uses a single `thinking.type` field with two valid values:
		// "adaptive" (default, thinking on) and "disabled" (off). Reasoning
		// depth is not a knob on M3, so reasoning_effort is omitted entirely.
		t := c.effort
		if t == "" {
			t = "adaptive" // /effort auto == the M3 model default
		}
		out.Thinking = &thinkingMode{Type: t}
		out.ReasoningEffort = ""
	case c.zhipu:
		// Zhipu GLM's binary thinking knob: "enabled" (default, thinking on) or
		// "disabled". reasoning_effort is silently ignored by the endpoint, so we
		// omit it and drive chain-of-thought purely through thinking.type.
		t := c.effort
		if t == "" {
			t = "enabled" // auto == the GLM default (thinking on)
		}
		if c.thinkingType != "" {
			t = c.thinkingType // explicit `thinking` config overrides the effort knob
		}
		out.Thinking = &thinkingMode{Type: t}
		out.ReasoningEffort = ""
	case c.longcat:
		// LongCat's binary thinking knob: "enabled" (default, thinking on) or
		// "disabled". The API documents reasoning_content in OpenAI responses but
		// not reasoning_effort, so keep depth out of the request.
		t := c.effort
		if t == "" {
			t = c.thinkingType
		}
		if t == "" {
			t = "enabled"
		}
		out.Thinking = &thinkingMode{Type: t}
		out.ReasoningEffort = ""
	case c.thinkingType != "":
		// Generic OpenAI-compatible provider with an explicit `thinking` config
		// field (e.g. opencode.ai) — emit thinking.type; reasoning_effort, if any,
		// is left untouched for backends that also honour it.
		out.Thinking = &thinkingMode{Type: c.thinkingType}
	}
	return out
}

// readStream parses one SSE response into chunks: text deltas stream live,
// tool-call fragments accumulate by index and emit complete on [DONE], and a
// ChunkToolCallStart fires the moment a call's name is known. It returns whether
// any model output was forwarded (so the caller can decide a replay is safe) and
// the first fatal error — a nil error means the stream reached [DONE].
func (c *client) readStream(ctx context.Context, resp *http.Response, out chan<- provider.Chunk) (emitted bool, _ error) {
	defer resp.Body.Close()

	// Close the response body when the context is canceled (user interrupt) or the
	// stream stalls past c.idleTimeout, so scanner.Scan() unblocks instead of
	// hanging on a half-open connection. done lets the watchdog exit on a normal
	// return — otherwise it outlives the call and blocks forever on a non-cancellable
	// context whose Done() is nil. The watchdog owns the timer; the read loop only
	// pings the buffered activity channel, so there's no Timer.Reset race.
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

	acc := map[int]*provider.ToolCall{}
	started := map[int]bool{}
	var order []int
	var lastFinishReason string
	var sawDone bool
	var think thinkSplitter

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		select { // ping the idle watchdog; non-blocking so a full buffer is fine
		case activity <- struct{}{}:
		default:
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			sawDone = true
			break
		}

		var sr streamResponse
		if err := json.Unmarshal([]byte(data), &sr); err != nil {
			return emitted, fmt.Errorf("%s: decode stream: %w", c.name, err)
		}
		if sr.Error != nil {
			return emitted, fmt.Errorf("%s: %s", c.name, sr.Error.Message)
		}
		if len(sr.Choices) > 0 && sr.Choices[0].FinishReason != nil && *sr.Choices[0].FinishReason != "" {
			lastFinishReason = *sr.Choices[0].FinishReason
		}
		if sr.Usage != nil {
			u := normaliseUsage(sr.Usage)
			u.FinishReason = lastFinishReason
			emitted = true
			if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkUsage, Usage: u}) {
				return emitted, ctx.Err()
			}
		}
		if len(sr.Choices) == 0 {
			continue
		}

		delta := sr.Choices[0].Delta
		if delta.ReasoningContent != "" {
			emitted = true
			if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkReasoning, Text: delta.ReasoningContent}) {
				return emitted, ctx.Err()
			}
		}
		if delta.Content != "" {
			r, txt := think.push(delta.Content)
			if r != "" {
				emitted = true
				if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkReasoning, Text: r}) {
					return emitted, ctx.Err()
				}
			}
			if txt != "" {
				emitted = true
				if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkText, Text: txt}) {
					return emitted, ctx.Err()
				}
			}
		}
		for _, tc := range delta.ToolCalls {
			cur, ok := acc[tc.Index]
			if !ok {
				cur = &provider.ToolCall{}
				acc[tc.Index] = cur
				order = append(order, tc.Index)
			}
			if tc.ID != "" {
				cur.ID = tc.ID
			}
			if tc.Function.Name != "" {
				cur.Name = tc.Function.Name
			}
			cur.Arguments += tc.Function.Arguments
			// Signal the call's start the moment its name is known, so a frontend
			// can show the tool card immediately rather than only after its
			// (possibly large) arguments finish streaming.
			if !started[tc.Index] && cur.Name != "" {
				started[tc.Index] = true
				emitted = true
				if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkToolCallStart, ToolCall: &provider.ToolCall{ID: cur.ID, Name: cur.Name}}) {
					return emitted, ctx.Err()
				}
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return emitted, err
	}
	if stalled.Load() {
		return emitted, fmt.Errorf("%s: stream stalled — no data for %s, connection likely dropped", c.name, idleTimeout)
	}
	if err := scanner.Err(); err != nil {
		return emitted, fmt.Errorf("%s: read stream: %w", c.name, err)
	}
	// A proxy that idle-closes with a clean FIN ends the scan with no error. Without
	// this check the turn would be committed as complete — including half-streamed
	// tool-call arguments, which then 400 on every replay (#3953).
	if !sawDone && lastFinishReason == "" {
		return emitted, fmt.Errorf("%s: stream ended before completion: %w", c.name, io.ErrUnexpectedEOF)
	}

	if r, txt := think.flush(); r != "" || txt != "" {
		if r != "" {
			if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkReasoning, Text: r}) {
				return emitted, ctx.Err()
			}
		}
		if txt != "" {
			if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkText, Text: txt}) {
				return emitted, ctx.Err()
			}
		}
	}

	sort.Ints(order)
	for _, idx := range order {
		tc := acc[idx]
		if tc.ID == "" {
			// Some OpenAI-compatible gateways stream tool calls by index with no id.
			// Synthesize a stable one so the result can be paired back to its call —
			// an empty tool_call_id collapses multi-tool turns downstream.
			tc.ID = fmt.Sprintf("call_%d", idx)
		}
		if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkToolCall, ToolCall: tc}) {
			return emitted, ctx.Err()
		}
	}
	if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkDone}) {
		return emitted, ctx.Err()
	}
	return emitted, nil
}

// normaliseUsage folds the two cache-hit shapes the OpenAI-compatible ecosystem
// uses into a single Usage: DeepSeek puts prompt_cache_{hit,miss}_tokens at the
// top of usage; OpenAI and MiMo put it nested under prompt_tokens_details.
// Whichever side reports non-zero wins; miss is derived when only hit is given.
// Reasoning tokens land in completion_tokens_details on thinking-mode models.
func normaliseUsage(u *wireUsage) *provider.Usage {
	hit := u.PromptCacheHitTokens
	miss := u.PromptCacheMissTokens
	if hit == 0 && u.PromptTokensDetails != nil {
		hit = u.PromptTokensDetails.CachedTokens
	}
	if miss == 0 && hit > 0 && u.PromptTokens > hit {
		miss = u.PromptTokens - hit
	}
	reasoning := 0
	if u.CompletionTokensDetails != nil {
		reasoning = u.CompletionTokensDetails.ReasoningTokens
	}
	return &provider.Usage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.TotalTokens,
		CacheHitTokens:   hit,
		CacheMissTokens:  miss,
		ReasoningTokens:  reasoning,
	}
}

// --- OpenAI-compatible wire protocol ---

type chatRequest struct {
	Model           string         `json:"model"`
	Messages        []chatMessage  `json:"messages"`
	Tools           []chatTool     `json:"tools,omitempty"`
	Stream          bool           `json:"stream"`
	StreamOptions   *streamOptions `json:"stream_options,omitempty"`
	Temperature     *float64       `json:"temperature,omitempty"`
	MaxTokens       int            `json:"max_tokens,omitempty"`
	ReasoningEffort string         `json:"reasoning_effort,omitempty"`
	Thinking        *thinkingMode  `json:"thinking,omitempty"`
	ExtraBody       map[string]any `json:"-"`
}

func (r chatRequest) MarshalJSON() ([]byte, error) {
	type wire chatRequest
	baseReq := wire(r)
	baseReq.ExtraBody = nil
	raw, err := json.Marshal(baseReq)
	if err != nil {
		return nil, err
	}
	if len(r.ExtraBody) == 0 {
		return raw, nil
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	for key, value := range cleanExtraBody(r.ExtraBody) {
		body[key] = value
	}
	return json.Marshal(body)
}

type thinkingMode struct {
	Type string `json:"type"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatMessage struct {
	Role string `json:"role"`
	// content is always present (never omitted): DeepSeek's strict deserializer
	// rejects a message missing the field. A pure tool_calls assistant turn
	// serializes as null (nil here); a string for every other text message
	// (empty included — null is rejected by some backends for a tool message);
	// and a []chatContentPart array for a vision user turn carrying images.
	Content          any            `json:"content"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	ToolCalls        []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string         `json:"tool_call_id,omitempty"`
	Name             string         `json:"name,omitempty"`
}

type chatContentPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *chatImageURL `json:"image_url,omitempty"`
}

type chatImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

func imageContentParts(text string, images []string, detail string) []chatContentPart {
	parts := make([]chatContentPart, 0, len(images)+1)
	if text != "" {
		parts = append(parts, chatContentPart{Type: "text", Text: text})
	}
	for _, url := range images {
		parts = append(parts, chatContentPart{Type: "image_url", ImageURL: &chatImageURL{URL: url, Detail: detail}})
	}
	return parts
}

type chatTool struct {
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

type chatFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type chatToolCall struct {
	Index    int    `json:"index,omitempty"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type streamResponse struct {
	Choices []struct {
		Delta struct {
			Content          string         `json:"content"`
			ReasoningContent string         `json:"reasoning_content"`
			ToolCalls        []chatToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *wireUsage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// wireUsage covers both DeepSeek's top-level cache fields and the
// OpenAI/MiMo nested details — normaliseUsage chooses whichever side
// reports values.
type wireUsage struct {
	PromptTokens          int `json:"prompt_tokens"`
	CompletionTokens      int `json:"completion_tokens"`
	TotalTokens           int `json:"total_tokens"`
	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens"`
	PromptTokensDetails   *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}
