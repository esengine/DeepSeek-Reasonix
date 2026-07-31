// Package responses implements a provider for the OpenAI Responses API
// (/v1/responses), an upgrade over the Chat Completions API. Providers:
//   - DashScope (https://dashscope.aliyuncs.com or Token Plan endpoints):
//     stateful, server-managed context via previous_response_id, eliminating
//     the need to resend full conversation history each turn. Yields
//     constant-size request bodies and 4x lower latency on multi-turn
//     conversations (measured: 7s vs 28s on turn 3 with qwen3.7-plus).
//   - DeepSeek (https://api.deepseek.com): stateless, rejects
//     previous_response_id; every turn sends the full input array.
//
// The provider implements provider.Provider and can be selected via
// kind = "responses" (legacy alias: "dashscope-responses") in reasonix.toml.
package responses

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
)

// defaultStreamIdleTimeout caps how long a started SSE stream may go silent
// before it's treated as a dropped connection — a half-open TCP connection
// (proxy switched mid-stream) sends no RST, so scanner.Scan() would block
// forever. Generous on purpose; live streams emit far more often.
const defaultStreamIdleTimeout = 120 * time.Second

func init() {
	provider.Register("responses", newFromConfig)
	// Legacy alias: pre-rename kind name.
	provider.Register("dashscope-responses", newFromConfig)
}

// newFromConfig adapts the provider.Factory signature to our client.
func newFromConfig(cfg provider.Config) (provider.Provider, error) {
	effort, _ := cfg.Extra["effort"].(string)
	var stateful *bool
	if v, ok := cfg.Extra["stateful"].(bool); ok {
		stateful = &v
	}
	mode, _ := cfg.Extra["mode"].(string)
	proxy, _ := cfg.Extra["proxy_spec"].(netclient.ProxySpec)
	keyEnv, _ := cfg.Extra["api_key_env"].(string)
	keySource, _ := cfg.Extra["api_key_source"].(string)
	return New(Config{
		Name:      cfg.Name,
		APIKey:    cfg.APIKey,
		BaseURL:   cfg.BaseURL,
		Model:     cfg.Model,
		Effort:    effort,
		Mode:      mode,
		Stateful:  stateful,
		Proxy:     proxy,
		KeyEnv:    keyEnv,
		KeySource: keySource,
	}), nil
}

// Config holds the Responses API provider configuration.
type Config struct {
	Name    string
	APIKey  string
	BaseURL string // e.g. https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1
	Model   string
	Effort  string // "", "low", "medium", "high", "xhigh", "disabled"

	// Mode selects the server-context strategy. Supported values:
	//   "stateful"  (default) — server-managed context via previous_response_id.
	//     DashScope, OpenAI, Azure OpenAI, Volcano Ark.
	//   "stateless" — every turn sends the full input array; the API rejects
	//     previous_response_id. DeepSeek, and OpenAI Codex's own preference.
	//   "" — resolved from Stateful, then vendor auto-detection.
	Mode string

	// Stateful is the legacy boolean form of Mode (nil = unset, falls back to
	// vendor auto-detection; true → "stateful"; false → "stateless").
	Stateful *bool

	// SessionCache enables the x-dashscope-session-cache header (DashScope;
	// default true).
	SessionCache *bool

	// Proxy carries the resolved network proxy spec from the config Extra.
	// nil means the process environment proxy applies.
	Proxy netclient.ProxySpec

	// KeyEnv names the api_key_env the key came from; KeySource its
	// human-readable source. Surfaced in AuthError messages.
	KeyEnv    string
	KeySource string
}

// ProxySpec returns the configured proxy spec.
func (c Config) ProxySpec() any { return c.Proxy }

// mode resolves the effective context mode: explicit Mode wins, then legacy
// Stateful, then vendor auto-detection (stateless for DeepSeek, stateful
// otherwise).
func (c Config) mode() string {
	switch strings.ToLower(strings.TrimSpace(c.Mode)) {
	case "stateful", "stateless":
		return strings.ToLower(strings.TrimSpace(c.Mode))
	}
	if c.Stateful != nil {
		if *c.Stateful {
			return "stateful"
		}
		return "stateless"
	}
	if DetectVendor(c.BaseURL) == "deepseek" {
		return "stateless"
	}
	return "stateful"
}

