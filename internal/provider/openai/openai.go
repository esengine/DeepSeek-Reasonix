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
//   - official Kimi API + kimi-k3 preserves complete assistant messages and
//     uses K3's fixed-sampling/max_completion_tokens request shape.
//   - everything else (MiMo and other OpenAI-compatible gateways) uses the
//     vanilla reasoning_effort scale (low/medium/high), unless its config
//     declares a custom supported_efforts validation contract.
//
// See docs/REASONING_PROVIDERS.md for the per-backend protocol reference.
package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// maxPrefixContinuations keeps automatic recovery bounded. A second length
// finish is surfaced through the existing truncation notice instead of opening
// an unbounded (and billable) continuation loop against the Beta endpoint.
const maxPrefixContinuations = 1

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
	supportedEfforts, _ := cfg.Extra["supported_efforts"].([]string)
	// A meaningful explicit list is the endpoint's declared effort vocabulary;
	// auto remains implicit and is therefore ignored here.
	hasExplicitEfforts := hasExplicitSupportedEfforts(supportedEfforts)
	protocol, _ := cfg.Extra["reasoning_protocol"].(string)
	protocol = normalizeReasoningProtocol(protocol)
	chatURL, _ := cfg.Extra["chat_url"].(string)
	chatURL = normalizeChatURL(cfg.BaseURL, chatURL)
	prefixChatURL := deepSeekPrefixChatURL(chatURL)
	headers, _ := cfg.Extra["headers"].(map[string]string)
	extraBody, _ := cfg.Extra["extra_body"].(map[string]any)
	vision, _ := cfg.Extra["vision"].(bool)
	explicitModelVision, _ := cfg.Extra["vision_model_explicit"].(bool)
	officialDeepSeek := IsDeepSeek(cfg.BaseURL)
	// DeepSeek's official chat API accepts string message content only. Keep
	// this provider-boundary guard even though config capability resolution
	// normally prevents image attachments from reaching this layer. A positive
	// model-scoped capability can opt in without letting stale provider-wide
	// vision=true settings affect current text-only models.
	vision = vision && (!officialDeepSeek || explicitModelVision)
	visionDetail, _ := cfg.Extra["vision_detail"].(string)
	visionDetail = strings.ToLower(strings.TrimSpace(visionDetail))
	if visionDetail != "low" && visionDetail != "high" {
		visionDetail = "" // auto — omit the field
	}
	deepseek := protocol == "deepseek" || (protocol == "" && officialDeepSeek)
	maxOutputTokens, _ := cfg.Extra["max_output_tokens"].(int)
	deepseekV4Flash := strings.EqualFold(strings.TrimSpace(cfg.Model), "deepseek-v4-flash")
	minimax := protocol == "" && IsMiniMax(cfg.BaseURL)
	zhipu := protocol == "glm" || (protocol == "" && IsZhipu(cfg.BaseURL))
	longcat := protocol == "" && IsLongCat(cfg.BaseURL)
	ollamaCloud := protocol == "" && IsOllamaCloud(cfg.BaseURL)
	kimiK3 := IsKimiAPI(cfg.BaseURL) && strings.EqualFold(strings.TrimSpace(cfg.Model), "kimi-k3")
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
			if hasExplicitEfforts && !supportsEffort(supportedEfforts, effort) {
				return nil, fmt.Errorf("openai: provider %q: effort %q is not listed in supported_efforts: %v", name, effort, supportedEfforts)
			}
			// DeepSeek can turn thinking off too; route through thinking.type and
			// drop the depth hint so the wire carries thinking.type=disabled only.
			effort = ""
			thinkingType = "disabled"
		default:
			if hasExplicitEfforts {
				// A provider that declares supported_efforts defines the endpoint's
				// complete effort vocabulary. Honor that list for compatible DeepSeek
				// request shapes instead of applying the built-in official scale.
				if !supportsEffort(supportedEfforts, effort) {
					return nil, fmt.Errorf("openai: provider %q: effort %q is not listed in supported_efforts: %v", name, effort, supportedEfforts)
				}
				break
			}
			switch effort {
			case "low":
				if !deepseekV4Flash {
					return nil, fmt.Errorf("openai: provider %q uses DeepSeek thinking; effort low requires deepseek-v4-flash or explicit supported_efforts", name)
				}
			case "high", "max":
			default:
				return nil, fmt.Errorf("openai: provider %q uses DeepSeek thinking; effort must be low, high, max, or disabled", name)
			}
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
		if hasExplicitEfforts {
			// Explicit endpoint metadata overrides the generic OpenAI enum and its
			// legacy max-to-high compatibility clamp.
			if !supportsEffort(supportedEfforts, effort) {
				return nil, fmt.Errorf("openai: provider %q: effort %q is not listed in supported_efforts: %v", name, effort, supportedEfforts)
			}
			break
		}
		// Non-DeepSeek backends use OpenAI's reasoning_effort scale (low/medium/
		// high) by default. Without an explicit provider vocabulary, max remains
		// clamped to the OpenAI ceiling because MiMo and similar backends reject it.
		switch effort {
		case "max":
			effort = "high"
		case "low", "medium", "high":
		default:
			return nil, fmt.Errorf("openai: provider %q: effort must be low, medium, or high", name)
		}
	}
	// The automatic cap protects DeepSeek reasoning, not ordinary long-form
	// output. Preserve an explicit user budget in either mode, but leave a
	// thinking-disabled request uncapped unless the user configured one.
	if maxOutputTokens == 0 && officialDeepSeek && thinkingType != "disabled" {
		maxOutputTokens = provider.DefaultReasoningOutputTokens
	}
	httpClient, err := newHTTPClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("openai: network: %w", err)
	}
	return &client{
		name:            name,
		apiKey:          cfg.APIKey,
		keyEnv:          keyEnv,
		keySource:       keySource,
		baseURL:         strings.TrimRight(cfg.BaseURL, "/"),
		chatURL:         chatURL,
		prefixChatURL:   prefixChatURL,
		headers:         cleanCustomHeaders(headers),
		extraBody:       cleanExtraBody(extraBody),
		model:           normalizeModelID(cfg.BaseURL, cfg.Model),
		deepseek:        deepseek,
		minimax:         minimax,
		zhipu:           zhipu,
		longcat:         longcat,
		kimiK3:          kimiK3,
		mimo:            IsMiMo(cfg.BaseURL),
		thinkingType:    thinkingType,
		vision:          vision,
		visionDetail:    visionDetail,
		maxOutputTokens: maxOutputTokens,
		effort:          effort,
		http:            httpClient,
		idleTimeout:     defaultStreamIdleTimeout,
	}, nil
}

