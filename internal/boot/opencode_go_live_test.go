//go:build live

package boot

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/netclient"
	"reasonix/internal/provider"
	"reasonix/internal/websearch"
)

// Opt-in release acceptance: 11 logical requests, at most 512 output tokens
// per request. No user history is sent. The credential exists only in the
// process environment and neither prompts nor reasoning are logged.
func TestLiveOpenCodeGoV10Acceptance(t *testing.T) {
	key := os.Getenv("OPENCODE_GO_API_KEY")
	if key == "" {
		t.Skip("OPENCODE_GO_API_KEY is required")
	}
	requests, attempts, input, output := 0, 0, 0, 0
	t.Cleanup(func() {
		t.Logf("logical requests=%d actual HTTP attempts=%d input_tokens=%d output_tokens=%d", requests, attempts, input, output)
	})
	newModel := func(t *testing.T, kind, model, effort string, search bool) provider.Provider {
		t.Helper()
		base := "https://opencode.ai/zen/go/v1"
		endpoint := map[string]string{"openai": "chat/completions", "anthropic": "messages", "responses": "responses"}[kind]
		entry := config.ProviderEntry{Name: "opencode-v10-acceptance", Kind: kind, BaseURL: base,
			RequestURL: base + "/" + endpoint, Model: model, APIKeyEnv: "OPENCODE_GO_API_KEY", Effort: effort,
			Thinking: "enabled", MaxOutputTokens: 512, WebSearch: &search, ResponsesMode: "stateless"}
		if !provider.OpenCodeGoDeepSeekModel(model) {
			entry.Thinking = "adaptive"
		}
		entry.ResolveAPIKeyFromProcessEnvForProbe()
		p, err := newProviderWithSearchMode(&entry, netclient.ProxySpec{Mode: netclient.ModeAuto}, nil, !search)
		if err != nil {
			t.Fatal(strings.ReplaceAll(err.Error(), key, "[redacted]"))
		}
		t.Cleanup(func() {
			if closer, ok := p.(interface{ CloseIdleConnections() }); ok {
				closer.CloseIdleConnections()
			}
		})
		return p
	}
	collect := func(t *testing.T, p provider.Provider, req provider.Request) provider.Message {
		t.Helper()
		requests++
		ctx, cancel := context.WithTimeout(provider.WithIndependentRequestAttemptCounter(provider.WithCacheSession(context.Background(), "opencode-v10-release-acceptance")), 90*time.Second)
		defer cancel()
		defer func() {
			if u := provider.UsageWithRequestAttemptCount(ctx, nil); u != nil {
				attempts += u.RequestCount
			}
		}()
		ch, err := p.Stream(ctx, req)
		if err != nil {
			t.Fatal(strings.ReplaceAll(err.Error(), key, "[redacted]"))
		}
		message := provider.Message{Role: provider.RoleAssistant}
		var usage provider.Usage
		for chunk := range ch {
			switch chunk.Type {
			case provider.ChunkText:
				message.Content += chunk.Text
			case provider.ChunkReasoning:
				message.ReasoningContent += chunk.Text
			case provider.ChunkToolCall:
				if chunk.ToolCall != nil {
					message.ToolCalls = append(message.ToolCalls, *chunk.ToolCall)
				}
			case provider.ChunkUsage:
				if chunk.Usage != nil {
					usage = *chunk.Usage
				}
			case provider.ChunkServerSearch:
				if chunk.ServerSearch != nil {
					message.ServerSearch = append(message.ServerSearch, *chunk.ServerSearch)
				}
			case provider.ChunkResponsesItem:
				message.ResponsesItems = append(message.ResponsesItems, chunk.ResponsesItem)
			case provider.ChunkError:
				if chunk.Err != nil {
					t.Error(strings.ReplaceAll(chunk.Err.Error(), key, "[redacted]"))
				}
			}
		}
		input += usage.PromptTokens
		output += usage.CompletionTokens
		t.Logf("request=%d prompt=%d completion=%d text_bytes=%d reasoning_bytes=%d tool_calls=%d", requests, usage.PromptTokens, usage.CompletionTokens, len(message.Content), len(message.ReasoningContent), len(message.ToolCalls))
		if len(message.Content) == 0 && len(message.ToolCalls) == 0 {
			t.Fatal("no visible answer or tool call")
		}
		return message
	}
	for _, test := range []struct{ kind, model string }{
		{"openai", "deepseek-v4-flash"}, {"openai", "deepseek-v4-pro"}, {"openai", "deepseek-v4-flash-vision-exp"},
		{"anthropic", "minimax-m3"}, {"responses", "grok-4.6"},
	} {
		t.Run(test.kind+"/"+test.model, func(t *testing.T) {
			effort := "high"
			if provider.OpenCodeGoDeepSeekModel(test.model) {
				effort = "max"
			} else if test.model == "grok-4.6" {
				// The current Responses route does not expose a local reasoning
				// effort contract for Grok 4.6; leave it unset for the wire probe.
				effort = ""
			}
			p := newModel(t, test.kind, test.model, effort, false)
			collect(t, p, provider.Request{Messages: []provider.Message{{Role: provider.RoleSystem, Content: "You are Reasonix, a coding assistant running a small protocol integration test."}, {Role: provider.RoleUser, Content: "Reply with just OK."}}, MaxTokens: 128})
		})
	}
	t.Run("tool-replay", func(t *testing.T) {
		p := newModel(t, "openai", "deepseek-v4-flash", "max", false)
		tools := []provider.ToolSchema{{Name: "get_marker", Description: "Return the integration-test marker. Call this tool before answering.", Parameters: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)}}
		messages := []provider.Message{{Role: provider.RoleSystem, Content: "You are Reasonix, a coding assistant. Call get_marker once, then report its result."}, {Role: provider.RoleUser, Content: "Call get_marker now."}}
		first := collect(t, p, provider.Request{Messages: messages, Tools: tools, MaxTokens: 512})
		if len(first.ToolCalls) == 0 || first.ReasoningContent == "" {
			t.Fatal("missing tool call or reasoning to replay")
		}
		messages = append(messages, first)
		for _, call := range first.ToolCalls {
			messages = append(messages, provider.Message{Role: provider.RoleTool, ToolCallID: call.ID, Name: call.Name, Content: "protocol-round-trip-ok"})
		}
		second := collect(t, p, provider.Request{Messages: messages, Tools: tools, MaxTokens: 256})
		if !strings.Contains(second.Content, "protocol-round-trip-ok") {
			t.Fatal("tool result was not retained")
		}
		messages = append(messages, second, provider.Message{Role: provider.RoleUser, Content: "Repeat only the marker from the previous tool result."})
		third := collect(t, p, provider.Request{Messages: messages, Tools: tools, MaxTokens: 128})
		if !strings.Contains(third.Content, "protocol-round-trip-ok") {
			t.Fatal("multi-turn history lost the tool result")
		}
	})
	t.Run("vision", func(t *testing.T) {
		p := newModel(t, "openai", "deepseek-v4-flash-vision-exp", "max", false)
		img := image.NewRGBA(image.Rect(0, 0, 32, 32))
		for y := 0; y < 32; y++ {
			for x := 0; x < 32; x++ {
				img.SetRGBA(x, y, color.RGBA{R: 255, A: 255})
			}
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
		answer := collect(t, p, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "What is the main color of this image? Reply with one English color word.", Images: []string{dataURL}}}, MaxTokens: 128})
		if !strings.Contains(strings.ToLower(answer.Content), "red") {
			t.Fatal("image content was not recognized")
		}
	})
	for _, kind := range []string{"anthropic", "responses"} {
		t.Run("search/"+kind, func(t *testing.T) {
			p := newModel(t, kind, "deepseek-v4-flash", "low", true)
			requests++
			search := &websearch.Tool{Factory: func() (provider.Provider, error) { return boundedOpenCodeLiveProvider{p}, nil }, ReportUsage: func(u *provider.Usage) {
				input += u.PromptTokens
				output += u.CompletionTokens
				attempts += u.RequestCount
				t.Logf("search HTTP attempts=%d prompt=%d completion=%d", u.RequestCount, u.PromptTokens, u.CompletionTokens)
			}}
			result, err := search.Execute(context.Background(), json.RawMessage(`{"query":"Find the official OpenCode Go coding subscription documentation. Give one source URL and a short description."}`))
			if err != nil {
				t.Fatal(strings.ReplaceAll(err.Error(), key, "[redacted]"))
			}
			var decoded websearch.Result
			if err := json.Unmarshal([]byte(result), &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.Summary == "" || len(decoded.Sources) == 0 {
				t.Fatal("search produced no summary and structured source evidence")
			}
			t.Logf("search sources=%d summary_bytes=%d", len(decoded.Sources), len(decoded.Summary))
		})
	}
}

type boundedOpenCodeLiveProvider struct{ provider.Provider }

func (p boundedOpenCodeLiveProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if req.MaxTokens > 512 {
		req.MaxTokens = 512
	}
	return p.Provider.Stream(ctx, req)
}
