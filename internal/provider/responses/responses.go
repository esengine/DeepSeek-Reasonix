// Package responses implements the OpenAI Responses API wire protocol.
// DeepSeek uses it statelessly and requires the complete input history on every
// request; compatible stateful endpoints may opt into previous_response_id.
package responses

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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

const defaultStreamIdleTimeout = 120 * time.Second

func init() {
	provider.Register("responses", newFromConfig)
	provider.Register("dashscope-responses", newFromConfig)
}

func newFromConfig(cfg provider.Config) (provider.Provider, error) {
	effort, _ := cfg.Extra["effort"].(string)
	mode, _ := cfg.Extra["mode"].(string)
	var stateful *bool
	switch value := cfg.Extra["stateful"].(type) {
	case bool:
		stateful = &value
	case *bool:
		stateful = value
	}
	proxy, _ := cfg.Extra["proxy_spec"].(netclient.ProxySpec)
	keyEnv, _ := cfg.Extra["api_key_env"].(string)
	keySource, _ := cfg.Extra["api_key_source"].(string)
	return New(Config{
		Name: cfg.Name, APIKey: cfg.APIKey, BaseURL: cfg.BaseURL, Model: cfg.Model,
		Effort: effort, Mode: mode, Stateful: stateful, Proxy: proxy,
		KeyEnv: keyEnv, KeySource: keySource,
	}), nil
}

// Config holds Responses API provider settings.
type Config struct {
	Name      string
	APIKey    string
	BaseURL   string
	Model     string
	Effort    string
	Mode      string // stateful | stateless; empty uses vendor detection.
	Stateful  *bool  // legacy form of Mode; nil preserves vendor detection.
	Proxy     netclient.ProxySpec
	KeyEnv    string
	KeySource string
	// SessionCache controls DashScope's opt-in header. The header is never sent
	// to non-DashScope endpoints even when this value is true.
	SessionCache *bool
}

func (c Config) mode() string {
	mode := strings.ToLower(strings.TrimSpace(c.Mode))
	if mode == "stateful" || mode == "stateless" {
		return mode
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

// DetectVendor identifies endpoint behavior that affects the Responses wire.
func DetectVendor(baseURL string) string {
	u := strings.ToLower(strings.TrimSpace(baseURL))
	switch {
	case strings.Contains(u, "dashscope.aliyuncs.com"), strings.Contains(u, ".maas.aliyuncs.com"):
		return "dashscope"
	case strings.Contains(u, "api.deepseek.com"):
		return "deepseek"
	default:
		return ""
	}
}

type client struct {
	name, apiKey, keyEnv, keySource string
	baseURL, model, effort          string
	vendor, mode                    string
	sessionCache                    bool
	http                            *http.Client
	idleTimeout                     time.Duration
	authed                          atomic.Bool

	mu                   sync.Mutex
	lastResponseID       string
	expectedPrefixDigest string
}

// New creates a Responses API provider.
func New(cfg Config) provider.Provider {
	vendor := DetectVendor(cfg.BaseURL)
	sessionCache := vendor == "dashscope"
	if cfg.SessionCache != nil {
		sessionCache = *cfg.SessionCache
	}
	httpClient := &http.Client{Timeout: 300 * time.Second}
	if built, err := netclient.NewHTTPClient(cfg.Proxy, netclient.TransportOptions{
		DialTimeout: 30 * time.Second, KeepAlive: 30 * time.Second,
		TLSHandshakeTimeout: 15 * time.Second, ResponseHeaderTimeout: 120 * time.Second,
	}); err == nil {
		httpClient = built
	}
	return &client{
		name: cfg.Name, apiKey: cfg.APIKey, keyEnv: cfg.KeyEnv, keySource: cfg.KeySource,
		baseURL: strings.TrimRight(cfg.BaseURL, "/"), model: cfg.Model, effort: cfg.Effort,
		vendor: vendor, mode: cfg.mode(), sessionCache: sessionCache,
		http: httpClient, idleTimeout: defaultStreamIdleTimeout,
	}
}

func (c *client) Name() string { return c.name }

// RequiresToolCallReasoning tells the agent to preserve DeepSeek reasoning on
// assistant tool-call turns so the stateless follow-up can replay it.
func (c *client) RequiresToolCallReasoning() bool { return c.vendor == "deepseek" }

func (c *client) sendOpts() provider.SendOptions {
	return provider.SendOptions{Provider: c.name, KeyEnv: c.keyEnv, KeySource: c.keySource, KeyPresent: c.apiKey != "", RetryAuth: c.authed.Load()}
}

// ResetContext drops stateful continuation metadata. Full-input stateless mode
// is unaffected.
func (c *client) ResetContext() {
	c.mu.Lock()
	c.lastResponseID = ""
	c.expectedPrefixDigest = ""
	c.mu.Unlock()
}

func (c *client) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	body, usedPrevious, wireMessages := c.buildRequestBody(req)
	resp, err := c.send(ctx, body)
	if err != nil && usedPrevious && isStalePreviousResponseError(err) {
		// A stateful response ID may expire server-side. Retrying once with full
		// history is safe because no response body has started streaming.
		c.ResetContext()
		body, _, wireMessages = c.buildRequestBody(req)
		resp, err = c.send(ctx, body)
	}
	if err != nil {
		return nil, err
	}
	c.authed.Store(true)
	out := make(chan provider.Chunk, 64)
	go c.readStream(ctx, resp, out, wireMessages)
	return out, nil
}

func (c *client) send(ctx context.Context, body map[string]any) (*http.Response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("responses: marshal request: %w", err)
	}
	newRequest := func(ctx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/responses", bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		if c.vendor == "dashscope" && c.sessionCache {
			req.Header.Set("x-dashscope-session-cache", "enable")
		}
		return req, nil
	}
	return provider.SendWithRetry(ctx, c.http, c.sendOpts(), newRequest)
}

