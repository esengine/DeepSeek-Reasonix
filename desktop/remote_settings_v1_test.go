package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/runtimeapi"
)

type remoteSettingsV1Runtime struct {
	runtimeapi.V1RuntimeAPI
	events         chan runtimeapi.Event
	workspace      runtimeapi.Workspace
	created        runtimeapi.CreatedSession
	snapshot       runtimeapi.SessionSnapshot
	capabilities   runtimeapi.Capabilities
	configSummary  runtimeapi.HostConfigSummary
	catalog        runtimeapi.SessionCatalog
	catalogErr     error
	catalogCalls   int
	suggestions    runtimeapi.MemorySuggestionsView
	acceptedMemory []runtimeapi.AcceptMemorySuggestionInput
	acceptedSkill  []runtimeapi.AcceptSkillSuggestionInput
}

func newRemoteSettingsV1Runtime() *remoteSettingsV1Runtime {
	ref := runtimeapi.SessionRef{WorkspaceID: "workspace_remote_settings", SessionID: "session_remote_settings"}
	return &remoteSettingsV1Runtime{
		events:       make(chan runtimeapi.Event, 1),
		workspace:    runtimeapi.Workspace{ID: ref.WorkspaceID, Name: "Remote repo", DisplayPath: "/srv/repo"},
		created:      runtimeapi.CreatedSession{Session: ref, TopicID: "topic_remote_settings", TopicTitle: "Remote settings"},
		snapshot:     runtimeapi.SessionSnapshot{Session: ref, TopicID: "topic_remote_settings", Title: "Remote settings"},
		capabilities: runtimeapi.Capabilities{HostConfig: true, Features: runtimeapi.Features{CoreSession: true, Memory: true, Research: true}},
		configSummary: runtimeapi.HostConfigSummary{
			Available: true, Revision: "config_revision_opaque", Models: []string{}, CollaborationModes: []string{},
			TokenModes: []string{}, ToolApprovalModes: []string{},
			DisplayPaths:  []runtimeapi.ConfigDisplayPath{{Scope: "workspace", DisplayPath: "/srv/repo/.reasonix/config.toml"}},
			FeatureStates: []runtimeapi.FeatureState{{Feature: "memory", Available: true}},
		},
	}
}

func (r *remoteSettingsV1Runtime) Connection(context.Context) (runtimeapi.ConnectionView, error) {
	return runtimeapi.ConnectionView{Label: "Remote settings Host"}, nil
}
func (r *remoteSettingsV1Runtime) BrowseWorkspace(context.Context, runtimeapi.BrowseWorkspaceInput) (runtimeapi.WorkspacePage, error) {
	return runtimeapi.WorkspacePage{}, nil
}
func (r *remoteSettingsV1Runtime) OpenWorkspace(context.Context, runtimeapi.OpenWorkspaceInput) (runtimeapi.OpenWorkspaceResult, error) {
	return runtimeapi.OpenWorkspaceResult{Workspace: r.workspace}, nil
}
func (r *remoteSettingsV1Runtime) CreateSession(context.Context, runtimeapi.CreateSessionInput) (runtimeapi.CreatedSession, error) {
	return r.created, nil
}
func (r *remoteSettingsV1Runtime) AttachAndSubscribe(context.Context, runtimeapi.AttachAndSubscribeInput) (runtimeapi.SessionSnapshot, error) {
	return r.snapshot, nil
}
func (r *remoteSettingsV1Runtime) ComposerSubmit(context.Context, runtimeapi.ComposerSubmitInput) (runtimeapi.ComposerSubmitResult, error) {
	return runtimeapi.ComposerSubmitResult{Kind: runtimeapi.SubmitTurn, TurnID: "turn_settings"}, nil
}
func (r *remoteSettingsV1Runtime) SteerTurn(context.Context, runtimeapi.SteerInput) error { return nil }
func (r *remoteSettingsV1Runtime) CancelTurn(context.Context, runtimeapi.CancelTurnInput) error {
	return nil
}
func (r *remoteSettingsV1Runtime) ApprovePrompt(context.Context, runtimeapi.ApproveInput) error {
	return nil
}
func (r *remoteSettingsV1Runtime) AnswerPrompt(context.Context, runtimeapi.AnswerInput) error {
	return nil
}
func (r *remoteSettingsV1Runtime) Events() <-chan runtimeapi.Event { return r.events }
func (r *remoteSettingsV1Runtime) HostCapabilities(context.Context) (runtimeapi.Capabilities, error) {
	return r.capabilities, nil
}
func (r *remoteSettingsV1Runtime) HostConfigSummary(context.Context) (runtimeapi.HostConfigSummary, error) {
	return r.configSummary, nil
}
func (r *remoteSettingsV1Runtime) SessionCatalog(_ context.Context, input runtimeapi.SessionCatalogInput) (runtimeapi.SessionCatalog, error) {
	r.catalogCalls++
	if input.Session != r.created.Session {
		return runtimeapi.SessionCatalog{}, errors.New("unexpected Session identity")
	}
	return r.catalog, r.catalogErr
}
func (r *remoteSettingsV1Runtime) MemorySuggestions(context.Context, runtimeapi.MemoryInput) (runtimeapi.MemorySuggestionsView, error) {
	return r.suggestions, nil
}
func (r *remoteSettingsV1Runtime) AcceptMemorySuggestion(_ context.Context, input runtimeapi.AcceptMemorySuggestionInput) (runtimeapi.AcceptMemorySuggestionResult, error) {
	r.acceptedMemory = append(r.acceptedMemory, input)
	return runtimeapi.AcceptMemorySuggestionResult{MemoryID: "memory_remote_opaque"}, nil
}
func (r *remoteSettingsV1Runtime) AcceptSkillSuggestion(_ context.Context, input runtimeapi.AcceptSkillSuggestionInput) (runtimeapi.AcceptSkillSuggestionResult, error) {
	r.acceptedSkill = append(r.acceptedSkill, input)
	return runtimeapi.AcceptSkillSuggestionResult{SkillID: "skill_remote_opaque"}, nil
}

