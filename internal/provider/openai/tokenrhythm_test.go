package openai

import (
	"strings"
	"testing"

	"reasonix/internal/provider"
)

const (
	wantTokenRhythmChat   = "https://tokenrhythm.studio/v1/chat/completions"
	wantTokenRhythmModels = "https://tokenrhythm.studio/v1/models"
)

func TestCanonicalTokenRhythmEndpoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		ok   bool
	}{
		{name: "v1 base", raw: "https://tokenrhythm.studio/v1", ok: true},
		{name: "v1 chat", raw: "https://tokenrhythm.studio/v1/chat/completions", ok: true},
		{name: "v1 models", raw: "https://tokenrhythm.studio/v1/models", ok: true},
		{name: "missing v1 root", raw: "https://tokenrhythm.studio", ok: true},
		{name: "missing v1 slash root", raw: "https://tokenrhythm.studio/", ok: true},
		{name: "missing v1 chat", raw: "https://tokenrhythm.studio/chat/completions", ok: true},
		{name: "missing v1 models", raw: "https://tokenrhythm.studio/models", ok: true},
		{name: "double v1 base", raw: "https://tokenrhythm.studio/v1/v1", ok: true},
		{name: "double v1 chat", raw: "https://tokenrhythm.studio/v1/v1/chat/completions", ok: true},
		{name: "double v1 models", raw: "https://tokenrhythm.studio/v1/v1/models", ok: true},
		{name: "repeated chat suffix", raw: "https://tokenrhythm.studio/v1/chat/completions/chat/completions", ok: true},
		{name: "repeated chat suffix without v1", raw: "https://tokenrhythm.studio/chat/completions/chat/completions", ok: true},
		{name: "repeated chat suffix with double v1", raw: "https://tokenrhythm.studio/v1/v1/chat/completions/chat/completions", ok: true},
		{name: "repeated models suffix", raw: "https://tokenrhythm.studio/v1/models/models", ok: true},
		{name: "repeated models suffix without v1", raw: "https://tokenrhythm.studio/models/models", ok: true},
		{name: "repeated models suffix with double v1", raw: "https://tokenrhythm.studio/v1/v1/models/models", ok: true},
		{name: "uppercase hostname", raw: "https://TOKENRHYTHM.STUDIO/v1", ok: true},
		{name: "mixed-case hostname chat", raw: "https://TokenRhythm.Studio/v1/chat/completions", ok: true},
		{name: "explicit 443", raw: "https://tokenrhythm.studio:443/v1", ok: true},
		{name: "explicit 443 models", raw: "https://tokenrhythm.studio:443/v1/models", ok: true},
		{name: "trailing slash base", raw: "https://tokenrhythm.studio/v1/", ok: true},
		{name: "trailing slash chat", raw: "https://tokenrhythm.studio/v1/chat/completions/", ok: true},
		{name: "trailing slash models", raw: "https://tokenrhythm.studio/v1/models/", ok: true},
		{name: "whitespace around official url", raw: "  https://tokenrhythm.studio/v1  ", ok: true},

		{name: "api subdomain", raw: "https://api.tokenrhythm.studio/v1", ok: false},
		{name: "suffix domain", raw: "https://tokenrhythm.studio.example.com/v1", ok: false},
		{name: "third-party gateway", raw: "https://gateway.example.com/v1", ok: false},
		{name: "http scheme", raw: "http://tokenrhythm.studio/v1", ok: false},
		{name: "non-443 port", raw: "https://tokenrhythm.studio:8443/v1", ok: false},
		{name: "userinfo", raw: "https://user@tokenrhythm.studio/v1", ok: false},
		{name: "userinfo password", raw: "https://user:pass@tokenrhythm.studio/v1", ok: false},
		{name: "query", raw: "https://tokenrhythm.studio/v1?foo=1", ok: false},
		{name: "empty query", raw: "https://tokenrhythm.studio/v1?", ok: false},
		{name: "fragment", raw: "https://tokenrhythm.studio/v1#frag", ok: false},
		{name: "empty fragment", raw: "https://tokenrhythm.studio/v1#", ok: false},
		{name: "escaped path slash", raw: "https://tokenrhythm.studio/v1%2Fchat%2Fcompletions", ok: false},
		{name: "escaped v1", raw: "https://tokenrhythm.studio/%76%31", ok: false},
		{name: "invalid url", raw: "://tokenrhythm.studio/v1", ok: false},
		{name: "relative url", raw: "/v1/chat/completions", ok: false},
		{name: "missing scheme", raw: "tokenrhythm.studio/v1", ok: false},
		{name: "unknown path", raw: "https://tokenrhythm.studio/openai", ok: false},
		{name: "triple v1", raw: "https://tokenrhythm.studio/v1/v1/v1", ok: false},
		{name: "chat suffix repeated twice", raw: "https://tokenrhythm.studio/v1/chat/completions/chat/completions/chat/completions", ok: false},
		{name: "anthropic messages", raw: "https://tokenrhythm.studio/v1/messages", ok: false},
		{name: "embeddings", raw: "https://tokenrhythm.studio/v1/embeddings", ok: false},
		{name: "trailing-dot hostname", raw: "https://tokenrhythm.studio./v1", ok: false},
		{name: "empty", raw: "", ok: false},
		{name: "garbage", raw: "not a url", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotChat, chatOK := canonicalTokenRhythmChatURL(tt.raw)
			gotModels, modelsOK := CanonicalTokenRhythmModelsURL(tt.raw)
			if chatOK != tt.ok || modelsOK != tt.ok {
				t.Fatalf("ok chat=%v models=%v, want %v", chatOK, modelsOK, tt.ok)
			}
			if !tt.ok {
				if gotChat != "" || gotModels != "" {
					t.Fatalf("rewrote unknown input %q to chat=%q models=%q", tt.raw, gotChat, gotModels)
				}
				return
			}
			if gotChat != wantTokenRhythmChat {
				t.Fatalf("chat = %q, want %q", gotChat, wantTokenRhythmChat)
			}
			if gotModels != wantTokenRhythmModels {
				t.Fatalf("models = %q, want %q", gotModels, wantTokenRhythmModels)
			}
			if strings.Count(gotChat, "/v1") != 1 || strings.Count(gotChat, "/chat/completions") != 1 {
				t.Fatalf("chat %q is not a single canonical endpoint", gotChat)
			}
			if strings.Count(gotModels, "/v1") != 1 || strings.Count(gotModels, "/models") != 1 {
				t.Fatalf("models %q is not a single canonical endpoint", gotModels)
			}
		})
	}
}

