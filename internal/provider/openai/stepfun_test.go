package openai

import (
	"strings"
	"testing"

	"reasonix/internal/provider"
)

const (
	wantStepFunComChat   = "https://api.stepfun.com/step_plan/v1/chat/completions"
	wantStepFunComModels = "https://api.stepfun.com/step_plan/v1/models"
	wantStepFunAIChat    = "https://api.stepfun.ai/step_plan/v1/chat/completions"
	wantStepFunAIModels  = "https://api.stepfun.ai/step_plan/v1/models"
)

func TestCanonicalStepFunPlanEndpoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		raw        string
		ok         bool
		wantChat   string
		wantModels string
	}{
		{name: "com v1 base", raw: "https://api.stepfun.com/step_plan/v1", ok: true, wantChat: wantStepFunComChat, wantModels: wantStepFunComModels},
		{name: "com missing v1 base", raw: "https://api.stepfun.com/step_plan", ok: true, wantChat: wantStepFunComChat, wantModels: wantStepFunComModels},
		{name: "com missing v1 trailing slash", raw: "https://api.stepfun.com/step_plan/", ok: true, wantChat: wantStepFunComChat, wantModels: wantStepFunComModels},
		{name: "com missing v1 chat", raw: "https://api.stepfun.com/step_plan/chat/completions", ok: true, wantChat: wantStepFunComChat, wantModels: wantStepFunComModels},
		{name: "com missing v1 models", raw: "https://api.stepfun.com/step_plan/models", ok: true, wantChat: wantStepFunComChat, wantModels: wantStepFunComModels},
		{name: "com v1 chat", raw: "https://api.stepfun.com/step_plan/v1/chat/completions", ok: true, wantChat: wantStepFunComChat, wantModels: wantStepFunComModels},
		{name: "com v1 models", raw: "https://api.stepfun.com/step_plan/v1/models", ok: true, wantChat: wantStepFunComChat, wantModels: wantStepFunComModels},
		{name: "com double v1 base", raw: "https://api.stepfun.com/step_plan/v1/v1", ok: true, wantChat: wantStepFunComChat, wantModels: wantStepFunComModels},
		{name: "com double v1 chat", raw: "https://api.stepfun.com/step_plan/v1/v1/chat/completions", ok: true, wantChat: wantStepFunComChat, wantModels: wantStepFunComModels},
		{name: "com repeated chat suffix", raw: "https://api.stepfun.com/step_plan/v1/chat/completions/chat/completions", ok: true, wantChat: wantStepFunComChat, wantModels: wantStepFunComModels},
		{name: "ai v1 base", raw: "https://api.stepfun.ai/step_plan/v1", ok: true, wantChat: wantStepFunAIChat, wantModels: wantStepFunAIModels},
		{name: "ai missing v1 base", raw: "https://api.stepfun.ai/step_plan", ok: true, wantChat: wantStepFunAIChat, wantModels: wantStepFunAIModels},
		{name: "ai missing v1 chat", raw: "https://api.stepfun.ai/step_plan/chat/completions", ok: true, wantChat: wantStepFunAIChat, wantModels: wantStepFunAIModels},
		{name: "uppercase hostname", raw: "https://API.STEPFUN.COM/step_plan/v1", ok: true, wantChat: wantStepFunComChat, wantModels: wantStepFunComModels},
		{name: "explicit 443", raw: "https://api.stepfun.com:443/step_plan/v1", ok: true, wantChat: wantStepFunComChat, wantModels: wantStepFunComModels},
		{name: "whitespace around official url", raw: "  https://api.stepfun.com/step_plan/v1  ", ok: true, wantChat: wantStepFunComChat, wantModels: wantStepFunComModels},

		{name: "stepfun apex domain", raw: "https://stepfun.com/step_plan/v1", ok: false},
		{name: "subdomain of stepfun.com", raw: "https://gateway.stepfun.com/step_plan/v1", ok: false},
		{name: "lookalike domain", raw: "https://api.stepfun.com.example.com/step_plan/v1", ok: false},
		{name: "third-party gateway", raw: "https://relay.example.com/step_plan/v1", ok: false},
		{name: "standard api root keeps openai shape", raw: "https://api.stepfun.com/v1", ok: false},
		{name: "standard api chat keeps openai shape", raw: "https://api.stepfun.com/v1/chat/completions", ok: false},
		{name: "bare host", raw: "https://api.stepfun.com", ok: false},
		{name: "http scheme", raw: "http://api.stepfun.com/step_plan/v1", ok: false},
		{name: "non-443 port", raw: "https://api.stepfun.com:8443/step_plan/v1", ok: false},
		{name: "userinfo", raw: "https://user@api.stepfun.com/step_plan/v1", ok: false},
		{name: "query", raw: "https://api.stepfun.com/step_plan/v1?foo=1", ok: false},
		{name: "fragment", raw: "https://api.stepfun.com/step_plan/v1#frag", ok: false},
		{name: "escaped path slash", raw: "https://api.stepfun.com/step_plan%2Fv1", ok: false},
		{name: "unknown path under step_plan", raw: "https://api.stepfun.com/step_plan/openai", ok: false},
		{name: "anthropic messages path", raw: "https://api.stepfun.com/step_plan/v1/messages", ok: false},
		{name: "triple v1", raw: "https://api.stepfun.com/step_plan/v1/v1/v1", ok: false},
		{name: "empty", raw: "", ok: false},
		{name: "garbage", raw: "not a url", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotChat, chatOK := canonicalStepFunPlanChatURL(tt.raw)
			gotModels, modelsOK := CanonicalStepFunPlanModelsURL(tt.raw)
			if chatOK != tt.ok || modelsOK != tt.ok {
				t.Fatalf("ok chat=%v models=%v, want %v", chatOK, modelsOK, tt.ok)
			}
			if !tt.ok {
				if gotChat != "" || gotModels != "" {
					t.Fatalf("rewrote unknown input %q to chat=%q models=%q", tt.raw, gotChat, gotModels)
				}
				return
			}
			if gotChat != tt.wantChat {
				t.Fatalf("chat = %q, want %q", gotChat, tt.wantChat)
			}
			if gotModels != tt.wantModels {
				t.Fatalf("models = %q, want %q", gotModels, tt.wantModels)
			}
			if strings.Count(gotChat, "/v1") != 1 || strings.Count(gotChat, "/chat/completions") != 1 {
				t.Fatalf("chat %q is not a single canonical endpoint", gotChat)
			}
		})
	}
}