// Endpoint detection: identify well-known Responses API providers by base URL
// so callers can pick sensible defaults (mode, session-cache header) without
// explicit config. Each returns the vendor name or "" when unknown.
//
//   - DashScope:   dashscope.aliyuncs.com / token-plan.*.maas.aliyuncs.com
//   - DeepSeek:    api.deepseek.com
//   - MiniMax:     api.minimaxi.com (Responses API documented)
//   - Volcano Ark: ark.cn-beijing.volces.com (Responses API supported)
func DetectVendor(baseURL string) string {
	u := strings.ToLower(strings.TrimSpace(baseURL))
	switch {
	case strings.Contains(u, "dashscope.aliyuncs.com"),
		strings.Contains(u, ".maas.aliyuncs.com"),
		strings.Contains(u, "token-plan."):
		return "dashscope"
	case strings.Contains(u, "api.deepseek.com"):
		return "deepseek"
	case strings.Contains(u, "api.minimaxi.com"), strings.Contains(u, "api.minimax.io"):
		return "minimax"
	case strings.Contains(u, "volces.com"), strings.Contains(u, "volcengine.com"):
		return "volcano"
	}
	return ""
}

type client struct {
	name         string
	apiKey       string
	keyEnv       string // api_key_env name, surfaced in auth errors
	keySource    string // source of keyEnv, surfaced in auth errors
	baseURL      string
	model        string
	effort       string
	vendor       string // DetectVendor(baseURL); "" when unknown (tests may override)
	mode         string // "stateful" (previous_response_id) | "stateless" (full input)
	sessionCache bool
	http         *http.Client
	authed       atomic.Bool   // a request has succeeded — gate transient-401 retry
	idleTimeout  time.Duration // SSE stall watchdog window; defaultStreamIdleTimeout unless a test overrides

	mu             sync.Mutex
	lastResponseID string // previous_response_id for server-managed context
}

// sendOpts feeds SendWithRetry's auth/retry behavior.
func (c *client) sendOpts() provider.SendOptions {
	return provider.SendOptions{
		Provider:   c.name,
		KeyEnv:     c.keyEnv,
		KeySource:  c.keySource,
		KeyPresent: c.apiKey != "",
		RetryAuth:  c.authed.Load(),
	}
}

// New creates a Responses API provider.
func New(cfg Config) provider.Provider {
	sc := true
	if cfg.SessionCache != nil {
		sc = *cfg.SessionCache
	}
	// netclient.NewHTTPClient honors the proxy spec (env proxy or explicit
	// proxy_spec from config); transport timeouts are generous because
	// thinking-mode models can be silent for a while before the first token.
	httpClient := &http.Client{Timeout: 300 * time.Second}
	if spec, ok := cfg.ProxySpec().(netclient.ProxySpec); ok {
		if c, err := netclient.NewHTTPClient(spec, netclient.TransportOptions{
			DialTimeout:           30 * time.Second,
			KeepAlive:             30 * time.Second,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 120 * time.Second,
		}); err == nil {
			httpClient = c
		}
	}
	return &client{
		name:         cfg.Name,
		apiKey:       cfg.APIKey,
		keyEnv:       cfg.KeyEnv,
		keySource:    cfg.KeySource,
		baseURL:      strings.TrimRight(cfg.BaseURL, "/"),
		model:        cfg.Model,
		effort:       cfg.Effort,
		vendor:       DetectVendor(cfg.BaseURL),
		mode:         cfg.mode(),
		sessionCache: sc,
		http:         httpClient,
		idleTimeout:  defaultStreamIdleTimeout,
	}
}

// sendChunk delivers a chunk without blocking the SSE reader indefinitely:
// first a non-blocking attempt, then a ctx-aware blocking send. Returns false
// when the caller's context is done (reader gone), so the stream loop can stop
// decoding early instead of stalling scanner.Scan() on a slow consumer.
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

func (c *client) Name() string { return c.name }

// ResetContext clears the previous_response_id, forcing the next Stream call
// to send the full message history. Called after compaction or session switch.
func (c *client) ResetContext() {
	c.mu.Lock()
	c.lastResponseID = ""
	c.mu.Unlock()
}

