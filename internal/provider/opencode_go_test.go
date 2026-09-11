package provider

import "testing"

func TestOpenCodeGoChatModelsMatchPinnedLimits(t *testing.T) {
	want := map[string]OpenCodeGoModelLimits{
		"glm-5.3":                      {Context: 1_000_000, MaxOutput: 131_072},
		"glm-5.2":                      {Context: 1_000_000, MaxOutput: 131_072},
		"glm-5.1":                      {Context: 202_752, MaxOutput: 32_768},
		"kimi-k3":                      {Context: 1_048_576, MaxOutput: 131_072},
		"kimi-k2.7-code":               {Context: 262_144, MaxOutput: 262_144},
		"kimi-k2.6":                    {Context: 262_144, MaxOutput: 65_536},
		"deepseek-v4-pro":              {Context: 1_000_000, MaxOutput: 384_000},
		"deepseek-v4-flash":            {Context: 1_000_000, MaxOutput: 384_000},
		"deepseek-v4-flash-vision-exp": {Context: 1_000_000, MaxOutput: 384_000},
		"mimo-v2.5-pro":                {Context: 1_048_576, MaxOutput: 128_000},
		"mimo-v2.5":                    {Context: 1_000_000, MaxOutput: 128_000},
		"hy3":                          {Context: 256_000, MaxOutput: 64_000},
	}
	got := OpenCodeGoChatModels()
	for id, lim := range want {
		if got[id] != lim {
			t.Fatalf("%s = %+v, want %+v", id, got[id], lim)
		}
	}
}

func TestOpenCodeGoAnthropicModelsMatchPinnedLimits(t *testing.T) {
	want := map[string]OpenCodeGoModelLimits{
		"qwen3.8-max":  {Context: 1_000_000, MaxOutput: 131_072},
		"qwen3.7-max":  {Context: 1_000_000, MaxOutput: 65_536},
		"qwen3.7-plus": {Context: 1_000_000, MaxOutput: 65_536},
		"qwen3.6-plus": {Context: 1_000_000, MaxOutput: 65_536},
		"minimax-m3":   {Context: 1_000_000, MaxOutput: 131_072},
		"minimax-m2.7": {Context: 204_800, MaxOutput: 131_072},
		"minimax-m2.5": {Context: 204_800, MaxOutput: 65_536},
	}
	got := OpenCodeGoAnthropicModels()
	for id, lim := range want {
		if got[id] != lim {
			t.Fatalf("%s = %+v, want %+v", id, got[id], lim)
		}
	}
}

func TestOpenCodeGoResponsesModelsMatchPinnedLimits(t *testing.T) {
	want := map[string]OpenCodeGoModelLimits{
		"grok-4.5":                   {Context: 500_000, MaxOutput: 500_000},
		"gpt-5.6-luna":               {Context: 1_050_000, MaxOutput: 128_000},
		"muse-spark-1.2-contributor": {Context: 1_048_576, MaxOutput: 131_072},
	}
	got := OpenCodeGoResponsesModels()
	for id, lim := range want {
		if got[id] != lim {
			t.Fatalf("%s = %+v, want %+v", id, got[id], lim)
		}
	}
}

func TestFilterOfficialOpenCodeGoModelsKeepsOnlyRouteCompatibleModels(t *testing.T) {
	all := []string{"grok-4.5", "gpt-5.6-luna", "muse-spark-1.2-contributor", "glm-5.3", "glm-5.2", "hy3", "qwen3.8-max", "qwen3.7-plus", "deepseek-v4-flash", "unknown-future-model"}
	tests := []struct {
		name    string
		kind    string
		baseURL string
		want    []string
	}{
		{name: "chat", kind: "openai", baseURL: "https://opencode.ai/zen/go/v1", want: []string{"glm-5.3", "glm-5.2", "hy3", "qwen3.8-max", "qwen3.7-plus", "deepseek-v4-flash"}},
		{name: "anthropic", kind: "anthropic", baseURL: "https://opencode.ai/zen/go", want: []string{"qwen3.8-max", "qwen3.7-plus", "deepseek-v4-flash"}},
		{name: "responses", kind: "responses", baseURL: "https://opencode.ai/zen/go/v1", want: []string{"grok-4.5", "gpt-5.6-luna", "muse-spark-1.2-contributor", "deepseek-v4-flash"}},
		{name: "custom endpoint is untouched", kind: "anthropic", baseURL: "https://relay.example/zen/go", want: all},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterOfficialOpenCodeGoModels(tt.kind, tt.baseURL, all)
			if len(got) != len(tt.want) {
				t.Fatalf("filtered models = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("filtered models = %v, want %v", got, tt.want)
				}
			}
		})
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
	grok, ok := LookupOfficialOpenCodeGo("responses", "https://opencode.ai/zen/go/v1", "grok-4.5")
	if !ok || grok.Context != 500_000 || grok.MaxOutput != 500_000 {
		t.Fatalf("responses grok = %+v ok=%v", grok, ok)
	}
}

