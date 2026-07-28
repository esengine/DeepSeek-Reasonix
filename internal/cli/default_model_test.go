package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/event"
)

const (
	defaultModelTestConfiguredEnv = "REASONIX_CLI_TEST_CONFIGURED_KEY"
	defaultModelTestKeylessEnv    = "REASONIX_CLI_TEST_KEYLESS_KEY"
)

func newDefaultModelTestConfig() *config.Config {
	return &config.Config{
		Providers: []config.ProviderEntry{
			{Name: "deepseek-flash", Kind: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", APIKeyEnv: defaultModelTestKeylessEnv},
			{Name: "audio", Kind: "openai", BaseURL: "https://audio.example.com", Model: "tts-1", APIKeyEnv: defaultModelTestConfiguredEnv},
			{Name: "embedding", Kind: "openai", BaseURL: "https://embedding.example.com", Model: "text-embedding-3-small", APIKeyEnv: defaultModelTestConfiguredEnv},
			{Name: "minimax", Kind: "openai", BaseURL: "https://api.MiniMax.chat/v1", Model: "MiniMax-M3", APIKeyEnv: defaultModelTestConfiguredEnv},
		},
	}
}

func TestResolveModelForCLI(t *testing.T) {
	cases := []struct {
		name         string
		explicitRef  string
		defaultModel string
		configured   bool
		keyless      bool
		wantRef      string
		wantFallback bool
		wantErrSub   string
	}{
		{
			// The original bug: default_model points to a keyless provider
			// while another provider has a configured key. CLI must fall
			// through to that provider rather than fail on the keyless
			// default. Issue #6996.
			name:         "keyless default falls back to configured provider",
			explicitRef:  "",
			defaultModel: "deepseek-flash/deepseek-v4-flash",
			configured:   true,
			keyless:      false,
			wantRef:      "minimax/MiniMax-M3",
			wantFallback: true,
		},
		{
			name:         "configured default is used verbatim",
			explicitRef:  "",
			defaultModel: "minimax/MiniMax-M3",
			configured:   true,
			keyless:      false,
			wantRef:      "minimax/MiniMax-M3",
			wantFallback: false,
		},
		{
			// Nothing configured anywhere. The raw keyless default is
			// returned so the boot-time missing-key banner can still tell
			// the user which API key to set.
			name:         "all keyless preserves raw default for downstream banner",
			explicitRef:  "",
			defaultModel: "deepseek-flash/deepseek-v4-flash",
			configured:   false,
			keyless:      false,
			wantRef:      "deepseek-flash/deepseek-v4-flash",
			wantFallback: false,
		},
		{
			name:         "empty default falls back to first configured provider",
			explicitRef:  "",
			defaultModel: "",
			configured:   true,
			keyless:      false,
			wantRef:      "minimax/MiniMax-M3",
			wantFallback: true,
		},
		{
			name:         "empty default with all keyless providers keeps chat candidate",
			explicitRef:  "",
			defaultModel: "",
			configured:   false,
			keyless:      false,
			wantRef:      "deepseek-flash/deepseek-v4-flash",
			wantFallback: true,
		},
		{
			name:         "explicit unknown ref errors",
			explicitRef:  "does-not-exist/whatever",
			defaultModel: "minimax/MiniMax-M3",
			configured:   true,
			keyless:      false,
			wantErrSub:   "unknown model",
		},
		{
			// User passed --model with a keyless ref. Must fail loudly even
			// when another provider is configured and could be used; the
			// user asked for a specific model.
			name:         "explicit keyless ref errors even with configured fallback available",
			explicitRef:  "deepseek-flash/deepseek-v4-flash",
			defaultModel: "minimax/MiniMax-M3",
			configured:   true,
			keyless:      false,
			wantErrSub:   "requires " + defaultModelTestKeylessEnv,
		},
		{
			name:         "explicit configured ref is used verbatim",
			explicitRef:  "minimax/MiniMax-M3",
			defaultModel: "deepseek-flash/deepseek-v4-flash",
			configured:   true,
			keyless:      false,
			wantRef:      "minimax/MiniMax-M3",
			wantFallback: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateCLIConfigHome(t)
			setCredential(t, defaultModelTestConfiguredEnv, tc.configured)
			setCredential(t, defaultModelTestKeylessEnv, tc.keyless)

			cfg := newDefaultModelTestConfig()
			cfg.DefaultModel = tc.defaultModel

			got, gotFallback, err := resolveModelForCLI(tc.explicitRef, cfg)
			if tc.wantErrSub != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got (ref=%q, fallback=%v, nil err)", tc.wantErrSub, got, gotFallback)
				}
				if !strings.Contains(err.Error(), tc.wantErrSub) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErrSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantRef {
				t.Fatalf("ref = %q, want %q", got, tc.wantRef)
			}
			if gotFallback != tc.wantFallback {
				t.Fatalf("fallback = %v, want %v", gotFallback, tc.wantFallback)
			}
		})
	}
}

