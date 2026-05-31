// Package anthropic implements the Anthropic Messages API provider (POST
// /v1/messages, SSE streaming) with a hand-written net/http client — no SDK. It
// self-registers under the "anthropic" kind, so any Claude model is a config
// instance rather than code.
//
// Two deliberate v1 scope limits, both driven by the transport-agnostic
// provider.Message abstraction:
//
//   - No extended/adaptive thinking. Anthropic requires the signed `thinking`
//     block be replayed on the next turn when a tool call followed thinking;
//     provider.Message.ReasoningContent is a plain string with no signature, so it
//     can't be round-tripped faithfully. Since reasonix is tool-heavy, enabling
//     thinking would break multi-turn tool use — so the `thinking` field is omitted
//     entirely. (Supporting it means extending Message to carry the signed block.)
//   - No temperature/top_p. Current Claude models (Opus 4.8/4.7) reject sampling
//     parameters with a 400; Anthropic steers behavior via prompting instead.
package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"reasonix/internal/provider"
)

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
	return &client{
		name:    name,
		apiKey:  cfg.APIKey,
		keyEnv:  keyEnv,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   cfg.Model,
		http:    &http.Client{}, // no overall timeout; lifecycle is ctx-driven
	}, nil
}

type client struct {
	name    string
	apiKey  string
	keyEnv  string // api_key_env name, surfaced in auth errors
	baseURL string
	model   string
	http    *http.Client
}

func (c *client) Name() string { return c.name }

func (c *client) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	body, err := json.Marshal(c.buildRequest(req))
	if err != nil {
		return nil, fmt.Errorf("%s: marshal request: %w", c.name, err)
	}

	resp, err := c.sendWithRetry(ctx, body)
	if err != nil {
		return nil, err
	}

	out := make(chan provider.Chunk)
	go c.readStream(resp, out)
	return out, nil
}

// sendWithRetry POSTs the request and returns the streaming response, retrying the
// connection+header phase on transient errors and retryable statuses (408, 429,
// 5xx — which covers Anthropic's 529 overloaded) with exponential backoff + jitter.
// Mid-stream failures are not retried (the model has already emitted tokens).
func (c *client) sendWithRetry(ctx context.Context, body []byte) (*http.Response, error) {
	const maxAttempts = 3
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			delay := time.Duration(1<<(attempt-1))*500*time.Millisecond + time.Duration(rand.Intn(250))*time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("%s: build request: %w", c.name, err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")
		httpReq.Header.Set("x-api-key", c.apiKey)
		httpReq.Header.Set("anthropic-version", anthropicVersion)

		resp, err := c.http.Do(httpReq)
		if err != nil {
			if !isTransientErr(err) {
				return nil, fmt.Errorf("%s: request failed: %w", c.name, err)
			}
			lastErr = fmt.Errorf("%s: request failed: %w", c.name, err)
			continue
		}
		if resp.StatusCode == http.StatusOK {
			return resp, nil
		}
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		// A rejected key is a configuration problem, not a transient one.
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, &provider.AuthError{Provider: c.name, KeyEnv: c.keyEnv, Status: resp.StatusCode}
		}
		statusErr := fmt.Errorf("%s: status %d: %s", c.name, resp.StatusCode, strings.TrimSpace(string(msg)))
		if !isRetryableStatus(resp.StatusCode) {
			return nil, statusErr
		}
		lastErr = statusErr
	}
	return nil, lastErr
}

// isRetryableStatus matches 408, 429, and 5xx (incl. Anthropic's 529 overloaded).
func isRetryableStatus(s int) bool {
	return s == http.StatusRequestTimeout || s == http.StatusTooManyRequests || (s >= 500 && s <= 599)
}

