// Package responses implements the OpenAI Responses API wire protocol.
// DeepSeek uses it statelessly and requires the complete input history on every
// request; compatible stateful endpoints may opt into previous_response_id.
package responses

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/provider"
	provideropenai "reasonix/internal/model/openai"
)

const (
	defaultStreamIdleTimeout     = provider.StreamIdleTimeout
	maxReplayableSearchItemBytes = 512 * 1024
)

func init() {
	provider.Register("responses", newFromConfig)
	provider.Register("dashscope-responses", newFromConfig)
}

func newFromConfig(cfg provider.Config) (provider.Provider, error) {
	effort, _ := cfg.Extra["effort"].(string)
	mode, _ := cfg.Extra["mode"].(string)
	webSearch, _ := cfg.Extra["web_search"].(bool)
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
	maxOutputTokens, _ := cfg.Extra["max_output_tokens"].(int)
	requestURL, _ := cfg.Extra["request_url"].(string)
	reasoningModes, _ := cfg.Extra["reasoning_modes"].(map[string]string)
	return New(Config{
		Name: cfg.Name, APIKey: cfg.APIKey, APIKeyFunc: cfg.APIKeyFunc, BaseURL: cfg.BaseURL, Model: cfg.Model,
		Effort: effort, Mode: mode, Stateful: stateful, WebSearch: webSearch, Proxy: proxy,
		KeyEnv: keyEnv, KeySource: keySource, MaxOutputTokens: maxOutputTokens, RequestURL: requestURL,
		ReasoningModes: reasoningModes,
		// Extra 原样透传：vision 等能力开关由调用方（boot/CLI）写入
		// cfg.Extra，factory 若丢弃则 New() 读不到（评审 #7234 第 3 点）。
		Extra: cfg.Extra,
	}), nil
}

// Config holds Responses API provider settings.
type Config struct {
	Name              string
	APIKey            string
	APIKeyFunc        func() string // asked per request; an empty answer falls back to APIKey
	BaseURL           string
	Model             string
	Effort            string
	Mode              string // stateful | stateless; empty uses vendor detection.
	Stateful          *bool  // legacy form of Mode; nil preserves vendor detection.
	WebSearch         bool   // expose the provider-executed web_search tool.
	Proxy             netclient.ProxySpec
	KeyEnv, KeySource string
	RequestURL        string // optional exact Responses request URL; empty derives from BaseURL
	// ReasoningModes maps a model mode id to the reasoning.mode it sends; a
	// request naming any other mode sends none.
	ReasoningModes map[string]string
	// MaxOutputTokens is the total provider output budget. Zero enables Reasonix's
	// 32K reasoning safety default on official DeepSeek and otherwise omits the
	// field; thinking-disabled DeepSeek requests and negative values omit it.
	MaxOutputTokens int
	// SessionCache controls DashScope's opt-in header. The header is never sent
	// to non-DashScope endpoints even when this value is true.
	SessionCache *bool
	// Extra carries kind-specific options; "vision" (bool) enables embedding
	// attached Images as input_image parts on user turns.
	Extra map[string]any
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
	if capabilitiesFor(DetectVendor(c.BaseURL)).stateless {
		return "stateless"
	}
	return "stateful"
}

// DetectVendor lives in vendor.go (capabilities table): it covers dashscope/
// deepseek (incl. eu.deepseek.com) / mimo via exact-host matching.

type client struct {
	name, keyEnv, keySource            string
	apiKey                             func() string
	baseURL, requestURL, model, effort string
	vendor, mode                       string
	caps                               vendorCapabilities
	webSearch                          bool
	session                            sessionHeaders
	maxOutputTokens                    int
	vision                             bool // model accepts image input; embed Images as input_image parts
	reasoningModes                     map[string]string
	http                               *http.Client
	idleTimeout                        time.Duration // SSE stall watchdog window; per-provider idle_timeout_seconds, else defaultStreamIdleTimeout
	authed                             atomic.Bool

	mu                   sync.Mutex
	lastResponseID       string
	expectedPrefixDigest string
}

// sessionHeaders are the per-conversation routing headers a gateway may ask
// for, fixed for the client's lifetime.
type sessionHeaders struct {
	dashScopeCache bool   // x-dashscope-session-cache, where the vendor offers it
	openCode       string // x-opencode-session, sent only to OpenCode Go
}