func supportsEffort(levels []string, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	for _, level := range levels {
		if strings.ToLower(strings.TrimSpace(level)) == want {
			return true
		}
	}
	return false
}

func hasExplicitSupportedEfforts(levels []string) bool {
	for _, level := range levels {
		level = strings.ToLower(strings.TrimSpace(level))
		if level != "" && level != "auto" {
			return true
		}
	}
	return false
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
	name            string
	apiKey          string
	keyEnv          string // api_key_env name, surfaced in auth errors
	keySource       string // source of keyEnv, surfaced in auth errors
	baseURL         string
	chatURL         string
	prefixChatURL   string // official DeepSeek Beta endpoint; empty for custom gateways
	headers         map[string]string
	extraBody       map[string]any
	model           string
	http            *http.Client
	deepseek        bool
	minimax         bool          // true for api.minimaxi.com — emits MiniMax-M3's thinking knob instead of reasoning_effort
	zhipu           bool          // true for Zhipu GLM (bigmodel.cn / z.ai) — gates thinking via thinking.type, ignores reasoning_effort
	longcat         bool          // true for LongCat — gates thinking via thinking.type, ignores reasoning_effort
	kimiK3          bool          // true only for kimi-k3 on Moonshot's official direct API hosts
	mimo            bool          // true for MiMo — upgrades legacy tuple schemas to Draft 2020-12
	thinkingType    string        // explicit `thinking` config override (enabled|disabled); "" = no override
	vision          bool          // model accepts image input — embed attached images as image_url parts
	visionDetail    string        // image_url detail hint (low|high); "" = auto/omit
	maxOutputTokens int           // configured/default total output budget; <=0 omits the optional field
	effort          string        // reasoning_effort for OpenAI; thinking.type for MiniMax; "" = auto/provider default
	idleTimeout     time.Duration // SSE stall watchdog window; defaultStreamIdleTimeout unless a test overrides
	authed          atomic.Bool   // a request has succeeded — gate transient-401 retry
}

