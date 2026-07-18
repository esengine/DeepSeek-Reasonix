package configsummary

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/remote/protocol"
)

func TestSummaryIsDeterministicWireValidShape(t *testing.T) {
	state := sourceState{userConfigPresent: true, memoryCompilerActive: true}
	service, err := New(protocol.FrozenCapabilities(true, false))
	if err != nil {
		t.Fatal(err)
	}
	service.read = func(context.Context) (sourceState, error) { return state, nil }

	first, err := service.Summary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Summary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("summary is not deterministic:\nfirst=%+v\nsecond=%+v", first, second)
	}
	if !strings.HasPrefix(string(first.Revision), "host_config_") || len(first.Revision) != len("host_config_")+64 {
		t.Fatalf("revision = %q", first.Revision)
	}
	wantScopes := []protocol.EffectiveScope{
		{Name: "built-in", Active: true}, {Name: "user", Active: true}, {Name: "workspace", Active: true},
	}
	if !reflect.DeepEqual(first.EffectiveScopes, wantScopes) {
		t.Fatalf("effective scopes = %+v, want %+v", first.EffectiveScopes, wantScopes)
	}
	if len(first.DisplayPaths) != 2 || first.DisplayPaths[0].DisplayPath != userDisplayPath || first.DisplayPaths[1].DisplayPath != workspaceDisplayPath {
		t.Fatalf("display paths = %+v", first.DisplayPaths)
	}
	if len(first.FeatureStates) != 3 || !first.FeatureStates[0].Available || !first.FeatureStates[1].Available || first.FeatureStates[2].Available {
		t.Fatalf("feature states = %+v", first.FeatureStates)
	}
	for _, hint := range first.CLIHints {
		switch hint.Command {
		case "reasonix setup", "reasonix remote status", "reasonix remote doctor":
		default:
			t.Fatalf("uncontrolled CLI hint = %+v", hint)
		}
	}
}

func TestSummaryNeverProjectsOrHashesSensitiveConfig(t *testing.T) {
	secretA := adversarialConfig("alpha")
	secretB := adversarialConfig("beta")
	project := func(cfg *config.Config) protocol.HostConfigSummaryResult {
		service, err := New(protocol.FrozenCapabilities(true, true))
		if err != nil {
			t.Fatal(err)
		}
		service.read = func(context.Context) (sourceState, error) {
			// Exercise the same production projection boundary with a full
			// adversarial config: only normalized safe booleans may cross it.
			return sourceStateFromConfig(cfg, true, false), nil
		}
		result, err := service.Summary(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return result
	}

	first := project(secretA)
	second := project(secretB)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("hidden config changed the safe projection or revision:\nfirst=%+v\nsecond=%+v", first, second)
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, forbidden := range []string{
		"alpha", "beta", "provider-secret", "TOP_SECRET_ENV", "secret.example",
		"Authorization", "/srv/private", "/opt/private", "mcp-secret", "bearer-secret",
		"--token", "diagnostic stack", "skill body",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("summary leaked %q: %s", forbidden, text)
		}
	}
	if strings.Contains(text, "/home/") || strings.Contains(text, `C:\\`) {
		t.Fatalf("summary leaked a raw Host path: %s", text)
	}
}

func TestProductionSummaryDoesNotResolveCredentialStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	key := "REASONIX_CONFIG_SUMMARY_TEST_SECRET"
	previous, hadPrevious := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if hadPrevious {
			_ = os.Setenv(key, previous)
		} else {
			_ = os.Unsetenv(key)
		}
	})
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	configBody := `[agent]
memory_compiler = { enabled = false }

[[providers]]
name = "credential-probe"
kind = "openai"
base_url = "https://private.example/v1"
model = "private-model"
api_key_env = "` + key + `"
`
	if err := os.WriteFile(config.UserConfigPath(), []byte(configBody), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.UserCredentialsPath(), []byte(key+"=credential-store-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	service, err := New(protocol.FrozenCapabilities(true, true))
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Summary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, resolved := os.LookupEnv(key); resolved {
		t.Fatalf("host/configSummary resolved %s into the process environment", key)
	}
	if result.FeatureStates[1].Feature != "memoryCompiler" || result.FeatureStates[1].Available {
		t.Fatalf("memory compiler state = %+v", result.FeatureStates)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"credential-store-secret", "private.example", "credential-probe", key, home} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("production summary leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestSummarySafeModeDisablesMutableConfigScopes(t *testing.T) {
	service, err := New(protocol.FrozenCapabilities(false, true))
	if err != nil {
		t.Fatal(err)
	}
	service.read = func(context.Context) (sourceState, error) {
		return sourceState{userConfigPresent: true, safeMode: true, memoryCompilerActive: true}, nil
	}
	result, err := service.Summary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.EffectiveScopes[0] != (protocol.EffectiveScope{Name: "built-in", Active: true}) || result.EffectiveScopes[1].Active || result.EffectiveScopes[2].Active {
		t.Fatalf("safe-mode scopes = %+v", result.EffectiveScopes)
	}
	if result.FeatureStates[0].Available || result.FeatureStates[1].Available || !result.FeatureStates[2].Available {
		t.Fatalf("safe-mode features = %+v", result.FeatureStates)
	}
}

func TestSummaryPropagatesCancellationAndReadFailure(t *testing.T) {
	service, err := New(protocol.FrozenCapabilities(false, false))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Summary(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Summary error = %v", err)
	}
	want := errors.New("malformed private config at /not-for-wire")
	service.read = func(context.Context) (sourceState, error) { return sourceState{}, want }
	if _, err := service.Summary(context.Background()); !errors.Is(err, want) {
		t.Fatalf("read failure = %v, want %v", err, want)
	}
}

func TestNewRejectsCapabilityDrift(t *testing.T) {
	capabilities := protocol.FrozenCapabilities(false, false)
	capabilities.Features.SFTP = true
	if _, err := New(capabilities); err == nil {
		t.Fatal("New accepted deferred capability drift")
	}
}

func adversarialConfig(secret string) *config.Config {
	cfg := config.Default()
	cfg.DefaultModel = "provider-secret/model-" + secret
	cfg.Providers = []config.ProviderEntry{{
		Name: "provider-secret", Kind: "openai", Model: "model-" + secret,
		BaseURL: "https://" + secret + ".secret.example/v1", APIKeyEnv: "TOP_SECRET_ENV",
		Headers: map[string]string{"Authorization": "bearer-secret-" + secret},
	}}
	cfg.Plugins = []config.PluginEntry{{
		Name: "mcp-secret", Command: "/opt/private/mcp-" + secret,
		Args: []string{"--token", secret}, URL: "https://mcp.secret.example/" + secret,
		Env: map[string]string{"TOKEN": "bearer-secret-" + secret},
	}}
	cfg.Network.ProxyURL = "https://proxy.secret.example/" + secret
	cfg.Network.Proxy.Password = "proxy-password-" + secret
	cfg.Skills.Paths = []string{"/srv/private/skill-" + secret, "skill body " + secret}
	cfg.Statusline.Command = "diagnostic stack " + secret
	cfg.Bot.Connections = []config.BotConnectionConfig{{
		ID: "bot-" + secret, Credential: config.BotConnectionCredential{TokenEnv: "BOT_SECRET_" + secret},
		WorkspaceRoot: "/srv/private/workspace-" + secret, LastError: "diagnostic stack " + secret,
	}}
	return cfg
}
