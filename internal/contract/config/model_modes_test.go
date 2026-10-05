package config

import "testing"

func TestRequestModesByModelWireAndEndpoint(t *testing.T) {
	const official = "https://api.openai.com/v1"
	cases := []struct {
		name, kind, baseURL, model string
		wantPro                    bool
	}{
		{"gpt-5.6 alias on Responses", "responses", official, "gpt-5.6", true},
		{"gpt-5.6-sol on Responses", "responses", official, "gpt-5.6-sol", true},
		{"gpt-5.6-terra on Responses", "responses", official, "gpt-5.6-terra", true},
		{"gpt-5.6-luna on Responses", "responses", official, "gpt-5.6-luna", true},
		{"gpt-6-sol on Responses", "responses", official, "gpt-6-sol", true},
		{"gpt-6-luna on Responses", "responses", official, "gpt-6-luna", true},
		{"gpt-6-astra on Responses", "responses", official, "GPT-6-Astra", true},
		{"gpt-5.6 on Chat Completions", "openai", official, "gpt-5.6-sol", false},
		{"gpt-6 on a relay", "responses", "https://relay.example.com/v1", "gpt-6-sol", false},
		{"a model that declares none", "responses", official, "gpt-4.1", false},
		{"deepseek", "openai", "https://api.deepseek.com", "deepseek-v4-pro", false},
		{"qwen3.8", "openai", "https://dashscope.aliyuncs.com/compatible-mode/v1", "qwen3.8-max", false},
	}
	for _, tc := range cases {
		e := &ProviderEntry{Name: "p", Kind: tc.kind, BaseURL: tc.baseURL, Model: tc.model}
		modes := RequestModes(e)
		wire := RequestReasoningModes(e)
		gotPro := len(modes) == 1 && modes[0].ID == "pro" && modes[0].Costlier && wire["pro"] == "pro"
		if gotPro != tc.wantPro || (!tc.wantPro && (len(modes) != 0 || len(wire) != 0)) {
			t.Errorf("%s: modes %+v wire %v, want pro=%v", tc.name, modes, wire, tc.wantPro)
		}
	}
	if RequestModes(nil) != nil {
		t.Fatal("a nil entry declared modes")
	}
}