// Stream starts a streaming completion against POST /v1/responses. Stateful
// mode reuses previous_response_id when the conversation is a simple
// continuation; stateless mode (DeepSeek) always sends the full input array.
// Cancelling ctx aborts the request; the returned channel closes at stream
// end. Mid-stream transport cuts surface as ChunkError with a
// StreamInterruptedError so the agent can append a recovery prompt.
func (c *client) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	body, usePrevID := c.buildRequestBody(req)
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("responses: marshal request: %w", err)
	}

	newReq := func(ctx context.Context) (*http.Request, error) {
		httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/responses", bytes.NewReader(jsonBody))
		if err != nil {
			return nil, fmt.Errorf("responses: create request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		if c.sessionCache {
			httpReq.Header.Set("x-dashscope-session-cache", "enable")
		}
		return httpReq, nil
	}

	// SendWithRetry handles 401/403 → AuthError (actionable message naming the
	// key env), transient-401 retry after the first success, and 429 backoff.
	resp, err := provider.SendWithRetry(ctx, c.http, c.sendOpts(), newReq)
	if err != nil {
		// If the previous_response_id expired (7-day TTL), reset and let the
		// caller retry with full history.
		if resp != nil && resp.StatusCode == http.StatusBadRequest {
			c.ResetContext()
		}
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		if resp.StatusCode == http.StatusBadRequest && strings.Contains(string(b), "not found") {
			c.ResetContext()
		}
		return nil, fmt.Errorf("responses: HTTP %d: %s", resp.StatusCode, string(b))
	}
	c.authed.Store(true)

	out := make(chan provider.Chunk, 64)
	go c.readStream(ctx, resp, out, usePrevID)
	return out, nil
}

// buildRequestBody constructs the Responses API request. When a valid
// previous_response_id exists and the request looks like a simple continuation
// (single new user message appended), it sends only the new input + the ID.
// Otherwise (first turn, compaction, multi-message delta) it sends the full
// messages array as input.
func (c *client) buildRequestBody(req provider.Request) (map[string]any, bool) {
	c.mu.Lock()
	prevID := c.lastResponseID
	c.mu.Unlock()

	body := map[string]any{
		"model":  c.model,
		"stream": true,
	}

	// Effort → reasoning.effort (replaces deprecated enable_thinking).
	// Vendor effort ladders differ: DashScope accepts low/medium/high/xhigh/max;
	// DeepSeek accepts low/high/max only (its default is high). Normalise by
	// vendor so an unsupported tier never 400s.
	effort := c.effort
	switch c.vendor {
	case "deepseek":
		// DeepSeek Responses API supports the full ladder:
		// none/minimal/low/medium/high/xhigh/max (official docs). Omitted
		// reasoning uses the model default (thinking ON). Codex's catalog
		// (low/high/max) is a client UI subset, not an API limit.
		switch effort {
		case "":
			// leave unset → model default (thinking on)
		case "disabled", "off", "none":
			effort = "none"
		case "auto":
			effort = ""
		default:
			// minimal/low/medium/high/xhigh/max pass through verbatim
		}
	case "minimax":
		// MiniMax: effort none/minimal/low/medium/high. Omitted reasoning
		// disables it for M3; minimal/low/medium/high enable without tuning
		// depth; M2.x cannot disable reasoning.
		switch effort {
		case "", "minimal", "low", "medium", "high":
			// pass through; "" = omit reasoning entirely (M3 default off)
		case "disabled", "none":
			effort = "none"
		case "xhigh", "max":
			effort = "high"
		default:
			effort = ""
		}
	}
	if effort == "disabled" {
		body["enable_thinking"] = false
	} else if effort != "" {
		body["reasoning"] = map[string]any{"effort": effort}
	}

	if req.MaxTokens > 0 {
		body["max_output_tokens"] = req.MaxTokens
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}

	// Tools
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			parameters := t.Parameters
			if len(parameters) == 0 {
				// A tool with no declared parameters must still send a
				// well-formed JSON Schema object ("type":"object"); some
				// Responses API backends reject an empty/absent schema.
				parameters = provider.CanonicalizeSchema(nil)
			}
			tools = append(tools, map[string]any{
				"type":        "function",
				"name":        t.Name,
				"description": t.Description,
				"parameters":  json.RawMessage(parameters),
			})
		}
		body["tools"] = tools
	}

	// Decide: use previous_response_id (stateful mode) or send full input.
	// Stateless providers (DeepSeek, Codex-style) reject previous_response_id,
	// so every turn sends the complete input array.
	usePrevID := false
	if c.mode == "stateful" && prevID != "" && isSimpleContinuation(req.Messages) {
		// Only the last user message is new; server has the rest.
		lastUser := lastUserContent(req.Messages)
		body["input"] = lastUser
		body["previous_response_id"] = prevID
		usePrevID = true
	} else {
		// Send full conversation as input array, lifting the leading system
		// message to the top-level instructions field when present (the API
		// treats instructions as the first system message; keeping it in-band
		// too would duplicate it for multi-system sessions, so only the FIRST
		// system message moves up and the rest stay in the array).
		instructions, rest := splitInstructions(req.Messages)
		if instructions != "" {
			body["instructions"] = instructions
		}
		body["input"] = messagesToInput(rest)
	}

	return body, usePrevID
}

