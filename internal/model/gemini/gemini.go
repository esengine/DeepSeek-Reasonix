package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"reasonix/internal/contract/provider"
)

const (
	defaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"
	// thoughtSummaries asks for the summaries that carry the reasoning text a
	// frontend shows; without it only the final answer comes back.
	thoughtSummaries = "auto"
)

func init() {
	provider.Register("gemini", New)
}

// New builds a Gemini provider from a resolved config.
func New(cfg provider.Config) (provider.Provider, error) {
	if cfg.Model == "" {
		return nil, fmt.Errorf("gemini: model is required for provider %q", cfg.Name)
	}
	name := cfg.Name
	if name == "" {
		name = "gemini"
	}
	root := strings.TrimRight(cfg.BaseURL, "/")
	if root == "" {
		root = defaultBaseURL
	}
	requestURL, _ := cfg.Extra["request_url"].(string)
	if requestURL = strings.TrimSpace(requestURL); requestURL == "" {
		requestURL = root + "/interactions"
	}
	keyEnv, _ := cfg.Extra["api_key_env"].(string)
	keySource, _ := cfg.Extra["api_key_source"].(string)
	effort, _ := cfg.Extra["effort"].(string)
	vision, _ := cfg.Extra["vision"].(bool)
	headers, _ := cfg.Extra["headers"].(map[string]string)
	maxOutput, _ := cfg.Extra["max_output_tokens"].(int)
	videoProtocol, _ := cfg.Extra["video_protocol"].(string)
	httpClient, err := newHTTPClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("gemini: network: %w", err)
	}
	return &client{
		name: name, apiKey: cfg.APIKeyResolver(), keyEnv: keyEnv, keySource: keySource,
		requestURL: requestURL, model: cfg.Model, effort: normalizeLevel(effort), vision: vision,
		headers: cleanHeaders(headers), maxOutput: maxOutput, http: httpClient, video: newVideoWire(videoProtocol, root),
		idleTimeout: provider.IdleTimeoutFromExtra(cfg.Extra),
	}, nil
}

type client struct {
	name        string
	apiKey      func() string
	keyEnv      string
	keySource   string
	requestURL  string
	model       string
	effort      string // thinking_level the entry declares; "" = the model's own default
	vision      bool
	video       *videoWire
	headers     map[string]string
	maxOutput   int
	http        *http.Client
	idleTimeout time.Duration
	authed      atomic.Bool
}

func (c *client) Name() string { return c.name }

// SharedWindowInputPolicy says thought steps replay into the window.
func (c *client) SharedWindowInputPolicy() provider.SharedWindowInputPolicy {
	return provider.SharedWindowInputPolicy{ReplaysResponsesItems: true}
}

// normalizeLevel keeps a level the reference page lists and drops anything else,
// so an unknown spelling reaches no wire.
func normalizeLevel(level string) string {
	switch l := strings.ToLower(strings.TrimSpace(level)); l {
	case "minimal", "low", "medium", "high":
		return l
	}
	return ""
}