func isStalePreviousResponseError(err error) bool {
	var apiErr *provider.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
		return false
	}
	body := strings.ToLower(apiErr.Body)
	mentionsID := strings.Contains(body, "previous_response_id") || strings.Contains(body, "previous response") || strings.Contains(body, "response id")
	return mentionsID &&
		(strings.Contains(body, "not found") || strings.Contains(body, "invalid") || strings.Contains(body, "expired"))
}

func (c *client) buildRequestBody(req provider.Request) (map[string]any, bool, []provider.Message) {
	messages := provider.SanitizeToolPairing(provider.ModelMessages(req.Messages))
	body := map[string]any{"model": c.model, "stream": true}

	effort := strings.ToLower(strings.TrimSpace(c.effort))
	switch effort {
	case "auto":
		effort = ""
	case "disabled", "off":
		effort = "none"
	}
	if effort != "" {
		body["reasoning"] = map[string]any{"effort": effort}
	}
	if req.MaxTokens > 0 {
		body["max_output_tokens"] = req.MaxTokens
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, tool := range req.Tools {
			parameters := tool.Parameters
			if len(parameters) == 0 {
				parameters = provider.CanonicalizeSchema(nil)
			}
			tools = append(tools, map[string]any{
				"type": "function", "name": tool.Name, "description": tool.Description,
				"parameters": json.RawMessage(parameters),
			})
		}
		body["tools"] = tools
	}
	instructions, rest := splitInstructions(messages)
	if instructions != "" {
		body["instructions"] = instructions
	}

	c.mu.Lock()
	previousID, expectedDigest := c.lastResponseID, c.expectedPrefixDigest
	c.mu.Unlock()
	if c.mode == "stateful" && previousID != "" && len(messages) > 0 &&
		messages[len(messages)-1].Role == provider.RoleUser &&
		conversationDigest(messages[:len(messages)-1]) == expectedDigest {
		body["input"] = messages[len(messages)-1].Content
		body["previous_response_id"] = previousID
		return body, true, messages
	}

	body["input"] = messagesToInput(rest)
	return body, false, messages
}

func splitInstructions(messages []provider.Message) (string, []provider.Message) {
	if len(messages) == 0 || messages[0].Role != provider.RoleSystem {
		return "", messages
	}
	return messages[0].Content, messages[1:]
}

func messagesToInput(messages []provider.Message) []map[string]any {
	input := make([]map[string]any, 0, len(messages)*2)
	for _, message := range messages {
		switch message.Role {
		case provider.RoleSystem, provider.RoleUser:
			input = append(input, map[string]any{"role": string(message.Role), "content": message.Content})
		case provider.RoleAssistant:
			if message.ReasoningContent != "" {
				input = append(input, map[string]any{
					"type":    "reasoning",
					"content": []map[string]string{{"type": "reasoning_text", "text": message.ReasoningContent}},
				})
			}
			if message.Content != "" || len(message.ToolCalls) == 0 {
				input = append(input, map[string]any{"role": "assistant", "content": message.Content})
			}
			for _, call := range message.ToolCalls {
				input = append(input, map[string]any{
					"type": "function_call", "call_id": call.ID,
					"name": call.Name, "arguments": call.Arguments,
				})
			}
		case provider.RoleTool:
			input = append(input, map[string]any{
				"type": "function_call_output", "call_id": message.ToolCallID, "output": message.Content,
			})
		}
	}
	return input
}