type remoteSettingsTargetAdapter struct {
	target  TargetDescriptor
	runtime *remoteSettingsV1Runtime
}

func (a *remoteSettingsTargetAdapter) Descriptor() TargetDescriptor      { return a.target }
func (a *remoteSettingsTargetAdapter) RuntimeAPI() runtimeapi.RuntimeAPI { return a.runtime }
func (*remoteSettingsTargetAdapter) CanRelease(context.Context) (ReleaseStatus, error) {
	return ReleaseStatus{}, nil
}
func (*remoteSettingsTargetAdapter) Detach(context.Context) error { return nil }

func newRemoteSettingsTestApp(t *testing.T, runtime *remoteSettingsV1Runtime) *App {
	t.Helper()
	target := TargetDescriptor{Kind: TargetRemote, ID: "host_remote_settings", Label: "Remote settings Host"}
	manager, err := NewTargetManager(TargetConnectorFunc(func(context.Context, TargetDescriptor) (TargetAdapter, error) {
		return nil, context.Canceled
	}), &remoteSettingsTargetAdapter{target: target, runtime: runtime}, TargetManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	app := &App{ctx: context.Background()}
	setRemoteWorkbenchTestEmitter(app, func(context.Context, string, ...interface{}) {})
	app.readyHook = func() {}
	app.projectTreeChangedHook = func() {}
	installRemoteAppTestState(t, app, newRemoteAppTestStore(t), manager)
	manager.SetEventSink(app.handleTargetRuntimeEvent)
	manager.SetStateSink(app.handleTargetState)
	return app
}

func TestDesktopHistoryPageTurnsDefaultsPersistsAndRejectsInvalid(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	if got := app.desktopHistoryPageTurns(); got != config.DefaultDesktopHistoryPageTurns {
		t.Fatalf("default history page turns = %d, want %d", got, config.DefaultDesktopHistoryPageTurns)
	}
	if got := app.DesktopStartupSettings().HistoryPageTurns; got != config.DefaultDesktopHistoryPageTurns {
		t.Fatalf("startup history page turns = %d", got)
	}
	if err := app.SetDesktopHistoryPageTurns(125); err != nil {
		t.Fatalf("SetDesktopHistoryPageTurns: %v", err)
	}
	if got := app.Settings().HistoryPageTurns; got != 125 {
		t.Fatalf("Settings history page turns = %d, want 125", got)
	}
	if got := app.DesktopStartupSettings().HistoryPageTurns; got != 125 {
		t.Fatalf("startup history page turns = %d, want 125", got)
	}
	if raw, err := os.ReadFile(config.UserConfigPath()); err != nil {
		t.Fatalf("read user config: %v", err)
	} else if string(raw) == "" {
		t.Fatal("persisted user config is empty")
	}
	for _, turns := range []int{0, 201} {
		if err := app.SetDesktopHistoryPageTurns(turns); err == nil {
			t.Fatalf("SetDesktopHistoryPageTurns(%d) succeeded", turns)
		}
		if got := app.desktopHistoryPageTurns(); got != 125 {
			t.Fatalf("invalid setter changed history page turns to %d", got)
		}
	}
}

func TestRemoteHostRuntimeSummaryContainsOnlyRuntimeSafeProjection(t *testing.T) {
	runtime := newRemoteSettingsV1Runtime()
	app := newRemoteSettingsTestApp(t, runtime)
	view, err := app.RemoteHostRuntimeSummary()
	if err != nil {
		t.Fatal(err)
	}
	if !view.Capabilities.Features.CoreSession || !view.Capabilities.Features.Memory || !view.Config.Available {
		t.Fatalf("Remote Host summary = %+v", view)
	}
	if len(view.Config.DisplayPaths) != 1 || view.Config.DisplayPaths[0].DisplayPath != "/srv/repo/.reasonix/config.toml" {
		t.Fatalf("config display paths = %+v", view.Config.DisplayPaths)
	}
	if view.Config.Models == nil || view.Config.CollaborationModes == nil || view.Config.TokenModes == nil || view.Config.ToolApprovalModes == nil || view.Config.EffectiveScopes == nil || view.Config.CLIHints == nil {
		t.Fatalf("safe summary arrays must be non-nil: %+v", view.Config)
	}
	if view.Catalog.Available || view.Catalog.MCPServers == nil || view.Catalog.Skills == nil || view.Catalog.Plugins == nil {
		t.Fatalf("no active Session must produce generic non-nil unavailable catalog: %+v", view.Catalog)
	}
	if runtime.catalogCalls != 0 {
		t.Fatalf("SessionCatalog called without an active Session: %d", runtime.catalogCalls)
	}
}

func TestRemoteHostRuntimeSummaryProjectsCurrentSessionCatalogOnceWithoutSecrets(t *testing.T) {
	runtime := newRemoteSettingsV1Runtime()
	runtime.catalog = runtimeapi.SessionCatalog{
		Revision: "catalog_revision_secret",
		Commands: []runtimeapi.CommandCatalogItem{{Name: "/secret-command", Description: "https://secret.invalid/command"}},
		MCPServers: []runtimeapi.MCPServerCatalogItem{
			{Name: "browser", Available: true, ToolCount: 3},
			{Name: "offline", Available: false, ToolCount: -2},
		},
		Skills: []runtimeapi.SkillCatalogItem{{
			ID: "skill_opaque_secret", Name: "review", Description: "Review changes", Scope: "workspace",
		}},
		Plugins: []runtimeapi.PluginCatalogItem{{
			ID: "plugin_opaque_secret", Name: "quality", Enabled: true,
		}},
	}
	app := newRemoteSettingsTestApp(t, runtime)
	if _, err := app.CreateRemoteWorkspaceSession(RemoteCreateWorkspaceSessionInput{
		PrimaryDirectoryRef: "directory_opaque", TopicTitle: "Remote catalog",
	}); err != nil {
		t.Fatal(err)
	}

	view, err := app.RemoteHostRuntimeSummary()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.catalogCalls != 1 {
		t.Fatalf("SessionCatalog calls = %d, want exactly 1", runtime.catalogCalls)
	}
	if !view.Catalog.Available || len(view.Catalog.MCPServers) != 2 || len(view.Catalog.Skills) != 1 || len(view.Catalog.Plugins) != 1 {
		t.Fatalf("catalog projection = %+v", view.Catalog)
	}
	if got := view.Catalog.MCPServers[0]; got.Name != "browser" || got.Status != "available" || got.ToolCount != 3 {
		t.Fatalf("available MCP projection = %+v", got)
	}
	if got := view.Catalog.MCPServers[1]; got.Status != "unavailable" || got.ToolCount != 0 {
		t.Fatalf("unavailable MCP projection = %+v", got)
	}
	if got := view.Catalog.Skills[0]; got.Name != "review" || got.Description != "Review changes" || got.Scope != "workspace" {
		t.Fatalf("Skill projection = %+v", got)
	}
	if got := view.Catalog.Plugins[0]; got.Name != "quality" || !got.Enabled {
		t.Fatalf("Plugin projection = %+v", got)
	}
	encoded, err := json.Marshal(view.Catalog)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"catalog_revision_secret", "secret-command", "https://secret.invalid/command",
		"skill_opaque_secret", "plugin_opaque_secret",
	} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("catalog projection leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestRemoteHostRuntimeSummaryCollapsesCatalogFailureToGenericUnavailable(t *testing.T) {
	runtime := newRemoteSettingsV1Runtime()
	runtime.catalogErr = errors.New("raw transport secret /home/host/.ssh/config")
	app := newRemoteSettingsTestApp(t, runtime)
	if _, err := app.CreateRemoteWorkspaceSession(RemoteCreateWorkspaceSessionInput{
		PrimaryDirectoryRef: "directory_opaque", TopicTitle: "Remote catalog failure",
	}); err != nil {
		t.Fatal(err)
	}

	view, err := app.RemoteHostRuntimeSummary()
	if err != nil {
		t.Fatalf("catalog failure must not fail the safe Host summary: %v", err)
	}
	if runtime.catalogCalls != 1 || view.Catalog.Available || view.Catalog.MCPServers == nil || view.Catalog.Skills == nil || view.Catalog.Plugins == nil {
		t.Fatalf("generic unavailable catalog = %+v, calls = %d", view.Catalog, runtime.catalogCalls)
	}
	encoded, err := json.Marshal(view.Catalog)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "raw transport secret") || strings.Contains(string(encoded), "/home/host") {
		t.Fatalf("raw catalog failure leaked into projection: %s", encoded)
	}
}