// New creates a Responses API provider.
func New(cfg Config) provider.Provider {
	vendor := DetectVendor(cfg.BaseURL)
	cap := capabilitiesFor(vendor)
	maxOutputTokens := cfg.MaxOutputTokens
	// max_output_tokens=0 is automatic. Known vendors use the 16K/32K/64K ladder;
	// thinking-disabled DeepSeek still gets the ordinary 16K auto budget.
	// 128K is never chosen automatically. Compact_ratio is independent.
	if maxOutputTokens == 0 && cap.defaultMaxOutputTokens > 0 {
		if vendor == "deepseek" || vendor == "mimo" {
			maxOutputTokens = responsesAutoOutputBudget(vendor, cfg.Effort)
		} else {
			maxOutputTokens = cap.defaultMaxOutputTokens
		}
	}
	sessionCache := cap.sessionCacheHeader
	if cfg.SessionCache != nil {
		sessionCache = *cfg.SessionCache
	}
	vision, _ := cfg.Extra["vision"].(bool)
	// DeepSeek serves text-only and image-taking models on the same endpoint.
	// Keep the boundary model-scoped so stale metadata cannot send input_image
	// parts to ordinary Flash, while the documented Vision model can use them.
	if vendor == "deepseek" {
		vision = vision && provideropenai.DeepSeekTakesImages(cfg.Model)
	}
	httpClient := &http.Client{Timeout: 300 * time.Second}
	if built, err := netclient.NewHTTPClient(cfg.Proxy, netclient.TransportOptions{
		DialTimeout: 30 * time.Second, KeepAlive: 30 * time.Second,
		TLSHandshakeTimeout: 15 * time.Second, ResponseHeaderTimeout: provider.IdleTimeoutFromExtra(cfg.Extra),
	}); err == nil {
		httpClient = built
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if reject, _ := cfg.Extra["reject_redirects"].(bool); reject {
		httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	requestURL := strings.TrimSpace(cfg.RequestURL)
	if requestURL == "" || provider.EndpointOverrideRepeatsBase(requestURL, baseURL) {
		requestURL = baseURL + "/responses"
	}
	return &client{
		name: cfg.Name, apiKey: cfg.apiKeyResolver(), keyEnv: cfg.KeyEnv, keySource: cfg.KeySource,
		baseURL: baseURL, requestURL: requestURL, model: cfg.Model, effort: cfg.Effort,
		vendor: vendor, caps: cap, mode: cfg.mode(), session: sessionHeaders{dashScopeCache: sessionCache, openCode: provider.NewOpenCodeSessionID()}, webSearch: cfg.WebSearch, maxOutputTokens: maxOutputTokens,
		vision: vision, reasoningModes: cfg.ReasoningModes,
		http: httpClient, idleTimeout: provider.IdleTimeoutFromExtra(cfg.Extra),
	}
}

func responsesReasoningDisabled(effort string) bool {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "none", "disabled", "off":
		return true
	default:
		return false
	}
}

// responsesAutoOutputBudget mirrors Chat Completions: DeepSeek defaults empty
// effort to high (64K), low stays 32K, thinking disabled is ordinary 16K.
// Never auto-selects 128K.
func responsesAutoOutputBudget(vendor, effort string) int {
	if responsesReasoningDisabled(effort) {
		return provider.AutoOutputBudget(false, effort)
	}
	e := strings.ToLower(strings.TrimSpace(effort))
	if vendor == "deepseek" && (e == "" || e == "auto") {
		e = "high"
	}
	return provider.AutoOutputBudget(true, e)
}

func (c *client) Name() string { return c.name }

// RequiresToolCallReasoning tells the agent to preserve stateless vendors'
// reasoning on assistant tool-call turns so the follow-up can replay it.
// DeepSeek and MiMo document this requirement for multi-turn tool calls.
func (c *client) RequiresToolCallReasoning() bool {
	return c.caps.toolCallReasoning
}

func (c *client) MissingToolCallReasoningWarningIdentity() string {
	if c == nil {
		return ""
	}
	return strings.Join([]string{
		"responses", strings.TrimSpace(c.name), strings.TrimSpace(c.requestURL),
		strings.TrimSpace(c.model), strings.TrimSpace(c.vendor), strings.TrimSpace(c.mode), strings.TrimSpace(c.effort),
	}, "\x00")
}

// WarnOnMissingToolCallReasoning reports a tool_calls turn that arrived
// without reasoning only for vendors whose endpoint reliably emits it.
// DeepSeek's official API emits tool-call reasoning for its pro-tier models,
// so a missing chain-of-thought there is a real degradation worth one warning.
// MiMo documents reasoning alongside tool calls but does not guarantee it on
// every round (observed: mimo-v2.5-pro tool-call turn with empty reasoning),
// so a missing chain-of-thought is endpoint-conditional, not a degradation
// signal — silence the warning. Capability-driven (review #7234):
// toolCallReasoning=false vendors (DashScope) never warn — no round-trip
// contract; singleSegmentReasoning=true vendors (MiMo) never warn — their
// tool-call thinking is a single optional segment. Only multi-segment
// thinking vendors that require replay (DeepSeek) warn, scoped to non-flash.
func (c *client) WarnOnMissingToolCallReasoning() bool {
	if !c.caps.toolCallReasoning || c.caps.singleSegmentReasoning {
		return false
	}
	model := strings.ToLower(strings.TrimSpace(c.model))
	// Flash-tier DeepSeek models do not emit tool-call reasoning (same carve
	// as openai.go expectsDeepSeekToolCallReasoning).
	return !strings.Contains(model, "flash")
}

func (c *client) sendOpts() provider.SendOptions {
	return provider.SendOptions{Provider: c.name, KeyEnv: c.keyEnv, KeySource: c.keySource, KeyPresent: c.apiKey() != "", RetryAuth: c.authed.Load()}
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
	requestCtx := provider.WithRequestAttemptCounter(ctx)
	body, usedPrevious, wireMessages := c.buildRequestBody(req)
	resp, err := c.send(requestCtx, body)
	if err != nil && usedPrevious && isStalePreviousResponseError(err) {
		// A stateful response ID may expire server-side. Retrying once with full
		// history is safe because no response body has started streaming.
		c.ResetContext()
		body, _, wireMessages = c.buildRequestBody(req)
		resp, err = c.send(requestCtx, body)
	}
	if err != nil {
		return nil, err
	}
	c.authed.Store(true)
	out := make(chan provider.Chunk, 64)
	go c.readStream(requestCtx, resp, out, wireMessages)
	return out, nil
}

func (c *client) send(ctx context.Context, body map[string]any) (*http.Response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("responses: marshal request: %w", err)
	}
	newRequest := func(ctx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.requestURL, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.apiKey())
		if c.caps.sessionCacheHeader && c.session.dashScopeCache {
			req.Header.Set("x-dashscope-session-cache", "enable")
		}
		provider.ApplyOpenCodeGoIdentity(req, c.session.openCode)
		provider.ApplyClientIdentity(req)
		return req, nil
	}
	return provider.SendWithRetry(ctx, c.http, c.sendOpts(), newRequest)
}

