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
	"time"

	"reasonix/internal/provider"
)

func init() {
	provider.Register("responses", newFromConfig)
	// Legacy alias: pre-rename kind name.
	provider.Register("dashscope-responses", newFromConfig)
}

// newFromConfig adapts the provider.Factory signature to our client.
func newFromConfig(cfg provider.Config) (provider.Provider, error) {
	effort, _ := cfg.Extra["effort"].(string)
	stateful, _ := cfg.Extra["stateful"].(bool)
	return New(Config{
		Name:     cfg.Name,
		APIKey:   cfg.APIKey,
		BaseURL:  cfg.BaseURL,
		Model:    cfg.Model,
		Effort:   effort,
		Stateful: stateful,
	}), nil
}

// Config holds the DashScope Responses API provider configuration.
type Config struct {
	Name    string
	APIKey  string
	BaseURL string // e.g. https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1
	Model   string
	Effort  string // "", "low", "medium", "high", "xhigh", "disabled"

	// Stateful enables server-managed context via previous_response_id.
	// DashScope supports it; DeepSeek's Responses API is stateless and
	// rejects previous_response_id — set Stateful=false for DeepSeek so
	// every turn sends the full input array.
	Stateful bool

	// SessionCache enables the x-dashscope-session-cache header (default true).
	SessionCache *bool
}

type client struct {
	name         string
	apiKey       string
	baseURL      string
	model        string
	effort       string
	stateful     bool // DashScope: previous_response_id; DeepSeek: stateless
	sessionCache bool
	http         *http.Client

	mu             sync.Mutex
	lastResponseID string // previous_response_id for server-managed context
}

// New creates a DashScope Responses API provider.
func New(cfg Config) provider.Provider {
	sc := true
	if cfg.SessionCache != nil {
		sc = *cfg.SessionCache
	}
	return &client{
		name:         cfg.Name,
		apiKey:       cfg.APIKey,
		baseURL:      strings.TrimRight(cfg.BaseURL, "/"),
		model:        cfg.Model,
		effort:       cfg.Effort,
		stateful:     cfg.Stateful,
		sessionCache: sc,
		http: &http.Client{
			Timeout: 300 * time.Second,
		},
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

func (c *client) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	body, usePrevID := c.buildRequestBody(req)
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("responses: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/responses", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("responses: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	if c.sessionCache {
		httpReq.Header.Set("x-dashscope-session-cache", "enable")
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("responses: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		// If the previous_response_id expired (7-day TTL), reset and let the
		// caller retry with full history.
		if resp.StatusCode == http.StatusBadRequest && strings.Contains(string(b), "not found") {
			c.ResetContext()
		}
		return nil, fmt.Errorf("responses: HTTP %d: %s", resp.StatusCode, string(b))
	}

	out := make(chan provider.Chunk, 16)
	go c.readStream(resp, out, usePrevID)
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

	// Effort → reasoning.effort (replaces deprecated enable_thinking)
	if c.effort == "disabled" {
		body["enable_thinking"] = false
	} else if c.effort != "" {
		body["reasoning"] = map[string]any{"effort": c.effort}
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
			tools = append(tools, map[string]any{
				"type":        "function",
				"name":        t.Name,
				"description": t.Description,
				"parameters":  json.RawMessage(t.Parameters),
			})
		}
		body["tools"] = tools
	}

	// Decide: use previous_response_id (stateful DashScope) or send full input.
	// DeepSeek's Responses API is stateless and rejects previous_response_id,
	// so every turn sends the complete input array.
	usePrevID := false
	if c.stateful && prevID != "" && isSimpleContinuation(req.Messages) {
		// Only the last user message is new; server has the rest.
		lastUser := lastUserContent(req.Messages)
		body["input"] = lastUser
		body["previous_response_id"] = prevID
		usePrevID = true
	} else {
		// Send full conversation as input array.
		body["input"] = messagesToInput(req.Messages)
	}

	return body, usePrevID
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
// format. System messages become instructions; tool results become
// function_call_output items.
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
func (c *client) readStream(resp *http.Response, out chan<- provider.Chunk, usePrevID bool) {
	defer resp.Body.Close()
	defer close(out)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)

	var responseID string

	for scanner.Scan() {
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
			out <- provider.Chunk{Type: provider.ChunkText, Text: event.Delta}

		case "response.reasoning_summary_text.delta":
			// DashScope dialect
			out <- provider.Chunk{Type: provider.ChunkReasoning, Text: event.Delta}

		case "response.reasoning_text.delta":
			// DeepSeek dialect
			out <- provider.Chunk{Type: provider.ChunkReasoning, Text: event.Delta}

		case "response.output_item.added":
			if event.Item != nil && event.Item.Type == "function_call" {
				out <- provider.Chunk{
					Type: provider.ChunkToolCallStart,
					ToolCall: &provider.ToolCall{
						ID:   event.Item.CallID,
						Name: event.Item.Name,
					},
				}
			}

		case "response.mcp_call_arguments.delta":
			// DashScope dialect: tool call arguments streaming
			if event.Item != nil {
				out <- provider.Chunk{
					Type: provider.ChunkToolCallArgsDelta,
					ToolCall: &provider.ToolCall{
						ID:   event.Item.CallID,
						Name: event.Item.Name,
					},
				}
			}

		case "response.function_call_arguments.delta":
			// DeepSeek dialect: tool call arguments streaming
			if event.Item != nil {
				out <- provider.Chunk{
					Type: provider.ChunkToolCallArgsDelta,
					ToolCall: &provider.ToolCall{
						ID:   event.Item.CallID,
						Name: event.Item.Name,
					},
				}
			}

		case "response.output_item.done":
			if event.Item != nil && event.Item.Type == "function_call" {
				out <- provider.Chunk{
					Type: provider.ChunkToolCall,
					ToolCall: &provider.ToolCall{
						ID:        event.Item.CallID,
						Name:      event.Item.Name,
						Arguments: event.Item.Arguments,
					},
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
					out <- provider.Chunk{
						Type: provider.ChunkUsage,
						Usage: &provider.Usage{
							PromptTokens:     u.InputTokens,
							CompletionTokens: u.OutputTokens,
							TotalTokens:      u.TotalTokens,
							CacheHitTokens:   cached,
							CacheMissTokens:  u.InputTokens - cached,
							ReasoningTokens:  reasoning,
						},
					}
				}
			}
			if event.Type == "response.failed" {
				msg := "responses: response failed"
				if event.Response != nil && event.Response.Error != nil {
					msg = "responses: " + event.Response.Error.Message
				}
				out <- provider.Chunk{Type: provider.ChunkError, Err: fmt.Errorf("%s", msg)}
			}
			// Responses API sends no [DONE]; stop after the terminal event.
			goto done
		}
	}

	// Store the response ID for the next turn's previous_response_id.
	if responseID != "" {
		c.mu.Lock()
		c.lastResponseID = responseID
		c.mu.Unlock()
	}

done:

	if err := scanner.Err(); err != nil {
		out <- provider.Chunk{Type: provider.ChunkError, Err: err}
		return
	}
	out <- provider.Chunk{Type: provider.ChunkDone}
}

// sseEvent is the wire format for Responses API SSE events.
type sseEvent struct {
	Type     string       `json:"type"`
	Delta    string       `json:"delta"`
	Item     *sseItem     `json:"item"`
	Response *sseResponse `json:"response"`
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