func TestResolveServeModelUsesGlobalChatFallback(t *testing.T) {
	isolateCLIConfigHome(t)
	setCredential(t, defaultModelTestConfiguredEnv, true)
	setCredential(t, defaultModelTestKeylessEnv, false)

	cfg := newDefaultModelTestConfig()
	cfg.DefaultModel = "deepseek-flash/deepseek-v4-flash"
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("reasonix.toml", []byte(`
default_model = "project/project-chat"

[[providers]]
name = "project"
kind = "openai"
base_url = "https://project.example.com"
model = "project-chat"
api_key_env = "`+defaultModelTestConfiguredEnv+`"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := resolveServeModel(""); got != "minimax/MiniMax-M3" {
		t.Fatalf("resolveServeModel(\"\") = %q, want global chat fallback", got)
	}
	if got := resolveServeModel("explicit/chat"); got != "explicit/chat" {
		t.Fatalf("resolveServeModel(explicit) = %q, want explicit model preserved", got)
	}
}

func TestNewChatTUIKeepsExplicitKeylessControllerModel(t *testing.T) {
	isolateCLIConfigHome(t)
	setCredential(t, defaultModelTestConfiguredEnv, true)
	setCredential(t, defaultModelTestKeylessEnv, false)

	cfg := newDefaultModelTestConfig()
	cfg.DefaultModel = "minimax/MiniMax-M3"
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}

	const explicit = "deepseek-flash/deepseek-v4-flash"
	ctrl, err := setupProfile(context.Background(), explicit, 0, false, event.Discard, "balanced", "")
	if err != nil {
		t.Fatalf("interactive build should remain reachable for missing-key recovery: %v", err)
	}
	defer ctrl.Close()

	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	if got := m.modelRef; got != explicit {
		t.Fatalf("TUI modelRef = %q, want explicit controller model %q", got, explicit)
	}
}

// setCredential writes a "configured" sentinel key into Reasonix's user
// credentials store, or clears it. ProviderEntry.Configured() resolves keys
// only from that store (not from process env), so this is the only way to
// flip a test provider between configured and keyless.
func setCredential(t *testing.T, key string, configured bool) {
	t.Helper()
	if configured {
		if _, err := config.SetCredential(key, "sk-test"); err != nil {
			t.Fatalf("SetCredential(%s): %v", key, err)
		}
		return
	}
	if err := config.RemoveCredential(key); err != nil {
		t.Fatalf("RemoveCredential(%s): %v", key, err)
	}
	// Make sure the credentials file exists even after a clear so a follow-up
	// read on a different path does not synthesize a real one. Empty file is
	// fine; storedCredentialValue returns ("", false) for missing keys.
	if _, err := os.Stat(config.UserCredentialsPath()); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(config.UserCredentialsPath()), 0o700); err != nil {
			t.Fatalf("mkdir credentials dir: %v", err)
		}
		if err := os.WriteFile(config.UserCredentialsPath(), []byte{}, 0o600); err != nil {
			t.Fatalf("write empty credentials: %v", err)
		}
	}
}