func (c *client) Name() string { return c.name }

func (c *client) RequiresToolCallReasoning() bool {
	return c != nil && c.deepseek && c.thinkingType != "disabled"
}

func (c *client) RequiresReasoningRoundTrip() bool {
	return c != nil && (c.kimiK3 || c.glmThinkingEnabled())
}

func (c *client) WarnOnMissingToolCallReasoning() bool {
	return c.RequiresToolCallReasoning() && expectsDeepSeekToolCallReasoning(c.model, c.thinkingType)
}

func (c *client) glmThinkingEnabled() bool {
	if c == nil || !c.zhipu {
		return false
	}
	t := c.effort
	if c.thinkingType != "" {
		t = c.thinkingType
	}
	return t != "disabled"
}

func expectsDeepSeekToolCallReasoning(model, thinkingType string) bool {
	if strings.EqualFold(strings.TrimSpace(thinkingType), "enabled") {
		return true
	}
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.Contains(model, "deepseek-v4-flash") ||
		strings.Contains(model, "deepseek-v4-pro") ||
		strings.Contains(model, "deepseek-v3.2") ||
		strings.Contains(model, "deepseek-reasoner") ||
		strings.Contains(model, "deepseek-r1")
}

func (c *client) MissingToolCallReasoningWarningIdentity() string {
	if c == nil {
		return ""
	}
	protocol := "openai"
	if c.deepseek {
		protocol = "deepseek"
	}
	return strings.Join([]string{
		"openai", strings.TrimSpace(c.name), strings.TrimSpace(c.baseURL),
		strings.TrimSpace(c.model), protocol, strings.TrimSpace(c.thinkingType), strings.TrimSpace(c.effort),
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

func normalizeReasoningProtocol(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "deepseek", "glm", "openai", "none":
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
	case "model", "messages", "tools", "stream", "stream_options", "temperature", "max_tokens", "max_completion_tokens", "max_output_tokens", "reasoning_effort", "thinking":
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
	stream, err := c.openStream(ctx, c.chatURL, c.buildRequest(req), req.Tools)
	if err != nil {
		return nil, err
	}
	if c.prefixChatURL == "" {
		return stream, nil
	}

	out := make(chan provider.Chunk)
	go c.streamWithPrefixContinuation(ctx, req, stream, out)
	return out, nil
}

func (c *client) openStream(ctx context.Context, targetURL string, wireReq chatRequest, tools []provider.ToolSchema) (<-chan provider.Chunk, error) {
	requestCtx := provider.WithRequestAttemptCounter(ctx)
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	if err := json.NewEncoder(buf).Encode(wireReq); err != nil {
		bufPool.Put(buf)
		return nil, fmt.Errorf("%s: marshal request: %w", c.name, err)
	}
	body := make([]byte, buf.Len())
	copy(body, buf.Bytes())
	bufPool.Put(buf)

	newReq := func(ctx context.Context) (*http.Request, error) {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		applyAPIKeyHeader(httpReq.Header, c.baseURL, c.apiKey)
		httpReq.Header.Set("Accept", "text/event-stream")
		applyCustomHeaders(httpReq.Header, c.headers)
		return httpReq, nil
	}
	resp, err := provider.SendWithRetry(requestCtx, c.http, c.sendOpts(), newReq)
	if err != nil {
		return nil, provider.AnnotateToolSchemaError(err, tools)
	}
	c.authed.Store(true)

	out := make(chan provider.Chunk)
	// Body-phase stream cuts surface as StreamInterruptedError so the Agent
	// can replay the exact frozen request. Connection+header retries stay in
	// SendWithRetry; providers must not stack a second body-retry budget.
	go c.streamOnce(requestCtx, resp, out)
	return out, nil
}

// streamWithPrefixContinuation makes a DeepSeek Beta continuation look like one
// ordinary provider stream. Text/reasoning stays live, while usage is folded
// across both requests so cost and cache accounting remain truthful. If the
// Beta request fails before emitting anything, the original truncated response
// is kept and its finish_reason=length reaches the agent's existing warning.
func (c *client) streamWithPrefixContinuation(ctx context.Context, req provider.Request, current <-chan provider.Chunk, out chan<- provider.Chunk) {
	defer close(out)

	var fullText, fullReasoning strings.Builder
	var totalUsage *provider.Usage
	continuations := 0

	for {
		var currentUsage *provider.Usage
		currentHadTool := false
		currentEmitted := false

		for chunk := range current {
			switch chunk.Type {
			case provider.ChunkText:
				fullText.WriteString(chunk.Text)
				currentEmitted = currentEmitted || chunk.Text != ""
				if !sendChunk(ctx, out, chunk) {
					return
				}
			case provider.ChunkReasoning:
				fullReasoning.WriteString(chunk.Text)
				currentEmitted = currentEmitted || chunk.Text != ""
				if !sendChunk(ctx, out, chunk) {
					return
				}
			case provider.ChunkToolCallStart, provider.ChunkToolCallArgsDelta, provider.ChunkToolCall:
				currentHadTool = true
				currentEmitted = true
				if !sendChunk(ctx, out, chunk) {
					return
				}
			case provider.ChunkUsage:
				currentUsage = mergeUsage(currentUsage, chunk.Usage, false)
			case provider.ChunkDone:
				// The wrapper emits one final Done after any continuation.
			case provider.ChunkError:
				// A Beta failure before any continuation bytes is a safe fallback:
				// the already-streamed first response remains visible and its
				// length finish reason triggers the normal truncation warning.
				if continuations > 0 && !currentEmitted && ctx.Err() == nil {
					emitUsageAndDone(ctx, out, totalUsage)
					return
				}
				_ = sendChunk(ctx, out, chunk)
				return
			default:
				if !sendChunk(ctx, out, chunk) {
					return
				}
			}
		}

		totalUsage = mergeUsage(totalUsage, currentUsage, true)
		if continuations >= maxPrefixContinuations ||
			currentUsage == nil || currentUsage.FinishReason != "length" ||
			currentHadTool ||
			(fullText.Len() == 0 && (c.thinkingType == "disabled" || fullReasoning.Len() == 0)) {
			emitUsageAndDone(ctx, out, totalUsage)
			return
		}

		prefixReq := c.buildPrefixRequest(req, fullText.String(), fullReasoning.String())
		next, err := c.openStream(ctx, c.prefixChatURL, prefixReq, req.Tools)
		if err != nil {
			emitUsageAndDone(ctx, out, totalUsage)
			return
		}
		continuations++
		current = next
	}
}

func emitUsageAndDone(ctx context.Context, out chan<- provider.Chunk, usage *provider.Usage) {
	if usage != nil && !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkUsage, Usage: usage}) {
		return
	}
	_ = sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkDone})
}

