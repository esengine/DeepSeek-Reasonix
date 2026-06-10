// Package openai implements the OpenAI-compatible /chat/completions provider.
// It self-registers under the "openai" kind, so DeepSeek, MiMo, MiniMax-M3, and
// any other OpenAI-compatible endpoint are just config instances rather than
// code. Each instance picks the wire shape from its base URL:
//   - api.deepseek.com → emits thinking.type=enabled (DeepSeek-flavor CoT) plus
//     reasoning_effort as a depth hint.
//   - api.minimaxi.com → emits thinking.type=adaptive|disabled (M3's binary
//     knob) instead of reasoning_effort, since M3 has no level scale.
//   - everything else (MiMo and other OpenAI-compatible gateways) uses the
//     vanilla reasoning_effort scale (low/medium/high).
package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	effort, _ := cfg.Extra["effort"].(string)
	effort = strings.ToLower(strings.TrimSpace(effort))
	if effort == "auto" {
		effort = ""
	}
	protocol, _ := cfg.Extra["reasoning_protocol"].(string)
	protocol = normalizeReasoningProtocol(protocol)
	deepseek := protocol == "deepseek" || (protocol == "" && IsDeepSeek(cfg.BaseURL))
	minimax := protocol == "" && IsMiniMax(cfg.BaseURL)
	switch {
	case protocol == "none":
		effort = ""
	case deepseek:
		switch effort {
		case "", "off": // "off" is a retired level (disabled thinking); fall back to the default depth
			effort = "high"
		case "high", "max":
		default:
			return nil, fmt.Errorf("openai: provider %q uses DeepSeek thinking; effort must be high or max", name)
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
		name:        name,
		apiKey:      cfg.APIKey,
		keyEnv:      keyEnv,
		baseURL:     strings.TrimRight(cfg.BaseURL, "/"),
		model:       cfg.Model,
		deepseek:    deepseek,
		minimax:     minimax,
		effort:      effort,
		http:        httpClient,
		idleTimeout: defaultStreamIdleTimeout,
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
	name        string
	apiKey      string
	keyEnv      string // api_key_env name, surfaced in auth errors
	baseURL     string
	model       string
	http        *http.Client
	deepseek    bool
	minimax     bool          // true for api.minimaxi.com — emits MiniMax-M3's thinking knob instead of reasoning_effort
	effort      string        // reasoning_effort for OpenAI; thinking.type for MiniMax; "" = auto/provider default
	idleTimeout time.Duration // SSE stall watchdog window; defaultStreamIdleTimeout unless a test overrides
}

func (c *client) Name() string { return c.name }

func normalizeReasoningProtocol(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "deepseek", "openai", "none":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return ""
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
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		httpReq.Header.Set("Accept", "text/event-stream")
		return httpReq, nil
	}
	resp, err := provider.SendWithRetry(ctx, c.http, c.name, c.keyEnv, newReq)
	if err != nil {
		return nil, err
	}

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
			out <- provider.Chunk{Type: provider.ChunkError, Err: err}
			return
		}
		if emitted {
			out <- provider.Chunk{Type: provider.ChunkError, Err: &provider.StreamInterruptedError{Err: err}}
			return
		}
		if attempt >= maxStreamReconnects {
			out <- provider.Chunk{Type: provider.ChunkError, Err: err}
			return
		}
		next, rerr := provider.SendWithRetry(ctx, c.http, c.name, c.keyEnv, newReq)
		if rerr != nil {
			out <- provider.Chunk{Type: provider.ChunkError, Err: rerr}
			return
		}
		resp = next
	}
}