func conversationDigest(messages []provider.Message) string {
	instructions, rest := splitInstructions(messages)
	payload, _ := json.Marshal(struct {
		Instructions string           `json:"instructions,omitempty"`
		Input        []map[string]any `json:"input"`
	}{Instructions: instructions, Input: messagesToInput(rest)})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

type streamedCall struct {
	id, name, arguments string
	argChars            int
	completed           bool
}

func (c *client) readStream(ctx context.Context, resp *http.Response, out chan<- provider.Chunk, requestMessages []provider.Message) {
	defer resp.Body.Close()
	defer close(out)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	idle := c.idleTimeout
	if idle <= 0 {
		idle = defaultStreamIdleTimeout
	}
	watchDone := make(chan struct{})
	activity := make(chan struct{}, 1)
	var stalled atomic.Bool
	go func() {
		timer := time.NewTimer(idle)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				_ = resp.Body.Close()
				return
			case <-watchDone:
				return
			case <-activity:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(idle)
			case <-timer.C:
				stalled.Store(true)
				_ = resp.Body.Close()
				return
			}
		}
	}()
	defer close(watchDone)

	calls := make(map[string]*streamedCall)
	callOrder := make([]string, 0)
	callForItem := func(itemID string) *streamedCall {
		if call := calls[itemID]; call != nil {
			return call
		}
		call := &streamedCall{id: itemID}
		calls[itemID] = call
		callOrder = append(callOrder, itemID)
		return call
	}
	textDeltas := make(map[string]bool)
	reasoningDeltas := make(map[string]bool)
	var text, reasoning strings.Builder
	terminal := false
	failed := false
	completedResponseID := ""

	for scanner.Scan() {
		select {
		case activity <- struct{}{}:
		default:
		}
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			terminal = true
			break
		}
		var event sseEvent
		if json.Unmarshal([]byte(data), &event) != nil {
			continue
		}
		key := fmt.Sprintf("%s:%d", event.ItemID, event.ContentIndex)
		switch event.Type {
		case "response.output_text.delta":
			textDeltas[key] = true
			text.WriteString(event.Delta)
			if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkText, Text: event.Delta}) {
				return
			}
		case "response.output_text.done":
			if event.Text != "" && !textDeltas[key] {
				text.WriteString(event.Text)
				if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkText, Text: event.Text}) {
					return
				}
			}
		case "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
			reasoningDeltas[key] = true
			reasoning.WriteString(event.Delta)
			if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkReasoning, Text: event.Delta}) {
				return
			}
		case "response.reasoning_text.done", "response.reasoning_summary_text.done":
			if event.Text != "" && !reasoningDeltas[key] {
				reasoning.WriteString(event.Text)
				if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkReasoning, Text: event.Text}) {
					return
				}
			}
		case "response.output_item.added":
			if event.Item != nil && event.Item.Type == "function_call" {
				call := callForItem(event.Item.ID)
				call.id = event.Item.CallID
				call.name = event.Item.Name
				if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkToolCallStart, ToolCall: &provider.ToolCall{ID: call.id, Name: call.name}}) {
					return
				}
			}
		case "response.function_call_arguments.delta":
			call := callForItem(event.ItemID)
			call.arguments += event.Delta
			call.argChars += len(event.Delta)
			if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkToolCallArgsDelta, ToolCall: &provider.ToolCall{ID: call.id, Name: call.name}, ArgChars: call.argChars}) {
				return
			}
		case "response.function_call_arguments.done":
			call := callForItem(event.ItemID)
			if event.Arguments != "" {
				call.arguments = event.Arguments
			}
			if !call.completed {
				call.completed = true
				if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: call.id, Name: call.name, Arguments: call.arguments}}) {
					return
				}
			}
		case "response.output_item.done":
			if event.Item != nil && event.Item.Type == "function_call" {
				call := callForItem(event.Item.ID)
				if event.Item.CallID != "" {
					call.id = event.Item.CallID
				}
				if event.Item.Name != "" {
					call.name = event.Item.Name
				}
				if event.Item.Arguments != "" {
					call.arguments = event.Item.Arguments
				}
				if !call.completed {
					call.completed = true
					if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: call.id, Name: call.name, Arguments: call.arguments}}) {
						return
					}
				}
			}
		case "response.completed", "response.incomplete", "response.failed":
			terminal = true
			if event.Response != nil {
				if event.Type == "response.completed" {
					completedResponseID = event.Response.ID
				}
				usage := usageFromResponse(event.Response)
				if event.Type == "response.incomplete" {
					switch event.Response.IncompleteDetails.Reason {
					case "max_output_tokens":
						usage.FinishReason = "length"
					case "content_filter":
						usage.FinishReason = "content_filter"
					default:
						usage.FinishReason = "incomplete"
					}
				}
				if event.Response.Usage != nil || usage.FinishReason != "" {
					if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkUsage, Usage: usage}) {
						return
					}
				}
			}
			if event.Type == "response.failed" {
				failed = true
				err := fmt.Errorf("responses: response failed")
				if event.Response != nil && event.Response.Error != nil {
					if authErr := authErrorFromResponse(c, event.Response.Error); authErr != nil {
						err = authErr
					} else {
						err = fmt.Errorf("responses: %s", event.Response.Error.Message)
					}
				}
				if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkError, Err: err}) {
					return
				}
			}
		}
		if terminal {
			break
		}
	}

	if ctx.Err() != nil {
		return
	}
	if err := scanner.Err(); err != nil {
		if stalled.Load() {
			err = fmt.Errorf("responses: stream idle timeout after %s", idle)
		}
		_ = sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkError, Err: &provider.StreamInterruptedError{Err: err}})
		return
	}
	if !terminal {
		_ = sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkError, Err: &provider.StreamInterruptedError{Err: io.ErrUnexpectedEOF}})
		return
	}
	if completedResponseID != "" {
		assistant := provider.Message{Role: provider.RoleAssistant, Content: text.String(), ReasoningContent: reasoning.String()}
		for _, itemID := range callOrder {
			call := calls[itemID]
			if call.completed {
				assistant.ToolCalls = append(assistant.ToolCalls, provider.ToolCall{ID: call.id, Name: call.name, Arguments: call.arguments})
			}
		}
		expected := append(append([]provider.Message(nil), requestMessages...), assistant)
		c.mu.Lock()
		c.lastResponseID = completedResponseID
		c.expectedPrefixDigest = conversationDigest(expected)
		c.mu.Unlock()
	} else {
		c.ResetContext()
	}
	if !failed {
		_ = sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkDone})
	}
}