// mergeUsage folds token counters. countRequests is false for multiple usage
// chunks from one HTTP stream (keep its request count), and true when combining
// distinct prefix-continuation requests (sum their request counts).
func mergeUsage(total, next *provider.Usage, countRequests bool) *provider.Usage {
	if next == nil {
		return total
	}
	if total == nil {
		clone := *next
		return &clone
	}
	totalRequests := usageRequestCount(total)
	nextRequests := usageRequestCount(next)
	total.PromptTokens += next.PromptTokens
	total.CompletionTokens += next.CompletionTokens
	total.TotalTokens += next.TotalTokens
	total.CacheHitTokens += next.CacheHitTokens
	total.CacheMissTokens += next.CacheMissTokens
	total.CacheWriteTokens += next.CacheWriteTokens
	total.CacheWriteBilledTokens += next.CacheWriteBilledTokens
	total.ReasoningTokens += next.ReasoningTokens
	if countRequests {
		total.RequestCount = totalRequests + nextRequests
	} else if nextRequests > totalRequests {
		total.RequestCount = nextRequests
	} else {
		total.RequestCount = totalRequests
	}
	total.FinishReason = next.FinishReason
	return total
}

func usageRequestCount(usage *provider.Usage) int {
	if usage != nil && usage.RequestCount > 0 {
		return usage.RequestCount
	}
	return 1
}

