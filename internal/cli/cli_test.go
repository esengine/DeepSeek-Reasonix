package cli

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/i18n"
	"reasonix/internal/notify"
	"reasonix/internal/provider"
)

func TestChdirTo(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	if rc := chdirTo(""); rc != 0 {
		t.Fatalf(`chdirTo("") = %d, want 0`, rc)
	}
	if cwd, _ := os.Getwd(); cwd != orig {
		t.Fatalf(`chdirTo("") moved cwd to %q`, cwd)
	}

	tmp := t.TempDir()
	// Restore CWD before TempDir's RemoveAll runs (LIFO ordering): Windows can't
	// delete a directory that is still the process working directory.
	t.Cleanup(func() { _ = os.Chdir(orig) })
	if rc := chdirTo(tmp); rc != 0 {
		t.Fatalf("chdirTo(tmp) = %d, want 0", rc)
	}
	got, _ := filepath.EvalSymlinks(mustGetwd(t))
	want, _ := filepath.EvalSymlinks(tmp)
	if got != want {
		t.Fatalf("cwd = %q, want %q", got, want)
	}

	if rc := chdirTo(filepath.Join(tmp, "does-not-exist")); rc != 2 {
		t.Fatalf("chdirTo(missing) = %d, want 2", rc)
	}
}

func TestModelForResumePathUsesStoredModelWhenAvailable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	session := agent.NewSession("sys")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	if err := session.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := agent.SetBranchModelPreserveUpdated(path, "saved/model"); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		DefaultModel: "default/model",
		Providers: []config.ProviderEntry{
			{Name: "default", Kind: "openai", BaseURL: "https://default.invalid/v1", Model: "model"},
			{Name: "saved", Kind: "openai", BaseURL: "https://saved.invalid/v1", Model: "model"},
		},
	}

	if got := modelForResumePath("", path, cfg); got != "saved/model" {
		t.Fatalf("modelForResumePath = %q, want saved/model", got)
	}
	if got := modelForResumePath("explicit/model", path, cfg); got != "explicit/model" {
		t.Fatalf("explicit model was overwritten: %q", got)
	}
	if got := modelForResumePath("", filepath.Join(dir, "missing.jsonl"), cfg); got != "" {
		t.Fatalf("missing session model = %q, want empty fallback", got)
	}
	cfg.Providers = cfg.Providers[:1]
	if got := modelForResumePath("", path, cfg); got != "" {
		t.Fatalf("unknown stored model = %q, want empty fallback", got)
	}
}

func TestLoadResumableSessionRejectsCleanupPending(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pending.jsonl")
	saveTestSession(t, path, "pending prompt")
	if err := agent.MarkCleanupPending(path, "delete"); err != nil {
		t.Fatal(err)
	}

	if _, err := loadResumableSession(path); err == nil || !strings.Contains(err.Error(), "pending cleanup") {
		t.Fatalf("loadResumableSession cleanup-pending error = %v, want pending cleanup", err)
	}
}

func TestRunResumeRejectsCleanupPending(t *testing.T) {
	isolateCLIConfigHome(t)

	path := filepath.Join(t.TempDir(), "pending-run.jsonl")
	saveTestSession(t, path, "pending prompt")
	if err := agent.MarkCleanupPending(path, "delete"); err != nil {
		t.Fatal(err)
	}

	errOut := captureStderr(t, func() {
		if rc := runAgent([]string{"--resume", path, "continue task"}); rc != 1 {
			t.Fatalf("run --resume cleanup-pending rc = %d, want 1", rc)
		}
	})
	if !strings.Contains(errOut, "pending cleanup") {
		t.Fatalf("run --resume cleanup-pending stderr = %q, want pending cleanup", errOut)
	}
}

func TestServeResumeRejectsCleanupPending(t *testing.T) {
	isolateCLIConfigHome(t)

	path := filepath.Join(t.TempDir(), "pending-serve.jsonl")
	saveTestSession(t, path, "pending prompt")
	if err := agent.MarkCleanupPending(path, "delete"); err != nil {
		t.Fatal(err)
	}

	errOut := captureStderr(t, func() {
		if rc := runServe([]string{"--resume", path, "--addr", "127.0.0.1:0"}); rc != 1 {
			t.Fatalf("serve --resume cleanup-pending rc = %d, want 1", rc)
		}
	})
	if !strings.Contains(errOut, "pending cleanup") {
		t.Fatalf("serve --resume cleanup-pending stderr = %q, want pending cleanup", errOut)
	}
}

func TestServeRejectsUnknownAuthMode(t *testing.T) {
	isolateCLIConfigHome(t)

	errOut := captureStderr(t, func() {
		if rc := runServe([]string{"--auth", "tokne", "--addr", "127.0.0.1:0"}); rc != 1 {
			t.Fatalf("serve --auth tokne rc = %d, want 1", rc)
		}
	})
	if !strings.Contains(errOut, "auth mode must be none, token, or password") {
		t.Fatalf("serve --auth tokne stderr = %q, want auth mode validation", errOut)
	}
}

func TestServePasswordAuthRequiresPasswordMaterial(t *testing.T) {
	isolateCLIConfigHome(t)

	errOut := captureStderr(t, func() {
		if rc := runServe([]string{"--auth", "password", "--addr", "127.0.0.1:0"}); rc != 1 {
			t.Fatalf("serve --auth password without password rc = %d, want 1", rc)
		}
	})
	if !strings.Contains(errOut, "auth mode password requires --password or serve.password_hash") {
		t.Fatalf("serve --auth password stderr = %q, want password material validation", errOut)
	}
}

func TestReserveNativeScrollbackFrameWritesOnlyNewlines(t *testing.T) {
	var b bytes.Buffer
	reserveNativeScrollbackFrame(&b, 3)
	if got := b.String(); got != "\n\n\n" {
		t.Fatalf("reserveNativeScrollbackFrame wrote %q, want only three newlines", got)
	}

	reserveNativeScrollbackFrame(&b, 0)
	if got := b.String(); got != "\n\n\n" {
		t.Fatalf("reserveNativeScrollbackFrame(0) changed output to %q", got)
	}
}

func TestPrepareNativeScrollbackClearsBeforeFrame(t *testing.T) {
	var b bytes.Buffer
	prepareNativeScrollback(&b, 2)
	if got, want := b.String(), "\x1B[3J\x1B[2J\x1B[H\n\n"; got != want {
		t.Fatalf("prepareNativeScrollback wrote %q, want %q", got, want)
	}
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return cwd
}

func isolateCLIConfigHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Chdir(t.TempDir())
	return home
}