func cleanHeaders(in map[string]string) map[string]string {
	out := map[string]string{}
	for name, value := range in {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "", "content-type", "accept", "x-goog-api-key", "authorization":
			continue
		}
		out[strings.TrimSpace(name)] = strings.TrimSpace(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (c *client) sendOpts() provider.SendOptions {
	return provider.SendOptions{
		Provider: c.name, KeyEnv: c.keyEnv, KeySource: c.keySource,
		KeyPresent: c.apiKey() != "", RetryAuth: c.authed.Load(), HeaderTimeout: c.idleTimeout,
	}
}

func (c *client) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	req = provider.MaskOutbound(req, provider.Signed{Items: []string{"thought"}})
	requestCtx := provider.WithRequestAttemptCounter(ctx)
	var uploaded []string
	req.Messages, uploaded = c.resolveVideos(requestCtx, req.Messages)
	body, err := json.Marshal(c.buildRequest(req))
	if err != nil {
		c.deleteFiles(uploaded)
		return nil, fmt.Errorf("%s: marshal request: %w", c.name, err)
	}
	newReq := func(ctx context.Context) (*http.Request, error) {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.requestURL, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")
		httpReq.Header.Set("x-goog-api-key", c.apiKey())
		for name, value := range c.headers {
			httpReq.Header.Set(name, value)
		}
		provider.ApplyClientIdentity(httpReq)
		return httpReq, nil
	}
	resp, err := provider.SendWithRetry(requestCtx, c.http, c.sendOpts(), newReq)
	if err != nil {
		c.deleteFiles(uploaded)
		return nil, provider.AnnotateToolSchemaError(err, req.Tools)
	}
	c.authed.Store(true)
	out := make(chan provider.Chunk)
	go func() {
		defer c.deleteFiles(uploaded)
		c.readStream(requestCtx, resp, out)
	}()
	return out, nil
}

// buildRequest turns the transport-agnostic request into an Interactions request:
// system messages lift to system_instruction and everything else becomes a step,
// in order. Tool results are function_result steps; an assistant turn replays the
// provider items it was stored with.
func (c *client) buildRequest(req provider.Request) request {
	var system []string
	var steps []any
	for _, m := range provider.SanitizeToolPairing(provider.WithoutVideosAll(req.Messages, provider.VideoNotSentNote)) {
		switch m.Role {
		case provider.RoleSystem:
			if m.Content != "" {
				system = append(system, m.Content)
			}
		case provider.RoleUser:
			if blocks := c.userContent(m); len(blocks) > 0 {
				steps = append(steps, contentStep{Type: "user_input", Content: blocks})
			}
		case provider.RoleTool:
			steps = append(steps, c.toolResult(m))
		case provider.RoleAssistant:
			steps = append(steps, assistantSteps(m)...)
		}
	}
	if steps == nil {
		steps = []any{}
	}
	r := request{Model: c.model, Input: steps, SystemInstruction: strings.Join(system, "\n\n"), Stream: true}
	for _, t := range req.Tools {
		params := t.Parameters
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		r.Tools = append(r.Tools, tool{Type: "function", Name: t.Name, Description: t.Description, Parameters: params})
	}
	level := c.effort
	if o := normalizeLevel(req.EffortOverride); o != "" {
		level = o
	}
	maxOut := req.MaxTokens
	if maxOut <= 0 {
		maxOut = c.maxOutput
	}
	if maxOut <= 0 {
		maxOut = provider.AutoOutputBudget(level != "" && level != "minimal", level)
	}
	r.GenerationConfig = &genConfig{MaxOutputTokens: maxOut, ThinkingLevel: level}
	if level != "" {
		r.GenerationConfig.ThinkingSummaries = thoughtSummaries
	}
	return r
}

func (c *client) userContent(m provider.Message) []content {
	var blocks []content
	if m.Content != "" {
		blocks = append(blocks, content{Type: "text", Text: m.Content})
	}
	for _, v := range m.VideoParts {
		if c.video != nil && c.video.protocol == provider.VideoProtocolGeminiInline && v.MIME != "" {
			blocks = append(blocks, content{Type: "video", Data: v.URL, MimeType: v.MIME})
		} else {
			blocks = append(blocks, content{Type: "video", URI: v.URL, MimeType: v.MIME})
		}
	}
	if c.vision {
		for _, url := range m.Images {
			if mt, data, ok := provider.ParseImageDataURL(url); ok {
				blocks = append(blocks, content{Type: "image", Data: data, MimeType: mt})
			}
		}
	}
	return blocks
}

func (c *client) toolResult(m provider.Message) functionResultStep {
	text := m.Content
	if text == "" {
		text = "(no output)"
	}
	result := []content{{Type: "text", Text: text}}
	if c.vision {
		for _, url := range m.Images {
			if mt, data, ok := provider.ParseImageDataURL(url); ok {
				result = append(result, content{Type: "image", Data: data, MimeType: mt})
			}
		}
	}
	return functionResultStep{Type: "function_result", CallID: m.ToolCallID, Name: m.Name, IsError: m.ToolFailure != nil, Result: result}
}
