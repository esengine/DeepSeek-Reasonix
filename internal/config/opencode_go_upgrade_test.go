package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/BurntSushi/toml"
	"reasonix/internal/fileutil"
	fileencoding "reasonix/internal/fileutil/encoding"
)

const openCodeGoUpgradeFixture = `# keep the user's introduction
config_version = 9 # version comment
default_model = "go/deepseek-v4-pro"
future_root = "untouched"
[agent]
planner_model = "go/deepseek-v4-flash"
vision_model = "go/deepseek-v4-flash-vision-exp"
guardian_model = "go/deepseek-v4-pro"
recovery_model = "go/deepseek-v4-pro"
subagent_model = "go/deepseek-v4-pro"
web_search_model = "go/deepseek-v4-flash"
[agent.subagent_models]
review = "go/deepseek-v4-pro"
[desktop]
provider_access = ["go"]
[[providers]]
name = "go"
kind = "anthropic" # API comment
base_url = "https://opencode.ai/zen/go"
request_url = "https://opencode.ai/zen/go/v1/messages"
api_key_env = "ACCOUNT_A_KEY"
models = [
  "deepseek-v4-pro", # favorite
  "deepseek-v4-flash", "deepseek-v4-flash-vision-exp",
  "qwen3.8-max", "grok-4.5", "unknown-future-model"
]
default = "deepseek-v4-pro"
thinking = "enabled"
effort = "max"
web_search = true
max_output_tokens = 777
future_provider = { strange = "kept" }
[providers.headers]
X-User = "my-header"
[providers.prices.deepseek-v4-pro]
input = 0.123
output = 0.456
[providers.model_overrides.deepseek-v4-pro]
context_window = 765432
max_output_tokens = 555
future_override = "keep too"
[[providers]]
name = "go-chat"
kind = "openai"
base_url = "https://opencode.ai/zen/go/v1"
api_key_env = "ACCOUNT_B_KEY"
model = "deepseek-v4-pro"
web_search = false
[bot]
model = "go/deepseek-v4-pro"
[[bot.connections]]
name = "a"
model = "go/deepseek-v4-pro"
[[bot.connections]]
name = "b"
model = "go/deepseek-v4-flash"
`

func TestOpenCodeGoV10MigrationPreservesAccountsHistorySearchAndRawFields(t *testing.T) {
	t.Setenv("ACCOUNT_A_KEY", "test-account-a")
	t.Setenv("ACCOUNT_B_KEY", "test-account-b")
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(openCodeGoUpgradeFixture), 0600); err != nil {
		t.Fatal(err)
	}
	if changed, err := ApplyUserConfigUpgradesOnStartup(path); err != nil || !changed {
		t.Fatalf("upgrade=%v, %v", changed, err)
	}
	raw, _ := os.ReadFile(path)
	for _, text := range []string{"# keep the user's introduction", "# version comment", "# API comment", "# favorite", `future_root = "untouched"`, `future_provider = { strange = "kept" }`, `future_override = "keep too"`} {
		if !bytes.Contains(raw, []byte(text)) {
			t.Errorf("lost %s", text)
		}
	}
	var cfg Config
	if _, err := toml.Decode(string(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	cfg.loadOpenCodeGoJournal(path)
	for i := range cfg.Providers {
		cfg.Providers[i].resolvedAPIKey = "test-resolved-key"
	}
	if cfg.ConfigVersion != 10 || len(cfg.Providers) != 5 {
		t.Fatalf("version/providers=%d/%d", cfg.ConfigVersion, len(cfg.Providers))
	}
	if cfg.DefaultModel != "go-chat-2/deepseek-v4-pro" {
		t.Fatal(cfg.DefaultModel)
	}
	for _, ref := range []string{"go/deepseek-v4-pro", "go", "deepseek-v4-pro"} {
		target, err := cfg.ResolveHistoricalModel(ref)
		if err != nil {
			t.Fatal(err)
		}
		e, ok := cfg.ResolveModel(target)
		if !ok || e.Name != "go-chat-2" || e.APIKeyEnv != "ACCOUNT_A_KEY" || e.Kind != "openai" || e.Effort != "max" {
			t.Fatalf("alias %s: %+v, %v", ref, e, ok)
		}
		if e.ContextWindow != 765432 || e.MaxOutputTokens != 555 || e.Price.Input != 0.123 {
			t.Fatalf("lost model settings: %+v", e)
		}
		if err := ReasoningCapabilityForEntry(e).Validate(e.Model, EffectiveEffort(e)); err != nil {
			t.Fatal(err)
		}
	}
	for _, ref := range []string{cfg.Agent.PlannerModel, cfg.Agent.VisionModel, cfg.Agent.GuardianModel, cfg.Agent.RecoveryModel, cfg.Agent.SubagentModel, cfg.Agent.SubagentModels["review"], cfg.Bot.Model, cfg.Bot.Connections[0].Model, cfg.Bot.Connections[1].Model} {
		if !strings.HasPrefix(ref, "go-chat-2/") {
			t.Errorf("unmigrated role %q", ref)
		}
	}
	for _, ref := range []string{cfg.Agent.WebSearchModel, "go/deepseek-v4-flash"} {
		e, err := cfg.ResolveWebSearchModel(ref)
		if err != nil || e.Kind != "anthropic" || e.APIKeyEnv != "ACCOUNT_A_KEY" {
			t.Fatalf("search %q: %+v, %v", ref, e, err)
		}
	}
	chat, _ := cfg.ResolveModel(cfg.DefaultModel)
	cfg.Agent.WebSearchModel = "auto"
	if search := cfg.ResolveWebSearchProvider(chat); search == nil || search.APIKeyEnv != "ACCOUNT_A_KEY" || search.Kind != "anthropic" {
		t.Fatalf("automatic search: %+v", search)
	}
	chat.WebSearch = boolPointer(false)
	if result := cfg.ResolveWebSearch(chat); result.Status != "disabled" {
		t.Fatalf("disabled search: %+v", result)
	}
	backup, _ := os.ReadFile(path + ".opencode-go-v10.backup")
	if string(backup) != openCodeGoUpgradeFixture {
		t.Fatal("backup differs")
	}
	for _, suffix := range []string{".opencode-go-v10.backup", ".opencode-go-v10.json"} {
		info, err := os.Stat(path + suffix)
		// Windows carries no POSIX permission bits, so only the Unix legs can
		// prove the sidecar is private.
		if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
			t.Fatalf("private sidecar: %v %v", info, err)
		}
	}
	if changed, err := ApplyUserConfigUpgradesOnStartup(path); err != nil || changed {
		t.Fatalf("repeat: %v %v", changed, err)
	}
	second, _ := os.ReadFile(path)
	if !bytes.Equal(raw, second) {
		t.Fatal("second startup wrote configuration")
	}
	p, _ := cfg.Provider("go-chat-2")
	p.APIKeyEnv = "ACCOUNT_B_KEY"
	if _, _, ok := cfg.ResolveModelWithFallback("go/deepseek-v4-pro"); ok {
		t.Fatal("changed account silently fell back")
	}
	if err := cfg.ModelReferenceError("go/deepseek-v4-pro"); err == nil || !strings.Contains(err.Error(), "MIGRATED_MODEL_UNAVAILABLE") {
		t.Fatal(err)
	}
}