func (c *client) buildRequest(req provider.Request) chatRequest {
	// Repair tool-call pairing before sending: an interrupted/resumed history can
	// carry an assistant tool_calls turn whose results never landed, which DeepSeek
	// rejects with a 400 ("must be followed by tool messages …").
	src := provider.SanitizeToolPairing(req.Messages)
	msgs := make([]chatMessage, len(src))
	for i, m := range src {
		// reasoning_content is deliberately NOT sent back: it's a response-only
		// field. DeepSeek counts re-sent reasoning as billable prompt input
		// (measured ~500 extra tokens per turn on a reasoner chain); MiMo accepts
		// it but does not require it (verified empirically: multi-turn tool-call
		// sessions work fine without it, saving ~18 tokens/turn). The session
		// still keeps it (for display/archive); we just don't pay to re-upload it.
		cm := chatMessage{
			Role:       string(m.Role),
			ToolCallID: m.ToolCallID,
			Name:       m.Name,
		}
		for _, tc := range m.ToolCalls {
			wire := chatToolCall{ID: tc.ID, Type: "function"}
			wire.Function.Name = tc.Name
			wire.Function.Arguments = tc.Arguments
			cm.ToolCalls = append(cm.ToolCalls, wire)
		}
		if m.Role != provider.RoleAssistant || len(cm.ToolCalls) == 0 || m.Content != "" {
			content := m.Content
			cm.Content = &content
		}
		msgs[i] = cm
	}

	var tools []chatTool
	for _, t := range req.Tools {
		tools = append(tools, chatTool{
			Type:     "function",
			Function: chatFunction{Name: t.Name, Description: t.Description, Parameters: t.Parameters},
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
	}
	switch {
	case c.deepseek:
		// DeepSeek's CoT is controlled by `thinking` (always on) plus
		// `reasoning_effort` for depth. We never disable thinking for DeepSeek.
		out.Thinking = &thinkingMode{Type: "enabled"}
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
			out <- provider.Chunk{Type: provider.ChunkUsage, Usage: u}
		}
		if len(sr.Choices) == 0 {
			continue
		}

		delta := sr.Choices[0].Delta
		if delta.ReasoningContent != "" {
			emitted = true
			out <- provider.Chunk{Type: provider.ChunkReasoning, Text: delta.ReasoningContent}
		}
		if delta.Content != "" {
			r, txt := think.push(delta.Content)
			if r != "" {
				emitted = true
				out <- provider.Chunk{Type: provider.ChunkReasoning, Text: r}
			}
			if txt != "" {
				emitted = true
				out <- provider.Chunk{Type: provider.ChunkText, Text: txt}
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
				out <- provider.Chunk{Type: provider.ChunkToolCallStart, ToolCall: &provider.ToolCall{ID: cur.ID, Name: cur.Name}}
			}
		}
	}

	if stalled.Load() {
		return emitted, fmt.Errorf("%s: stream stalled — no data for %s, connection likely dropped", c.name, idleTimeout)
	}
	if err := scanner.Err(); err != nil {
		return emitted, fmt.Errorf("%s: read stream: %w", c.name, err)
	}

	if r, txt := think.flush(); r != "" || txt != "" {
		if r != "" {
			out <- provider.Chunk{Type: provider.ChunkReasoning, Text: r}
		}
		if txt != "" {
			out <- provider.Chunk{Type: provider.ChunkText, Text: txt}
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
		out <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: tc}
	}
	out <- provider.Chunk{Type: provider.ChunkDone}
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
	Temperature     float64        `json:"temperature,omitempty"`
	MaxTokens       int            `json:"max_tokens,omitempty"`
	ReasoningEffort string         `json:"reasoning_effort,omitempty"`
	Thinking        *thinkingMode  `json:"thinking,omitempty"`
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
	// serializes as null (OpenAI-spec, and what strict clones expect); every
	// other role/message serializes as a string, empty included — null is
	// rejected by some backends for a tool message.
	Content    *string        `json:"content"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Name       string         `json:"name,omitempty"`
	// no reasoning_content field: it is a response-only signal and is never sent
	// back upstream — re-uploading it is paid prompt input.
}

type chatTool struct {
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

type chatFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type chatToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
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
