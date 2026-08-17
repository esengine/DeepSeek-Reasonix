package provider

import "testing"

func TestOpenCodeGoChatModelsMatchPinnedLimits(t *testing.T) {
	want := map[string]OpenCodeGoModelLimits{
		"glm-5.2":           {Context: 1_000_000, MaxOutput: 131_072},
		"glm-5.1":           {Context: 202_752, MaxOutput: 32_768},
		"kimi-k3":           {Context: 1_048_576, MaxOutput: 131_072},
		"kimi-k2.7-code":    {Context: 262_144, MaxOutput: 262_144},
		"kimi-k2.6":         {Context: 262_144, MaxOutput: 65_536},
		"deepseek-v4-pro":   {Context: 1_000_000, MaxOutput: 384_000},
		"deepseek-v4-flash": {Context: 1_000_000, MaxOutput: 384_000},
		"mimo-v2.5-pro":     {Context: 1_048_576, MaxOutput: 128_000},
		"mimo-v2.5":         {Context: 1_000_000, MaxOutput: 128_000},
	}
	got := OpenCodeGoChatModels()
	if len(got) != len(want) {
		t.Fatalf("chat catalog size = %d, want %d", len(got), len(want))
	}
	for id, lim := range want {
		if got[id] != lim {
			t.Fatalf("%s = %+v, want %+v", id, got[id], lim)
		}
	}
}

func TestOpenCodeGoAnthropicModelsMatchPinnedLimits(t *testing.T) {
	want := map[string]OpenCodeGoModelLimits{
		"qwen3.7-max":  {Context: 1_000_000, MaxOutput: 65_536},
		"qwen3.7-plus": {Context: 1_000_000, MaxOutput: 65_536},
		"qwen3.6-plus": {Context: 1_000_000, MaxOutput: 65_536},
		"minimax-m3":   {Context: 1_000_000, MaxOutput: 131_072},
		"minimax-m2.7": {Context: 204_800, MaxOutput: 131_072},
		"minimax-m2.5": {Context: 204_800, MaxOutput: 65_536},
	}
	got := OpenCodeGoAnthropicModels()
	if len(got) != len(want) {
		t.Fatalf("anthropic catalog size = %d, want %d", len(got), len(want))
	}
	for id, lim := range want {
		if got[id] != lim {
			t.Fatalf("%s = %+v, want %+v", id, got[id], lim)
		}
	}
}

func TestLookupOfficialOpenCodeGoRejectsLookalikes(t *testing.T) {
	if _, ok := LookupOfficialOpenCodeGo("openai", "https://opencode.ai.attacker.example/zen/go/v1", "kimi-k3"); ok {
		t.Fatal("lookalike host must not match")
	}
	if _, ok := LookupOfficialOpenCodeGo("openai", "https://opencode.ai/zen/go/v1?x=1", "kimi-k3"); ok {
		t.Fatal("query string must not match")
	}
	if _, ok := LookupOfficialOpenCodeGo("openai", "http://opencode.ai/zen/go/v1", "kimi-k3"); ok {
		t.Fatal("http must not match")
	}
	if _, ok := LookupOfficialOpenCodeGo("openai", "https://opencode.ai/zen/go/v1", "future-model"); ok {
		t.Fatal("unknown model must not assume limits")
	}
	if _, ok := LookupOfficialOpenCodeGo("openai", "https://gateway.example/zen/go/v1", "kimi-k3"); ok {
		t.Fatal("custom proxy must not match")
	}
}

func TestLookupOfficialOpenCodeGoKnownRoutes(t *testing.T) {
	chat, ok := LookupOfficialOpenCodeGo("openai", "https://opencode.ai/zen/go/v1", "kimi-k3")
	if !ok || chat.Context != 1_048_576 || chat.MaxOutput != 131_072 {
		t.Fatalf("chat kimi-k3 = %+v ok=%v", chat, ok)
	}
	anth, ok := LookupOfficialOpenCodeGo("anthropic", "https://opencode.ai/zen/go", "qwen3.7-plus")
	if !ok || anth.MaxOutput != 65_536 {
		t.Fatalf("anthropic qwen = %+v ok=%v", anth, ok)
	}
	resp, ok := LookupOfficialOpenCodeGo("responses", "https://opencode.ai/zen/go/v1", "deepseek-v4-flash")
	if !ok || resp.MaxOutput != 384_000 {
		t.Fatalf("responses flash = %+v ok=%v", resp, ok)
	}
}