func TestMCPMigrationWaitsForCLIWorkspace(t *testing.T) {
	isolateCLIConfigHome(t)
	cwd := mustGetwd(t)
	if err := os.WriteFile(filepath.Join(cwd, "reasonix.toml"), []byte(`
[[plugins]]
name = "cwd-project"
command = "cwd-project-bin"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	migrateLegacyConfigForCLI()
	if cfg := config.LoadForEdit(config.UserConfigPath()); hasPluginNamed(cfg, "cwd-project") {
		t.Fatalf("early CLI legacy migration imported the cwd project plugin: %+v", cfg.Plugins)
	}

	migrateMCPConfigForCLIWorkspace()
	if cfg := config.LoadForEdit(config.UserConfigPath()); !hasPluginNamed(cfg, "cwd-project") {
		t.Fatalf("workspace-aware CLI migration did not import project plugin: %+v", cfg.Plugins)
	}
}

func hasPluginNamed(cfg *config.Config, name string) bool {
	if cfg == nil {
		return false
	}
	for _, plugin := range cfg.Plugins {
		if plugin.Name == name {
			return true
		}
	}
	return false
}

func TestMetadataCommandsDoNotProbeTerminalTheme(t *testing.T) {
	defer func(prev func() (terminalRGB, bool)) {
		queryTerminalBackgroundForTheme = prev
	}(queryTerminalBackgroundForTheme)
	queryTerminalBackgroundForTheme = func() (terminalRGB, bool) {
		t.Fatal("metadata command should not query terminal background")
		return terminalRGB{}, false
	}

	out := captureStdout(t, func() {
		if rc := Run([]string{"version"}, "test-version"); rc != 0 {
			t.Fatalf("version rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, "reasonix test-version") {
		t.Fatalf("version output = %q", out)
	}

	out = captureStdout(t, func() {
		if rc := Run([]string{"help"}, "test-version"); rc != 0 {
			t.Fatalf("help rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, "Usage:") && !strings.Contains(out, "用法：") {
		t.Fatalf("help output missing usage:\n%s", out)
	}
	if !strings.Contains(out, "reasonix run  [--model NAME] [--max-steps N] [-c|--continue] [--resume PATH] [--copy] <task>") {
		t.Fatalf("help output missing run resume flags:\n%s", out)
	}
}

func TestRunDispatchesACPLongFlagAlias(t *testing.T) {
	errOut := captureStderr(t, func() {
		if rc := Run([]string{"--acp", "-h"}, "test-version"); rc != 2 {
			t.Fatalf("Run --acp -h rc = %d, want 2", rc)
		}
	})
	if !strings.Contains(errOut, "Usage of acp:") {
		t.Fatalf("--acp should dispatch to the ACP command, got stderr:\n%s", errOut)
	}
	if strings.Contains(errOut, "unknown command") {
		t.Fatalf("--acp should not be treated as an unknown command:\n%s", errOut)
	}
}

func TestRunDefaultsToInteractiveSession(t *testing.T) {
	isolateCLIConfigHome(t)

	prev := runInteractiveSession
	prevInteractive := cliIsInteractive
	t.Cleanup(func() {
		runInteractiveSession = prev
		cliIsInteractive = prevInteractive
	})
	cliIsInteractive = func() bool { return true }

	var gotArgs []string
	runInteractiveSession = func(args []string) int {
		gotArgs = append([]string(nil), args...)
		return 17
	}

	if rc := Run(nil, "test-version"); rc != 17 {
		t.Fatalf("Run(nil) rc = %d, want 17", rc)
	}
	if gotArgs != nil {
		t.Fatalf("interactive args = %#v, want nil", gotArgs)
	}
}

func TestRunNoArgsNonInteractivePrintsUsage(t *testing.T) {
	isolateCLIConfigHome(t)

	prev := runInteractiveSession
	prevInteractive := cliIsInteractive
	t.Cleanup(func() {
		runInteractiveSession = prev
		cliIsInteractive = prevInteractive
	})
	cliIsInteractive = func() bool { return false }
	runInteractiveSession = func(args []string) int {
		t.Fatalf("non-interactive no-arg Run should not start session with %#v", args)
		return 99
	}

	out := captureStdout(t, func() {
		if rc := Run(nil, "test-version"); rc != 0 {
			t.Fatalf("Run(nil) rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, "reasonix —") || !strings.Contains(out, "reasonix run") {
		t.Fatalf("non-interactive no-arg Run should print usage, got:\n%s", out)
	}
}

func TestRunRoutesBareInteractiveFlagsToSession(t *testing.T) {
	isolateCLIConfigHome(t)

	prev := runInteractiveSession
	t.Cleanup(func() { runInteractiveSession = prev })

	for _, args := range [][]string{
		{"--continue"},
		{"--continue=true"},
		{"-c=true"},
		{"--resume=true"},
		{"--yolo=true"},
		{"--dangerously-skip-permissions=true"},
	} {
		var gotArgs []string
		runInteractiveSession = func(args []string) int {
			gotArgs = append([]string(nil), args...)
			return 23
		}

		if rc := Run(args, "test-version"); rc != 23 {
			t.Fatalf("Run(%#v) rc = %d, want 23", args, rc)
		}
		if !reflect.DeepEqual(gotArgs, args) {
			t.Fatalf("interactive args = %#v, want %#v", gotArgs, args)
		}
	}
}

func TestRunKeepsChatAndCodeCompatibilityAliases(t *testing.T) {
	isolateCLIConfigHome(t)

	prev := runInteractiveSession
	t.Cleanup(func() { runInteractiveSession = prev })

	var calls [][]string
	runInteractiveSession = func(args []string) int {
		calls = append(calls, append([]string(nil), args...))
		return 0
	}

	if rc := Run([]string{"chat", "--resume"}, "test-version"); rc != 0 {
		t.Fatalf("Run(chat --resume) rc = %d, want 0", rc)
	}
	if rc := Run([]string{"code", "--continue"}, "test-version"); rc != 0 {
		t.Fatalf("Run(code --continue) rc = %d, want 0", rc)
	}

	want := [][]string{{"--resume"}, {"--continue"}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("interactive calls = %#v, want %#v", calls, want)
	}
}

func TestRunMigratesLegacyConfigBeforeConfigOnlyCommands(t *testing.T) {
	isolateCLIConfigHome(t)
	legacyPath := filepath.Join(filepath.Dir(config.UserConfigPath()), "reasonix.toml")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, []byte(`
default_model = "deepseek-flash"

[[plugins]]
name = "legacy-cli"
command = "legacy-bin"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if rc := Run([]string{"mcp", "list"}, "test-version"); rc != 0 {
			t.Fatalf("mcp list rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, "legacy-cli") {
		t.Fatalf("mcp list should include migrated legacy config:\n%s", out)
	}

	body, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatalf("read migrated user config: %v", err)
	}
	for _, want := range []string{`config_version = 4`, `[desktop]`, `name    = "legacy-cli"`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("migrated config missing %q:\n%s", want, body)
		}
	}
}

func TestRunAppliesUserConfigUpgradesOnStartup(t *testing.T) {
	isolateCLIConfigHome(t)
	path := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("config_version = 2\ndefault_model = \"deepseek-flash\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	captureStdout(t, func() {
		if rc := Run([]string{"mcp", "list"}, "test-version"); rc != 0 {
			t.Fatalf("mcp list rc = %d, want 0", rc)
		}
	})

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read upgraded user config: %v", err)
	}
	if !strings.Contains(string(body), "config_version = 4") {
		t.Fatalf("CLI startup should apply user config upgrades:\n%s", body)
	}
}

