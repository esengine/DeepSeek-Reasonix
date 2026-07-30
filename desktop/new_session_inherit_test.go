package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/control"
)

func TestEnsureBlankTabInheritsActiveTabLocalSettings(t *testing.T) {
	isolateDesktopUserDirs(t)
	workspace := robustTempDir(t)
	if err := os.WriteFile(filepath.Join(workspace, "reasonix.toml"),
		[]byte(""), 0o644); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}

	effort := "max"
	app := NewApp()
	src := &WorkspaceTab{
		ID:               "src",
		Scope:            "project",
		WorkspaceRoot:    workspace,
		TopicID:          "topic_src",
		SessionPath:      filepath.Join(workspace, "src.jsonl"), // non-empty so src isn't reused as the blank tab
		model:            "inherit/model",
		effort:           &effort,
		tokenMode:        "economy",
		mode:             "plan",
		toolApprovalMode: control.ToolApprovalYolo,
		disabledMCP:      map[string]ServerView{"srv-x": {}},
		mcpOrder:         []string{"srv-x", "srv-y"},
	}
	src.sink = &tabEventSink{tabID: "src", app: app}
	app.tabs["src"] = src
	app.tabOrder = []string{"src"}
	app.activeTabID = "src"

	meta, err := app.EnsureBlankTab("project", workspace)
	if err != nil {
		t.Fatalf("EnsureBlankTab: %v", err)
	}
	if meta.ID == "" || meta.ID == "src" {
		t.Fatalf("expected a fresh tab, got meta.ID=%q", meta.ID)
	}

	created := app.tabs[meta.ID]
	if created == nil {
		t.Fatalf("new tab %q missing from app.tabs", meta.ID)
	}
	if created.effort == nil || *created.effort != "max" {
		t.Fatalf("effort = %v, want inherited \"max\"", created.effort)
	}
	if created.tokenMode != "economy" {
		t.Fatalf("tokenMode = %q, want inherited \"economy\"", created.tokenMode)
	}
	if created.toolApprovalMode != control.ToolApprovalAuto {
		t.Fatalf("toolApprovalMode = %q, want global default auto", created.toolApprovalMode)
	}
	if created.mode != "normal" {
		t.Fatalf("mode = %q, want global default normal", created.mode)
	}
	if _, ok := created.disabledMCP["srv-x"]; !ok {
		t.Fatalf("disabledMCP = %v, want inherited key \"srv-x\"", created.disabledMCP)
	}
	if len(created.mcpOrder) != 2 || created.mcpOrder[0] != "srv-x" {
		t.Fatalf("mcpOrder = %v, want inherited [srv-x srv-y]", created.mcpOrder)
	}
}

func TestEnsureBlankTabUsesGlobalSessionDefaultsForModelAndToolApproval(t *testing.T) {
	isolateDesktopUserDirs(t)
	// Seed the key into Reasonix's user credentials store (the only source
	// Configured() consults) so the default resolves as configured and is
	// inherited verbatim regardless of the developer machine's environment.
	seedUserCredentials(t, "DEEPSEEK_API_KEY=test-key\n")
	workspace := robustTempDir(t)
	if err := os.WriteFile(filepath.Join(workspace, "reasonix.toml"),
		[]byte(""), 0o644); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}

	cfg := config.LoadForEdit(config.UserConfigPath())
	if err := cfg.SetDefaultModel("deepseek-pro/deepseek-v4-pro"); err != nil {
		t.Fatalf("SetDefaultModel: %v", err)
	}
	if err := cfg.SetDesktopDefaultToolApprovalMode(control.ToolApprovalAuto); err != nil {
		t.Fatalf("SetDesktopDefaultToolApprovalMode: %v", err)
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save user config: %v", err)
	}

	app := NewApp()
	src := &WorkspaceTab{
		ID:               "src",
		Scope:            "project",
		WorkspaceRoot:    workspace,
		TopicID:          "topic_src",
		SessionPath:      filepath.Join(workspace, "src.jsonl"),
		model:            "deepseek-flash/deepseek-v4-flash",
		mode:             "plan",
		toolApprovalMode: control.ToolApprovalAsk,
		disabledMCP:      map[string]ServerView{},
	}
	src.sink = &tabEventSink{tabID: "src", app: app}
	app.tabs["src"] = src
	app.tabOrder = []string{"src"}
	app.activeTabID = "src"

	meta, err := app.EnsureBlankTab("project", workspace)
	if err != nil {
		t.Fatalf("EnsureBlankTab: %v", err)
	}
	created := app.tabs[meta.ID]
	if created == nil {
		t.Fatalf("new tab %q missing from app.tabs", meta.ID)
	}
	if created.model != "deepseek-pro/deepseek-v4-pro" {
		t.Fatalf("new tab model = %q, want global default model", created.model)
	}
	if created.toolApprovalMode != control.ToolApprovalAuto {
		t.Fatalf("new tab toolApprovalMode = %q, want global default auto", created.toolApprovalMode)
	}
	if src.model != "deepseek-flash/deepseek-v4-flash" || src.toolApprovalMode != control.ToolApprovalAsk {
		t.Fatalf("existing tab should not be overwritten, got model=%q approval=%q", src.model, src.toolApprovalMode)
	}
	if !strings.Contains(filepath.Base(created.SessionPath), "deepseek-v4-pro") {
		t.Fatalf("new session path = %q, want filename seeded by global default model", created.SessionPath)
	}
}

