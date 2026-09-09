package config

import (
	"net/url"
	"testing"
)

func TestDocumentedProtocolEndpoints(t *testing.T) {
	for scope, routes := range documentedProtocolEndpoints {
		for kind, e := range routes {
			if kind != "openai" && kind != "responses" && kind != "anthropic" {
				t.Fatalf("unknown kind %s", kind)
			}
			u, err := url.Parse(e.BaseURL)
			if err != nil || u.Host == "" || e.Source == "" || e.CheckedOn == "" {
				t.Fatalf("invalid route %s/%s: %+v", scope, kind, e)
			}
		}
	}
	cases := []struct{ id, kind, want string }{
		{"deepseek-anthropic", "anthropic", "https://api.deepseek.com/anthropic"},
		{"kimi-coding-plan", "openai", "https://api.kimi.com/coding/v1"},
		{"siliconflow", "anthropic", "https://api.siliconflow.cn"},
		{"ppio", "openai", "https://api.ppio.com/openai"},
		{"amd-gpu-cloud", "openai", "https://developer.amd.com.cn/radeon/api/v1"},
		{"lmstudio", "responses", "http://localhost:1234/v1"},
		{"mimo-api", "responses", "https://api.xiaomimimo.com/v1"},
	}
	for _, tc := range cases {
		p, ok := CuratedProviderPreset(tc.id)
		if !ok {
			t.Fatal(tc.id)
		}
		c := CatalogForProviderPreset(p)
		if got := c.Protocols[tc.kind].BaseURL; got != tc.want {
			t.Fatalf("%s/%s = %s", tc.id, tc.kind, got)
		}
		delete(c.Protocols, tc.kind)
		if CatalogForProviderPreset(p).Protocols[tc.kind].BaseURL != tc.want {
			t.Fatal("catalog shared mutable map")
		}
	}
	p, _ := CuratedProviderPreset("glm-cn")
	if _, ok := CatalogForProviderPreset(p).Protocols["responses"]; ok {
		t.Fatal("undocumented protocol guessed")
	}
	p, _ = CuratedProviderPreset("stepfun")
	if _, ok := CatalogForProviderPreset(p).Protocols["responses"]; ok {
		t.Fatal("payg endpoint leaked into subscription")
	}
}