func TestRemoteMemorySuggestionsAndAcceptanceUseOpaqueRuntimeIdentity(t *testing.T) {
	runtime := newRemoteSettingsV1Runtime()
	body, skillBody := "remote memory body", "# Remote skill"
	runtime.suggestions = runtimeapi.MemorySuggestionsView{
		Revision: "memory_revision_opaque", Available: true,
		Memories: []runtimeapi.MemorySuggestion{{SuggestionID: "suggestion_memory_opaque", Name: "remote-memory", Title: "Remote memory", Description: "Host candidate", Type: "project", Body: &body, Evidence: []string{"Host evidence"}}},
		Skills:   []runtimeapi.SkillSuggestion{{SuggestionID: "suggestion_skill_opaque", Name: "remote-skill", Description: "Host skill", Scope: "project", Body: &skillBody}},
	}
	app := newRemoteSettingsTestApp(t, runtime)
	status, err := app.CreateRemoteWorkspaceSession(RemoteCreateWorkspaceSessionInput{PrimaryDirectoryRef: "directory_opaque", TopicTitle: "Remote settings"})
	if err != nil {
		t.Fatal(err)
	}
	view := app.MemorySuggestionsForTab(status.TabID)
	if !view.Available || view.Source != "remote-runtime" || len(view.Memories) != 1 || len(view.Skills) != 1 {
		t.Fatalf("Remote suggestions = %+v", view)
	}
	if view.Memories[0].ExpectedRevision != "memory_revision_opaque" || view.Skills[0].ExpectedRevision != "memory_revision_opaque" {
		t.Fatalf("opaque revision was not projected: %+v", view)
	}
	memoryID, err := app.AcceptMemorySuggestionForTab(status.TabID, view.Memories[0])
	if err != nil {
		t.Fatal(err)
	}
	skillID, err := app.AcceptSkillSuggestionForTab(status.TabID, view.Skills[0])
	if err != nil {
		t.Fatal(err)
	}
	if memoryID != "memory_remote_opaque" || skillID != "skill_remote_opaque" {
		t.Fatalf("accepted ids = %q/%q", memoryID, skillID)
	}
	if len(runtime.acceptedMemory) != 1 || runtime.acceptedMemory[0].Session != runtime.created.Session || runtime.acceptedMemory[0].SuggestionID != "suggestion_memory_opaque" || runtime.acceptedMemory[0].ExpectedRevision != "memory_revision_opaque" {
		t.Fatalf("memory accept input = %+v", runtime.acceptedMemory)
	}
	if len(runtime.acceptedSkill) != 1 || runtime.acceptedSkill[0].Session != runtime.created.Session || runtime.acceptedSkill[0].SuggestionID != "suggestion_skill_opaque" || runtime.acceptedSkill[0].ExpectedRevision != "memory_revision_opaque" {
		t.Fatalf("skill accept input = %+v", runtime.acceptedSkill)
	}
}