// streamOnce drives a single body read. Mid-stream transport cuts become
// StreamInterruptedError so the Agent can commit-or-replay; providers no longer
// replay the body themselves (that would stack retry budgets with the Agent).
func (c *client) streamOnce(ctx context.Context, resp *http.Response, out chan<- provider.Chunk) {
	defer close(out)
	_, err := c.readStream(ctx, resp, out)
	if err == nil {
		return
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkError, Err: err})
		return
	}
	if provider.IsConnReset(err) {
		sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkError, Err: provider.StreamInterrupt(err, provider.ClassifyStreamInterrupt(err))})
		return
	}
	sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkError, Err: err})
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
	msgs := make([]chatMessage, 0, len(src))
	// Images returned by tool calls can't ride in the tool message itself — the
	// OpenAI API accepts only text content parts under role "tool" — so they are
	// carried by a synthetic user message injected after the turn's full run of
	// tool results, before the next non-tool message (splitting a tool-result
	// run would break the API's tool-call pairing validation).
	var pendingToolImages []string
	flushToolImages := func() {
		if len(pendingToolImages) == 0 {
			return
		}
		msgs = append(msgs, chatMessage{
			Role:    "user",
			Content: imageContentParts("Images returned by the preceding tool call(s):", pendingToolImages, c.visionDetail),
		})
		pendingToolImages = nil
	}
	for _, m := range src {
		if m.Role != provider.RoleTool {
			flushToolImages()
		}
		cm := chatMessage{
			Role:       string(m.Role),
			ToolCallID: m.ToolCallID,
		}
		if m.Role == provider.RoleTool {
			// Always send the tool message's name, even when empty: strict
			// backends (MiMo) 400 a tool result without the key (#4711).
			name := m.Name
			cm.Name = &name
		}
		// DeepSeek thinking mode 400s an assistant tool_calls turn whose
		// reasoning_content KEY is absent from the request JSON ("reasoning_content
		// … must be passed back"). The API accepts an empty string, and only
		// validates turns after the last user message, but emitting the field on
		// every tool_calls turn is uniform and verified accepted — so always send
		// it (empty included) rather than fail the request when reasoning was lost
		// upstream (e.g. a gateway renamed the field). With thinking disabled the
		// API tolerates every shape, so keep the exact pre-fix bytes there: send
		// the key only when a thinking-mode round left reasoning in the history
		// (dropping it would invalidate the prompt-cache prefix of mixed
		// thinking-on→off sessions for no gain).
		if m.Role == provider.RoleAssistant {
			switch {
			case c.kimiK3 && (m.ReasoningContent != "" || len(m.ToolCalls) > 0):
				// Kimi K3 requires the complete assistant message on multi-turn
				// and tool-call requests, including provider-issued reasoning.
				cm.ReasoningContent = &m.ReasoningContent
			case c.deepseek && len(m.ToolCalls) > 0:
				if c.RequiresToolCallReasoning() || m.ReasoningContent != "" {
					cm.ReasoningContent = &m.ReasoningContent
				}
			case c.zhipu && m.ReasoningContent != "":
				// GLM interleaved and preserved thinking require provider-issued
				// reasoning content to be returned unchanged in later history. Keep
				// an existing value even after thinking is turned off so an
				// enabled→disabled session retains its valid history bytes.
				cm.ReasoningContent = &m.ReasoningContent
			}
		}
		for _, tc := range m.ToolCalls {
			wire := chatToolCall{ID: tc.ID, Type: "function"}
			wire.Function.Name = tc.Name
			wire.Function.Arguments = tc.Arguments
			if tc.ThoughtSignature != "" && usesGeminiThoughtSignatures(c.baseURL, c.model) {
				// Gemini's current OpenAI compatibility schema carries the
				// opaque signature beside the function payload. Keep the
				// legacy function.thought_signature field decode-only below so
				// older gateways remain readable without sending an unknown
				// function parameter to current Google endpoints.
				wire.ExtraContent = &chatToolCallExtraContent{}
				wire.ExtraContent.Google.ThoughtSignature = tc.ThoughtSignature
			}
			cm.ToolCalls = append(cm.ToolCalls, wire)
		}
		switch {
		case c.vision && m.Role == provider.RoleUser && len(m.Images) > 0:
			cm.Content = imageContentParts(m.Content, m.Images, c.visionDetail)
		case m.Role != provider.RoleAssistant || len(cm.ToolCalls) == 0 || m.Content != "":
			cm.Content = m.Content
		}
		msgs = append(msgs, cm)
		if c.vision && m.Role == provider.RoleTool {
			pendingToolImages = append(pendingToolImages, m.Images...)
		}
	}
	flushToolImages()

	var tools []chatTool
	for _, t := range req.Tools {
		parameters := t.Parameters
		if len(parameters) == 0 {
			parameters = provider.CanonicalizeSchema(nil)
		}
		if c.mimo {
			parameters = provider.NormalizeLegacyTupleItemsForDraft202012(parameters)
		}
		tools = append(tools, chatTool{
			Type:     "function",
			Function: chatFunction{Name: t.Name, Description: t.Description, Parameters: parameters},
		})
	}

	maxOutputTokens := req.MaxTokens
	if maxOutputTokens == 0 {
		maxOutputTokens = c.maxOutputTokens
	}
	if maxOutputTokens < 0 {
		maxOutputTokens = 0
	}
	out := chatRequest{
		Model:           c.model,
		Messages:        msgs,
		Tools:           tools,
		Stream:          true,
		StreamOptions:   &streamOptions{IncludeUsage: true},
		Temperature:     req.Temperature,
		MaxTokens:       maxOutputTokens,
		ReasoningEffort: c.effort,
		ExtraBody:       c.extraBody,
	}
	switch {
	case c.kimiK3:
		// K3 fixes its sampling values and recommends omitting them. It also
		// names the output budget max_completion_tokens rather than max_tokens.
		out.Temperature = nil
		out.MaxTokens = 0
		out.MaxCompletionTokens = maxOutputTokens
		out.ExtraBody = omitExtraBodyFields(out.ExtraBody,
			"temperature", "top_p", "n", "presence_penalty", "frequency_penalty", "max_completion_tokens")
	case IsOpenAI(c.baseURL):
		// OpenAI's current Chat Completions contract replaces max_tokens with
		// max_completion_tokens, which includes visible and reasoning tokens and
		// is required by o-series models. Compatible gateways retain max_tokens.
		out.MaxTokens = 0
		out.MaxCompletionTokens = maxOutputTokens
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

func (c *client) buildPrefixRequest(req provider.Request, content, reasoning string) chatRequest {
	out := c.buildRequest(req)
	prefix := chatMessage{Role: "assistant", Content: content, Prefix: true}
	if c.deepseek && c.thinkingType != "disabled" {
		prefix.ReasoningContent = &reasoning
	}
	out.Messages = append(out.Messages, prefix)
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
	argBucket := map[int]int{}
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
		if data == "" {
			continue
		}

		var sr streamResponse
		if err := json.Unmarshal([]byte(data), &sr); err != nil {
			return emitted, provider.StreamDecodeError(c.name, data, err)
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
			provider.ApplyRequestAttemptCount(ctx, u)
			emitted = true
			if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkUsage, Usage: u}) {
				return emitted, ctx.Err()
			}
		}
		if len(sr.Choices) == 0 {
			continue
		}

		delta := sr.Choices[0].Delta
		reasoningDelta := delta.ReasoningContent
		if reasoningDelta == "" {
			reasoningDelta = delta.Reasoning
		}
		if reasoningDelta != "" {
			emitted = true
			if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkReasoning, Text: reasoningDelta}) {
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
			thoughtSignature := ""
			if tc.ExtraContent != nil {
				thoughtSignature = tc.ExtraContent.Google.ThoughtSignature
			}
			if thoughtSignature == "" {
				// Early Gemini OpenAI-compatible responses placed the field in
				// function. Accept that shape when replaying older sessions and
				// when talking to compatibility gateways that still emit it.
				thoughtSignature = tc.Function.ThoughtSignature
			}
			if thoughtSignature != "" {
				cur.ThoughtSignature = thoughtSignature
			}
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
			// Progress ticks while a large argument payload streams (a 30KB
			// write_file body can take a minute-plus): one chunk per 2KB bucket
			// so the consumer can show liveness without per-delta spam.
			if started[tc.Index] {
				if bucket := len(cur.Arguments) / 2048; bucket > argBucket[tc.Index] {
					argBucket[tc.Index] = bucket
					emitted = true
					if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkToolCallArgsDelta, ToolCall: &provider.ToolCall{ID: cur.ID, Name: cur.Name}, ArgChars: len(cur.Arguments)}) {
						return emitted, ctx.Err()
					}
				}
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return emitted, err
	}
	if stalled.Load() {
		// Idle stall is a body-phase cut: wrap so the Agent can replay the
		// frozen request. Providers no longer reconnect here.
		return emitted, fmt.Errorf("%s: stream stalled — no data for %s, connection likely dropped: %w", c.name, idleTimeout, io.ErrUnexpectedEOF)
	}
	if err := scanner.Err(); err != nil {
		return emitted, fmt.Errorf("%s: read stream: %w", c.name, err)
	}
	// A proxy that idle-closes with a clean FIN ends the scan with no error. Without
	// this check the turn would be committed as complete — including half-streamed
	// tool-call arguments, which then 400 on every replay (#3953). OpenAI Chat
	// accepts either [DONE] or a legal finish_reason as a complete terminal.
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