func TestOpenCodeGoV10MigrationVersionsAndInline(t *testing.T) {
	for _, version := range []int{7, 8, 9} {
		for _, inline := range []bool{false, true} {
			t.Run(fmt.Sprintf("v%d/inline=%v", version, inline), func(t *testing.T) {
				body := fmt.Sprintf("config_version = %d\n", version)
				if inline {
					body += `providers = [{name="go", kind="anthropic", base_url="https://opencode.ai/zen/go", request_url="https://opencode.ai/zen/go/v1/messages", api_key_env="GO_KEY", model="deepseek-v4-flash", thinking="enabled", effort="max", future={keep="yes"}}] # keep inline` + "\n"
				} else {
					body += "[[providers]]\nname='go'\nkind='anthropic'\nbase_url='https://opencode.ai/zen/go'\nmodel='deepseek-v4-flash'\nthinking='enabled'\neffort='max'\n"
				}
				path := filepath.Join(t.TempDir(), "config.toml")
				_ = os.WriteFile(path, []byte(body), 0600)
				if _, err := ApplyUserConfigUpgradesOnStartup(path); err != nil {
					t.Fatal(err)
				}
				var c Config
				if _, err := decodeTOMLFile(path, &c); err != nil {
					t.Fatal(err)
				}
				if c.ConfigVersion != 10 || c.Providers[0].Kind != "openai" || c.Providers[0].Effort != "max" {
					t.Fatalf("%+v", c.Providers)
				}
				if inline {
					raw, _ := os.ReadFile(path)
					if !bytes.Contains(raw, []byte(`# keep inline`)) || !bytes.Contains(raw, []byte(`future={keep="yes"}`)) {
						t.Fatal(string(raw))
					}
				}
			})
		}
	}
}

