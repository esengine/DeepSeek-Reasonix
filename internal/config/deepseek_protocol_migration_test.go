package config

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/BurntSushi/toml"

	fileencoding "reasonix/internal/fileutil/encoding"
)

func TestMigrateLegacyDeepSeekProtocolUserConfigPreservesTOMLAndIsIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := filepath.Join(home, "config.toml")
	raw := `# keep this user comment
config_version = 4
default_model = "deepseek-flash/deepseek-v4-flash"
future_top_level = "preserve-me"

[[providers]]
name        = "deepseek-flash"
kind        = "openai" # legacy wire
base_url    = "https://api.deepseek.com"
model       = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
balance_url = "https://api.deepseek.com/user/balance"
context_window = 1000000

[[providers]]
name        = "deepseek-pro"
kind        = "openai"
base_url    = "https://api.deepseek.com/"
model       = "deepseek-v4-pro"
api_key_env = "DEEPSEEK_API_KEY"

[[providers]]
name = "other"
kind = "openai"
base_url = "https://gateway.example/v1"
model = "other-model"
api_key_env = "OTHER_KEY"
future_provider_field = "untouched"
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	changed, err := MigrateLegacyDeepSeekProtocolUserConfig()
	if err != nil {
		t.Fatalf("MigrateLegacyDeepSeekProtocolUserConfig: %v", err)
	}
	if !changed {
		t.Fatal("legacy official providers were not migrated")
	}
	updatedBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	updated := string(updatedBytes)
	if strings.Count(updated, `kind        = "anthropic"`) != 2 ||
		strings.Count(updated, `base_url    = "https://api.deepseek.com/anthropic"`) != 2 {
		t.Fatalf("migrated provider protocol mismatch:\n%s", updated)
	}
	for _, preserved := range []string{
		"# keep this user comment",
		`future_top_level = "preserve-me"`,
		`future_provider_field = "untouched"`,
		`base_url = "https://gateway.example/v1"`,
		`kind        = "anthropic" # legacy wire`,
	} {
		if !strings.Contains(updated, preserved) {
			t.Errorf("migration dropped %q:\n%s", preserved, updated)
		}
	}

	cfg, err := LoadForEditReadOnlyStrict(path)
	if err != nil {
		t.Fatalf("load migrated config: %v", err)
	}
	for _, name := range []string{"deepseek-flash", "deepseek-pro"} {
		entry, ok := cfg.Provider(name)
		if !ok {
			t.Fatalf("migrated provider %q missing", name)
		}
		if entry.Kind != "anthropic" || entry.BaseURL != deepSeekAnthropicBaseURL ||
			entry.Thinking != "enabled" || !EffectiveWebSearch(entry) ||
			len(entry.SupportedEfforts) == 0 || entry.DefaultEffort != "high" {
			t.Errorf("migrated provider %q capabilities = %+v", name, entry)
		}
	}

	beforeSecondRun := string(updatedBytes)
	changed, err = MigrateLegacyDeepSeekProtocolUserConfig()
	if err != nil {
		t.Fatalf("second migration: %v", err)
	}
	if changed {
		t.Fatal("second migration unexpectedly reported a change")
	}
	afterSecondRun, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterSecondRun) != beforeSecondRun {
		t.Fatal("idempotent migration rewrote the config on its second run")
	}
}

func TestUpgradeDeepSeekProviderProtocolWritesThroughSymlinkAndPreservesMode(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "shared-config.toml")
	link := filepath.Join(dir, "config.toml")
	raw := `[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
`
	if err := os.WriteFile(target, []byte(raw), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}

	changed, err := UpgradeDeepSeekProviderProtocol(link, "deepseek")
	if err != nil {
		t.Fatalf("UpgradeDeepSeekProviderProtocol: %v", err)
	}
	if !changed {
		t.Fatal("symlinked DeepSeek provider was not upgraded")
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("logical config link was replaced: info=%v err=%v", info, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o640 {
			t.Fatalf("migrated target mode = %04o, want 0640", got)
		}
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `kind = "anthropic"`) ||
		!strings.Contains(string(got), `base_url = "https://api.deepseek.com/anthropic"`) {
		t.Fatalf("symlink target was not upgraded:\n%s", got)
	}
}