func TestNewCanonicalizesStepFunPlanChatURLs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  provider.Config
		want string
	}{
		{
			name: "anthropic-docs base repaired on com host",
			cfg: provider.Config{
				Name:    "stepfun",
				BaseURL: "https://api.stepfun.com/step_plan",
				Model:   "step-3.7-flash",
			},
			want: wantStepFunComChat,
		},
		{
			name: "anthropic-docs base repaired on ai host",
			cfg: provider.Config{
				Name:    "stepfun-en",
				BaseURL: "https://api.stepfun.ai/step_plan",
				Model:   "step-3.7-flash",
			},
			want: wantStepFunAIChat,
		},
		{
			name: "correct v1 base unchanged",
			cfg: provider.Config{
				Name:    "stepfun",
				BaseURL: "https://api.stepfun.com/step_plan/v1",
				Model:   "step-3.7-flash",
			},
			want: wantStepFunComChat,
		},
		{
			name: "request_url stepfun typo is repaired",
			cfg: provider.Config{
				Name:    "custom-gateway",
				BaseURL: "https://example.invalid/v1",
				Model:   "step-3.7-flash",
				Extra: map[string]any{
					"request_url": "https://api.stepfun.com/step_plan/v1/v1/chat/completions",
				},
			},
			want: wantStepFunComChat,
		},
		{
			name: "legacy chat_url stepfun typo is repaired",
			cfg: provider.Config{
				Name:    "stepfun",
				BaseURL: "https://example.invalid/v1",
				Model:   "step-3.7-flash",
				Extra: map[string]any{
					"chat_url": "https://api.stepfun.com/step_plan/chat/completions/",
				},
			},
			want: wantStepFunComChat,
		},
		{
			name: "third-party base_url still appends chat completions",
			cfg: provider.Config{
				Name:    "other",
				BaseURL: "https://base.example.com/v1",
				Model:   "test-model",
			},
			want: "https://base.example.com/v1/chat/completions",
		},
		{
			name: "standard stepfun api root keeps openai shape",
			cfg: provider.Config{
				Name:    "stepfun-standard",
				BaseURL: "https://api.stepfun.com/v1",
				Model:   "step-3.7-flash",
			},
			want: "https://api.stepfun.com/v1/chat/completions",
		},
		{
			name: "unknown official path is not guessed",
			cfg: provider.Config{
				Name:    "stepfun",
				BaseURL: "https://api.stepfun.com/step_plan/openai",
				Model:   "step-3.7-flash",
			},
			want: "https://api.stepfun.com/step_plan/openai/chat/completions",
		},
		{
			name: "third-party request_url wins untouched",
			cfg: provider.Config{
				Name:    "other",
				BaseURL: "https://api.stepfun.com/step_plan",
				Model:   "test-model",
				Extra: map[string]any{
					"request_url": "https://exact.example.com/custom/",
				},
			},
			want: "https://exact.example.com/custom/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := New(tt.cfg)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			got := p.(*client).chatURL
			if got != tt.want {
				t.Fatalf("chatURL = %q, want %q", got, tt.want)
			}
		})
	}
}

// The wizard's model probe and the chat client must agree on the endpoint, so
// an Anthropic-docs base can never pass setup while chat 404s.
func TestStepFunPlanProbeAndChatShareCanonicalURL(t *testing.T) {
	t.Parallel()

	for _, base := range []string{
		"https://api.stepfun.com/step_plan",
		"https://api.stepfun.com/step_plan/",
		"https://api.stepfun.com/step_plan/v1",
		"https://api.stepfun.ai/step_plan",
	} {
		chatURL, chatOK := canonicalStepFunPlanChatURL(base)
		modelsURL, modelsOK := CanonicalStepFunPlanModelsURL(base)
		if !chatOK || !modelsOK {
			t.Fatalf("base %q not recognized: chat=%v models=%v", base, chatOK, modelsOK)
		}
		wantModels := strings.Replace(chatURL, "/chat/completions", "/models", 1)
		if modelsURL != wantModels {
			t.Fatalf("base %q: models %q does not share chat root %q", base, modelsURL, chatURL)
		}
	}
}