// isStalePreviousResponseError reads the error object, not its prose: the
// protocol names the field it rejected in `param`, and a server that names
// nothing is not telling us the id expired.
func isStalePreviousResponseError(err error) bool {
	var apiErr *provider.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
		return false
	}
	var body struct {
		Error struct {
			Param string `json:"param"`
			Code  string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(apiErr.Body), &body) != nil {
		return false
	}
	return body.Error.Param == "previous_response_id" || body.Error.Code == "previous_response_not_found"
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
	reasoning := map[string]any{}
	if effort != "" {
		reasoning["effort"] = effort
	}
	if mode := c.reasoningModes[req.Mode]; req.Mode != "" && mode != "" {
		reasoning["mode"] = mode
	}
	if len(reasoning) > 0 {
		body["reasoning"] = reasoning
	}
	maxOutputTokens := req.MaxTokens
	if maxOutputTokens == 0 {
		maxOutputTokens = c.maxOutputTokens
	}
	if maxOutputTokens == 0 && c.caps.defaultMaxOutputTokens > 0 {
		if c.vendor == "deepseek" || c.vendor == "mimo" {
			maxOutputTokens = responsesAutoOutputBudget(c.vendor, c.effort)
		} else {
			maxOutputTokens = c.caps.defaultMaxOutputTokens
		}
	}
	if maxOutputTokens > 0 {
		body["max_output_tokens"] = maxOutputTokens
	}
	if req.ResponseFormat != nil && req.ResponseFormat.Type != "" {
		// Structured output: Responses text.format. MiMo/DashScope/OpenAI
		// all accept {"text":{"format":{"type":"json_object"}}}. The model
		// only emits JSON when the instructions also demand it.
		body["text"] = map[string]any{
			"format": map[string]any{"type": req.ResponseFormat.Type},
		}
	}
	if req.Temperature != nil && !c.caps.ignoresTemperature {
		body["temperature"] = *req.Temperature
	}
	if c.webSearch || len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools)+1)
		// Keep the server tool first and stable across turns. DeepSeek executes
		// this tool itself; ordinary Reasonix tools remain function entries.
		if c.webSearch {
			tools = append(tools, map[string]any{"type": "web_search"})
		}
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
		c.conversationDigest(messages[:len(messages)-1]) == expectedDigest {
		body["input"] = messages[len(messages)-1].Content
		body["previous_response_id"] = previousID
		return body, true, messages
	}

	body["input"] = messagesToInput(rest, c.vision, c.webSearch, c.caps)
	return body, false, messages
}

