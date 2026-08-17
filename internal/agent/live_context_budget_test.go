//go:build live

package agent

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"reasonix/internal/provider"
	"reasonix/internal/provider/openai"
)

type liveOutputFieldCacheResult struct {
	prompt int
	hit    int
	miss   int
}

// Opt-in A/B: same prefix+tools, compare cache-hit when the output field is
// omitted vs proactively limited. Skips when credentials or cache telemetry
// are unavailable.
func TestLiveSharedWindowOutputFieldCache(t *testing.T) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY not set")
	}
	baseURL := strings.TrimRight(os.Getenv("DEEPSEEK_BASE_URL"), "/")
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	chat, err := openai.New(provider.Config{Name: "live-ds", BaseURL: baseURL, Model: "deepseek-v4-flash", APIKey: key})
	if err != nil {
		t.Fatal(err)
	}
	policy := provider.ResolveContextBudgetPolicy(chat)
	if policy.LimitMode != provider.OutputLimitOmitWhenSafe {
		t.Fatalf("live DeepSeek policy = %+v", policy)
	}
	if closer, ok := chat.(interface{ CloseIdleConnections() }); ok {
		t.Cleanup(closer.CloseIdleConnections)
	}
	stablePrefix := "You are a coding agent. Keep this cache-test prefix byte-identical. " +
		strings.Repeat("Preserve the stable prefix and answer the final request concisely. ", 70)
	baseReq := provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: stablePrefix},
			{Role: provider.RoleUser, Content: "Do not call tools. Reply with the single word ok."},
		},
		Tools: []provider.ToolSchema{{
			Name: "live_cache_marker", Description: "Return a fixed cache marker only when explicitly requested.",
			Parameters: []byte(`{"type":"object","properties":{},"additionalProperties":false}`),
		}},
		Temperature: provider.TemperaturePtr(0),
	}

	collect := func(label string, req provider.Request) liveOutputFieldCacheResult {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		chunks, err := chat.Stream(ctx, req)
		if err != nil {
			t.Fatalf("%s stream: %v", label, err)
		}
		var result liveOutputFieldCacheResult
		for chunk := range chunks {
			switch chunk.Type {
			case provider.ChunkUsage:
				if chunk.Usage != nil {
					result.prompt = chunk.Usage.PromptTokens
					result.hit = chunk.Usage.CacheHitTokens
					result.miss = chunk.Usage.CacheMissTokens
				}
			case provider.ChunkError:
				t.Fatalf("%s chunk: %v", label, chunk.Err)
			}
		}
		return result
	}

	_ = collect("warm", baseReq)
	time.Sleep(3 * time.Second)
	omitted := collect("omitted", baseReq)
	limitedReq := baseReq
	limitedReq.MaxTokens = 64
	limited := collect("limited", limitedReq)
	if omitted.prompt != limited.prompt {
		t.Fatalf("output field changed prompt tokens: omitted=%d limited=%d", omitted.prompt, limited.prompt)
	}
	if omitted.hit+omitted.miss == 0 || limited.hit+limited.miss == 0 {
		t.Skipf("provider returned no cache telemetry: omitted=%+v limited=%+v", omitted, limited)
	}
	if omitted.hit == 0 {
		t.Skipf("provider cache did not warm: omitted=%+v", omitted)
	}
	if limited.hit*100 < omitted.hit*90 {
		t.Fatalf("limited cache hit regressed by more than 10%%: omitted=%+v limited=%+v", omitted, limited)
	}
	t.Logf("output-field A/B: omitted prompt=%d hit=%d miss=%d; limited prompt=%d hit=%d miss=%d",
		omitted.prompt, omitted.hit, omitted.miss, limited.prompt, limited.hit, limited.miss)
}