// splitInstructions lifts the first system message into a top-level
// instructions string (OpenAI Responses API convention) and returns the
// remaining messages. Extra system messages stay in-band.
func splitInstructions(msgs []provider.Message) (string, []provider.Message) {
	if len(msgs) == 0 || msgs[0].Role != provider.RoleSystem {
		return "", msgs
	}
	rest := append([]provider.Message(nil), msgs[1:]...)
	return msgs[0].Content, rest
}

// isSimpleContinuation returns true when the message list looks like a normal
// multi-turn conversation where only the last user message is new (i.e. the
// server already has everything else via previous_response_id).
func isSimpleContinuation(msgs []provider.Message) bool {
	if len(msgs) < 2 {
		return false
	}
	// The last message must be a user message (the new turn).
	last := msgs[len(msgs)-1]
	return last.Role == provider.RoleUser
}

func lastUserContent(msgs []provider.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == provider.RoleUser {
			return msgs[i].Content
		}
	}
	return ""
}

// messagesToInput converts Reasonix messages to the Responses API input array
// format. Mapping:
//
//	user/assistant   → {"role": ..., "content": ...}
//	system           → {"role": "system", "content": ...} (the API also accepts
//	                   a top-level instructions string; the provider keeps the
//	                   system message in-band so multi-system sessions survive)
//	assistant tool calls → {"type": "function_call", name, arguments, call_id}
//	tool results     → {"type": "function_call_output", call_id, output}
//
// The function_call / function_call_output pairs must stay adjacent in input
// order (the API merges them onto the surrounding assistant message), which
// the single pass preserves.
func messagesToInput(msgs []provider.Message) []map[string]any {
	input := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case provider.RoleSystem:
			// System messages are handled via "instructions" field; skip here
			// but include as a system input item for compatibility.
			input = append(input, map[string]any{
				"role":    "system",
				"content": m.Content,
			})
		case provider.RoleUser:
			input = append(input, map[string]any{
				"role":    "user",
				"content": m.Content,
			})
		case provider.RoleAssistant:
			item := map[string]any{
				"role":    "assistant",
				"content": m.Content,
			}
			if len(m.ToolCalls) > 0 {
				// Assistant tool calls become function_call items
				for _, tc := range m.ToolCalls {
					input = append(input, map[string]any{
						"type":      "function_call",
						"name":      tc.Name,
						"arguments": tc.Arguments,
						"call_id":   tc.ID,
					})
				}
				continue // don't add the assistant message itself if it has tool calls
			}
			input = append(input, item)
		case provider.RoleTool:
			input = append(input, map[string]any{
				"type":    "function_call_output",
				"call_id": m.ToolCallID,
				"output":  m.Content,
			})
		}
	}
	return input
}