func TestOpenCodeGoV10PreparedJournalAndDowngrade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	_ = os.WriteFile(path, []byte(openCodeGoUpgradeFixture), 0600)
	if _, err := ApplyUserConfigUpgradesOnStartup(path); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	j := readOpenCodeGoJournal(path, raw)
	if j == nil {
		t.Fatal("missing journal")
	}
	j.Committed = false
	data, _ := json.Marshal(j)
	_ = os.WriteFile(path+".opencode-go-v10.json", data, 0600)
	if readOpenCodeGoJournal(path, []byte(openCodeGoUpgradeFixture)) != nil {
		t.Fatal("prepared mapping activated before commit")
	}
	if readOpenCodeGoJournal(path, raw) == nil {
		t.Fatal("committed config hash did not recover prepared journal")
	}
	j.Committed = true
	data, _ = json.Marshal(j)
	_ = os.WriteFile(path+".opencode-go-v10.json", data, 0600)
	// Simulate an older renderer saving version 9 without new internal fields.
	var c Config
	_, _ = toml.Decode(string(raw), &c)
	c.ConfigVersion = 9
	if err := c.SaveToScope(path, RenderScopeFull); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyUserConfigUpgradesOnStartup(path); err != nil {
		t.Fatal(err)
	}
	var restored Config
	_, _ = decodeTOMLFile(path, &restored)
	restored.loadOpenCodeGoJournal(path)
	if len(restored.Providers) != len(c.Providers) {
		t.Fatal("downgrade created duplicate search connections")
	}
	if e, ok := restored.ResolveModel("go/deepseek-v4-pro"); !ok || e.Name != "go-chat-2" {
		t.Fatalf("lost historical alias: %+v", e)
	}
}

func TestOpenCodeGoV10ConcurrentAndEncoding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	// UTF-16LE with a BOM is a supported user configuration encoding.
	raw := []byte{0xff, 0xfe}
	for _, b := range []byte(openCodeGoUpgradeFixture) {
		raw = append(raw, b, 0)
	}
	_ = os.WriteFile(path, raw, 0600)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if _, err := ApplyUserConfigUpgradesOnStartup(path); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	next, _ := os.ReadFile(path)
	if !bytes.HasPrefix(next, []byte{0xff, 0xfe}) {
		t.Fatal("lost UTF16 encoding")
	}
	_, decoded := fileencoding.Detect(next)
	if len(decoded) == 0 {
		t.Fatal("empty output")
	}
	var c Config
	if _, err := decodeTOMLFile(path, &c); err != nil || len(c.Providers) != 5 {
		t.Fatalf("%d providers: %v", len(c.Providers), err)
	}
}

func TestOpenCodeGoV10PendingAcknowledgementSurvivesEditsAndSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(openCodeGoUpgradeFixture), 0600); err != nil {
		t.Fatal(err)
	}
	journalWrites := 0
	changed, err := upgradeOpenCodeGoFileWithWriterLocked(path, func(target string, data []byte, mode os.FileMode) error {
		if strings.HasSuffix(target, ".opencode-go-v10.json") {
			journalWrites++
			if journalWrites == 2 {
				return errors.New("interrupted acknowledgement")
			}
		}
		return fileutil.AtomicWriteFile(target, data, mode)
	})
	if !changed || err != nil {
		t.Fatalf("commit: %v %v", changed, err)
	}
	raw, _ := os.ReadFile(path)
	raw = append(raw, []byte("\n# unrelated later edit\n")...)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if j := readOpenCodeGoJournal(path, raw); j == nil || j.Committed {
		t.Fatal("prepared journal must recover using commit marker")
	}
	c := LoadForEdit(path)
	if err := c.SaveToScope(path, RenderScopeFull); err != nil {
		t.Fatal(err)
	}
	restored := LoadForEdit(path)
	if e, ok := restored.ResolveModel("go/deepseek-v4-pro"); !ok || e.Name != "go-chat-2" {
		t.Fatalf("lost alias after save: %+v", e)
	}
	raw, _ = os.ReadFile(path)
	if j := readOpenCodeGoJournal(path, raw); j == nil || !j.Committed {
		t.Fatal("save did not finalize durable acknowledgement")
	}
}

func TestOpenCodeGoV10AutoSearchDoesNotChangeAccountAfterDeletion(t *testing.T) {
	var c Config
	if _, err := toml.Decode(openCodeGoUpgradeFixture, &c); err != nil {
		t.Fatal(err)
	}
	j, _ := planOpenCodeGoUpgrade(&c)
	c.openCodeGoJournal = &j
	chat, ok := c.ResolveModel("go/deepseek-v4-flash")
	if !ok {
		t.Fatal("missing chat")
	}
	c.Agent.WebSearchModel = "auto"
	for i := range c.Providers {
		if strings.Contains(c.Providers[i].Name, "search") {
			c.Providers[i].APIKeyEnv = "OTHER_ACCOUNT"
		}
	}
	if got := c.ResolveWebSearch(chat); got.Status != "invalid" || !strings.Contains(got.Reason, "MIGRATED_MODEL_UNAVAILABLE") {
		t.Fatalf("search silently reassigned: %+v", got)
	}
}