// normaliseUsage folds the cache shapes used by OpenAI-compatible providers into
// a single Usage. DeepSeek reports prompt_cache_{hit,miss}_tokens at the top of
// usage; OpenAI and MiMo put cache hits under prompt_tokens_details; some
// compatible gateways return Anthropic-style input/cache counters instead.
// Reasoning tokens land in completion_tokens_details on thinking-mode models.
func normaliseUsage(u *wireUsage) *provider.Usage {
	prompt := u.PromptTokens
	anthropicPrompt := prompt == 0 &&
		(u.InputTokens != 0 || u.CacheCreationInputTokens != 0 || u.CacheReadInputTokens != 0)
	if anthropicPrompt {
		// Anthropic-style input_tokens excludes both cache reads and cache
		// writes, while Reasonix PromptTokens represents the complete input.
		prompt = u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
	}
	completion := u.CompletionTokens
	if completion == 0 {
		completion = u.OutputTokens
	}
	total := u.TotalTokens
	if total == 0 && (prompt != 0 || completion != 0) {
		total = prompt + completion
	}

	hit := u.PromptCacheHitTokens
	miss := u.PromptCacheMissTokens
	if hit == 0 && u.PromptTokensDetails != nil {
		hit = u.PromptTokensDetails.CachedTokens
	}
	if hit == 0 {
		hit = u.CacheReadInputTokens
	}
	if miss == 0 {
		switch {
		case anthropicPrompt:
			// Cache writes are still uncached input for Reasonix pricing and
			// cache-ratio accounting.
			miss = u.InputTokens + u.CacheCreationInputTokens
		case hit > 0 && prompt > hit:
			miss = prompt - hit
		}
	}
	reasoning := 0
	if u.CompletionTokensDetails != nil {
		reasoning = u.CompletionTokensDetails.ReasoningTokens
	}
	return &provider.Usage{
		PromptTokens:     prompt,
		CompletionTokens: completion,
		TotalTokens:      total,
		CacheHitTokens:   hit,
		CacheMissTokens:  miss,
		ReasoningTokens:  reasoning,
	}
}