// readStream parses the Responses API SSE event stream and maps events to
// provider.Chunk values.
func (c *client) readStream(ctx context.Context, resp *http.Response, out chan<- provider.Chunk, usePrevID bool) {
	defer resp.Body.Close()
	defer close(out)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)

	// SSE stall watchdog: a half-open connection sends no RST, so scanner.Scan()
	// would block forever. Close the body when idle exceeds the timeout; the
	// scan loop then unblocks and reports a recoverable interrupted error.
	idleTimeout := c.idleTimeout
	if idleTimeout <= 0 {
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
			case <-done:
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
			}
		}
	}()
	notifyActivity := func() {
		select {
		case activity <- struct{}{}:
		default:
		}
	}

	var responseID string

	for scanner.Scan() {
		notifyActivity()
		line := scanner.Text()
		// Responses API SSE uses "data:{...}" (no space); be lenient.
		var data string
		if strings.HasPrefix(line, "data: ") {
			data = strings.TrimPrefix(line, "data: ")
		} else if strings.HasPrefix(line, "data:") {
			data = strings.TrimPrefix(line, "data:")
		} else {
			continue
		}
		if data == "[DONE]" {
			break
		}

		var event sseEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "response.output_text.delta":
			if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkText, Text: event.Delta}) {
				return
			}

		case "response.output_text.done", "response.content_part.done":
			// Full text of the completed content part. Sent only when the
			// delta stream may have been incomplete (DeepSeek emits these with
			// the assembled text); callers dedupe by turning deltas off.
			if event.Text != "" {
				if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkText, Text: event.Text}) {
					return
				}
			}

		case "response.reasoning_summary_text.delta":
			// DashScope dialect
			if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkReasoning, Text: event.Delta}) {
				return
			}

		case "response.reasoning_text.delta":
			// DeepSeek dialect
			if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkReasoning, Text: event.Delta}) {
				return
			}

		case "response.output_item.added":
			if event.Item != nil && event.Item.Type == "function_call" {
				if !sendChunk(ctx, out, provider.Chunk{

					Type: provider.ChunkToolCallStart,

					ToolCall: &provider.ToolCall{

						ID: event.Item.CallID,

						Name: event.Item.Name,
					},
				}) {
					return
				}
			}

		case "response.mcp_call_arguments.delta":
			// DashScope dialect: tool call arguments streaming.
			// The delta carries {delta: "..."} with the item in the event.
			if event.Item != nil {
				if !sendChunk(ctx, out, provider.Chunk{

					Type: provider.ChunkToolCallArgsDelta,

					ToolCall: &provider.ToolCall{

						ID: event.Item.CallID,

						Name: event.Item.Name,
					},

					ArgChars: len(event.Delta),
				}) {
					return
				}
			}

		case "response.function_call_arguments.delta":
			// DeepSeek dialect: the delta is the raw partial argument string.
			if !sendChunk(ctx, out, provider.Chunk{

				Type: provider.ChunkToolCallArgsDelta,

				ToolCall: &provider.ToolCall{

					ID: event.FunctionCallID(),
				},

				ArgChars: len(event.Delta),
			}) {
				return
			}

		case "response.function_call_arguments.done":
			// DeepSeek dialect: complete arguments for the call.
			if event.Item != nil {
				if !sendChunk(ctx, out, provider.Chunk{

					Type: provider.ChunkToolCall,

					ToolCall: &provider.ToolCall{

						ID: event.Item.CallID,

						Name: event.Item.Name,

						Arguments: event.Item.Arguments,
					},
				}) {
					return
				}
			}

		case "response.output_item.done":
			if event.Item != nil && event.Item.Type == "function_call" {
				if !sendChunk(ctx, out, provider.Chunk{

					Type: provider.ChunkToolCall,

					ToolCall: &provider.ToolCall{

						ID: event.Item.CallID,

						Name: event.Item.Name,

						Arguments: event.Item.Arguments,
					},
				}) {
					return
				}
			}

		case "response.completed", "response.incomplete", "response.failed":
			// completed: normal end (both dialects). incomplete: output hit
			// max_output_tokens (DeepSeek). failed: error (DeepSeek). All three
			// carry the full response object with usage; the stream ends here
			// with no [DONE] marker.
			if event.Response != nil {
				responseID = event.Response.ID
				if event.Response.Usage != nil {
					u := event.Response.Usage
					cached := 0
					if u.InputTokensDetails != nil {
						cached = u.InputTokensDetails.CachedTokens
					}
					reasoning := 0
					if u.OutputTokensDetails != nil {
						reasoning = u.OutputTokensDetails.ReasoningTokens
					}
					if !sendChunk(ctx, out, provider.Chunk{
						Type:  provider.ChunkUsage,
						Usage: provider.ResponsesUsage(u.InputTokens, u.OutputTokens, u.TotalTokens, cached, reasoning),
					}) {
						return
					}
				}
			}
			if event.Type == "response.failed" {
				if event.Response != nil && event.Response.Error != nil {
					// An authentication-shaped failure (invalid/expired key,
					// no permission) surfaces as a provider.AuthError so the
					// agent shows an actionable message naming the key env.
					// Otherwise a plain error carries the server's reason.
					if err := authErrorFromResponse(c, event.Response.Error); err != nil {
						if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkError, Err: err}) {
							return
						}
					} else {
						msg := "responses: " + event.Response.Error.Message
						if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkError, Err: fmt.Errorf("%s", msg)}) {
							return
						}
					}
				} else {
					if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkError, Err: fmt.Errorf("responses: response failed")}) {
						return
					}
				}
			}
			// Responses API sends no [DONE]; stop after the terminal event.
			goto done
		}
	}