func TestNewCanonicalizesTokenRhythmChatURLs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  provider.Config
		want string
	}{
		{
			name: "request_url wins over legacy chat_url and base_url",
			cfg: provider.Config{
				Name:    "律动",
				BaseURL: "https://tokenrhythm.studio/v1/v1",
				Model:   "test-model",
				Extra: map[string]any{
					"chat_url":    "https://legacy.example.com/chat/completions/",
					"request_url": "https://exact.example.com/custom/?token=1",
				},
			},
			want: "https://exact.example.com/custom/?token=1",
		},
		{
			name: "legacy chat_url wins over base_url",
			cfg: provider.Config{
				Name:    "other",
				BaseURL: "https://base.example.com/v1",
				Model:   "test-model",
				Extra: map[string]any{
					"chat_url": "https://legacy.example.com/chat/completions/",
				},
			},
			want: "https://legacy.example.com/chat/completions",
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
			name: "request_url tokenrhythm typo is repaired",
			cfg: provider.Config{
				Name:    "custom-gateway",
				BaseURL: "https://example.invalid/v1",
				Model:   "test-model",
				Extra: map[string]any{
					"chat_url":    "https://legacy.example.com/chat/completions",
					"request_url": "https://tokenrhythm.studio/v1/v1/chat/completions",
				},
			},
			want: wantTokenRhythmChat,
		},
		{
			name: "legacy chat_url tokenrhythm typo is repaired",
			cfg: provider.Config{
				Name:    "律动",
				BaseURL: "https://example.invalid/v1",
				Model:   "test-model",
				Extra: map[string]any{
					"chat_url": "https://tokenrhythm.studio/chat/completions/",
				},
			},
			want: wantTokenRhythmChat,
		},
		{
			name: "base_url tokenrhythm typo is repaired",
			cfg: provider.Config{
				Name:    "anything",
				BaseURL: "https://tokenrhythm.studio/v1/chat/completions",
				Model:   "test-model",
			},
			want: wantTokenRhythmChat,
		},
		{
			name: "missing v1 base_url is repaired",
			cfg: provider.Config{
				Name:    "律动",
				BaseURL: "https://tokenrhythm.studio",
				Model:   "test-model",
			},
			want: wantTokenRhythmChat,
		},
		{
			name: "third-party request_url keeps query and trailing slash",
			cfg: provider.Config{
				Name:    "other",
				BaseURL: "https://tokenrhythm.studio/v1",
				Model:   "test-model",
				Extra: map[string]any{
					"request_url": "https://exact.example.com/custom/?token=1",
				},
			},
			want: "https://exact.example.com/custom/?token=1",
		},
		{
			name: "third-party request_url keeps trailing slash",
			cfg: provider.Config{
				Name:    "other",
				BaseURL: "https://base.example.com/v1",
				Model:   "test-model",
				Extra: map[string]any{
					"request_url": "https://exact.example.com/custom/",
				},
			},
			want: "https://exact.example.com/custom/",
		},
		{
			name: "unknown official path is not guessed",
			cfg: provider.Config{
				Name:    "律动",
				BaseURL: "https://tokenrhythm.studio/openai",
				Model:   "test-model",
			},
			want: "https://tokenrhythm.studio/openai/chat/completions",
		},
		{
			name: "third-party query on official host is left alone",
			cfg: provider.Config{
				Name:    "律动",
				BaseURL: "https://example.invalid/v1",
				Model:   "test-model",
				Extra: map[string]any{
					"request_url": "https://tokenrhythm.studio/v1/chat/completions?trace=1",
				},
			},
			want: "https://tokenrhythm.studio/v1/chat/completions?trace=1",
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