// isTransientErr never retries ctx cancellation/deadline (caller intent); retries
// everything else (DNS, connection reset, abrupt EOF).
func isTransientErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return true
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

	for _, m := range req.Messages {
		switch m.Role {
		case provider.RoleSystem:
			if m.Content != "" {
				system = append(system, textBlock{Type: "text", Text: m.Content})
			}
		case provider.RoleUser:
			if m.Content != "" {
				appendBlocks("user", contentBlock{Type: "text", Text: m.Content})
			}
		case provider.RoleTool:
			content := m.Content
			if content == "" {
				content = "(no output)" // tool_result content must be non-empty
			}
			appendBlocks("user", contentBlock{Type: "tool_result", ToolUseID: m.ToolCallID, Content: content})
		case provider.RoleAssistant:
			var blocks []contentBlock
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
	return anthRequest{
		Model:     c.model,
		MaxTokens: maxTokens,
		System:    system,
		Messages:  msgs,
		Tools:     tools,
		Stream:    true,
	}
}

// readStream parses the Messages API SSE stream into Chunks. Text deltas emit live;
// each tool_use content block emits a ChunkToolCallStart when its id+name are known
// and a complete ChunkToolCall when the block closes; usage is assembled from
// message_start (input/cache) + message_delta (output + stop_reason) and emitted
// once before ChunkDone.
func (c *client) readStream(resp *http.Response, out chan<- provider.Chunk) {
	defer resp.Body.Close()
	defer close(out)

	tools := map[int]*provider.ToolCall{} // tool_use blocks, keyed by content index
	var inTok, outTok, cacheCreate, cacheRead int
	var stopReason string
	haveUsage := false

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
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
			out <- provider.Chunk{Type: provider.ChunkError, Err: fmt.Errorf("%s: decode stream: %w", c.name, err)}
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
				out <- provider.Chunk{Type: provider.ChunkToolCallStart, ToolCall: &provider.ToolCall{ID: tc.ID, Name: tc.Name}}
			}
		case "content_block_delta":
			if ev.Delta == nil {
				continue
			}
			switch ev.Delta.Type {
			case "text_delta":
				if ev.Delta.Text != "" {
					out <- provider.Chunk{Type: provider.ChunkText, Text: ev.Delta.Text}
				}
			case "input_json_delta":
				if tc := tools[ev.Index]; tc != nil {
					tc.Arguments += ev.Delta.PartialJSON
				}
			}
		case "content_block_stop":
			if tc := tools[ev.Index]; tc != nil {
				out <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: tc}
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
			out <- provider.Chunk{Type: provider.ChunkError, Err: fmt.Errorf("%s: %s", c.name, msg)}
			return
		}
	}

	if err := scanner.Err(); err != nil {
		out <- provider.Chunk{Type: provider.ChunkError, Err: fmt.Errorf("%s: read stream: %w", c.name, err)}
		return
	}

	if haveUsage {
		out <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{
			PromptTokens:     inTok + cacheCreate + cacheRead,
			CompletionTokens: outTok,
			TotalTokens:      inTok + cacheCreate + cacheRead + outTok,
			CacheHitTokens:   cacheRead,
			CacheMissTokens:  inTok + cacheCreate, // uncached input + cache writes (billed ≥1×)
			FinishReason:     mapStopReason(stopReason),
		}}
	}
	out <- provider.Chunk{Type: provider.ChunkDone}
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
	Model     string        `json:"model"`
	MaxTokens int           `json:"max_tokens"`
	System    []textBlock   `json:"system,omitempty"`
	Messages  []anthMessage `json:"messages"`
	Tools     []anthTool    `json:"tools,omitempty"`
	Stream    bool          `json:"stream"`
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
	ID           string          `json:"id,omitempty"`          // tool_use
	Name         string          `json:"name,omitempty"`        // tool_use
	Input        json.RawMessage `json:"input,omitempty"`       // tool_use
	ToolUseID    string          `json:"tool_use_id,omitempty"` // tool_result
	Content      string          `json:"content,omitempty"`     // tool_result
	CacheControl *cacheControl   `json:"cache_control,omitempty"`
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
		Type        string `json:"type"`         // text_delta | input_json_delta | thinking_delta
		Text        string `json:"text"`         // text_delta
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
