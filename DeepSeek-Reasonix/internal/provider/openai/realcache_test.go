package openai

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"reasonix/internal/provider"
)

// probeResult captures the cache-relevant numbers from one real completion.
type probeResult struct {
	prompt, hit, miss, reasoning int
	sawReasoning                 bool
	reasoningText                string
}

// TestRealDeepSeekCacheProbe is an env-gated end-to-end probe against the live
// DeepSeek API. It answers, with real numbers:
//  1. does DeepSeek's auto cache actually serve reasonix's request shape, and how
//     much does a repeated prefix hit;
//  2. does deepseek-v4-flash even return reasoning_content (i.e. is the round-trip
//     amplifier real for this model);
//  3. does re-sending reasoning_content inflate prompt_tokens and/or break the
//     cache hit on the next turn (the open question the mock can't answer).
//
// Run with:  set -a; source .env; set +a; go test ./internal/provider/openai/ -run TestRealDeepSeekCacheProbe -v -count=1
func TestRealDeepSeekCacheProbe(t *testing.T) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY not set — skipping live probe")
	}

	p, err := New(provider.Config{
		Name:    "deepseek",
		BaseURL: "https://api.deepseek.com",
		Model:   "deepseek-v4-flash",
		APIKey:  key,
		Extra:   map[string]any{"api_key_env": "DEEPSEEK_API_KEY"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	send := func(msgs []provider.Message) (probeResult, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		ch, err := p.Stream(ctx, provider.Request{Messages: msgs, Temperature: 0, MaxTokens: 16})
		if err != nil {
			return probeResult{}, err
		}
		var res probeResult
		var rb strings.Builder
		for chunk := range ch {
			switch chunk.Type {
			case provider.ChunkReasoning:
				res.sawReasoning = true
				rb.WriteString(chunk.Text)
			case provider.ChunkUsage:
				if chunk.Usage != nil {
					res.prompt = chunk.Usage.PromptTokens
					res.hit = chunk.Usage.CacheHitTokens
					res.miss = chunk.Usage.CacheMissTokens
					res.reasoning = chunk.Usage.ReasoningTokens
				}
			case provider.ChunkError:
				return res, chunk.Err
			}
		}
		res.reasoningText = rb.String()
		return res, nil
	}

	rate := func(r probeResult) string {
		denom := r.hit + r.miss
		if denom == 0 {
			denom = r.prompt
		}
		if denom == 0 {
			return "n/a"
		}
		return formatPct(r.hit, denom)
	}

	// A large, stable head so the repeated prefix comfortably exceeds DeepSeek's
	// 64-token cache block granularity and a hit is unambiguous.
	bigHead := "You are a coding agent. Follow these standing instructions precisely. " +
		strings.Repeat("Keep the prefix identical across turns so the context cache can serve it. ", 60)

	// ---- Probe 1: does the cache serve a repeated prefix at all? ----
	base := []provider.Message{
		{Role: provider.RoleSystem, Content: bigHead},
		{Role: provider.RoleUser, Content: "Reply with the single word: ok."},
	}
	p1a, err := send(base)
	if err != nil {
		t.Fatalf("probe1 first call: %v", err)
	}
	time.Sleep(3 * time.Second) // give the async cache a moment to populate
	p1b, err := send(base)
	if err != nil {
		t.Fatalf("probe1 second call: %v", err)
	}
	t.Logf("==== Probe 1: cache on a repeated prefix ====")
	t.Logf("call 1 (cold): prompt=%d hit=%d miss=%d  rate=%s", p1a.prompt, p1a.hit, p1a.miss, rate(p1a))
	t.Logf("call 2 (warm): prompt=%d hit=%d miss=%d  rate=%s", p1b.prompt, p1b.hit, p1b.miss, rate(p1b))
	if p1b.hit == 0 {
		t.Logf("WARNING: warm call still shows 0 cache hit — either caching is off for this account/model, " +
			"or the prefix is below the cacheable size")
	}

	// ---- Probe 3 (cheap, do it early): does v4-flash emit reasoning_content? ----
	t.Logf("==== Probe 3: does deepseek-v4-flash return reasoning_content? ====")
	t.Logf("saw reasoning chunks: %v   reasoning_tokens reported: %d   reasoning_text_len: %d",
		p1a.sawReasoning, p1a.reasoning, len(p1a.reasoningText))
	if !p1a.sawReasoning && p1a.reasoning == 0 {
		t.Logf("→ v4-flash does NOT produce reasoning_content, so the reasoning round-trip " +
			"amplifier does not apply to your active model (it only bites deepseek-reasoner).")
	}

	// ---- Probe 2: does re-sending reasoning_content inflate prompt / break cache? ----
	longReasoning := strings.Repeat("Let me think carefully about each requirement and weigh the trade-offs. ", 40)
	histBase := func(withReasoning bool) []provider.Message {
		asst := provider.Message{
			Role:    provider.RoleAssistant,
			Content: "",
			ToolCalls: []provider.ToolCall{
				{ID: "call_1", Name: "read_file", Arguments: `{"path":"config.toml"}`},
			},
		}
		if withReasoning {
			asst.ReasoningContent = longReasoning
		}
		return []provider.Message{
			{Role: provider.RoleSystem, Content: bigHead},
			{Role: provider.RoleUser, Content: "Read the config and tell me the model."},
			asst,
			{Role: provider.RoleTool, Content: "model = deepseek-v4-flash", ToolCallID: "call_1", Name: "read_file"},
			{Role: provider.RoleUser, Content: "Thanks. Now reply with the single word: ok."},
		}
	}

	withR := histBase(true)
	noR := histBase(false)

	// warm each prefix once, then measure the second (cache-eligible) call.
	if _, err := send(withR); err != nil {
		t.Logf("==== Probe 2 ====")
		t.Logf("DeepSeek REJECTED a request carrying reasoning_content in history: %v", err)
		t.Logf("→ round-tripping reasoning_content is not just a cache concern; the API refuses it.")
	} else {
		if _, err := send(noR); err != nil {
			t.Fatalf("probe2 no-reasoning warm: %v", err)
		}
		time.Sleep(3 * time.Second)
		p2withR, err := send(withR)
		if err != nil {
			t.Fatalf("probe2 with-reasoning measure: %v", err)
		}
		p2noR, err := send(noR)
		if err != nil {
			t.Fatalf("probe2 no-reasoning measure: %v", err)
		}
		t.Logf("==== Probe 2: reasoning_content round-trip on real cache ====")
		t.Logf("WITH reasoning_content: prompt=%d hit=%d miss=%d  rate=%s", p2withR.prompt, p2withR.hit, p2withR.miss, rate(p2withR))
		t.Logf("WITHOUT (stripped):     prompt=%d hit=%d miss=%d  rate=%s", p2noR.prompt, p2noR.hit, p2noR.miss, rate(p2noR))
		// Before the fix this delta was ~+500 (DeepSeek billed the re-sent
		// reasoning as prompt input). After the fix the openai provider drops
		// reasoning_content from the request, so both variants send an identical
		// wire request and the delta should be ~0.
		t.Logf("prompt_tokens delta (with - without) = %d  (~0 confirms the provider no longer re-uploads reasoning_content)", p2withR.prompt-p2noR.prompt)
	}
}

func formatPct(a, b int) string {
	if b == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%d%%", a*100/b)
}