func TestRunMetadataCommandsDoNotMigrateLegacyConfig(t *testing.T) {
	isolateCLIConfigHome(t)
	legacyPath := filepath.Join(filepath.Dir(config.UserConfigPath()), "reasonix.toml")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, []byte(`default_model = "deepseek-flash"`), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if rc := Run([]string{"version"}, "test-version"); rc != 0 {
			t.Fatalf("version rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, "reasonix test-version") {
		t.Fatalf("version output = %q", out)
	}
	if _, err := os.Stat(config.UserConfigPath()); !os.IsNotExist(err) {
		t.Fatalf("version should not migrate legacy config, stat err=%v", err)
	}
}

func TestConfigAutoPlanCommandWritesUserConfig(t *testing.T) {
	isolateCLIConfigHome(t)

	out := captureStdout(t, func() {
		if rc := Run([]string{"config", "auto-plan", "on"}, "test-version"); rc != 0 {
			t.Fatalf("config auto-plan rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, `auto_plan = "on"`) {
		t.Fatalf("config auto-plan output = %q", out)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if cfg.Agent.AutoPlan != "on" {
		t.Fatalf("saved auto_plan = %q, want on", cfg.Agent.AutoPlan)
	}
}

func TestConfigAutoPlanLocalIsRejected(t *testing.T) {
	isolateCLIConfigHome(t)

	userCfg := config.Default()
	userCfg.DefaultModel = "mimo-pro"
	if err := userCfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("write user config: %v", err)
	}

	errOut := captureStderr(t, func() {
		if rc := Run([]string{"config", "auto-plan", "--local", "on"}, "test-version"); rc != 2 {
			t.Fatalf("config auto-plan --local rc = %d, want 2", rc)
		}
	})
	if !strings.Contains(errOut, "--local is not supported") {
		t.Fatalf("config auto-plan --local stderr = %q", errOut)
	}
	if _, err := os.Stat("reasonix.toml"); !os.IsNotExist(err) {
		t.Fatalf("reasonix.toml should not be written, stat err=%v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load merged config: %v", err)
	}
	if cfg.DefaultModel != "mimo-pro" {
		t.Fatalf("default_model = %q, want global mimo-pro", cfg.DefaultModel)
	}
	if cfg.Agent.AutoPlan != "off" {
		t.Fatalf("auto_plan = %q, want global off", cfg.Agent.AutoPlan)
	}
}

func TestConfigMemoryV5CommandWritesUserConfig(t *testing.T) {
	isolateCLIConfigHome(t)

	out := captureStdout(t, func() {
		if rc := Run([]string{"config", "memory-v5", "off"}, "test-version"); rc != 0 {
			t.Fatalf("config memory-v5 rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, "memory_compiler.enabled = false") {
		t.Fatalf("config memory-v5 output = %q", out)
	}
	if !strings.Contains(out, `memory_compiler.verbosity = "observe"`) {
		t.Fatalf("config memory-v5 output missing verbosity = %q", out)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if cfg.MemoryCompilerEnabled() {
		t.Fatalf("saved memory_compiler.enabled = true, want false")
	}
	if got := cfg.MemoryCompilerVerbosity(); got != config.MemoryCompilerVerbosityObserve {
		t.Fatalf("saved memory_compiler.verbosity = %q, want observe", got)
	}

	out = captureStdout(t, func() {
		if rc := Run([]string{"config", "memory-v5", "status"}, "test-version"); rc != 0 {
			t.Fatalf("config memory-v5 status rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, "memory_compiler.enabled = false") {
		t.Fatalf("config memory-v5 status output = %q", out)
	}
	if !strings.Contains(out, `memory_compiler.verbosity = "observe"`) {
		t.Fatalf("config memory-v5 status output = %q", out)
	}

	out = captureStdout(t, func() {
		if rc := Run([]string{"config", "memory-v5", "compact"}, "test-version"); rc != 0 {
			t.Fatalf("config memory-v5 compact rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, "memory_compiler.enabled = true") ||
		!strings.Contains(out, `memory_compiler.verbosity = "compact"`) {
		t.Fatalf("config memory-v5 compact output = %q", out)
	}
}

func TestConfigMemoryV5LocalIsRejected(t *testing.T) {
	isolateCLIConfigHome(t)

	errOut := captureStderr(t, func() {
		if rc := Run([]string{"config", "memory-v5", "--local", "off"}, "test-version"); rc != 2 {
			t.Fatalf("config memory-v5 --local rc = %d, want 2", rc)
		}
	})
	if !strings.Contains(errOut, "--local is not supported") {
		t.Fatalf("config memory-v5 --local stderr = %q", errOut)
	}
	if _, err := os.Stat("reasonix.toml"); !os.IsNotExist(err) {
		t.Fatalf("reasonix.toml should not be written, stat err=%v", err)
	}
}

func TestConfigAutoPlanIgnoresProjectConfig(t *testing.T) {
	isolateCLIConfigHome(t)

	userCfg := config.Default()
	if err := userCfg.SetAutoPlan("off"); err != nil {
		t.Fatal(err)
	}
	if err := userCfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("write user config: %v", err)
	}
	if err := os.WriteFile("reasonix.toml", []byte("[agent]\nauto_plan = \"on\"\n"), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Agent.AutoPlan != "off" {
		t.Fatalf("auto_plan = %q, want user-level off despite project on", cfg.Agent.AutoPlan)
	}

	if err := userCfg.SetAutoPlan("on"); err != nil {
		t.Fatal(err)
	}
	if err := userCfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("rewrite user config: %v", err)
	}
	if err := os.WriteFile("reasonix.toml", []byte("[agent]\nauto_plan = \"off\"\n"), 0o644); err != nil {
		t.Fatalf("rewrite project config: %v", err)
	}
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if cfg.Agent.AutoPlan != "on" {
		t.Fatalf("auto_plan = %q, want user-level on despite project off", cfg.Agent.AutoPlan)
	}
}

func TestConfigReasoningLanguageCommandWritesUserConfig(t *testing.T) {
	isolateCLIConfigHome(t)

	out := captureStdout(t, func() {
		if rc := Run([]string{"config", "reasoning-language", "zh"}, "test-version"); rc != 0 {
			t.Fatalf("config reasoning-language rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, `reasoning_language = "zh"`) {
		t.Fatalf("config reasoning-language output = %q", out)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if cfg.Agent.ReasoningLanguage != "zh" || cfg.ReasoningLanguage() != "zh" {
		t.Fatalf("saved reasoning_language = %q/%q, want zh", cfg.Agent.ReasoningLanguage, cfg.ReasoningLanguage())
	}
}

func TestConfigReasoningLanguageLocalCreatesMinimalProjectOverride(t *testing.T) {
	isolateCLIConfigHome(t)

	userCfg := config.Default()
	userCfg.DefaultModel = "mimo-pro"
	if err := userCfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("write user config: %v", err)
	}

	out := captureStdout(t, func() {
		if rc := Run([]string{"config", "reasoning-language", "--local", "en"}, "test-version"); rc != 0 {
			t.Fatalf("config reasoning-language --local rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, `reasoning_language = "en"`) {
		t.Fatalf("config reasoning-language --local output = %q", out)
	}

	body, err := os.ReadFile("reasonix.toml")
	if err != nil {
		t.Fatalf("read project config: %v", err)
	}
	if strings.Contains(string(body), "default_model") {
		t.Fatalf("project reasoning-language override should not pin default_model:\n%s", body)
	}
	if !strings.Contains(string(body), "[agent]") || !strings.Contains(string(body), `reasoning_language = "en"`) {
		t.Fatalf("project config missing reasoning_language override:\n%s", body)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load merged config: %v", err)
	}
	if cfg.DefaultModel != "mimo-pro" {
		t.Fatalf("default_model = %q, want global mimo-pro", cfg.DefaultModel)
	}
	if cfg.ReasoningLanguage() != "en" {
		t.Fatalf("reasoning_language = %q, want local en", cfg.ReasoningLanguage())
	}
}

func TestConfigReasoningLanguageRejectsAliases(t *testing.T) {
	isolateCLIConfigHome(t)

	errOut := captureStderr(t, func() {
		if rc := Run([]string{"config", "reasoning-language", "中文"}, "test-version"); rc != 2 {
			t.Fatalf("config reasoning-language alias rc = %d, want 2", rc)
		}
	})
	if !strings.Contains(errOut, "must be auto|zh|en") {
		t.Fatalf("config reasoning-language alias stderr = %q", errOut)
	}
}

func TestProvidersWithMissingKeysOnlyChecksActiveDefaultModel(t *testing.T) {
	cfg := config.Default()
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("MIMO_API_KEY", "")

	missing := providersWithMissingKeys(cfg)
	if len(missing) != 1 {
		t.Fatalf("missing providers = %+v, want only active default model provider", missing)
	}
	if missing[0].APIKeyEnv != "DEEPSEEK_API_KEY" {
		t.Fatalf("missing key env = %q, want DEEPSEEK_API_KEY", missing[0].APIKeyEnv)
	}
}

func TestProvidersWithMissingKeysIgnoresUnusedBuiltInPresets(t *testing.T) {
	cfg := config.Default()
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	t.Setenv("MIMO_API_KEY", "")

	if missing := providersWithMissingKeys(cfg); len(missing) != 0 {
		t.Fatalf("missing providers = %+v, want none when only the configured default is keyed", missing)
	}
}

func TestProvidersWithMissingKeysIncludesReferencedSecondaryModels(t *testing.T) {
	cfg := config.Default()
	cfg.Providers = append(cfg.Providers,
		config.ProviderEntry{Name: "mimo-pro", Kind: "openai", BaseURL: "https://token-plan-cn.xiaomimimo.com/v1", Model: "mimo-v2.5-pro", APIKeyEnv: "MIMO_API_KEY"},
		config.ProviderEntry{Name: "mimo-flash", Kind: "openai", BaseURL: "https://token-plan-cn.xiaomimimo.com/v1", Model: "mimo-v2.5", APIKeyEnv: "MIMO_API_KEY"},
	)
	cfg.Agent.PlannerModel = "mimo-pro"
	cfg.Agent.SubagentModel = "mimo-flash"
	cfg.Agent.SubagentModels = map[string]string{
		"review": "mimo-pro/mimo-v2.5-pro",
	}
	cfg.Agent.AutoPlanClassifier = "mimo-flash/mimo-v2.5"
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	t.Setenv("MIMO_API_KEY", "")

	missing := providersWithMissingKeys(cfg)
	if len(missing) != 1 {
		t.Fatalf("missing providers = %+v, want MiMo once", missing)
	}
	if missing[0].APIKeyEnv != "MIMO_API_KEY" {
		t.Fatalf("missing key env = %q, want MIMO_API_KEY", missing[0].APIKeyEnv)
	}
}

func TestProvidersWithMissingKeysSkipsDisabledAutoPlanClassifier(t *testing.T) {
	cfg := config.Default()
	cfg.Providers = append(cfg.Providers, config.ProviderEntry{Name: "mimo-flash", Kind: "openai", BaseURL: "https://token-plan-cn.xiaomimimo.com/v1", Model: "mimo-v2.5", APIKeyEnv: "MIMO_API_KEY"})
	cfg.Agent.AutoPlan = "off"
	cfg.Agent.AutoPlanClassifier = "mimo-flash/mimo-v2.5"
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	t.Setenv("MIMO_API_KEY", "")

	if missing := providersWithMissingKeys(cfg); len(missing) != 0 {
		t.Fatalf("missing providers = %+v, want none when auto-plan classifier is disabled", missing)
	}

	cfg.Agent.AutoPlan = "on"
	missing := providersWithMissingKeys(cfg)
	if len(missing) != 1 {
		t.Fatalf("missing providers = %+v, want enabled auto-plan classifier provider", missing)
	}
	if missing[0].APIKeyEnv != "MIMO_API_KEY" {
		t.Fatalf("missing key env = %q, want MIMO_API_KEY", missing[0].APIKeyEnv)
	}
}

type cliRecordSink struct {
	events []event.Kind
}

func (s *cliRecordSink) Emit(e event.Event) {
	s.events = append(s.events, e.Kind)
}

type cliRecordSender struct {
	messages []notify.Message
}

func (s *cliRecordSender) Send(m notify.Message) error {
	s.messages = append(s.messages, m)
	return nil
}

func TestWithNotificationsWrapsCLISinkWithConfiguredSender(t *testing.T) {
	inner := &cliRecordSink{}
	sender := &cliRecordSender{}
	calls := 0
	prev := newNotificationSender
	newNotificationSender = func() notify.Sender {
		calls++
		return sender
	}
	t.Cleanup(func() { newNotificationSender = prev })

	cfg := config.Default()
	cfg.Notifications.Enabled = true

	wrapped := withNotifications(inner, cfg)
	wrapped.Emit(event.Event{Kind: event.TurnDone})

	if calls != 1 {
		t.Fatalf("newNotificationSender calls = %d, want 1", calls)
	}
	if len(inner.events) != 1 || inner.events[0] != event.TurnDone {
		t.Fatalf("forwarded events = %v, want [TurnDone]", inner.events)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("notifications = %d, want 1", len(sender.messages))
	}
	if sender.messages[0].Body != "Turn finished" {
		t.Fatalf("notification body = %q, want Turn finished", sender.messages[0].Body)
	}
}

func TestSetupOverwritePromptShowsYNDefault(t *testing.T) {
	t.Cleanup(func() { i18n.DetectLanguage("en") })
	for _, lang := range []string{"en", "zh"} {
		i18n.DetectLanguage(lang)
		var out bytes.Buffer
		if confirmReconfigureExistingConfig("config.toml", bufio.NewScanner(strings.NewReader("\n")), &out) {
			t.Fatalf("%s empty overwrite answer should keep existing config", lang)
		}
		if !strings.Contains(out.String(), "[y/N]:") {
			t.Fatalf("%s overwrite prompt should show explicit [y/N] default, got %q", lang, out.String())
		}
	}
}

// TestConfigureKeys verifies that a shared api_key_env (each vendor's SKUs use
// the same env var) is asked only once, and entered keys become env lines.
func TestConfigureKeys(t *testing.T) {
	// Force a clean baseline: any DEEPSEEK_API_KEY in the
	// process env (e.g. inherited from the test runner) would be picked up
	// by the new "reuse existing" path and the prompt would be skipped,
	// making the assertion below noisy.
	t.Setenv("DEEPSEEK_API_KEY", "")

	selected := config.Default().Providers

	input := "ds-key\n"
	env := configureKeys(selected, strings.NewReader(input), io.Discard)

	if len(env) != 1 {
		t.Fatalf("env = %v (want 1: DeepSeek asked once)", env)
	}
	if env[0] != "DEEPSEEK_API_KEY=ds-key" {
		t.Errorf("env[0] = %q", env[0])
	}
}

// TestConfigureKeysReusesExistingEnv covers the "user already typed the key
// in the URL-fetch flow, don't ask again" path. When the env var is set
// (either from .env or from a prior os.Setenv in the wizard), configureKeys
// must NOT consume from the input stream — otherwise the user's next typed
// line bleeds into the next provider's prompt. It also must include the
// existing value in envLines so the value is re-pinned into .env on
// re-runs of setup.
func TestConfigureKeysReusesExistingEnv(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "preset-ds-key")

	selected := config.Default().Providers
	var output bytes.Buffer
	env := configureKeys(selected, strings.NewReader("\n"), &output)

	if len(env) != 1 {
		t.Fatalf("env = %v (want 1: DeepSeek reused)", env)
	}
	if env[0] != "DEEPSEEK_API_KEY=preset-ds-key" {
		t.Errorf("env[0] = %q, want re-pinned existing value", env[0])
	}
	if !strings.Contains(output.String(), "DEEPSEEK_API_KEY") {
		t.Errorf("expected a 'reusing' confirmation for DEEPSEEK_API_KEY, got:\n%s", output.String())
	}
}

func TestConfigureKeysCanResetExistingEnv(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "stale-ds-key")

	selected := config.Default().Providers
	var output bytes.Buffer
	env := configureKeys(selected, strings.NewReader("y\nfresh-ds-key\n"), &output)

	if len(env) != 1 {
		t.Fatalf("env = %v (want 1: DeepSeek reset)", env)
	}
	if env[0] != "DEEPSEEK_API_KEY=fresh-ds-key" {
		t.Errorf("env[0] = %q, want freshly entered value", env[0])
	}
	if !strings.Contains(output.String(), "[y/N]:") || !strings.Contains(output.String(), "DEEPSEEK_API_KEY") {
		t.Errorf("expected a reset confirmation for DEEPSEEK_API_KEY, got:\n%s", output.String())
	}
}

// TestConfigureKeysAllSetDefaultsToReusingInput ensures that when every env var
// is already populated, pressing Enter at each confirmation keeps the values.
func TestConfigureKeysAllSetDefaultsToReusingInput(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "ds")

	selected := config.Default().Providers
	env := configureKeys(selected, strings.NewReader("\n"), io.Discard)
	if len(env) != 1 {
		t.Errorf("env = %v, want 1 (DeepSeek reused)", env)
	}
}

// TestAppendEnvUpsertReplacesExistingKey covers the bug where re-running the
// wizard with a corrected key would append a second line for the same env
// var. Without dedupe, different dotenv readers can disagree on which
// assignment wins, leaving stale keys hard to diagnose.
func TestAppendEnvUpsertReplacesExistingKey(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "") // also covers the os.Setenv pin path
	p := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(p, []byte("# initial\nDEEPSEEK_API_KEY=stale\nMIMO_API_KEY=keepme\n"), 0o600)

	if err := appendEnv(p, []string{"DEEPSEEK_API_KEY=fresh"}); err != nil {
		t.Fatalf("appendEnv: %v", err)
	}
	got, _ := os.ReadFile(p)
	want := "# initial\nMIMO_API_KEY=keepme\nDEEPSEEK_API_KEY=fresh\n"
	if string(got) != want {
		t.Errorf("after upsert =\n%s\nwant =\n%s", got, want)
	}
	if got := os.Getenv("DEEPSEEK_API_KEY"); got != "fresh" {
		t.Errorf("process env DEEPSEEK_API_KEY = %q, want %q (upsert should pin in-process)", got, "fresh")
	}
}

// TestAppendEnvUpsertHandlesExportPrefix proves `export FOO=...` style lines
// also get replaced, since users might hand-edit .env in shell-friendly form.
func TestAppendEnvUpsertHandlesExportPrefix(t *testing.T) {
	t.Setenv("FOO", "")
	p := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(p, []byte("export FOO=old\nKEEP=yes\n"), 0o600)
	if err := appendEnv(p, []string{"FOO=new"}); err != nil {
		t.Fatalf("appendEnv: %v", err)
	}
	got, _ := os.ReadFile(p)
	if !strings.Contains(string(got), "FOO=new") || strings.Contains(string(got), "FOO=old") {
		t.Errorf("export-prefixed line not replaced:\n%s", got)
	}
}

// TestGroupByFamily verifies the wizard groups the default preset into
// "deepseek" (flash + pro), preserving the order each family first appears in.
func TestGroupByFamily(t *testing.T) {
	order, members, info := groupByFamily(config.Default().Providers)

	if got := order; !reflect.DeepEqual(got, []string{"deepseek"}) {
		t.Fatalf("family order = %v, want [deepseek]", got)
	}
	if got := members["deepseek"]; !reflect.DeepEqual(got, []int{0, 1}) {
		t.Errorf("deepseek members = %v, want [0 1]", got)
	}
	if info["deepseek"].name != "DeepSeek" {
		t.Errorf("display name = %q", info["deepseek"].name)
	}
}

// TestFetchOrFallbackLiveReturns covers the happy path: a live /models call
// succeeds and its result wins over the preset's static list. We can't run
// the real probe (no key) so the FetchModels call is expected to 401 and the
// fallback path runs; the assertion below is that fallback works (static
// list returned) and that an empty base URL short-circuits to the static
// list with no network call.
func TestFetchOrFallback(t *testing.T) {
	t.Run("empty base URL returns static list", func(t *testing.T) {
		probe := config.ProviderEntry{
			BaseURL: "",
			Models:  []string{"preset-a", "preset-b"},
		}
		got := fetchOrFallback(&probe, "Test")
		if !reflect.DeepEqual(got, []string{"preset-a", "preset-b"}) {
			t.Errorf("got %v, want preset-a/b", got)
		}
	})

	t.Run("no key set returns static list (offline first-run)", func(t *testing.T) {
		t.Setenv("REASONIX_FETCH_TEST_KEY", "")
		probe := config.ProviderEntry{
			BaseURL:   "http://127.0.0.1:1", // unreachable, no listener
			APIKeyEnv: "REASONIX_FETCH_TEST_KEY",
			Models:    []string{"preset-a"},
		}
		got := fetchOrFallback(&probe, "Test")
		if !reflect.DeepEqual(got, []string{"preset-a"}) {
			t.Errorf("got %v, want preset-a", got)
		}
	})
}

// TestFetchModelListCompatWalksCandidates covers the wizard's custom-provider
// model probe. Previously the probe was a single URL (baseURL+"/models"),
// which worked for OpenAI vendors with a /v1 base URL but silently failed
// for Anthropic-style root URLs (no /v1) and Anthropic-compatible proxies
// (a /v1 base URL but a /v1/messages endpoint). The new helper walks
// BuildModelFetchURLs's candidate list — root + /v1 + known compat
// suffixes — so the same probe now succeeds for both shapes, matching
// what the conversation-time client URL will actually be.
func TestFetchModelListCompatWalksCandidates(t *testing.T) {
	t.Run("anthropic root form resolves via v1 fallback", func(t *testing.T) {
		var gotPath atomic.Value
		gotPath.Store("")
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath.Store(r.URL.Path)
			if r.URL.Path == "/v1/models" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"data":[{"id":"claude-test"}]}`)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		models, err := fetchModelListCompat(context.Background(), srv.URL, "k")
		if err != nil {
			t.Fatalf("fetchModelListCompat: %v", err)
		}
		if !reflect.DeepEqual(models, []string{"claude-test"}) {
			t.Errorf("models = %v, want [claude-test]", models)
		}
		if got := gotPath.Load().(string); got != "/v1/models" {
			t.Errorf("probe path = %q, want /v1/models (root form should fall through to v1 candidate)", got)
		}
	})

	t.Run("versioned v1 base URL hits models directly", func(t *testing.T) {
		var gotPath atomic.Value
		gotPath.Store("")
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath.Store(r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":[{"id":"model-a"}]}`)
		}))
		defer srv.Close()

		models, err := fetchModelListCompat(context.Background(), srv.URL+"/v1", "k")
		if err != nil {
			t.Fatalf("fetchModelListCompat: %v", err)
		}
		if !reflect.DeepEqual(models, []string{"model-a"}) {
			t.Errorf("models = %v, want [model-a]", models)
		}
		if got := gotPath.Load().(string); got != "/v1/models" {
			t.Errorf("probe path = %q, want /v1/models", got)
		}
	})

	t.Run("endpoint-miss on every candidate returns empty (manual flow)", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		models, err := fetchModelListCompat(context.Background(), srv.URL, "k")
		if err != nil {
			t.Fatalf("expected graceful empty result on all-miss, got err: %v", err)
		}
		if len(models) != 0 {
			t.Errorf("expected empty models on all-miss, got %v", models)
		}
	})

	t.Run("non-404 network error short-circuits with the real error", func(t *testing.T) {
		// Point at a closed port — connection refused, not a 404.
		models, err := fetchModelListCompat(context.Background(), "http://127.0.0.1:1", "k")
		if err == nil {
			t.Fatalf("expected error for unreachable host, got models=%v", models)
		}
	})
}

// TestFamilyStaticModels proves the offline fallback unions every member of a
// family (the flash + pro SKUs), not just the first — the regression that left
// users with only flash when the live /models probe failed.
func TestFamilyStaticModels(t *testing.T) {
	providers := []config.ProviderEntry{
		{Name: "deepseek-flash", Model: "deepseek-v4-flash"},
		{Name: "deepseek-pro", Model: "deepseek-v4-pro"},
		{Name: "mimo-flash", Model: "mimo-v2.5"},
	}
	got := familyStaticModels(providers, []int{0, 1})
	want := []string{"deepseek-v4-flash", "deepseek-v4-pro"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFamilyStaticModelsDedupes(t *testing.T) {
	providers := []config.ProviderEntry{
		{Name: "a", Models: []string{"x", "y"}},
		{Name: "b", Models: []string{"y", "z"}},
	}
	got := familyStaticModels(providers, []int{0, 1})
	if !reflect.DeepEqual(got, []string{"x", "y", "z"}) {
		t.Errorf("got %v, want x/y/z deduped", got)
	}
}

// TestBuildFamilyEntriesSplitsPricing proves flash and pro land in separate
// entries carrying their own price, rather than collapsing into one entry that
// would bill pro at flash's rate.
func TestBuildFamilyEntriesSplitsPricing(t *testing.T) {
	flash := config.ProviderEntry{Name: "deepseek-flash", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", Price: &provider.Pricing{Input: 1, Output: 2}}
	pro := config.ProviderEntry{Name: "deepseek-pro", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-pro", Price: &provider.Pricing{Input: 3, Output: 6}}
	got := buildFamilyEntries(flash, []config.ProviderEntry{flash, pro}, []string{"deepseek-v4-flash", "deepseek-v4-pro"})
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	byName := map[string]config.ProviderEntry{}
	for _, e := range got {
		byName[e.Name] = e
	}
	if e := byName["deepseek-flash"]; e.Model != "deepseek-v4-flash" || e.Price == nil || e.Price.Output != 2 {
		t.Errorf("flash entry wrong: %+v (price %+v)", e, e.Price)
	}
	if e := byName["deepseek-pro"]; e.Model != "deepseek-v4-pro" || e.Price == nil || e.Price.Output != 6 {
		t.Errorf("pro entry wrong: %+v (price %+v)", e, e.Price)
	}
}

// TestBuildFamilyEntriesUnknownModelUsesProbe puts a live-only SKU (no matching
// preset) under the probe entry rather than dropping it.
func TestBuildFamilyEntriesUnknownModelUsesProbe(t *testing.T) {
	flash := config.ProviderEntry{Name: "deepseek-flash", Model: "deepseek-v4-flash", Price: &provider.Pricing{Input: 1}}
	got := buildFamilyEntries(flash, []config.ProviderEntry{flash}, []string{"deepseek-v4-flash", "deepseek-v9-experimental"})
	if len(got) != 1 || got[0].Name != "deepseek-flash" {
		t.Fatalf("got %+v, want one deepseek-flash entry", got)
	}
	if !reflect.DeepEqual(got[0].Models, []string{"deepseek-v4-flash", "deepseek-v9-experimental"}) {
		t.Errorf("Models = %v, want both under the probe entry", got[0].Models)
	}
}

// TestBuildFamilyEntry covers the three observable behaviors:
//   - The selected models land in the entry's Models field, with Model
//     pointed at the first one so legacy single-model lookups still work.
//   - A preset Default that points to a model the user didn't pick is
//     reset to the first selected model (otherwise resolve-by-default
//     would silently break).
//   - A preset Default that IS in the selection is preserved.
func TestBuildFamilyEntry(t *testing.T) {
	t.Run("default reset when not in selection", func(t *testing.T) {
		probe := config.ProviderEntry{
			Name: "deepseek", Kind: "openai",
			BaseURL: "https://api.deepseek.com",
			Models:  []string{"deepseek-v4-flash", "deepseek-v4-pro"},
			Default: "deepseek-v4-pro",
		}
		got := buildFamilyEntry(probe, []string{"deepseek-v4-flash"})
		if got.Model != "deepseek-v4-flash" {
			t.Errorf("Model = %q, want deepseek-v4-flash", got.Model)
		}
		if got.Default != "deepseek-v4-flash" {
			t.Errorf("Default = %q, want reset to first selected", got.Default)
		}
		if !reflect.DeepEqual(got.Models, []string{"deepseek-v4-flash"}) {
			t.Errorf("Models = %v", got.Models)
		}
		if got.BaseURL != "https://api.deepseek.com" {
			t.Errorf("BaseURL lost: %q", got.BaseURL)
		}
	})

	t.Run("default preserved when in selection", func(t *testing.T) {
		probe := config.ProviderEntry{
			Name: "deepseek", Default: "deepseek-v4-pro",
			BaseURL: "https://api.deepseek.com",
		}
		got := buildFamilyEntry(probe, []string{"deepseek-v4-flash", "deepseek-v4-pro"})
		if got.Default != "deepseek-v4-pro" {
			t.Errorf("Default = %q, want preserved", got.Default)
		}
	})

	t.Run("empty default filled from first selected", func(t *testing.T) {
		probe := config.ProviderEntry{Name: "x", BaseURL: "u"}
		got := buildFamilyEntry(probe, []string{"alpha", "beta"})
		if got.Default != "alpha" {
			t.Errorf("Default = %q, want alpha", got.Default)
		}
	})
}

// TestProviderSlug covers the host-derivation rules and the sha1 fallback
// for unparseable URLs. The exact format isn't load-bearing — what matters
// is that the slug (a) starts with the kind prefix, (b) is stable across
// calls with the same URL, and (c) never produces the bare "custom" /
// "anthropic" magic names that would collide with the wizard menu items.
func TestProviderSlug(t *testing.T) {
	cases := []struct {
		name, kind, url, want string
	}{
		{"standard host with port", "custom", "https://token.sensenova.cn/v1", "custom-token-sensenova-cn"},
		{"api subdomain", "custom", "https://api.openai.com/v1", "custom-api-openai-com"},
		{"www stripped", "custom", "https://www.example.com/v1", "custom-example-com"},
		{"port preserved", "custom", "http://localhost:11434/v1", "custom-localhost-11434"},
		{"anthropic kind", "anthropic", "https://api.anthropic.com", "anthropic-api-anthropic-com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := providerSlug(tc.kind, tc.url); got != tc.want {
				t.Errorf("providerSlug(%q, %q) = %q, want %q", tc.kind, tc.url, got, tc.want)
			}
		})
	}

	t.Run("stable across calls", func(t *testing.T) {
		a := providerSlug("custom", "https://token.sensenova.cn/v1")
		b := providerSlug("custom", "https://token.sensenova.cn/v1")
		if a != b {
			t.Errorf("not stable: %q vs %q", a, b)
		}
		if a == "custom" {
			t.Error("slug degenerated to bare magic name — collision risk")
		}
	})

	t.Run("sha1 fallback for unparseable URL", func(t *testing.T) {
		got := providerSlug("custom", "://not a url::://")
		if !strings.HasPrefix(got, "custom-") || got == "custom" {
			t.Errorf("fallback slug = %q, want custom-<hex>", got)
		}
		// sha1 is 40 hex chars; we take 4 bytes (8 hex chars).
		if len(got) != len("custom-")+8 {
			t.Errorf("fallback slug = %q, want 8 hex chars after prefix", got)
		}
	})

	t.Run("sha1 fallback for non-ascii host", func(t *testing.T) {
		got := providerSlug("custom", "https://例子.测试/v1")
		if !strings.HasPrefix(got, "custom-") || got == "custom-" {
			t.Errorf("fallback slug = %q, want custom-<hex>", got)
		}
		if len(got) != len("custom-")+8 {
			t.Errorf("fallback slug = %q, want 8 hex chars after prefix", got)
		}
	})
}

func TestAPIKeyEnvFromProviderName(t *testing.T) {
	cases := []struct {
		name, providerName, want string
	}{
		{"custom host slug", "custom-token-sensenova-cn", "CUSTOM_TOKEN_SENSENOVA_CN_API_KEY"},
		{"localhost slug with port", "custom-localhost-11434", "CUSTOM_LOCALHOST_11434_API_KEY"},
		{"desktop-style custom name", "Local Gateway", "LOCAL_GATEWAY_API_KEY"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := apiKeyEnvFromProviderName(tc.providerName); got != tc.want {
				t.Errorf("apiKeyEnvFromProviderName(%q) = %q, want %q", tc.providerName, got, tc.want)
			}
		})
	}

	t.Run("non-ascii provider names use desktop-compatible hash fallback", func(t *testing.T) {
		if got, want := apiKeyEnvFromProviderName("商汤"), "CUSTOM_d39b9067_API_KEY"; got != want {
			t.Errorf("apiKeyEnvFromProviderName(non-ascii) = %q, want %q", got, want)
		}
		if got := apiKeyEnvFromProviderName("通义千问"); got == "CUSTOM_d39b9067_API_KEY" || got == "CUSTOM_API_KEY" {
			t.Errorf("apiKeyEnvFromProviderName(second non-ascii) = %q, want distinct stable fallback", got)
		}
	})
}

func TestPromptCustomProviderManualDefaultsKeyEnvFromBaseURL(t *testing.T) {
	entries, err := promptCustomProviderManualWith(
		bufio.NewScanner(strings.NewReader("\n\nsensenova-chat\n")),
		"https://token.sensenova.cn/v1",
		"",
		"",
	)
	if err != nil {
		t.Fatalf("promptCustomProviderManualWith: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if got, want := entries[0].APIKeyEnv, "CUSTOM_TOKEN_SENSENOVA_CN_API_KEY"; got != want {
		t.Errorf("APIKeyEnv = %q, want %q", got, want)
	}
}

func TestPromptCustomProviderManualPreservesExplicitKeyEnv(t *testing.T) {
	entries, err := promptCustomProviderManualWith(
		bufio.NewScanner(strings.NewReader("\nmanual-chat\n")),
		"https://token.sensenova.cn/v1",
		"CUSTOM_API_KEY",
		"",
	)
	if err != nil {
		t.Fatalf("promptCustomProviderManualWith: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if got := entries[0].APIKeyEnv; got != "CUSTOM_API_KEY" {
		t.Errorf("APIKeyEnv = %q, want explicit CUSTOM_API_KEY", got)
	}
}

// TestFilterStaleCustomEntries covers the wizard's auto-cleanup of legacy
// "custom" / "anthropic" magic-name entries that previous versions wrote
// into reasonix.toml. These collide with the wizard's own menu items, so
// they're dropped from the providers list before grouping — but the caller
// still gets them back in the dropped slice to surface a warning.
func TestFilterStaleCustomEntries(t *testing.T) {
	in := []config.ProviderEntry{
		{Name: "deepseek", Kind: "openai", BaseURL: "https://api.deepseek.com"},
		{Name: "custom", Kind: "openai", BaseURL: "https://old.example/v1"},                // stale
		{Name: "anthropic", Kind: "anthropic", BaseURL: "https://old.example/v1/messages"}, // stale
		{Name: "mimo-tp", Kind: "openai", BaseURL: "https://token-plan-cn.xiaomimimo.com/v1"},
	}
	kept, dropped := filterStaleCustomEntries(in)
	if len(kept) != 2 {
		t.Errorf("kept = %d entries, want 2: %+v", len(kept), kept)
	}
	if len(dropped) != 2 {
		t.Errorf("dropped = %d entries, want 2: %+v", len(dropped), dropped)
	}
	for _, k := range kept {
		if k.Name == "custom" || k.Name == "anthropic" {
			t.Errorf("magic name leaked through: %q", k.Name)
		}
	}

	t.Run("non-magic names with kind anthropic are kept", func(t *testing.T) {
		// An entry someone deliberately named "claude" (kind=anthropic) must
		// not be touched by the filter — only the bare "anthropic" magic name.
		in := []config.ProviderEntry{
			{Name: "claude", Kind: "anthropic", BaseURL: "https://api.anthropic.com"},
		}
		kept, dropped := filterStaleCustomEntries(in)
		if len(kept) != 1 || len(dropped) != 0 {
			t.Errorf("claude should be kept, got kept=%d dropped=%d", len(kept), len(dropped))
		}
	})

	t.Run("custom kind anthropic is kept", func(t *testing.T) {
		// Name="custom" with kind=anthropic is ambiguous — keep it.
		in := []config.ProviderEntry{
			{Name: "custom", Kind: "anthropic", BaseURL: "https://x"},
		}
		kept, dropped := filterStaleCustomEntries(in)
		if len(kept) != 1 || len(dropped) != 0 {
			t.Errorf("custom+anthropic should be kept (ambiguous), got kept=%d dropped=%d", len(kept), len(dropped))
		}
	})
}

func TestWithBuiltinFamiliesDoesNotAddMissingMimo(t *testing.T) {
	// The user's case: a reasonix.toml that defines only deepseek providers.
	cfg := []config.ProviderEntry{
		{Name: "deepseek-flash", Kind: "openai", BaseURL: "https://api.deepseek.com"},
		{Name: "deepseek-pro", Kind: "openai", BaseURL: "https://api.deepseek.com"},
	}
	order, _, info := groupByFamily(withBuiltinFamilies(cfg))
	seen := map[string]bool{}
	for _, k := range order {
		seen[info[k].name] = true
	}
	if !seen["DeepSeek"] {
		t.Fatalf("wizard families = %v, want DeepSeek", order)
	}
	if seen["MiMo (Xiaomi)"] {
		t.Fatalf("wizard families = %v, should not inject MiMo", order)
	}
	// A user's customized deepseek must not be duplicated.
	if n := len(groupByFamilyKeys(withBuiltinFamilies(cfg), "deepseek")); n != 2 {
		t.Fatalf("deepseek members = %d, want the user's 2 (no injected duplicate)", n)
	}
}

func TestWithBuiltinFamiliesForLanguageUsesDeepSeekPricing(t *testing.T) {
	providers := withBuiltinFamiliesForLanguage(nil, "zh")
	var flash *config.ProviderEntry
	for i := range providers {
		if providers[i].Name == "deepseek-flash" {
			flash = &providers[i]
			break
		}
	}
	if flash == nil {
		t.Fatal("deepseek-flash provider missing")
	}
	if flash.Price == nil || flash.Price.Output != 2 || flash.Price.Currency != "¥" {
		t.Fatalf("flash price = %+v, want CNY preset", flash.Price)
	}
}

// TestWithBuiltinFamiliesRestoresSiblingEntries covers the re-run scenario:
// a user previously selected only deepseek-v4-flash (saved as deepseek-flash
// with a single model). Re-running `reasonix setup` must still surface the
// sibling deepseek-pro entry so the user can pick deepseek-v4-pro too,
// rather than only showing the previously selected model.
func TestWithBuiltinFamiliesRestoresSiblingEntries(t *testing.T) {
	cfg := []config.ProviderEntry{
		{Name: "deepseek-flash", Kind: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", Models: []string{"deepseek-v4-flash"}, APIKeyEnv: "DEEPSEEK_API_KEY"},
	}
	got := withBuiltinFamilies(cfg)

	// deepseek-pro must be restored even though deepseek family already exists.
	var found bool
	for _, p := range got {
		if p.Name == "deepseek-pro" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("withBuiltinFamilies(%+v) = %v, want deepseek-pro sibling restored", cfg, namesOf(got))
	}

	// The static model list for the deepseek family must include both SKUs.
	_, members, _ := groupByFamily(got)
	deepseekIdxs := members["deepseek"]
	models := familyStaticModels(got, deepseekIdxs)
	wantModels := map[string]bool{"deepseek-v4-flash": true, "deepseek-v4-pro": true}
	for _, m := range models {
		delete(wantModels, m)
	}
	if len(wantModels) > 0 {
		t.Errorf("familyStaticModels = %v, missing %v", models, wantModels)
	}
}

func namesOf(ps []config.ProviderEntry) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Name
	}
	return out
}

func groupByFamilyKeys(ps []config.ProviderEntry, key string) []int {
	_, members, _ := groupByFamily(ps)
	return members[key]
}

func TestWriteDefaultConfigOmitsLegacyInternalMCPSections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reasonix.toml")
	if rc := writeDefaultConfig(path); rc != 0 {
		t.Fatalf("writeDefaultConfig rc = %d", rc)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{"[codegraph]", "[builtin_mcp]", "[builtin_mcp_updates]"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("default config should omit %s:\n%s", forbidden, text)
		}
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() { os.Stderr = old }()

	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestProvidersWithMissingKeysOnlyReferenced(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("MIMO_API_KEY", "")
	cfg := config.Default()

	got := providersWithMissingKeys(cfg)
	envs := map[string]bool{}
	for _, p := range got {
		envs[p.APIKeyEnv] = true
	}
	if !envs["DEEPSEEK_API_KEY"] {
		t.Errorf("the default model's missing key must be prompted, got %v", got)
	}
	if envs["MIMO_API_KEY"] {
		t.Errorf("unreferenced preset keys must not be prompted, got %v", got)
	}
}

func TestProvidersWithMissingKeysIncludesPlannerModel(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "set")
	t.Setenv("MIMO_API_KEY", "")
	cfg := config.Default()
	cfg.Providers = append(cfg.Providers, config.ProviderEntry{Name: "mimo-pro", Kind: "openai", BaseURL: "https://token-plan-cn.xiaomimimo.com/v1", Model: "mimo-v2.5-pro", APIKeyEnv: "MIMO_API_KEY"})
	cfg.Agent.PlannerModel = "mimo-pro"

	got := providersWithMissingKeys(cfg)
	if len(got) != 1 || got[0].APIKeyEnv != "MIMO_API_KEY" {
		t.Errorf("planner model's missing key must be prompted, got %+v", got)
	}
}

func TestParseRuntimeProfile(t *testing.T) {
	for input, want := range map[string]string{
		"":         boot.TokenModeFull,
		"balanced": boot.TokenModeFull,
		"full":     boot.TokenModeFull,
		"economy":  boot.TokenModeEconomy,
		"delivery": boot.TokenModeDelivery,
	} {
		got, err := parseRuntimeProfile(input)
		if err != nil || got != want {
			t.Errorf("parseRuntimeProfile(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := parseRuntimeProfile("fast"); err == nil {
		t.Fatal("unknown profile should fail")
	}
}