func TestDesktopNewSessionDefaultsHonorExplicitAsk(t *testing.T) {
	isolateDesktopUserDirs(t)
	cfg := config.LoadForEdit(config.UserConfigPath())
	if err := cfg.SetDesktopDefaultToolApprovalMode(control.ToolApprovalAsk); err != nil {
		t.Fatalf("SetDesktopDefaultToolApprovalMode: %v", err)
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save user config: %v", err)
	}

	_, approvalMode := desktopNewSessionDefaults("global", "")
	if approvalMode != control.ToolApprovalAsk {
		t.Fatalf("explicit desktop approval default = %q, want ask", approvalMode)
	}
}

// seedUserCredentials writes key=value lines into Reasonix's user credentials
// store. ProviderEntry.Configured() resolves keys only from that store, not
// from process env, so tests must seed it explicitly.
func seedUserCredentials(t *testing.T, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(config.UserCredentialsPath()), 0o755); err != nil {
		t.Fatalf("mkdir credentials dir: %v", err)
	}
	if err := os.WriteFile(config.UserCredentialsPath(), []byte(data), 0o600); err != nil {
		t.Fatalf("write user credentials: %v", err)
	}
}

func TestDesktopNewSessionDefaultsSkipsKeylessDefaultModel(t *testing.T) {
	isolateDesktopUserDirs(t)
	seedUserCredentials(t, "REASONIX_TEST_SESSION_KEY=test-key\n")

	cfg := config.LoadForEdit(config.UserConfigPath())
	cfg.Providers = append(cfg.Providers, config.ProviderEntry{
		Name:      "test-prov",
		Kind:      "openai",
		BaseURL:   "https://example.com",
		Model:     "test-model",
		APIKeyEnv: "REASONIX_TEST_SESSION_KEY",
	})
	if err := cfg.SetDefaultModel("deepseek-pro/deepseek-v4-pro"); err != nil {
		t.Fatalf("SetDefaultModel: %v", err)
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save user config: %v", err)
	}

	model, _ := desktopNewSessionDefaults("global", "")
	if model != "test-prov/test-model" {
		t.Fatalf("new session model = %q, want fallback to configured provider test-prov/test-model", model)
	}
}

func TestDesktopNewSessionDefaultsKeepsConfiguredDefaultModel(t *testing.T) {
	isolateDesktopUserDirs(t)
	seedUserCredentials(t, "DEEPSEEK_API_KEY=test-key\n")

	cfg := config.LoadForEdit(config.UserConfigPath())
	if err := cfg.SetDefaultModel("deepseek-pro/deepseek-v4-pro"); err != nil {
		t.Fatalf("SetDefaultModel: %v", err)
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save user config: %v", err)
	}

	model, _ := desktopNewSessionDefaults("global", "")
	if model != "deepseek-pro/deepseek-v4-pro" {
		t.Fatalf("new session model = %q, want configured default verbatim", model)
	}
}

func TestDesktopNewSessionDefaultsKeepsKeylessDefaultWhenNothingConfigured(t *testing.T) {
	isolateDesktopUserDirs(t)
	// No credentials seeded: every provider resolves as unconfigured.

	cfg := config.LoadForEdit(config.UserConfigPath())
	if err := cfg.SetDefaultModel("deepseek-pro/deepseek-v4-pro"); err != nil {
		t.Fatalf("SetDefaultModel: %v", err)
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save user config: %v", err)
	}

	// With no configured provider at all, the raw default must survive so the
	// boot-time missing-key notice still tells the user what to fix.
	model, _ := desktopNewSessionDefaults("global", "")
	if model != "deepseek-pro/deepseek-v4-pro" {
		t.Fatalf("new session model = %q, want raw keyless default preserved", model)
	}
}

