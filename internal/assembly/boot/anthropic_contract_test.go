package boot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
)

const anthropicStopStream = `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":5}}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}

event: message_stop
data: {"type":"message_stop"}
`

// The effort a user selects reaches an Anthropic-compatible gateway as request
// fields the gateway actually implements. An undeclared relay carrying the
// adaptive thinking an older effort edit wrote for it sends none of them;
// declaring the contract is what opts an endpoint into Anthropic's own fields.
func TestNewProviderScopesAnthropicDepthFieldsToDeclaredEndpoints(t *testing.T) {
	for _, tc := range []struct {
		name         string
		protocol     string
		wantThinking map[string]any
		wantEffort   any
		wantMaxTok   float64
	}{
		{
			name:       "undeclared gateway",
			wantMaxTok: float64(provider.DefaultOrdinaryOutputTokens),
		},
		{
			name:         "declared gateway",
			protocol:     config.ReasoningProtocolAnthropic,
			wantThinking: map[string]any{"type": "adaptive", "display": "summarized"},
			wantEffort:   "max",
			wantMaxTok:   float64(provider.DefaultHighReasoningOutputTokens),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotReq map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
					t.Errorf("decode request: %v", err)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(anthropicStopStream))
			}))
			defer srv.Close()

			entry := &config.ProviderEntry{
				Name: "relay", Kind: "anthropic", BaseURL: srv.URL, Model: "claude-opus-4-8",
				Thinking: "adaptive", Effort: "max", ReasoningProtocol: tc.protocol,
			}
			p, err := newProviderForTest(entry)
			if err != nil {
				t.Fatalf("NewProvider: %v", err)
			}
			ch, err := p.Stream(context.Background(), provider.Request{
				Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
			})
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}
			for chunk := range ch {
				if chunk.Type == provider.ChunkError {
					t.Fatalf("stream error: %v", chunk.Err)
				}
			}
			thinking, _ := gotReq["thinking"].(map[string]any)
			if len(thinking) != len(tc.wantThinking) {
				t.Fatalf("thinking = %+v, want %+v", gotReq["thinking"], tc.wantThinking)
			}
			for k, want := range tc.wantThinking {
				if thinking[k] != want {
					t.Fatalf("thinking[%s] = %v, want %v", k, thinking[k], want)
				}
			}
			outputConfig, _ := gotReq["output_config"].(map[string]any)
			if outputConfig["effort"] != tc.wantEffort {
				t.Fatalf("output_config = %+v, want effort %v", gotReq["output_config"], tc.wantEffort)
			}
			if gotReq["max_tokens"] != tc.wantMaxTok {
				t.Fatalf("max_tokens = %v, want %v", gotReq["max_tokens"], tc.wantMaxTok)
			}
		})
	}
}
