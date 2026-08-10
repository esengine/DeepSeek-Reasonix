package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func TestBuildRequestNativeContextEditingIsExplicitAndOfficialOnly(t *testing.T) {
	policy := &provider.ContextEditingPolicy{
		Mode: "native", TriggerInputTokens: 12000, KeepToolUses: 3,
		ClearAtLeastInputTokens: 4096,
	}
	official := (&client{model: "claude-test", nativeAnthropic: true}).buildRequest(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hello"}}, ContextEditing: policy,
	})
	b, err := json.Marshal(official)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"context_management"`) ||
		!strings.Contains(string(b), `"clear_tool_uses_20250919"`) ||
		!strings.Contains(string(b), `"value":12000`) {
		t.Fatalf("native context editing missing from official request: %s", b)
	}
	gateway := (&client{model: "gateway", nativeAnthropic: false}).buildRequest(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hello"}}, ContextEditing: policy,
	})
	gb, err := json.Marshal(gateway)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gb), `"context_management"`) {
		t.Fatalf("compatible gateway must not receive native context editing: %s", gb)
	}
}

func TestStreamClassifiesOnlyNativeContextEditingRejections(t *testing.T) {
	responses := []struct {
		name string
		body string
		want bool
	}{
		{name: "feature rejection", body: `{"error":{"message":"context_management: extra inputs are not permitted"}}`, want: true},
		{name: "unrelated bad request", body: `{"error":{"message":"tools[0].input_schema is invalid"}}`, want: false},
	}
	for _, tc := range responses {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("anthropic-beta"); got != anthropicContextManagementBeta {
					t.Errorf("anthropic-beta = %q", got)
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read request body: %v", err)
				}
				if !strings.Contains(string(body), `"context_management"`) {
					t.Errorf("request lacks native context management: %s", body)
				}
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()

			c := &client{
				name: "anthropic-test", apiKey: "test", baseURL: server.URL, model: "claude-test",
				nativeAnthropic: true, http: server.Client(),
			}
			_, err := c.Stream(context.Background(), provider.Request{
				Messages: []provider.Message{{Role: provider.RoleUser, Content: "hello"}},
				ContextEditing: &provider.ContextEditingPolicy{
					Mode: "native", TriggerInputTokens: 12_000, KeepToolUses: 3,
					ClearAtLeastInputTokens: 4_096,
				},
			})
			if got := provider.IsNativeContextEditingUnsupported(err); got != tc.want {
				t.Fatalf("IsNativeContextEditingUnsupported(%v) = %t, want %t", err, got, tc.want)
			}
		})
	}
}

func TestReadStreamReportsAppliedNativeToolClearing(t *testing.T) {
	fixture := `event: message_start
data: {"type":"message_start","message":{"usage":{"input_tokens":100}}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2},"context_management":{"applied_edits":[{"type":"clear_tool_uses_20250919","cleared_tool_uses":8,"cleared_input_tokens":50000}]}}

event: message_stop
data: {"type":"message_stop"}
`
	c := &client{name: "anthropic", nativeAnthropic: true}
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(fixture))}
	ch := make(chan provider.Chunk)
	go c.readStream(context.Background(), resp, ch)
	var usage *provider.Usage
	for chunk := range ch {
		if chunk.Type == provider.ChunkUsage {
			usage = chunk.Usage
		}
	}
	if usage == nil || usage.ContextEditingType != anthropicToolClearPolicyVersion ||
		usage.ContextEditingClearedToolUses != 8 || usage.ContextEditingClearedTokens != 50000 {
		t.Fatalf("native applied edits usage = %+v", usage)
	}
}
