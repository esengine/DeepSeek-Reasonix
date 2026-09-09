package config

import "testing"

func TestAdditionalProviderRoutes(t *testing.T) {
	cases := []struct{ id, brand, kind, url string }{
		{"doubao-chat", "doubao", "openai", "https://ark.cn-beijing.volces.com/api/v3"},
		{"doubao-responses", "doubao", "responses", "https://ark.cn-beijing.volces.com/api/v3"},
		{"baidu-cloud", "baidu", "openai", "https://qianfan.baidubce.com/v2"},
		{"ppio", "ppio", "openai", "https://api.ppio.com/openai"},
		{"qiniu", "qiniu", "openai", "https://api.qnaigc.com/v1"},
		{"xai-chat", "xai", "openai", "https://api.x.ai/v1"},
		{"xai-responses", "xai", "responses", "https://api.x.ai/v1"},
		{"cerebras", "cerebras", "openai", "https://api.cerebras.ai/v1"},
		{"together", "together", "openai", "https://api.together.ai/v1"},
		{"fireworks-chat", "fireworks", "openai", "https://api.fireworks.ai/inference/v1"},
		{"fireworks-anthropic", "fireworks", "anthropic", "https://api.fireworks.ai/inference"},
		{"fireworks-responses", "fireworks", "responses", "https://api.fireworks.ai/inference/v1"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			p, ok := CuratedProviderPreset(tc.id)
			if !ok || len(p.Entries) != 1 {
				t.Fatal("missing preset")
			}
			e := p.Entries[0]
			if e.Kind != tc.kind || e.BaseURL != tc.url || len(e.Models) == 0 || e.Default != e.Models[0] {
				t.Fatal("invalid route")
			}
			if CatalogForProviderPreset(p).BrandID != tc.brand {
				t.Fatal("brand split")
			}
			if e.WebSearch == nil || *e.WebSearch {
				t.Fatal("search must not be enabled implicitly")
			}
		})
	}
}