// OpenAI-compatible wire protocol

type chatRequest struct {
	Model               string         `json:"model"`
	Messages            []chatMessage  `json:"messages"`
	Tools               []chatTool     `json:"tools,omitempty"`
	Stream              bool           `json:"stream"`
	StreamOptions       *streamOptions `json:"stream_options,omitempty"`
	Temperature         *float64       `json:"temperature,omitempty"`
	MaxTokens           int            `json:"max_tokens,omitempty"`
	MaxCompletionTokens int            `json:"max_completion_tokens,omitempty"`
	ReasoningEffort     string         `json:"reasoning_effort,omitempty"`
	Thinking            *thinkingMode  `json:"thinking,omitempty"`
	ExtraBody           map[string]any `json:"-"`
}

func omitExtraBodyFields(in map[string]any, names ...string) map[string]any {
	if len(in) == 0 {
		return nil
	}
	omit := make(map[string]struct{}, len(names))
	for _, name := range names {
		omit[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	out := make(map[string]any, len(in))
	for name, value := range in {
		if _, blocked := omit[strings.ToLower(strings.TrimSpace(name))]; !blocked {
			out[name] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
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
	Content any `json:"content"`
	// Prefix is wire-only and is set exclusively on an automatically recovered
	// DeepSeek assistant tail. omitempty keeps every ordinary request byte-stable.
	Prefix bool `json:"prefix,omitempty"`
	// A pointer so the field can serialize as an empty string: DeepSeek thinking
	// mode requires the reasoning_content key to be PRESENT on assistant
	// tool_calls turns (an empty value passes; a missing key 400s), while every
	// other message must keep omitting it.
	ReasoningContent *string        `json:"reasoning_content,omitempty"`
	ToolCalls        []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string         `json:"tool_call_id,omitempty"`
	// Name is the role=tool message's function name. A pointer so ordinary
	// messages omit the key (byte-stable prefix), while tool messages always
	// serialize it — even empty: strict OpenAI-compatible backends (MiMo, per
	// its error table) reject a tool message whose `name` key is absent
	// ("name is not set"), and OpenAI's spec requires the field on role=tool.
	Name *string `json:"name,omitempty"`
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
	Index        int                       `json:"index,omitempty"`
	ID           string                    `json:"id,omitempty"`
	Type         string                    `json:"type,omitempty"`
	ExtraContent *chatToolCallExtraContent `json:"extra_content,omitempty"`
	Function     struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
		// Decode compatibility for the early Gemini OpenAI shape. New requests
		// use extra_content.google.thought_signature.
		ThoughtSignature string `json:"thought_signature,omitempty"`
	} `json:"function"`
}

type chatToolCallExtraContent struct {
	Google struct {
		ThoughtSignature string `json:"thought_signature,omitempty"`
	} `json:"google"`
}

type streamResponse struct {
	Choices []struct {
		Delta struct {
			Content          string         `json:"content"`
			ReasoningContent string         `json:"reasoning_content"`
			Reasoning        string         `json:"reasoning"`
			ToolCalls        []chatToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *wireUsage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// wireUsage covers DeepSeek's top-level cache fields, OpenAI/MiMo's nested
// details, and Anthropic-style fallbacks returned by compatible gateways.
type wireUsage struct {
	PromptTokens             int `json:"prompt_tokens"`
	CompletionTokens         int `json:"completion_tokens"`
	TotalTokens              int `json:"total_tokens"`
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	PromptCacheHitTokens     int `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens    int `json:"prompt_cache_miss_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	PromptTokensDetails      *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}