func sendChunk(ctx context.Context, out chan<- provider.Chunk, chunk provider.Chunk) bool {
	select {
	case out <- chunk:
		return true
	default:
	}
	select {
	case out <- chunk:
		return true
	case <-ctx.Done():
		return false
	}
}

func usageFromResponse(response *sseResponse) *provider.Usage {
	usage := &provider.Usage{}
	if response == nil || response.Usage == nil {
		return usage
	}
	u := response.Usage
	cached, reasoning := 0, 0
	if u.InputTokensDetails != nil {
		cached = u.InputTokensDetails.CachedTokens
	}
	if u.OutputTokensDetails != nil {
		reasoning = u.OutputTokensDetails.ReasoningTokens
	}
	miss := u.InputTokens - cached
	if miss < 0 {
		miss = 0
	}
	total := u.TotalTokens
	if total == 0 {
		total = u.InputTokens + u.OutputTokens
	}
	return &provider.Usage{PromptTokens: u.InputTokens, CompletionTokens: u.OutputTokens, TotalTokens: total, CacheHitTokens: cached, CacheMissTokens: miss, ReasoningTokens: reasoning}
}

func authErrorFromResponse(c *client, responseError *sseError) error {
	if responseError == nil {
		return nil
	}
	value := strings.ToLower(responseError.Code + " " + responseError.Message)
	if !strings.Contains(value, "auth") && !strings.Contains(value, "api key") && !strings.Contains(value, "unauthorized") && !strings.Contains(value, "forbidden") && !strings.Contains(value, "permission") {
		return nil
	}
	status := http.StatusUnauthorized
	if strings.Contains(value, "forbidden") || strings.Contains(value, "permission") {
		status = http.StatusForbidden
	}
	return &provider.AuthError{Provider: c.name, KeyEnv: c.keyEnv, KeySource: c.keySource, Status: status, HasKey: c.apiKey != "", Body: responseError.Message}
}

type sseEvent struct {
	Type         string       `json:"type"`
	Delta        string       `json:"delta"`
	Text         string       `json:"text"`
	Arguments    string       `json:"arguments"`
	ItemID       string       `json:"item_id"`
	ContentIndex int          `json:"content_index"`
	Item         *sseItem     `json:"item"`
	Response     *sseResponse `json:"response"`
}

type sseItem struct {
	ID, Type, CallID, Name, Arguments string
}

func (i *sseItem) UnmarshalJSON(data []byte) error {
	var wire struct {
		ID        string `json:"id"`
		Type      string `json:"type"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*i = sseItem{ID: wire.ID, Type: wire.Type, CallID: wire.CallID, Name: wire.Name, Arguments: wire.Arguments}
	return nil
}

type sseResponse struct {
	ID                string            `json:"id"`
	Usage             *sseUsage         `json:"usage"`
	Error             *sseError         `json:"error"`
	IncompleteDetails incompleteDetails `json:"incomplete_details"`
}

type incompleteDetails struct {
	Reason string `json:"reason"`
}
type sseError struct {
	Message string `json:"message"`
	Code    string `json:"code"`
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