func TestMigrateLegacyDeepSeekProtocolUserConfigSerializesConcurrentUpgrades(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := filepath.Join(home, "config.toml")
	raw := `[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	const workers = 12
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			<-start
			_, err := MigrateLegacyDeepSeekProtocolUserConfig()
			errs <- err
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent migration: %v", err)
		}
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(got), `kind = "anthropic"`) != 1 ||
		strings.Count(string(got), `base_url = "https://api.deepseek.com/anthropic"`) != 1 {
		t.Fatalf("concurrent migration produced a corrupt or partial config:\n%s", got)
	}
	if _, err := LoadForEditReadOnlyStrict(path); err != nil {
		t.Fatalf("concurrently migrated config is invalid: %v", err)
	}
}

func TestDeepSeekProtocolUpgradeAvailabilityUsesUserConfigSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := filepath.Join(home, "config.toml")
	if err := os.WriteFile(path, []byte("# unrelated user settings\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if CanUpgradeDeepSeekProviderProtocolUserConfig("deepseek") {
		t.Fatal("an unrelated user config must not expose an upgrade for a project-only provider")
	}

	raw := `[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if !CanUpgradeDeepSeekProviderProtocolUserConfig("deepseek") {
		t.Fatal("eligible user-global provider did not expose the grouped upgrade")
	}
	if CanUpgradeDeepSeekProviderProtocolUserConfig("unrelated") {
		t.Fatal("an unrelated provider target unexpectedly exposed the DeepSeek upgrade")
	}
}

func TestMigrateLegacyDeepSeekProtocolPreservesConfigEncoding(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := filepath.Join(home, "config.toml")
	raw := `# preserve UTF-16 configuration
[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
`
	encoded := fileencoding.Encode(raw, fileencoding.UTF16LE)
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}

	changed, err := MigrateLegacyDeepSeekProtocolUserConfig()
	if err != nil || !changed {
		t.Fatalf("MigrateLegacyDeepSeekProtocolUserConfig: changed=%v err=%v", changed, err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(got, []byte{0xff, 0xfe}) {
		t.Fatalf("migrated config lost its UTF-16LE BOM: %x", got[:min(len(got), 8)])
	}
	decoded := string(fileencoding.DecodeToUTF8(got))
	if !strings.Contains(decoded, `kind = "anthropic"`) ||
		!strings.Contains(decoded, `base_url = "https://api.deepseek.com/anthropic"`) ||
		!strings.Contains(decoded, "# preserve UTF-16 configuration") {
		t.Fatalf("migrated UTF-16 config = %q", decoded)
	}
}

func TestDeepSeekProtocolMigrationSupportsInlineProviderArrays(t *testing.T) {
	raw := `config_version = 4
future_prompt = """
[not-a-section]
providers = [{ name = "quoted-example", kind = "openai" }]
"""
providers = [{ name = "deepseek-flash", kind = "openai", base_url = "https://api.deepseek.com", model = "deepseek-v4-flash", api_key_env = "DEEPSEEK_API_KEY" }, { name = "other", kind = "openai", base_url = "https://gateway.example/v1", model = "other-model", api_key_env = "OTHER_KEY", headers = { X-Trace = "keep,=#value" } }, { name = "local", kind = "ollama", model = "local-model" }]
`
	next, changed, err := rewriteLegacyDeepSeekProtocol(raw, "", true)
	if err != nil {
		t.Fatalf("rewrite inline providers: %v", err)
	}
	if !changed {
		t.Fatal("eligible inline DeepSeek provider was not migrated")
	}
	for _, want := range []string{
		`kind = "anthropic"`,
		`base_url = "https://api.deepseek.com/anthropic"`,
		`headers = { X-Trace = "keep,=#value" }`,
		`base_url = "https://gateway.example/v1"`,
		`{ name = "local", kind = "ollama", model = "local-model" }`,
		`providers = [{ name = "quoted-example", kind = "openai" }]`,
	} {
		if !strings.Contains(next, want) {
			t.Errorf("inline migration dropped %q:\n%s", want, next)
		}
	}
	var decoded Config
	if _, err := toml.Decode(next, &decoded); err != nil {
		t.Fatalf("migrated inline TOML is invalid: %v\n%s", err, next)
	}
	if len(decoded.Providers) != 3 || decoded.Providers[0].Kind != "anthropic" || decoded.Providers[1].Kind != "openai" {
		t.Fatalf("migrated inline providers = %+v", decoded.Providers)
	}
	again, changed, err := rewriteLegacyDeepSeekProtocol(next, "", true)
	if err != nil {
		t.Fatalf("second inline migration: %v", err)
	}
	if changed || again != next {
		t.Fatal("inline provider migration is not idempotent")
	}
}

func TestManualDeepSeekProtocolUpgradeSupportsMultilineInlineArray(t *testing.T) {
	raw := `providers = [
  { name = "deepseek-pro", kind = 'openai', base_url = 'https://api.deepseek.com/v1', model = "deepseek-v4-pro", api_key_env = "CUSTOM_KEY", headers = { X-Route = "keep,=#route" }, future_capability = true },
]
`
	next, changed, err := rewriteLegacyDeepSeekProtocol(raw, "deepseek", false)
	if err != nil {
		t.Fatalf("manual inline upgrade: %v", err)
	}
	if !changed || !strings.Contains(next, `kind = "anthropic"`) ||
		!strings.Contains(next, `base_url = "https://api.deepseek.com/anthropic"`) {
		t.Fatalf("manual inline upgrade mismatch:\n%s", next)
	}
	for _, want := range []string{
		`api_key_env = "CUSTOM_KEY"`,
		`headers = { X-Route = "keep,=#route" }`,
		`future_capability = true`,
	} {
		if !strings.Contains(next, want) {
			t.Errorf("manual inline upgrade dropped %q:\n%s", want, next)
		}
	}
}

func TestAutomaticDeepSeekProtocolMigrationKeepsCustomizedProviders(t *testing.T) {
	tests := []struct {
		name  string
		extra string
	}{
		{name: "proxy endpoint", extra: `base_url = "https://proxy.example/v1"`},
		{name: "custom headers", extra: `headers = { X-Route = "custom" }`},
		{name: "explicit model list", extra: `models = ["deepseek-v4-flash"]`},
		{name: "vision override", extra: `vision = true`},
		{name: "reasoning override", extra: `reasoning_protocol = "none"`},
		{name: "effort override", extra: `supported_efforts = ["high"]`},
		{name: "custom key", extra: `api_key_env = "MY_DEEPSEEK_KEY"`},
		{name: "unknown future field", extra: `future_capability = true`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseURL := `base_url = "https://api.deepseek.com"`
			apiKey := `api_key_env = "DEEPSEEK_API_KEY"`
			model := `model = "deepseek-v4-flash"`
			switch {
			case strings.HasPrefix(tt.extra, "base_url"):
				baseURL = tt.extra
			case strings.HasPrefix(tt.extra, "api_key_env"):
				apiKey = tt.extra
			case strings.HasPrefix(tt.extra, "models"):
				model = tt.extra
			}
			extra := tt.extra
			if tt.extra == baseURL || tt.extra == apiKey || tt.extra == model {
				extra = ""
			}
			raw := `[[providers]]
name = "deepseek-flash"
kind = "openai"
` + baseURL + "\n" + model + "\n" + apiKey + "\n" + extra + "\n"
			next, changed, err := rewriteLegacyDeepSeekProtocol(raw, "", true)
			if err != nil {
				t.Fatalf("rewriteLegacyDeepSeekProtocol: %v", err)
			}
			if changed || next != raw {
				t.Fatalf("customized provider was automatically migrated:\n%s", next)
			}
		})
	}
}

func TestManualDeepSeekProtocolUpgradePreservesCapabilitiesAndUnknownFields(t *testing.T) {
	raw := `[[providers]]
name = "deepseek-flash"
kind = 'openai'
base_url = 'https://api.deepseek.com'
model = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
vision = true
future_capability = "keep"

[[providers]]
name = "deepseek-pro"
kind = "openai"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-pro"
api_key_env = "DEEPSEEK_API_KEY"
reasoning_protocol = "none"
`
	next, changed, err := rewriteLegacyDeepSeekProtocol(raw, "deepseek", false)
	if err != nil {
		t.Fatalf("rewriteLegacyDeepSeekProtocol: %v", err)
	}
	if !changed || strings.Count(next, `kind = "anthropic"`) != 2 ||
		strings.Count(next, `base_url = "https://api.deepseek.com/anthropic"`) != 2 {
		t.Fatalf("manual family upgrade mismatch:\n%s", next)
	}
	for _, preserved := range []string{
		`vision = true`,
		`future_capability = "keep"`,
		`reasoning_protocol = "none"`,
	} {
		if !strings.Contains(next, preserved) {
			t.Errorf("manual upgrade dropped %q:\n%s", preserved, next)
		}
	}
}

func TestDeepSeekProtocolMigrationSupportsQuotedKeys(t *testing.T) {
	raw := `[[providers]]
"name" = "deepseek-flash"
'kind' = 'openai'
"base_url" = 'https://api.deepseek.com'
'model' = "deepseek-v4-flash"
"api_key_env" = "DEEPSEEK_API_KEY"
`
	next, changed, err := rewriteLegacyDeepSeekProtocol(raw, "", true)
	if err != nil {
		t.Fatalf("rewrite quoted-key provider: %v", err)
	}
	if !changed {
		t.Fatal("quoted-key provider was not migrated")
	}
	for _, want := range []string{
		`'kind' = "anthropic"`,
		`"base_url" = "https://api.deepseek.com/anthropic"`,
	} {
		if !strings.Contains(next, want) {
			t.Errorf("migration changed or dropped %q:\n%s", want, next)
		}
	}
	var decoded Config
	if _, err := toml.Decode(next, &decoded); err != nil {
		t.Fatalf("migrated quoted-key TOML is invalid: %v\n%s", err, next)
	}
	if len(decoded.Providers) != 1 || decoded.Providers[0].Kind != "anthropic" || decoded.Providers[0].BaseURL != deepSeekAnthropicBaseURL {
		t.Fatalf("migrated quoted-key provider = %+v", decoded.Providers)
	}
}

func TestDeepSeekProtocolMigrationSupportsQuotedProviderTableHeaders(t *testing.T) {
	raw := `[["providers"]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"

[['providers']]
name = "deepseek-pro"
kind = "openai"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-pro"
api_key_env = "DEEPSEEK_API_KEY"
`
	next, changed, err := rewriteLegacyDeepSeekProtocol(raw, "", true)
	if err != nil {
		t.Fatalf("rewrite quoted provider table headers: %v", err)
	}
	if !changed || strings.Count(next, `kind = "anthropic"`) != 2 ||
		strings.Count(next, `base_url = "https://api.deepseek.com/anthropic"`) != 2 {
		t.Fatalf("quoted provider table headers were not migrated:\n%s", next)
	}
	for _, header := range []string{`[["providers"]]`, `[['providers']]`} {
		if !strings.Contains(next, header) {
			t.Errorf("migration changed provider table header %q:\n%s", header, next)
		}
	}
	var decoded Config
	if _, err := toml.Decode(next, &decoded); err != nil {
		t.Fatalf("migrated quoted-header TOML is invalid: %v\n%s", err, next)
	}
	if len(decoded.Providers) != 2 || decoded.Providers[0].Kind != "anthropic" || decoded.Providers[1].Kind != "anthropic" {
		t.Fatalf("migrated quoted-header providers = %+v", decoded.Providers)
	}
}

func TestManualDeepSeekProtocolUpgradeSkipsMultilineProviderText(t *testing.T) {
	raw := `[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
description = '''
kind = "example"
base_url = "https://example.invalid"
'''
`
	next, changed, err := rewriteLegacyDeepSeekProtocol(raw, "deepseek", false)
	if err != nil {
		t.Fatalf("manual multiline upgrade: %v", err)
	}
	if !changed || !strings.Contains(next, `kind = "anthropic"`) || !strings.Contains(next, `base_url = "https://api.deepseek.com/anthropic"`) {
		t.Fatalf("manual multiline upgrade mismatch:\n%s", next)
	}
	for _, want := range []string{
		`kind = "example"`,
		`base_url = "https://example.invalid"`,
	} {
		if !strings.Contains(next, want) {
			t.Errorf("multiline provider text changed or dropped %q:\n%s", want, next)
		}
	}
	var decoded Config
	if _, err := toml.Decode(next, &decoded); err != nil {
		t.Fatalf("migrated multiline TOML is invalid: %v\n%s", err, next)
	}
	if len(decoded.Providers) != 1 || decoded.Providers[0].Kind != "anthropic" {
		t.Fatalf("migrated multiline provider = %+v", decoded.Providers)
	}
}

func TestCanUpgradeDeepSeekProviderProtocolRejectsProxyButAllowsExplicitUpgradeOfCustomization(t *testing.T) {
	base := ProviderEntry{
		Name: "deepseek-flash", Kind: "openai", BaseURL: "https://api.deepseek.com",
		Model: "deepseek-v4-flash", APIKeyEnv: "DEEPSEEK_API_KEY",
	}
	if !CanUpgradeDeepSeekProviderProtocol(&base) {
		t.Fatal("standard official provider should offer manual upgrade")
	}
	proxy := base
	proxy.BaseURL = "https://deepseek.example/v1"
	if CanUpgradeDeepSeekProviderProtocol(&proxy) {
		t.Fatal("proxy endpoint should not offer manual upgrade")
	}
	headers := base
	headers.Headers = map[string]string{"X-Route": "custom"}
	if !CanUpgradeDeepSeekProviderProtocol(&headers) {
		t.Fatal("custom headers should block automatic migration, not the explicit upgrade action")
	}
	versioned := base
	versioned.BaseURL = "https://api.deepseek.com/v1"
	if !CanUpgradeDeepSeekProviderProtocol(&versioned) {
		t.Fatal("the official /v1 compatibility address should offer the explicit upgrade action")
	}
	customKey := base
	customKey.APIKeyEnv = "MY_DEEPSEEK_KEY"
	if !CanUpgradeDeepSeekProviderProtocol(&customKey) {
		t.Fatal("an official provider with a custom key env should offer the explicit upgrade action")
	}
}

func TestNormalizeOfficialDeepSeekModelsDoesNotAddProToResponses(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{{
		Name: "deepseek", Kind: "responses", BaseURL: "https://api.deepseek.com",
		Model: "deepseek-v4-flash",
	}}}

	normalizeOfficialDeepSeekModels(c)
	p, ok := c.Provider("deepseek")
	if !ok {
		t.Fatal("DeepSeek provider missing after normalization")
	}
	if !p.HasModel("deepseek-v4-flash") {
		t.Fatal("Responses provider lost its Flash model")
	}
	if p.HasModel("deepseek-v4-pro") {
		t.Fatalf("Responses normalization added unsupported Pro model: %+v", p.ModelList())
	}
}