func TestRemoteSettingsAndStartupStripLocalRuntimeSecrets(t *testing.T) {
	isolateDesktopUserDirs(t)
	path := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := `default_model = "local-model-secret"

[desktop]
language = "zh"
layout_style = "creation"
theme = "dark"
theme_style = "aurora"
close_behavior = "quit"
display_mode = "compact"
history_page_turns = 125
status_bar_style = "icon"
status_bar_items = ["model", "cache"]
check_updates = false
telemetry = false
metrics = false
expand_thinking = true

[agent]
system_prompt = "LOCAL_SYSTEM_PROMPT_SECRET"

[sandbox]
workspace_root = "/local/private/workspace"

[network]
proxy_mode = "manual"
proxy_url = "http://local-proxy-secret"

[network.proxy]
type = "http"
server = "local-proxy-host-secret"
username = "local-proxy-user-secret"
password = "local-proxy-password-secret"

[bot]
enabled = true
model = "local-bot-model-secret"

[bot.control]
enabled = true
addr = "127.0.0.1:secret"
token_env = "LOCAL_BOT_TOKEN_ENV_SECRET"

[bot.feishu]
app_id = "local-feishu-app-secret"
verification_token = "local-feishu-verification-secret"
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	app := newRemoteSettingsTestApp(t, newRemoteSettingsV1Runtime())
	settings := app.Settings()
	encoded, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{
		"local-model-secret", "LOCAL_SYSTEM_PROMPT_SECRET", "/local/private/workspace", "local-proxy-secret",
		"local-proxy-host-secret", "local-proxy-user-secret", "local-proxy-password-secret", "local-bot-model-secret",
		"LOCAL_BOT_TOKEN_ENV_SECRET", "local-feishu-app-secret", "local-feishu-verification-secret", path,
	} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("Remote Settings leaked Local value %q: %s", secret, encoded)
		}
	}
	if settings.DesktopLanguage != "zh" || settings.DesktopLayoutStyle != "creation" || settings.DesktopTheme != "dark" || settings.CloseBehavior != "quit" || settings.DisplayMode != "compact" || settings.HistoryPageTurns != 125 || settings.StatusBarStyle != "icon" || settings.CheckUpdates || settings.Telemetry || settings.Metrics || !settings.ExpandThinking {
		t.Fatalf("Desktop-only settings were not preserved: %+v", settings)
	}
	if settings.ConfigPath != "" || settings.DefaultModel != "" || settings.Agent.SystemPrompt != "" || settings.Network.Proxy.Password != "" || settings.Bot.Model != "" || settings.Bot.Control.TokenEnv != "" || settings.Bot.Feishu.VerificationToken != "" {
		t.Fatalf("Remote Settings contains Local runtime fields: %+v", settings)
	}
	startup := app.DesktopStartupSettings()
	startupJSON, err := json.Marshal(startup)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"local-bot-model-secret", "LOCAL_BOT_TOKEN_ENV_SECRET", "local-feishu-app-secret", "local-feishu-verification-secret"} {
		if strings.Contains(string(startupJSON), secret) {
			t.Fatalf("Remote startup settings leaked Local bot value %q: %s", secret, startupJSON)
		}
	}
	if startup.HistoryPageTurns != 125 || startup.DesktopLanguage != "zh" || startup.Bot.Model != "" || startup.Bot.Control.TokenEnv != "" {
		t.Fatalf("Remote startup projection = %+v", startup)
	}
}