func splitInstructions(messages []provider.Message) (string, []provider.Message) {
	if len(messages) == 0 || messages[0].Role != provider.RoleSystem {
		return "", messages
	}
	return messages[0].Content, messages[1:]
}

func messagesToInput(messages []provider.Message, vision, replayWebSearchItems bool, caps vendorCapabilities) []map[string]any {
	input := make([]map[string]any, 0, len(messages)*2)
	// A tool's images follow its run of outputs as a user turn: input_image on a
	// user turn is the shape these vendors take, and a turn between two outputs
	// would split the calls from their results.
	var toolImages []string
	flushToolImages := func() {
		if len(toolImages) == 0 {
			return
		}
		parts := make([]map[string]string, 0, len(toolImages)+1)
		parts = append(parts, map[string]string{"type": "input_text", "text": "Images returned by the preceding tool call(s):"})
		for _, url := range toolImages {
			parts = append(parts, map[string]string{"type": "input_image", "image_url": url})
		}
		input = append(input, map[string]any{"role": "user", "content": parts})
		toolImages = nil
	}
	for _, message := range messages {
		if message.Role != provider.RoleTool {
			flushToolImages()
		}
		switch message.Role {
		case provider.RoleSystem, provider.RoleUser:
			// Text-only turns keep the documented TextInput string shape.
			// Vision-capable user turns with attached images switch to the
			// InputItemList array form ({type:input_text} + {type:input_image})
			// so the text and every image ride the same message, matching the
			// MiMo/DashScope multimodal example. The system message is always
			// plain text: images only attach to user turns.
			if vision && message.Role == provider.RoleUser && len(message.Images) > 0 {
				parts := make([]map[string]string, 0, len(message.Images)+1)
				if message.Content != "" {
					parts = append(parts, map[string]string{"type": "input_text", "text": message.Content})
				}
				for _, url := range message.Images {
					parts = append(parts, map[string]string{"type": "input_image", "image_url": url})
				}
				input = append(input, map[string]any{"role": "user", "content": parts})
			} else {
				input = append(input, map[string]any{"role": string(message.Role), "content": message.Content})
			}
		case provider.RoleAssistant:
			if message.ReasoningContent != "" {
				// Reasoning items carry `content`; what `summary` looks
				// like beside it is the vendor's, and the default is the
				// published schema's own shape (see summaryShape).
				item := map[string]any{
					"type":    "reasoning",
					"content": []map[string]string{{"type": "reasoning_text", "text": message.ReasoningContent}},
				}
				if message.ReasoningID != "" && !caps.omitReasoningIdentity {
					// OpenAI Responses schema marks Reasoning.id required;
					// round-trip the provider-issued id when we captured one.
					item["id"] = message.ReasoningID
				}
				if message.ReasoningStatus != "" && !caps.omitReasoningIdentity {
					item["status"] = message.ReasoningStatus
				}
				switch caps.summary {
				case summaryEcho:
					item["summary"] = []map[string]string{{"type": "summary_text", "text": message.ReasoningContent}}
				case summaryOmit:
				case summaryEmpty:
					item["summary"] = []map[string]string{}
				}
				input = append(input, item)
			}
			if replayWebSearchItems {
				for _, raw := range message.ResponsesItems {
					if item, ok := decodeReplayableWebSearchItem(raw); ok {
						input = append(input, item)
					}
				}
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
			if vision {
				toolImages = append(toolImages, message.Images...)
			}
		}
	}
	flushToolImages()
	return input
}

func decodeReplayableWebSearchItem(raw json.RawMessage) (map[string]any, bool) {
	if len(raw) == 0 || len(raw) > maxReplayableSearchItemBytes || !json.Valid(raw) {
		return nil, false
	}
	var item map[string]any
	if err := json.Unmarshal(raw, &item); err != nil || item["type"] != "web_search_call" {
		return nil, false
	}
	id, _ := item["id"].(string)
	status, _ := item["status"].(string)
	if strings.TrimSpace(id) == "" || status != "completed" {
		return nil, false
	}
	return item, true
}

func (c *client) conversationDigest(messages []provider.Message) string {
	instructions, rest := splitInstructions(messages)
	// Digest must mirror the wire exactly: the stateful fast path compares
	// this against the previous request's input, so a mismatch would skip
	// previous_response_id and force a full replay (cache-hit loss). Use the
	// same vision and capability knobs as buildRequestBody.
	payload, _ := json.Marshal(struct {
		Instructions string           `json:"instructions,omitempty"`
		Input        []map[string]any `json:"input"`
	}{Instructions: instructions, Input: messagesToInput(rest, c.vision, c.webSearch, c.caps)})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
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
	miss := max(u.InputTokens-cached, 0)
	total := u.TotalTokens
	if total == 0 {
		total = u.InputTokens + u.OutputTokens
	}
	return &provider.Usage{PromptTokens: u.InputTokens, CompletionTokens: u.OutputTokens, TotalTokens: total, CacheHitTokens: cached, CacheMissTokens: miss, ReasoningTokens: reasoning}
}

// responsesAuthCodes maps the protocol's credential error codes to the status
// each stands for. A code outside it is an ordinary stream error: the message
// beside it is prose, and reading prose is how a classifier starts misfiring.
var responsesAuthCodes = map[string]int{
	"invalid_api_key":        http.StatusUnauthorized,
	"invalid_authentication": http.StatusUnauthorized,
	"authentication_error":   http.StatusUnauthorized,
	"permission_denied":      http.StatusForbidden,
	"account_deactivated":    http.StatusForbidden,
}

func authErrorFromResponse(c *client, responseError *sseError) error {
	if responseError == nil {
		return nil
	}
	status, ok := responsesAuthCodes[strings.ToLower(strings.TrimSpace(responseError.Code))]
	if !ok {
		return nil
	}
	return &provider.AuthError{Provider: c.name, KeyEnv: c.keyEnv, KeySource: c.keySource, Status: status, HasKey: c.apiKey() != "", Body: responseError.Message}
}

type sseEvent struct {
	Type      string `json:"type"`
	Delta     string `json:"delta"`
	Text      string `json:"text"`
	Refusal   string `json:"refusal"`
	Arguments string `json:"arguments"`
	// Code and Message ride the protocol's top-level `error` event, which is
	// not a response event and carries no response object to read them from.
	Code         string       `json:"code"`
	Message      string       `json:"message"`
	ItemID       string       `json:"item_id"`
	ContentIndex int          `json:"content_index"`
	Item         *sseItem     `json:"item"`
	Response     *sseResponse `json:"response"`
}

type sseItem struct {
	ID, Type, CallID, Name, Arguments, Status string
	Raw                                       json.RawMessage
}

func (i *sseItem) UnmarshalJSON(data []byte) error {
	var wire struct {
		ID        string `json:"id"`
		Type      string `json:"type"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
		Status    string `json:"status"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*i = sseItem{ID: wire.ID, Type: wire.Type, CallID: wire.CallID, Name: wire.Name, Arguments: wire.Arguments, Status: wire.Status, Raw: append(json.RawMessage(nil), data...)}
	return nil
}

type sseResponse struct {
	ID                string            `json:"id"`
	Output            []json.RawMessage `json:"output"`
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