done:
	// Store the response ID for the next turn's previous_response_id.
	// Reached from both the normal loop end ([DONE]/EOF) and terminal events
	// (response.completed/incomplete/failed jump here via goto).
	if responseID != "" {
		c.mu.Lock()
		c.lastResponseID = responseID
		c.mu.Unlock()
	}

	if err := scanner.Err(); err != nil {
		// A mid-stream transport cut after some output was already delivered is
		// recoverable: the agent can append a tail recovery prompt instead of
		// replaying the whole request (which would duplicate visible text).
		if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkError, Err: &provider.StreamInterruptedError{Err: err}}) {
			return
		}
		return
	}
	if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkDone}) {
		return
	}
}

// authErrorFromResponse maps an authentication-shaped server error to a
// provider.AuthError (status 401/403 semantics). Returns nil when the error
// is not authentication-related.
func authErrorFromResponse(c *client, se *sseError) error {
	if se == nil {
		return nil
	}
	code := strings.ToLower(strings.TrimSpace(se.Code))
	msg := strings.ToLower(strings.TrimSpace(se.Message))
	auth := strings.Contains(code, "auth") || strings.Contains(code, "invalid_api_key") ||
		strings.Contains(code, "permission") || strings.Contains(code, "unauthorized") ||
		strings.Contains(code, "forbidden") ||
		strings.Contains(msg, "api key") || strings.Contains(msg, "invalid key") ||
		strings.Contains(msg, "unauthorized") || strings.Contains(msg, "authentication")
	if !auth {
		return nil
	}
	status := 401
	if strings.Contains(code, "forbidden") || strings.Contains(msg, "permission") {
		status = 403
	}
	return &provider.AuthError{
		Provider:  c.name,
		KeyEnv:    c.keyEnv,
		KeySource: c.keySource,
		Status:    status,
		HasKey:    c.apiKey != "",
		Body:      se.Message,
	}
}

// sseEvent is the wire format for Responses API SSE events.
type sseEvent struct {
	Type     string       `json:"type"`
	Delta    string       `json:"delta"`
	Text     string       `json:"text"`
	Item     *sseItem     `json:"item"`
	ItemID   string       `json:"item_id"`
	Response *sseResponse `json:"response"`
}

// FunctionCallID returns the function_call id for tool-argument events.
// OpenAI's Responses API events carry the id at the event top level for
// function_call_arguments.delta events; the nested item may be absent.
func (e sseEvent) FunctionCallID() string {
	if e.Item != nil && e.Item.CallID != "" {
		return e.Item.CallID
	}
	return e.ItemID
}

type sseItem struct {
	ID        string `json:"id"`
	Type      string `json:"type"` // "message", "function_call", "reasoning"
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type sseResponse struct {
	ID    string    `json:"id"`
	Usage *sseUsage `json:"usage"`
	Error *sseError `json:"error"`
}

type sseError struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

type sseUsage struct {
	InputTokens        int `json:"input_tokens"`
	OutputTokens       int `json:"output_tokens"`
	TotalTokens        int `json:"total_tokens"`
	InputTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputTokensDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}