func TestDesktopNewSessionDefaultsRejectsNonChatDefaultWithoutFallback(t *testing.T) {
	isolateDesktopUserDirs(t)
	seedUserCredentials(t, "REASONIX_TEST_AUDIO_KEY=test-key\n")

	cfg := config.LoadForEdit(config.UserConfigPath())
	cfg.Providers = []config.ProviderEntry{{
		Name:      "audio",
		Kind:      "openai",
		BaseURL:   "https://audio.example.com",
		Model:     "tts-1",
		APIKeyEnv: "REASONIX_TEST_AUDIO_KEY",
	}}
	cfg.Desktop.ProviderAccess = []string{"audio"}
	if err := cfg.SetDefaultModel("audio/tts-1"); err != nil {
		t.Fatalf("SetDefaultModel: %v", err)
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save user config: %v", err)
	}

	app := NewApp()
	meta, err := app.EnsureBlankTab("global", "")
	if err != nil {
		t.Fatalf("EnsureBlankTab: %v", err)
	}
	created := app.tabs[meta.ID]
	if created == nil {
		t.Fatalf("new tab %q missing from app.tabs", meta.ID)
	}
	if created.model != "" {
		t.Fatalf("new session model = %q, want no ineligible model selected", created.model)
	}
	if created.Ctrl != nil || !strings.Contains(created.StartupErr, "chat-capable provider") {
		t.Fatalf("new session runtime = (%v, %q), want an actionable no-chat-model startup error", created.Ctrl, created.StartupErr)
	}
}

func TestDesktopNewSessionDefaultsHonorExplicitEmptyProviderAccess(t *testing.T) {
	isolateDesktopUserDirs(t)
	seedUserCredentials(t, "REASONIX_TEST_SESSION_KEY=test-key\n")

	cfg := config.LoadForEdit(config.UserConfigPath())
	cfg.Providers = []config.ProviderEntry{{
		Name:      "test-prov",
		Kind:      "openai",
		BaseURL:   "https://example.com",
		Model:     "test-model",
		APIKeyEnv: "REASONIX_TEST_SESSION_KEY",
	}}
	cfg.Desktop.ProviderAccess = []string{}
	if err := cfg.SetDefaultModel("test-prov/test-model"); err != nil {
		t.Fatalf("SetDefaultModel: %v", err)
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save user config: %v", err)
	}

	app := NewApp()
	meta, err := app.EnsureBlankTab("global", "")
	if err != nil {
		t.Fatalf("EnsureBlankTab: %v", err)
	}
	created := app.tabs[meta.ID]
	if created == nil {
		t.Fatalf("new tab %q missing from app.tabs", meta.ID)
	}
	if created.model != "" {
		t.Fatalf("new session model = %q, want no provider selected", created.model)
	}
	if created.Ctrl != nil || !strings.Contains(created.StartupErr, "chat-capable provider") {
		t.Fatalf("new session runtime = (%v, %q), want an actionable no-provider startup error", created.Ctrl, created.StartupErr)
	}
	if !app.NeedsOnboarding() {
		t.Fatal("NeedsOnboarding should be true when provider access is explicitly empty")
	}
}

func TestDesktopNewSessionDefaultsUsesProjectDefaultAndSkipsItsKeylessProvider(t *testing.T) {
	isolateDesktopUserDirs(t)
	seedUserCredentials(t, "REASONIX_TEST_SESSION_KEY=test-key\n")

	userCfg := config.LoadForEdit(config.UserConfigPath())
	userCfg.Providers = append(userCfg.Providers, config.ProviderEntry{
		Name:      "test-prov",
		Kind:      "openai",
		BaseURL:   "https://example.com",
		Model:     "test-model",
		APIKeyEnv: "REASONIX_TEST_SESSION_KEY",
	})
	if err := userCfg.SetDefaultModel("test-prov/test-model"); err != nil {
		t.Fatalf("SetDefaultModel: %v", err)
	}
	if err := userCfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save user config: %v", err)
	}

	workspace := robustTempDir(t)
	if err := os.WriteFile(filepath.Join(workspace, "reasonix.toml"), []byte(`default_model = "deepseek-pro/deepseek-v4-pro"
`), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	app := NewApp()
	meta, err := app.EnsureBlankTab("project", workspace)
	if err != nil {
		t.Fatalf("EnsureBlankTab: %v", err)
	}
	created := app.tabs[meta.ID]
	if created == nil {
		t.Fatalf("new tab %q missing from app.tabs", meta.ID)
	}
	if created.model != "test-prov/test-model" {
		t.Fatalf("new project session model = %q, want configured fallback test-prov/test-model", created.model)
	}
	if !strings.Contains(filepath.Base(created.SessionPath), "test-model") {
		t.Fatalf("new project session path = %q, want filename seeded by fallback model", created.SessionPath)
	}
}
