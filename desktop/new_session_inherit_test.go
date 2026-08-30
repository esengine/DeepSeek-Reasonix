package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
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
	if created.qualityFloor != "" && created.qualityFloor != control.QualityFloorStandard {
		t.Fatalf("qualityFloor = %q, want standard default (must not inherit economy)", created.qualityFloor)
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

func TestEnsureBlankTabRetargetsReusedSameNameModelToDefaultProvider(t *testing.T) {
	isolateDesktopUserDirs(t)
	model, oldRef, defaultRef := configureSameNameModelProviders(t)

	globalRoot := globalWorkspaceRoot()
	path, err := createEmptySessionFile(desktopSessionDir(globalRoot), model)
	if err != nil {
		t.Fatalf("create empty session: %v", err)
	}
	if err := agent.SetBranchModelPreserveUpdated(path, oldRef); err != nil {
		t.Fatalf("seed old session model: %v", err)
	}

	app := NewApp()
	app.ctx = context.Background()
	tab := testTab("reused", globalRoot)
	tab.Scope = "global"
	tab.WorkspaceRoot = globalRoot
	tab.SessionPath = path
	tab.model = oldRef
	tab.Ctrl.SetSessionPath(path)
	tab.sink = &tabEventSink{tabID: tab.ID, app: app}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(func() {
		if tab.Ctrl != nil {
			tab.Ctrl.Close()
		}
		tab.releaseSessionLease()
	})

	meta, err := app.EnsureBlankTab("global", "")
	if err != nil {
		t.Fatalf("EnsureBlankTab: %v", err)
	}
	if meta.ID != tab.ID {
		t.Fatalf("reused tab = %q, want %q", meta.ID, tab.ID)
	}
	if tab.model != defaultRef || tab.Ctrl.ModelRef() != defaultRef {
		t.Fatalf("reused runtime model = tab:%q controller:%q, want %q", tab.model, tab.Ctrl.ModelRef(), defaultRef)
	}
	if stored, ok := agent.LoadSessionModel(tab.currentSessionPath()); !ok || stored != defaultRef {
		t.Fatalf("stored session model = %q, %v, want %q", stored, ok, defaultRef)
	}

	current := ""
	for _, info := range app.ModelsForTab(tab.ID) {
		if info.Current {
			if current != "" {
				t.Fatalf("multiple current models: %q and %q", current, info.Ref)
			}
			current = info.Ref
		}
	}
	if current != defaultRef {
		t.Fatalf("model switcher current ref = %q, want %q", current, defaultRef)
	}
}

func TestEnsureBlankTabRepairsStaleStoredProviderWhenRuntimeAlreadyDefault(t *testing.T) {
	isolateDesktopUserDirs(t)
	model, oldRef, defaultRef := configureSameNameModelProviders(t)

	globalRoot := globalWorkspaceRoot()
	path, err := createEmptySessionFile(desktopSessionDir(globalRoot), model)
	if err != nil {
		t.Fatalf("create empty session: %v", err)
	}

	app := NewApp()
	app.ctx = context.Background()
	tab := testTab("stale-meta", globalRoot)
	tab.Scope = "global"
	tab.WorkspaceRoot = globalRoot
	tab.SessionPath = path
	tab.model = oldRef
	tab.Ctrl.SetSessionPath(path)
	tab.sink = &tabEventSink{tabID: tab.ID, app: app}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(func() {
		if tab.Ctrl != nil {
			tab.Ctrl.Close()
		}
		tab.releaseSessionLease()
	})

	if err := app.SetModelForTab(tab.ID, defaultRef); err != nil {
		t.Fatalf("seed default runtime: %v", err)
	}
	if err := agent.SetBranchModelPreserveUpdated(path, oldRef); err != nil {
		t.Fatalf("seed stale stored model: %v", err)
	}

	if _, err := app.EnsureBlankTab("global", ""); err != nil {
		t.Fatalf("EnsureBlankTab: %v", err)
	}
	if tab.model != defaultRef || tab.Ctrl.ModelRef() != defaultRef {
		t.Fatalf("runtime model = tab:%q controller:%q, want %q", tab.model, tab.Ctrl.ModelRef(), defaultRef)
	}
	if stored, ok := agent.LoadSessionModel(tab.currentSessionPath()); !ok || stored != defaultRef {
		t.Fatalf("stored session model = %q, %v, want repaired %q", stored, ok, defaultRef)
	}
}

func TestEnsureBlankTabConcurrentModelSwitchKeepsLastSelection(t *testing.T) {
	isolateDesktopUserDirs(t)
	model, oldRef, defaultRef := configureSameNameModelProviders(t)

	globalRoot := globalWorkspaceRoot()
	path, err := createEmptySessionFile(desktopSessionDir(globalRoot), model)
	if err != nil {
		t.Fatalf("create empty session: %v", err)
	}
	if err := agent.SetBranchModelPreserveUpdated(path, oldRef); err != nil {
		t.Fatalf("seed old session model: %v", err)
	}

	app := NewApp()
	app.ctx = context.Background()
	tab := testTab("last-click", globalRoot)
	tab.Scope = "global"
	tab.WorkspaceRoot = globalRoot
	tab.SessionPath = path
	tab.model = oldRef
	tab.Ctrl.SetSessionPath(path)
	tab.sink = &tabEventSink{tabID: tab.ID, app: app}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(func() {
		if tab.Ctrl != nil {
			tab.Ctrl.Close()
		}
		tab.releaseSessionLease()
	})

	firstSwitchReturned := make(chan struct{})
	releaseFirstSwitch := make(chan struct{})
	var switchCount atomic.Int32
	app.modelSwitchTimingHook = func(modelSwitchTiming) {
		if switchCount.Add(1) == 1 {
			close(firstSwitchReturned)
			<-releaseFirstSwitch
		}
	}

	ensureDone := make(chan error, 1)
	go func() {
		_, err := app.EnsureBlankTab("global", "")
		ensureDone <- err
	}()

	select {
	case <-firstSwitchReturned:
	case <-time.After(5 * time.Second):
		close(releaseFirstSwitch)
		t.Fatal("timed out waiting for default model switch")
	}

	// Model selection remains available while EnsureBlankTab is completing.
	// The explicit second switch is the user's last click and must own both the
	// live runtime and the persisted provider identity.
	lastSwitchErr := app.SetModelForTab(tab.ID, oldRef)
	close(releaseFirstSwitch)
	if lastSwitchErr != nil {
		t.Fatalf("last model switch: %v", lastSwitchErr)
	}
	select {
	case err := <-ensureDone:
		if err != nil {
			t.Fatalf("EnsureBlankTab: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for EnsureBlankTab")
	}

	if tab.model != oldRef || tab.Ctrl.ModelRef() != oldRef {
		t.Fatalf("last selected runtime = tab:%q controller:%q, want %q (default was %q)", tab.model, tab.Ctrl.ModelRef(), oldRef, defaultRef)
	}
	if stored, ok := agent.LoadSessionModel(tab.currentSessionPath()); !ok || stored != oldRef {
		t.Fatalf("stored session model = %q, %v, want last selection %q", stored, ok, oldRef)
	}
}

func TestEnsureBlankTabRestartsStartingBlankWithDefaultProvider(t *testing.T) {
	isolateDesktopUserDirs(t)
	model, oldRef, defaultRef := configureSameNameModelProviders(t)

	globalRoot := globalWorkspaceRoot()
	path, err := createEmptySessionFile(desktopSessionDir(globalRoot), model)
	if err != nil {
		t.Fatalf("create empty session: %v", err)
	}
	if err := agent.SetBranchModelPreserveUpdated(path, oldRef); err != nil {
		t.Fatalf("seed old session model: %v", err)
	}

	cancelled := false
	app := NewApp()
	tab := &WorkspaceTab{
		ID:            "starting",
		Scope:         "global",
		WorkspaceRoot: globalRoot,
		SessionPath:   path,
		model:         oldRef,
		buildCancel:   func() { cancelled = true },
		disabledMCP:   map[string]ServerView{},
	}
	tab.sink = &tabEventSink{tabID: tab.ID, app: app}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(func() {
		if tab.Ctrl != nil {
			tab.Ctrl.Close()
		}
		tab.releaseSessionLease()
	})

	meta, err := app.EnsureBlankTab("global", "")
	if err != nil {
		t.Fatalf("EnsureBlankTab: %v", err)
	}
	if meta.ID != tab.ID || !cancelled {
		t.Fatalf("starting blank reuse = id:%q cancelled:%v, want %q and cancelled old build", meta.ID, cancelled, tab.ID)
	}
	if !tab.Ready || tab.Ctrl == nil || tab.model != defaultRef || tab.Ctrl.ModelRef() != defaultRef {
		t.Fatalf("restarted blank runtime = ready:%v controller:%v tab:%q, want %q", tab.Ready, tab.Ctrl != nil, tab.model, defaultRef)
	}
	if stored, ok := agent.LoadSessionModel(tab.currentSessionPath()); !ok || stored != defaultRef {
		t.Fatalf("stored session model = %q, %v, want %q", stored, ok, defaultRef)
	}
}

func configureSameNameModelProviders(t *testing.T) (model, oldRef, defaultRef string) {
	t.Helper()
	seedUserCredentials(t, "DUPLICATE_MODEL_KEY=test-key\n")
	model = "deepseek-v4-flash-0731"
	oldRef = "qwen/" + model
	defaultRef = "token-rhythm/" + model
	cfg := config.LoadForEdit(config.UserConfigPath())
	cfg.DefaultModel = defaultRef
	cfg.Desktop.ProviderAccess = []string{"qwen", "token-rhythm"}
	cfg.Providers = []config.ProviderEntry{
		{Name: "qwen", Kind: "openai", BaseURL: "https://qwen.example.invalid/v1", Model: model, APIKeyEnv: "DUPLICATE_MODEL_KEY"},
		{Name: "token-rhythm", Kind: "openai", BaseURL: "https://token-rhythm.example.invalid/v1", Model: model, APIKeyEnv: "DUPLICATE_MODEL_KEY"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}
	return model, oldRef, defaultRef
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

func TestSetDefaultModelPersistsSessionSidecar(t *testing.T) {
	isolateDesktopUserDirs(t)
	oldRef, newRef := configureSwitchableDefaultModels(t)
	cfg := config.LoadForEdit(config.UserConfigPath())
	cfg.DefaultModel = oldRef
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("seed previous default: %v", err)
	}

	globalRoot := globalWorkspaceRoot()
	path, err := createEmptySessionFile(desktopSessionDir(globalRoot), "old-model")
	if err != nil {
		t.Fatalf("create empty session: %v", err)
	}
	if err := agent.SetBranchModelPreserveUpdated(path, oldRef); err != nil {
		t.Fatalf("seed old session model: %v", err)
	}

	app := NewApp()
	tab := testTab("default-sidecar", globalRoot)
	tab.Scope = "global"
	tab.WorkspaceRoot = globalRoot
	tab.SessionPath = path
	tab.model = oldRef
	tab.Ctrl.SetSessionPath(path)
	tab.sink = &tabEventSink{tabID: tab.ID, app: app}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(func() {
		if tab.Ctrl != nil {
			tab.Ctrl.Close()
		}
	})

	if err := app.SetDefaultModel(newRef); err != nil {
		t.Fatalf("SetDefaultModel: %v", err)
	}
	if got := config.LoadForEdit(config.UserConfigPath()).DefaultModel; got != newRef {
		t.Fatalf("default_model = %q, want %q", got, newRef)
	}
	if tab.model != newRef {
		t.Fatalf("tab model = %q, want %q", tab.model, newRef)
	}
	if stored, ok := agent.LoadSessionModel(path); !ok || stored != newRef {
		t.Fatalf("stored session model = %q, %v, want %q", stored, ok, newRef)
	}
}

func TestNewSessionForTabUsesConfiguredDefaultModel(t *testing.T) {
	isolateDesktopUserDirs(t)
	oldRef, newRef := configureSwitchableDefaultModels(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	oldSession := agent.NewSession("sys")
	oldSession.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	oldExec := agent.New(nil, nil, oldSession, agent.Options{}, event.Discard)
	oldPath := filepath.Join(dir, "old.jsonl")
	if err := os.WriteFile(oldPath, []byte(`{"role":"user","content":"hello"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write old session: %v", err)
	}
	if err := agent.SetBranchModelPreserveUpdated(oldPath, oldRef); err != nil {
		t.Fatalf("seed old session model: %v", err)
	}
	oldCtrl := control.New(control.Options{Executor: oldExec, SessionDir: dir, SessionPath: oldPath, Label: oldRef, Sink: event.Discard})

	app := NewApp()
	app.ctx = context.Background()
	tab := &WorkspaceTab{
		ID:          "new-session-default",
		Scope:       "global",
		Ready:       true,
		model:       oldRef,
		SessionPath: oldPath,
		Ctrl:        oldCtrl,
		sink:        &tabEventSink{tabID: "new-session-default", app: app},
		disabledMCP: map[string]ServerView{},
	}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(func() {
		if tab.Ctrl != nil {
			tab.Ctrl.Close()
		}
		tab.releaseSessionLease()
	})

	if err := app.NewSessionForTab(tab.ID); err != nil {
		t.Fatalf("NewSessionForTab: %v", err)
	}
	if tab.model != newRef {
		t.Fatalf("new session tab model = %q, want configured default %q", tab.model, newRef)
	}
	if stored, ok := agent.LoadSessionModel(tab.currentSessionPath()); !ok || stored != newRef {
		t.Fatalf("new session stored model = %q, %v, want %q", stored, ok, newRef)
	}
	if oldStored, ok := agent.LoadSessionModel(oldPath); !ok || oldStored != oldRef {
		t.Fatalf("previous session model = %q, %v, want unchanged %q", oldStored, ok, oldRef)
	}
}

func TestNewSessionForTabAlignsBlankTabToDefault(t *testing.T) {
	isolateDesktopUserDirs(t)
	oldRef, newRef := configureSwitchableDefaultModels(t)

	globalRoot := globalWorkspaceRoot()
	path, err := createEmptySessionFile(desktopSessionDir(globalRoot), "old-model")
	if err != nil {
		t.Fatalf("create empty session: %v", err)
	}
	if err := agent.SetBranchModelPreserveUpdated(path, oldRef); err != nil {
		t.Fatalf("seed old session model: %v", err)
	}

	blank := agent.NewSession("sys")
	exec := agent.New(nil, nil, blank, agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: exec, SessionDir: desktopSessionDir(globalRoot), SessionPath: path, Label: oldRef, Sink: event.Discard})

	app := NewApp()
	app.ctx = context.Background()
	tab := &WorkspaceTab{
		ID:            "blank-default",
		Scope:         "global",
		WorkspaceRoot: globalRoot,
		Ready:         true,
		model:         oldRef,
		SessionPath:   path,
		Ctrl:          ctrl,
		sink:          &tabEventSink{tabID: "blank-default", app: app},
		disabledMCP:   map[string]ServerView{},
	}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(func() {
		if tab.Ctrl != nil {
			tab.Ctrl.Close()
		}
		tab.releaseSessionLease()
	})

	if err := app.NewSessionForTab(tab.ID); err != nil {
		t.Fatalf("NewSessionForTab: %v", err)
	}
	if tab.model != newRef {
		t.Fatalf("blank tab model = %q, want configured default %q", tab.model, newRef)
	}
	if stored, ok := agent.LoadSessionModel(tab.currentSessionPath()); !ok || stored != newRef {
		t.Fatalf("blank stored model = %q, %v, want %q", stored, ok, newRef)
	}
}

func configureSwitchableDefaultModels(t *testing.T) (oldRef, newRef string) {
	t.Helper()
	setDesktopTestCredential(t, "OLD_MODEL_KEY", "sk-test")
	setDesktopTestCredential(t, "NEW_MODEL_KEY", "sk-test")
	oldRef = "old/old-model"
	newRef = "new/new-model"
	cfg := config.Default()
	cfg.DefaultModel = newRef
	cfg.Desktop.ProviderAccess = []string{"old", "new"}
	cfg.Providers = []config.ProviderEntry{
		{Name: "old", Kind: "openai", BaseURL: "https://example.invalid/v1", Model: "old-model", APIKeyEnv: "OLD_MODEL_KEY"},
		{Name: "new", Kind: "openai", BaseURL: "https://example.invalid/v1", Model: "new-model", APIKeyEnv: "NEW_MODEL_KEY"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}
	return oldRef, newRef
}