func TestOpenCodeGoV10DowngradeRetryKeepsOriginalProviderAlias(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	fixture := strings.ReplaceAll(openCodeGoUpgradeFixture, "qwen3.8-max", "minimax-m3")
	if err := os.WriteFile(path, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyUserConfigUpgradesOnStartup(path); err != nil {
		t.Fatal(err)
	}
	c := LoadForEdit(path)
	c.ConfigVersion = 9
	if err := c.SaveToScope(path, RenderScopeFull); err != nil {
		t.Fatal(err)
	}
	resolved, _, _ := statConfigPath(path)
	_, err := upgradeOpenCodeGoFileWithWriterLocked(path, func(target string, data []byte, mode os.FileMode) error {
		if target == resolved {
			return errors.New("interrupted second upgrade")
		}
		return fileutil.AtomicWriteFile(target, data, mode)
	})
	if err == nil {
		t.Fatal("expected interrupted commit")
	}
	// The prepared second generation cannot erase the first committed aliases.
	c = LoadForEdit(path)
	if ref, err := c.ResolveHistoricalModel("go"); err != nil || ref != "go-chat-2/deepseek-v4-pro" {
		t.Fatalf("interruption changed historical provider alias: %q, %v", ref, err)
	}
	if _, err := ApplyUserConfigUpgradesOnStartup(path); err != nil {
		t.Fatal(err)
	}
	c = LoadForEdit(path)
	if ref, err := c.ResolveHistoricalModel("go"); err != nil || ref != "go-chat-2/deepseek-v4-pro" {
		t.Fatalf("retry changed historical provider alias to the remaining Anthropic group: %q, %v", ref, err)
	}
}

func TestOpenCodeGoV10InterruptedCommitRetriesWithoutActivatingAliases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	_ = os.WriteFile(path, []byte(openCodeGoUpgradeFixture), 0600)
	interrupted := errors.New("injected atomic replacement failure")
	resolved, _, _ := statConfigPath(path)
	changed, err := upgradeOpenCodeGoFileWithWriterLocked(path, func(target string, data []byte, mode os.FileMode) error {
		if target == resolved {
			return interrupted
		}
		return fileutil.AtomicWriteFile(target, data, mode)
	})
	if changed || !errors.Is(err, interrupted) {
		t.Fatalf("%v %v", changed, err)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != openCodeGoUpgradeFixture {
		t.Fatal("failed migration modified original")
	}
	if readOpenCodeGoJournal(path, raw) != nil {
		t.Fatal("failed migration activated pending aliases")
	}
	if _, err := ApplyUserConfigUpgradesOnStartup(path); err != nil {
		t.Fatal(err)
	}
	var cfg Config
	_, _ = decodeTOMLFile(path, &cfg)
	if len(cfg.Providers) != 5 {
		t.Fatal("retry created duplicate providers")
	}
	backup, _ := os.ReadFile(path + ".opencode-go-v10.backup")
	// Restoring the original backup and upgrading again produces the same
	// stable identities; the durable previous alias remains valid.
	_ = os.WriteFile(path, backup, 0600)
	if _, err := ApplyUserConfigUpgradesOnStartup(path); err != nil {
		t.Fatal(err)
	}
	var restored Config
	_, _ = decodeTOMLFile(path, &restored)
	if len(restored.Providers) != 5 {
		t.Fatal("backup recovery duplicated providers")
	}
}

func TestOpenCodeGoV10CustomSettingsAndManualAlternates(t *testing.T) {
	for _, customize := range []string{
		"request_url='https://opencode.ai/zen/go/v1/messages?custom=1'\n",
		"request_url='https://relay.example/v1/messages'\n",
		"extra_body={ thinking={type='custom'} }\n",
	} {
		path := filepath.Join(t.TempDir(), "config.toml")
		body := "config_version=9\n[[providers]]\nname='custom'\nkind='anthropic'\nbase_url='https://opencode.ai/zen/go'\nmodel='deepseek-v4-pro'\neffort='custom-depth'\n" + customize
		_ = os.WriteFile(path, []byte(body), 0600)
		if _, err := ApplyUserConfigUpgradesOnStartup(path); err != nil {
			t.Fatal(err)
		}
		var cfg Config
		_, _ = decodeTOMLFile(path, &cfg)
		if cfg.Providers[0].Kind != "anthropic" || cfg.Providers[0].Effort != "custom-depth" {
			t.Fatal("custom routing or effort overwritten")
		}
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	body := "config_version=10\n[[providers]]\nname='manual'\nkind='anthropic'\nbase_url='https://opencode.ai/zen/go'\nmodel='deepseek-v4-pro'\nthinking='enabled'\neffort='max'\n"
	_ = os.WriteFile(path, []byte(body), 0600)
	if changed, err := ApplyUserConfigUpgradesOnStartup(path); changed || err != nil {
		t.Fatalf("manual alternate: %v %v", changed, err)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != body {
		t.Fatal("manual alternate rewritten")
	}
}