func TestOpenCodeGoModelInfoUsesExactLocalCatalog(t *testing.T) {
	vision, ok := OpenCodeGoModelInfo("openai", "https://opencode.ai/zen/go/v1", "kimi-k3")
	if !ok || !vision.SupportsInput(ModalityImage) {
		t.Fatalf("kimi-k3 metadata = %+v, ok=%t", vision, ok)
	}
	text, ok := OpenCodeGoModelInfo("openai", "https://opencode.ai/zen/go/v1", "glm-5.2")
	if !ok || text.SupportsInput(ModalityImage) || len(text.InputModalities) != 1 || text.InputModalities[0] != ModalityText {
		t.Fatalf("glm-5.2 metadata = %+v, ok=%t", text, ok)
	}
	if _, ok := OpenCodeGoModelInfo("openai", "https://opencode.ai/zen/go/v1", "omen-alpha"); ok {
		t.Fatal("uncatalogued model must not be inferred from its endpoint")
	}
	kimi, ok := OpenCodeGoModelInfo("openai", "https://opencode.ai/zen/go/v1", "kimi-k2.6")
	if !ok || !kimi.SupportsInput(ModalityImage) {
		t.Fatalf("pi catalog kimi-k2.6 metadata = %+v, ok=%t", kimi, ok)
	}
	if kimi.ContextWindow == 0 || kimi.MaxOutputTokens == 0 || kimi.API == "" {
		t.Fatalf("pi catalog should preserve model metadata, got %+v", kimi)
	}
}

func TestPiCatalogModelInfosContainsMultipleProviders(t *testing.T) {
	for _, id := range []string{"opencode-go", "deepseek", "anthropic", "openai"} {
		if models := PiCatalogModelInfos(id); len(models) == 0 {
			t.Fatalf("pi catalog provider %q is empty", id)
		}
	}
}

func TestPiCatalogModelInfoForProviderRequiresExactServingRoute(t *testing.T) {
	model, ok := PiCatalogModelInfoForProvider("opencode-go", "openai", "https://opencode.ai/zen/go/v1", "qwen3.6-plus")
	if !ok || !model.SupportsInput(ModalityImage) || model.API != "openai-completions" {
		t.Fatalf("OpenCode catalog model = %+v, ok=%t", model, ok)
	}
	if _, ok := PiCatalogModelInfoForProvider("opencode-go", "openai", "https://gateway.example/v1", "qwen3.6-plus"); ok {
		t.Fatal("custom endpoint must not inherit pi catalog metadata")
	}
}

func TestPiCatalogContractKeepsKnownCapabilityFacts(t *testing.T) {
	cases := []struct {
		kind, baseURL, model string
		image                bool
	}{
		{"openai", "https://opencode.ai/zen/go/v1", "kimi-k3", true},
		{"openai", "https://opencode.ai/zen/go/v1", "glm-5.2", false},
		{"openai", "https://opencode.ai/zen/go/v1", "deepseek-v4-flash-vision-exp", true},
		{"anthropic", "https://opencode.ai/zen/go", "qwen3.8-flash", true},
		{"responses", "https://opencode.ai/zen/go/v1", "grok-4.6", true},
	}
	for _, tc := range cases {
		info, ok := PiCatalogModelInfo(tc.kind, tc.baseURL, tc.model)
		if !ok || info.SupportsInput(ModalityImage) != tc.image {
			t.Fatalf("pi catalog contract %s/%s = %+v, ok=%t", tc.kind, tc.model, info, ok)
		}
	}
}

func TestModelScopeModelInfoUsesVerifiedLocalCatalog(t *testing.T) {
	vision, ok := ModelScopeModelInfo("openai", "https://api-inference.modelscope.cn/v1", "Qwen/Qwen3.5-27B")
	if !ok || !vision.SupportsInput(ModalityImage) {
		t.Fatalf("ModelScope vision metadata = %+v, ok=%t", vision, ok)
	}
	text, ok := ModelScopeModelInfo("openai", "https://api-inference.modelscope.cn/v1", "ZhipuAI/GLM-5.2")
	if !ok || text.SupportsInput(ModalityImage) {
		t.Fatalf("ModelScope text metadata = %+v, ok=%t", text, ok)
	}
	if _, ok := ModelScopeModelInfo("openai", "https://gateway.example/v1", "Qwen/Qwen3.5-27B"); ok {
		t.Fatal("custom ModelScope lookalike must not use the local catalog")
	}
}

func TestBuiltinModelInfoIncludesDeepSeekVisionSKU(t *testing.T) {
	vision, ok := BuiltinModelInfo("openai", "https://api.deepseek.com/v1", "deepseek-v4-flash-vision-exp")
	if !ok || !vision.SupportsInput(ModalityImage) {
		t.Fatalf("DeepSeek vision metadata = %+v, ok=%t", vision, ok)
	}
	text, ok := BuiltinModelInfo("openai", "https://api.deepseek.com/v1", "deepseek-v4-pro")
	if !ok || text.SupportsInput(ModalityImage) {
		t.Fatalf("DeepSeek text metadata = %+v, ok=%t", text, ok)
	}
}
