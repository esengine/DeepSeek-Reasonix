package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func isolateUserConfigHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))
	return home
}

// setRuntimeGOOS overrides the package-level runtimeGOOS for one test. The
// t.Setenv call is a guard: it panics if the test also uses t.Parallel, which
// would otherwise race on the shared global.
func setRuntimeGOOS(t *testing.T, goos string) {
	t.Helper()
	t.Setenv("REASONIX_TEST_GOOS", goos)
	old := runtimeGOOS
	runtimeGOOS = goos
	t.Cleanup(func() { runtimeGOOS = old })
}

func expectedDefaultReasonixHome(home string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(home, "AppData", "Roaming", "reasonix")
	}
	return filepath.Join(home, ".reasonix")
}

func TestUserConfigDisplayPathCollapsesHome(t *testing.T) {
	home := isolateUserConfigHome(t)
	got := userConfigDisplayPath()
	if !strings.HasPrefix(got, "~/") {
		t.Fatalf("display path = %q, want ~/ prefix", got)
	}
	if !strings.HasSuffix(got, "reasonix/config.toml") {
		t.Fatalf("display path = %q, want reasonix/config.toml suffix", got)
	}
	if strings.Contains(got, home) {
		t.Fatalf("display path %q must not embed the absolute home", got)
	}
}

func TestUserConfigPathUsesReasonixHome(t *testing.T) {
	home := isolateUserConfigHome(t)
	want := filepath.Join(expectedDefaultReasonixHome(home), "config.toml")
	if got := UserConfigPath(); filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("UserConfigPath() = %q, want %q", got, want)
	}
}

func TestUserConfigPathHonorsReasonixHome(t *testing.T) {
	home := isolateUserConfigHome(t)
	custom := filepath.Join(home, "custom-home")
	t.Setenv("REASONIX_HOME", custom)

	want := filepath.Join(custom, "config.toml")
	if got := UserConfigPath(); filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("UserConfigPath() = %q, want %q", got, want)
	}
}

func TestLoadForRootUsesWindowsHomeFallbackWhenConfigDirUnavailable(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()

	oldGOOS := runtimeGOOS
	oldConfigDir := osUserConfigDir
	oldHomeDir := osUserHomeDir
	runtimeGOOS = "windows"
	osUserConfigDir = func() string { return "" }
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() {
		runtimeGOOS = oldGOOS
		osUserConfigDir = oldConfigDir
		osUserHomeDir = oldHomeDir
	})

	t.Setenv("REASONIX_HOME", "")

	configPath := filepath.Join(home, "AppData", "Roaming", "reasonix", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("default_model = \"custom/from-home\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRoot(project)
	if err != nil {
		t.Fatalf("LoadForRoot() error = %v", err)
	}
	if cfg.DefaultModel != "custom/from-home" {
		t.Fatalf("DefaultModel = %q, want %q", cfg.DefaultModel, "custom/from-home")
	}
}

func TestRenderTOMLHeaderShowsResolvedConfigPath(t *testing.T) {
	isolateUserConfigHome(t)
	out := RenderTOML(Default())
	want := "> " + userConfigDisplayPath() + " > built-in defaults."
	if !strings.Contains(out, want) {
		t.Fatalf("rendered header missing resolved config path %q", want)
	}
}

func TestWriteRootsForRootExcludesUserConfigDirByDefault(t *testing.T) {
	isolateUserConfigHome(t)
	project := t.TempDir()
	cfg := Default()

	roots := cfg.WriteRootsForRoot(project)
	want := filepath.Clean(filepath.Dir(UserConfigPath()))
	for _, root := range roots {
		if filepath.Clean(root) == want {
			t.Fatalf("WriteRootsForRoot() = %v, must not include user config dir %q by default", roots, want)
		}
	}
	if got := filepath.Clean(roots[0]); got != filepath.Clean(project) {
		t.Fatalf("first write root = %q, want project %q", got, project)
	}
}

// TestRenderTOMLRoundTrips ensures the annotated TOML we emit parses back into
// an equivalent config — i.e. the wizard never writes a file it can't read.
func TestRenderTOMLRoundTrips(t *testing.T) {
	orig := Default()
	orig.Providers = append(orig.Providers, legacyMimoCustomProvider("mimo-pro"))
	orig.DefaultModel = "mimo-pro"
	orig.Language = "zh"
	orig.UI.Theme = "light"
	orig.UI.ThemeStyle = "glacier"
	orig.UI.ShortcutLayout = "desktop"
	orig.UI.CursorShape = "bar"
	orig.Desktop.Language = "en"
	orig.Desktop.LayoutStyle = "workbench"
	orig.Desktop.Theme = "dark"
	orig.Desktop.ThemeStyle = "graphite"
	orig.Desktop.CloseBehavior = "background"
	orig.Desktop.DisplayMode = "compact"
	orig.Desktop.StatusBarStyle = "text"
	orig.Desktop.StatusBarItems = []string{"model", "balance", "cache"}
	orig.Desktop.DefaultToolApprovalMode = "auto"
	orig.Desktop.CheckUpdates = boolPtr(false)
	orig.Desktop.Telemetry = boolPtr(false)
	orig.Notifications.Enabled = true
	orig.Notifications.TurnDone = true
	orig.Notifications.ApprovalRequest = true
	orig.Notifications.AskRequest = true
	orig.Agent.MaxSteps = 30
	orig.Agent.PlannerMaxSteps = 0
	orig.Agent.AutoPlanClassifier = "deepseek-flash"
	orig.Agent.ReasoningLanguage = "zh"
	orig.Agent.ToolResultSnipRatio = 0.65
	orig.Agent.SubagentModel = "mimo-pro"
	orig.Agent.SubagentModels = map[string]string{"review": "deepseek-pro"}
	orig.Agent.MaxSubagentDepth = 3
	orig.Agent.Keep = []string{"errors", "user_marked"}
	orig.Agent.RecentKeep = 4
	orig.Tools.BashTimeoutSeconds = intPtr(900)
	orig.Tools.BackgroundJobs.StalledWarningSeconds = intPtr(30)
	orig.Tools.Shell.Prefer = "bash"
	orig.Tools.Shell.Path = "/usr/local/bin/bash"
	orig.Permissions = PermissionsConfig{
		Mode:  "deny",
		Deny:  []string{"Bash(rm -rf*)"},
		Allow: []string{"Bash(go test:*)", "read_file"},
	}
	orig.Network = NetworkConfig{
		ProxyMode: "custom",
		NoProxy:   "localhost,127.0.0.1",
		Proxy: NetworkProxyConfig{
			Type:     "socks5",
			Server:   "127.0.0.1",
			Port:     7890,
			Username: "user",
			Password: "${REASONIX_PROXY_PASSWORD}",
		},
	}
	orig.Environment.Enabled = boolPtr(false)
	orig.Environment.Tools = map[string]string{"go": "/opt/homebrew/bin/go", "python3": "~/.pyenv/shims/python3"}
	orig.Skills.Paths = []string{"~/my-skills", "../shared/skills"}
	orig.Skills.ExcludedPaths = []string{"~/.agents/skills"}
	orig.Skills.DisabledSkills = []string{"review", "explore"}
	orig.Skills.MaxDepth = 2
	orig.Bot.ToolApprovalMode = "auto"
	orig.Bot.Control = BotControlConfig{Enabled: true, Addr: "127.0.0.1:39001", TokenEnv: "BOT_CONTROL_TOKEN"}
	orig.Bot.Routes = []BotRouteConfig{{
		ConnectionID:     "feishu-lark",
		ChatType:         "group",
		ChatID:           "oc_group",
		Model:            "deepseek-pro",
		ToolApprovalMode: "ask",
		WorkspaceRoot:    "/tmp/reasonix-route",
	}}
	orig.Bot.Connections = []BotConnectionConfig{{
		ID:               "feishu-lark",
		Provider:         "feishu",
		Domain:           "lark",
		Label:            "Lark",
		Enabled:          true,
		Status:           "connected",
		Model:            "deepseek-pro",
		ToolApprovalMode: "yolo",
		WorkspaceRoot:    "/tmp/reasonix-bot",
		Credential:       BotConnectionCredential{AppID: "cli_lark", AppSecretEnv: "LARK_BOT_APP_SECRET"},
		SessionMappings: []BotConnectionSessionMapping{{
			RemoteID:      "ou_123",
			SessionID:     "topic:topic_bot",
			Scope:         "project",
			WorkspaceRoot: "/tmp/reasonix-bot",
			UpdatedAt:     "2026-06-11T00:00:00Z",
		}},
	}}
	orig.LSP = LSPConfig{
		Enabled: true,
		Servers: map[string]LSPServer{
			"lua": {
				Command:     "lua-language-server",
				Args:        []string{"--stdio"},
				Env:         map[string]string{"LUA_PATH": "./?.lua"},
				LanguageID:  "lua",
				Extensions:  []string{".lua", ".script", ".gui_script"},
				InstallHint: "install lua-language-server",
			},
		},
	}
	orig.Plugins = []PluginEntry{
		{Name: "example", Command: "reasonix-plugin-example"},
		{Name: "stripe", Type: "http", URL: "https://mcp.stripe.com", Headers: map[string]string{"Authorization": "Bearer x"}, TrustedReadOnlyTools: []string{"customer_read"}, AutoStart: boolPtr(false), Tier: "background"},
	}
	mm, _ := orig.Provider("mimo-pro")
	mm.BaseURL = "http://localhost:8000/v1"
	mm.ChatURL = "http://localhost:8000/v1/chat/completions"
	mm.ModelsURL = "http://localhost:8000/v1/models"
	mm.ReasoningProtocol = "openai"
	mm.PresetID = "mimo-api"
	mm.PresetVersion = ProviderPresetVersion
	ds, _ := orig.Provider("deepseek-flash")
	ds.Effort = "max"

	rendered := RenderTOML(orig)

	var got Config
	if _, err := toml.Decode(rendered, &got); err != nil {
		t.Fatalf("rendered TOML does not parse: %v\n---\n%s", err, rendered)
	}

	if got.DefaultModel != "mimo-pro" {
		t.Errorf("default_model = %q, want mimo-pro", got.DefaultModel)
	}
	if got.ConfigVersion != 4 {
		t.Errorf("config_version = %d, want 4", got.ConfigVersion)
	}
	if got.Language != "zh" {
		t.Errorf("language = %q, want zh", got.Language)
	}
	if got.UI.Theme != "light" {
		t.Errorf("ui.theme = %q, want light", got.UI.Theme)
	}
	if got.UI.ThemeStyle != "glacier" {
		t.Errorf("ui.theme_style = %q, want glacier", got.UI.ThemeStyle)
	}
	if got.UI.ShortcutLayout != "desktop" {
		t.Errorf("ui.shortcut_layout = %q, want desktop", got.UI.ShortcutLayout)
	}
	if got.UICursorShape() != "bar" {
		t.Errorf("ui.cursor_shape = %q, want bar", got.UICursorShape())
	}
	if got.Desktop.Language != "en" {
		t.Errorf("desktop.language = %q, want en", got.Desktop.Language)
	}
	if got.Desktop.LayoutStyle != "workbench" {
		t.Errorf("desktop.layout_style = %q, want workbench", got.Desktop.LayoutStyle)
	}
	if got.Desktop.Theme != "dark" {
		t.Errorf("desktop.theme = %q, want dark", got.Desktop.Theme)
	}
	if got.Desktop.ThemeStyle != "graphite" {
		t.Errorf("desktop.theme_style = %q, want graphite", got.Desktop.ThemeStyle)
	}
	if got.Desktop.CloseBehavior != "background" {
		t.Errorf("desktop.close_behavior = %q, want background", got.Desktop.CloseBehavior)
	}
	if got.DesktopDisplayMode() != "compact" {
		t.Errorf("desktop.display_mode = %q, want compact", got.DesktopDisplayMode())
	}
	if got.Desktop.StatusBarStyle != "text" {
		t.Errorf("desktop.status_bar_style = %q, want text", got.Desktop.StatusBarStyle)
	}
	if want := []string{"model", "balance", "cache"}; !reflect.DeepEqual(got.Desktop.StatusBarItems, want) {
		t.Errorf("desktop.status_bar_items = %v, want %v", got.Desktop.StatusBarItems, want)
	}
	if got.DesktopDefaultToolApprovalMode() != "auto" {
		t.Errorf("desktop.default_tool_approval_mode = %q, want auto", got.DesktopDefaultToolApprovalMode())
	}
	if got.Desktop.CheckUpdates == nil || *got.Desktop.CheckUpdates {
		t.Errorf("desktop.check_updates = %+v, want false", got.Desktop.CheckUpdates)
	}
	if !got.Notifications.Enabled || !got.Notifications.TurnDone || !got.Notifications.ApprovalRequest || !got.Notifications.AskRequest {
		t.Errorf("notifications not preserved: %+v", got.Notifications)
	}
	if got.Agent.MaxSteps != orig.Agent.MaxSteps {
		t.Errorf("max_steps = %d, want %d", got.Agent.MaxSteps, orig.Agent.MaxSteps)
	}
	if got.Agent.PlannerMaxSteps != orig.Agent.PlannerMaxSteps {
		t.Errorf("planner_max_steps = %d, want %d", got.Agent.PlannerMaxSteps, orig.Agent.PlannerMaxSteps)
	}
	if len(got.Bot.Connections) != 1 || got.Bot.Connections[0].Model != "deepseek-pro" || got.Bot.Connections[0].WorkspaceRoot != "/tmp/reasonix-bot" {
		t.Errorf("bot connection not preserved: %+v", got.Bot.Connections)
	}
	if got.Bot.ToolApprovalMode != "auto" || got.Bot.Connections[0].ToolApprovalMode != "yolo" {
		t.Errorf("bot tool approval mode not preserved: bot=%q connection=%q", got.Bot.ToolApprovalMode, got.Bot.Connections[0].ToolApprovalMode)
	}
	if !got.Bot.Control.Enabled || got.Bot.Control.Addr != "127.0.0.1:39001" || got.Bot.Control.TokenEnv != "BOT_CONTROL_TOKEN" {
		t.Errorf("bot control not preserved: %+v", got.Bot.Control)
	}
	if len(got.Bot.Routes) != 1 || got.Bot.Routes[0].WorkspaceRoot != "/tmp/reasonix-route" || got.Bot.Routes[0].ChatID != "oc_group" {
		t.Errorf("bot routes not preserved: %+v", got.Bot.Routes)
	}
	if len(got.Bot.Connections[0].SessionMappings) != 1 || got.Bot.Connections[0].SessionMappings[0].Scope != "project" || got.Bot.Connections[0].SessionMappings[0].WorkspaceRoot != "/tmp/reasonix-bot" {
		t.Errorf("bot session mapping scope not preserved: %+v", got.Bot.Connections[0].SessionMappings)
	}
	if got.Agent.Temperature != orig.Agent.Temperature {
		t.Errorf("temperature = %v, want %v", got.Agent.Temperature, orig.Agent.Temperature)
	}
	if got.Agent.AutoPlan != "off" {
		t.Errorf("auto_plan = %q, want off", got.Agent.AutoPlan)
	}
	if got.Agent.AutoPlanClassifier != "deepseek-flash" {
		t.Errorf("auto_plan_classifier = %q, want deepseek-flash", got.Agent.AutoPlanClassifier)
	}
	if got.Agent.ReasoningLanguage != "zh" {
		t.Errorf("reasoning_language = %q, want zh", got.Agent.ReasoningLanguage)
	}
	if got.Agent.SoftCompactRatio != orig.Agent.SoftCompactRatio {
		t.Errorf("soft_compact_ratio = %v, want %v", got.Agent.SoftCompactRatio, orig.Agent.SoftCompactRatio)
	}
	if got.Agent.ToolResultSnipRatio != orig.Agent.ToolResultSnipRatio {
		t.Errorf("tool_result_snip_ratio = %v, want %v", got.Agent.ToolResultSnipRatio, orig.Agent.ToolResultSnipRatio)
	}
	if got.Agent.CompactRatio != orig.Agent.CompactRatio {
		t.Errorf("compact_ratio = %v, want %v", got.Agent.CompactRatio, orig.Agent.CompactRatio)
	}
	if got.Agent.CompactForceRatio != orig.Agent.CompactForceRatio {
		t.Errorf("compact_force_ratio = %v, want %v", got.Agent.CompactForceRatio, orig.Agent.CompactForceRatio)
	}
	if strings.Join(got.Agent.Keep, ",") != strings.Join(orig.Agent.Keep, ",") {
		t.Errorf("keep = %v, want %v", got.Agent.Keep, orig.Agent.Keep)
	}
	if got.Agent.RecentKeep != orig.Agent.RecentKeep {
		t.Errorf("recent_keep = %d, want %d", got.Agent.RecentKeep, orig.Agent.RecentKeep)
	}
	if got.Agent.SystemPrompt != orig.Agent.SystemPrompt {
		t.Errorf("system_prompt mismatch:\n got %q\nwant %q", got.Agent.SystemPrompt, orig.Agent.SystemPrompt)
	}
	if !got.LSP.Enabled {
		t.Error("lsp.enabled = false, want true")
	}
	if got.Environment.Enabled == nil || *got.Environment.Enabled {
		t.Errorf("environment.enabled = %+v, want false", got.Environment.Enabled)
	}
	if !reflect.DeepEqual(got.Environment.Tools, orig.Environment.Tools) {
		t.Errorf("environment.tools = %v, want %v", got.Environment.Tools, orig.Environment.Tools)
	}
	lua := got.LSP.Servers["lua"]
	if lua.Command != "lua-language-server" || lua.LanguageID != "lua" || lua.InstallHint != "install lua-language-server" {
		t.Errorf("lsp.servers.lua scalar fields not preserved: %+v", lua)
	}
	if len(lua.Args) != 1 || lua.Args[0] != "--stdio" {
		t.Errorf("lsp.servers.lua.args = %v, want [--stdio]", lua.Args)
	}
	if lua.Env["LUA_PATH"] != "./?.lua" {
		t.Errorf("lsp.servers.lua.env = %v, want LUA_PATH", lua.Env)
	}
	if len(lua.Extensions) != 3 || lua.Extensions[2] != ".gui_script" {
		t.Errorf("lsp.servers.lua.extensions = %v", lua.Extensions)
	}
	if got.Agent.SubagentModel != "mimo-pro" {
		t.Errorf("subagent_model = %q, want mimo-pro", got.Agent.SubagentModel)
	}
	if got.Agent.SubagentModels["review"] != "deepseek-pro" {
		t.Errorf("subagent_models.review = %q, want deepseek-pro", got.Agent.SubagentModels["review"])
	}
	if got.Agent.MaxSubagentDepth != 3 {
		t.Errorf("max_subagent_depth = %d, want 3", got.Agent.MaxSubagentDepth)
	}
	if got.Tools.BashTimeoutSeconds == nil || *got.Tools.BashTimeoutSeconds != 900 {
		t.Errorf("tools.bash_timeout_seconds = %v, want 900", got.Tools.BashTimeoutSeconds)
	}
	if got.Tools.BackgroundJobs.StalledWarningSeconds == nil || *got.Tools.BackgroundJobs.StalledWarningSeconds != 30 {
		t.Errorf("tools.background_jobs.stalled_warning_seconds = %v, want 30", got.Tools.BackgroundJobs.StalledWarningSeconds)
	}
	if got.Tools.Shell.Prefer != "bash" {
		t.Errorf("tools.shell.prefer = %q, want bash", got.Tools.Shell.Prefer)
	}
	if got.Tools.Shell.Path != "/usr/local/bin/bash" {
		t.Errorf("tools.shell.path = %q, want /usr/local/bin/bash", got.Tools.Shell.Path)
	}
	if g, _ := got.Provider("mimo-pro"); g == nil || g.BaseURL != "http://localhost:8000/v1" || g.ChatURL != "http://localhost:8000/v1/chat/completions" || g.ModelsURL != "http://localhost:8000/v1/models" || g.ReasoningProtocol != "openai" {
		t.Errorf("mimo-pro endpoint fields not preserved: %+v", g)
	}
	if g, _ := got.Provider("mimo-pro"); g == nil || g.PresetID != "mimo-api" || g.PresetVersion != ProviderPresetVersion {
		t.Errorf("mimo-pro preset metadata not preserved: %+v", g)
	}
	if g, _ := got.Provider("deepseek-flash"); g == nil || g.Effort != "max" {
		t.Errorf("deepseek-flash effort not preserved: %+v", g)
	}
	if len(got.Providers) != len(orig.Providers) {
		t.Errorf("providers count = %d, want %d", len(got.Providers), len(orig.Providers))
	}
	if got.Permissions.Mode != "deny" {
		t.Errorf("permissions.mode = %q, want deny", got.Permissions.Mode)
	}
	if len(got.Permissions.Deny) != 1 || got.Permissions.Deny[0] != "Bash(rm -rf*)" {
		t.Errorf("permissions.deny = %v, want [Bash(rm -rf*)]", got.Permissions.Deny)
	}
	if len(got.Permissions.Allow) != 2 {
		t.Errorf("permissions.allow = %v, want 2 entries", got.Permissions.Allow)
	}
	if got.Network.ProxyMode != "custom" || got.Network.Proxy.Type != "socks5" || got.Network.Proxy.Port != 7890 {
		t.Errorf("network proxy not preserved: %+v", got.Network)
	}
	if len(got.Skills.Paths) != 2 || got.Skills.Paths[0] != "~/my-skills" {
		t.Errorf("skills.paths = %v", got.Skills.Paths)
	}
	if len(got.Skills.ExcludedPaths) != 1 || got.Skills.ExcludedPaths[0] != "~/.agents/skills" {
		t.Errorf("skills.excluded_paths = %v", got.Skills.ExcludedPaths)
	}
	if len(got.Skills.DisabledSkills) != 2 || got.Skills.DisabledSkills[0] != "review" || got.Skills.DisabledSkills[1] != "explore" {
		t.Errorf("skills.disabled_skills = %v", got.Skills.DisabledSkills)
	}
	if got.SkillMaxDepth() != 2 {
		t.Errorf("skills.max_depth = %d, want 2", got.SkillMaxDepth())
	}
	if len(got.Plugins) != 2 {
		t.Fatalf("plugins count = %d, want 2", len(got.Plugins))
	}
	stripe := got.Plugins[1]
	if stripe.Name != "stripe" || stripe.Type != "http" || stripe.URL != "https://mcp.stripe.com" {
		t.Errorf("http plugin not preserved: %+v", stripe)
	}
	if stripe.Headers["Authorization"] != "Bearer x" {
		t.Errorf("plugin headers not preserved: %v", stripe.Headers)
	}
	if len(stripe.TrustedReadOnlyTools) != 1 || stripe.TrustedReadOnlyTools[0] != "customer_read" {
		t.Errorf("plugin trusted_read_only_tools not preserved: %+v", stripe.TrustedReadOnlyTools)
	}
	if stripe.AutoStart == nil || *stripe.AutoStart {
		t.Errorf("auto_start should render and parse as false, got %+v", stripe.AutoStart)
	}
	if stripe.Tier != "" {
		t.Errorf("plugin tier should be omitted from new config, got %q", stripe.Tier)
	}
	if strings.Contains(rendered, "\ntier") {
		t.Errorf("rendered config should not contain MCP tier fields:\n%s", rendered)
	}
}

func TestRenderTOMLDocumentsPlanModeAllowedTools(t *testing.T) {
	cfg := Default()
	cfg.Agent.PlanModeAllowedTools = []string{"custom_reader"}
	cfg.Agent.PlanModeReadOnlyCommands = []string{"gh issue view"}

	rendered := RenderTOML(cfg)
	if !strings.Contains(rendered, `plan_mode_allowed_tools = ["custom_reader"]`) {
		t.Fatalf("rendered config should preserve plan_mode_allowed_tools:\n%s", rendered)
	}
	if !strings.Contains(rendered, "extra read-only declarations") || !strings.Contains(rendered, "cannot unlock known blocked tools or unsafe bash") {
		t.Fatalf("rendered config should document tightened plan_mode_allowed_tools semantics:\n%s", rendered)
	}

	var got Config
	if _, err := toml.Decode(rendered, &got); err != nil {
		t.Fatalf("rendered TOML does not parse: %v\n%s", err, rendered)
	}
	if !reflect.DeepEqual(got.Agent.PlanModeAllowedTools, cfg.Agent.PlanModeAllowedTools) {
		t.Fatalf("PlanModeAllowedTools round trip = %v, want %v", got.Agent.PlanModeAllowedTools, cfg.Agent.PlanModeAllowedTools)
	}
	if !strings.Contains(rendered, `plan_mode_read_only_commands = ["gh issue view"]`) {
		t.Fatalf("rendered config should preserve plan_mode_read_only_commands:\n%s", rendered)
	}
	if !strings.Contains(rendered, "concrete read-only shell prefixes") {
		t.Fatalf("rendered config should document plan_mode_read_only_commands semantics:\n%s", rendered)
	}
	if !reflect.DeepEqual(got.Agent.PlanModeReadOnlyCommands, cfg.Agent.PlanModeReadOnlyCommands) {
		t.Fatalf("PlanModeReadOnlyCommands round trip = %v, want %v", got.Agent.PlanModeReadOnlyCommands, cfg.Agent.PlanModeReadOnlyCommands)
	}
}

func TestRenderTOMLDocumentsPluginTrustedReadOnlyTools(t *testing.T) {
	cfg := Default()
	cfg.Plugins = []PluginEntry{{
		Name:                 "github",
		Command:              "github-mcp",
		TrustedReadOnlyTools: []string{"issue_read", "pull_request_read"},
	}}

	rendered := RenderTOML(cfg)
	if !strings.Contains(rendered, `trusted_read_only_tools = ["issue_read", "pull_request_read"]`) {
		t.Fatalf("rendered config should preserve trusted_read_only_tools:\n%s", rendered)
	}
	if !strings.Contains(rendered, "optional pre-seeded MCP read-only trust") {
		t.Fatalf("rendered config should document trusted_read_only_tools semantics:\n%s", rendered)
	}

	var got Config
	if _, err := toml.Decode(rendered, &got); err != nil {
		t.Fatalf("rendered TOML does not parse: %v\n%s", err, rendered)
	}
	if !reflect.DeepEqual(got.Plugins[0].TrustedReadOnlyTools, cfg.Plugins[0].TrustedReadOnlyTools) {
		t.Fatalf("TrustedReadOnlyTools round trip = %v, want %v", got.Plugins[0].TrustedReadOnlyTools, cfg.Plugins[0].TrustedReadOnlyTools)
	}
}

func TestRenderTOMLPreservesMCPCallTimeouts(t *testing.T) {
	cfg := Default()
	cfg.Tools.MCPCallTimeoutSeconds = intPtr(450)
	cfg.Plugins = []PluginEntry{{
		Name:               "maker",
		Command:            "maker-mcp",
		CallTimeoutSeconds: 600,
		ToolTimeoutSeconds: map[string]int{
			"generate/video": 1800,
			"search":         120,
		},
	}}

	rendered := RenderTOML(cfg)
	for _, want := range []string{
		"mcp_call_timeout_seconds = 450",
		"call_timeout_seconds = 600",
		`tool_timeout_seconds = { "generate/video" = 1800, "search" = 120 }`,
		"Raw MCP tool names",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered config missing %q:\n%s", want, rendered)
		}
	}

	var got Config
	if _, err := toml.Decode(rendered, &got); err != nil {
		t.Fatalf("rendered TOML does not parse: %v\n%s", err, rendered)
	}
	if got.Tools.MCPCallTimeoutSeconds == nil || *got.Tools.MCPCallTimeoutSeconds != 450 {
		t.Fatalf("MCPCallTimeoutSeconds round trip = %v, want 450", got.Tools.MCPCallTimeoutSeconds)
	}
	if got.Plugins[0].CallTimeoutSeconds != 600 {
		t.Fatalf("CallTimeoutSeconds round trip = %d, want 600", got.Plugins[0].CallTimeoutSeconds)
	}
	if !reflect.DeepEqual(got.Plugins[0].ToolTimeoutSeconds, cfg.Plugins[0].ToolTimeoutSeconds) {
		t.Fatalf("ToolTimeoutSeconds round trip = %v, want %v", got.Plugins[0].ToolTimeoutSeconds, cfg.Plugins[0].ToolTimeoutSeconds)
	}
}

func TestRenderTOMLCreationLayoutStyle(t *testing.T) {
	c := Default()
	if err := c.SetDesktopLayoutStyle("creation"); err != nil {
		t.Fatalf("SetDesktopLayoutStyle: %v", err)
	}
	rendered := RenderTOML(c)
	var got Config
	if _, err := toml.Decode(rendered, &got); err != nil {
		t.Fatalf("rendered TOML does not parse: %v\n---\n%s", err, rendered)
	}
	if got.Desktop.LayoutStyle != "creation" {
		t.Errorf("desktop.layout_style = %q, want creation", got.Desktop.LayoutStyle)
	}
	if got.DesktopLayoutStyle() != "creation" {
		t.Errorf("DesktopLayoutStyle() = %q, want creation", got.DesktopLayoutStyle())
	}
}

func TestScopedRenderPreservesLSPConfig(t *testing.T) {
	const src = `
config_version = 4
default_model = "mimo"

[lsp]
enabled = true

[lsp.servers.lua]
command = "lua-language-server"
args = ["--stdio"]
env = { LUA_PATH = "./?.lua" }
language_id = "lua"
extensions = [".lua", ".script", ".gui_script"]
install_hint = "install lua-language-server"

[lsp.servers."c++"]
command = "clangd"
extensions = [".cc", ".cpp", ".hpp"]
`

	var cfg Config
	if _, err := toml.Decode(src, &cfg); err != nil {
		t.Fatalf("decode source TOML: %v", err)
	}

	for _, scope := range []RenderScope{RenderScopeFull, RenderScopeUser, RenderScopeProject} {
		t.Run(string(scope), func(t *testing.T) {
			rendered := RenderTOMLForScope(&cfg, scope)
			if !strings.Contains(rendered, "[lsp]") {
				t.Fatalf("render missing [lsp]:\n%s", rendered)
			}
			if !strings.Contains(rendered, "[lsp.servers.lua]") {
				t.Fatalf("render missing [lsp.servers.lua]:\n%s", rendered)
			}
			if !strings.Contains(rendered, `[lsp.servers."c++"]`) {
				t.Fatalf("render missing quoted c++ server key:\n%s", rendered)
			}

			var got Config
			if _, err := toml.Decode(rendered, &got); err != nil {
				t.Fatalf("decode rendered TOML: %v\n---\n%s", err, rendered)
			}
			if !got.LSP.Enabled {
				t.Fatalf("lsp.enabled = false, want true")
			}
			lua, ok := got.LSP.Servers["lua"]
			if !ok {
				t.Fatalf("lsp.servers.lua missing after round-trip: %+v", got.LSP.Servers)
			}
			if lua.Command != "lua-language-server" || lua.LanguageID != "lua" || lua.InstallHint != "install lua-language-server" {
				t.Fatalf("lsp.servers.lua scalar fields not preserved: %+v", lua)
			}
			if len(lua.Args) != 1 || lua.Args[0] != "--stdio" {
				t.Fatalf("lsp.servers.lua.args = %v, want [--stdio]", lua.Args)
			}
			if lua.Env["LUA_PATH"] != "./?.lua" {
				t.Fatalf("lsp.servers.lua.env = %v, want LUA_PATH", lua.Env)
			}
			if len(lua.Extensions) != 3 || lua.Extensions[0] != ".lua" || lua.Extensions[2] != ".gui_script" {
				t.Fatalf("lsp.servers.lua.extensions = %v", lua.Extensions)
			}
			cpp, ok := got.LSP.Servers["c++"]
			if !ok {
				t.Fatalf("lsp.servers.c++ missing after round-trip: %+v", got.LSP.Servers)
			}
			if cpp.Command != "clangd" || len(cpp.Extensions) != 3 || cpp.Extensions[1] != ".cpp" {
				t.Fatalf("lsp.servers.c++ not preserved: %+v", cpp)
			}
		})
	}
}

func BenchmarkRenderTOMLWithLSPServers(b *testing.B) {
	cfg := Default()
	cfg.LSP.Servers = make(map[string]LSPServer, 64)
	for i := 0; i < 64; i++ {
		lang := "lang" + strconv.Itoa(i)
		cfg.LSP.Servers[lang] = LSPServer{
			Command:     "server-" + strconv.Itoa(i),
			Args:        []string{"--stdio", "--flag"},
			Env:         map[string]string{"SERVER_MODE": "stdio", "SERVER_ROOT": "."},
			LanguageID:  lang,
			Extensions:  []string{"." + lang, "." + lang + "x"},
			InstallHint: "install server-" + strconv.Itoa(i),
		}
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rendered := RenderTOML(cfg)
		if len(rendered) == 0 {
			b.Fatal("empty render")
		}
	}
}

func TestNotificationsDefaultsKeepEventSwitchesEnabled(t *testing.T) {
	cfg := Default()
	if cfg.Notifications.Enabled {
		t.Fatal("notifications.enabled default = true, want false")
	}
	if !cfg.Notifications.TurnDone || !cfg.Notifications.ApprovalRequest || !cfg.Notifications.AskRequest {
		t.Fatalf("notification event switches default off: %+v", cfg.Notifications)
	}

	if _, err := toml.Decode("[notifications]\nenabled = true\n", cfg); err != nil {
		t.Fatalf("decode notifications: %v", err)
	}
	if !cfg.Notifications.Enabled || !cfg.Notifications.TurnDone || !cfg.Notifications.ApprovalRequest || !cfg.Notifications.AskRequest {
		t.Fatalf("enabled-only config should keep event switches on: %+v", cfg.Notifications)
	}
}

func TestScopedRenderSeparatesUserAndProjectConfig(t *testing.T) {
	c := Default()
	c.Language = "zh"
	c.Desktop.Language = "zh"
	c.Desktop.Theme = "dark"
	c.Desktop.ThemeStyle = "graphite"
	c.Desktop.CloseBehavior = "background"
	c.Desktop.StatusBarStyle = "text"
	c.Desktop.DefaultToolApprovalMode = "auto"
	c.Desktop.CheckUpdates = boolPtr(false)

	user := RenderTOMLForScope(c, RenderScopeUser)
	for _, want := range []string{"config_version = 4", "[desktop]", `theme = "dark"`, `close_behavior = "background"`, `status_bar_style = "text"`, `default_tool_approval_mode = "auto"`, `check_updates = false`, "[notifications]", "[tools.shell]"} {
		if !strings.Contains(user, want) {
			t.Fatalf("user render missing %q:\n%s", want, user)
		}
	}

	project := RenderTOMLForScope(c, RenderScopeProject)
	for _, forbidden := range []string{"[desktop]", "[notifications]", "close_behavior =", "default_tool_approval_mode =", "check_updates =", "max_steps", "planner_max_steps"} {
		if strings.Contains(project, forbidden) {
			t.Fatalf("project render should not contain %q:\n%s", forbidden, project)
		}
	}
	if strings.Contains(project, "\nsystem_prompt = \"\"\"") {
		t.Fatalf("project render should not pin the built-in system prompt:\n%s", project)
	}
	if !strings.Contains(project, "# system_prompt =") {
		t.Fatalf("project render should leave a system prompt hint:\n%s", project)
	}
}

func TestProjectDeltaRendersToolsShellOverrides(t *testing.T) {
	c := Default()
	c.Tools.Shell.Prefer = "bash"
	c.Tools.Shell.Path = "/usr/local/bin/bash"

	delta := RenderTOMLProjectDelta(c)
	for _, want := range []string{"[tools.shell]", `prefer = "bash"`, `path = "/usr/local/bin/bash"`} {
		if !strings.Contains(delta, want) {
			t.Fatalf("project delta missing %q:\n%s", want, delta)
		}
	}
	if strings.Contains(delta, "[tools]\n\n") {
		t.Fatalf("project delta should not emit an empty [tools] block:\n%s", delta)
	}

	got := Default()
	if _, err := toml.Decode(delta, got); err != nil {
		t.Fatalf("decode project delta: %v\n%s", err, delta)
	}
	if got.Tools.Shell.Prefer != "bash" || got.Tools.Shell.Path != "/usr/local/bin/bash" {
		t.Fatalf("tools.shell = %+v, want bash with path", got.Tools.Shell)
	}
}

func TestProjectDeltaRendersUICursorShape(t *testing.T) {
	c := Default()
	c.UI.CursorShape = "block"

	delta := RenderTOMLProjectDelta(c)
	for _, want := range []string{"[ui]", `cursor_shape = "block"`} {
		if !strings.Contains(delta, want) {
			t.Fatalf("project delta missing %q:\n%s", want, delta)
		}
	}

	got := Default()
	if _, err := toml.Decode(delta, got); err != nil {
		t.Fatalf("decode project delta: %v\n%s", err, delta)
	}
	if got.UICursorShape() != "block" {
		t.Fatalf("ui.cursor_shape = %q, want block", got.UICursorShape())
	}
}

func TestProjectRenderPreservesNonDefaultLegacySections(t *testing.T) {
	c := Default()
	c.UI.Theme = "light"
	c.UI.CloseBehavior = "quit"
	c.Network.ProxyMode = "custom"
	c.Network.Proxy.Server = "127.0.0.1"
	c.Network.Proxy.Port = 7890

	project := RenderTOMLForScope(c, RenderScopeProject)
	for _, want := range []string{"[ui]", `theme = "light"`, `close_behavior = "quit"`, "[network]", `proxy_mode = "custom"`, `server = "127.0.0.1"`} {
		if !strings.Contains(project, want) {
			t.Fatalf("project render missing legacy/non-default %q:\n%s", want, project)
		}
	}
}

func TestRenderTOMLRoundTripsPerModelPrices(t *testing.T) {
	orig := Default()
	orig.Providers = []ProviderEntry{{
		Name:      "deepseek",
		Kind:      "openai",
		BaseURL:   "https://api.deepseek.com",
		Models:    []string{"deepseek-v4-flash", "deepseek-v4-pro"},
		Default:   "deepseek-v4-flash",
		APIKeyEnv: "DEEPSEEK_API_KEY",
		Prices:    deepSeekV4Prices(),
	}}

	var got Config
	if _, err := toml.Decode(RenderTOML(orig), &got); err != nil {
		t.Fatalf("rendered TOML does not parse: %v", err)
	}
	p, ok := got.Provider("deepseek")
	if !ok {
		t.Fatal("deepseek provider missing after round trip")
	}
	if p.Prices["deepseek-v4-flash"].Input != 1 || p.Prices["deepseek-v4-pro"].Output != 6 {
		t.Fatalf("prices after round trip = %+v", p.Prices)
	}
}

func TestRenderTOMLRoundTripsVisionModels(t *testing.T) {
	orig := Default()
	orig.Providers = []ProviderEntry{
		{
			Name:         "custom",
			Kind:         "openai",
			BaseURL:      "https://proxy.example.com/v1",
			Models:       []string{"text-only", "qwen-vl-plus"},
			Default:      "text-only",
			APIKeyEnv:    "CUSTOM_API_KEY",
			VisionModels: []string{"qwen-vl-plus"},
			VisionDetail: "low",
		},
		{
			Name:         "disabled-vision",
			Kind:         "openai",
			BaseURL:      "https://proxy.example.com/v1",
			Models:       []string{"qwen-vl-plus"},
			Default:      "qwen-vl-plus",
			APIKeyEnv:    "CUSTOM_API_KEY",
			VisionModels: []string{},
		},
	}

	rendered := RenderTOML(orig)
	if !strings.Contains(rendered, `vision_models = ["qwen-vl-plus"]`) {
		t.Fatalf("rendered TOML missing vision_models:\n%s", rendered)
	}
	if !strings.Contains(rendered, `vision_models = []`) {
		t.Fatalf("rendered TOML missing explicit empty vision_models:\n%s", rendered)
	}
	if !strings.Contains(rendered, `vision_detail = "low"`) {
		t.Fatalf("rendered TOML missing vision_detail:\n%s", rendered)
	}

	var got Config
	if _, err := toml.Decode(rendered, &got); err != nil {
		t.Fatalf("rendered TOML does not parse: %v", err)
	}
	p, ok := got.Provider("custom")
	if !ok {
		t.Fatal("custom provider missing after round trip")
	}
	if !reflect.DeepEqual(p.VisionModels, []string{"qwen-vl-plus"}) {
		t.Fatalf("vision_models after round trip = %v, want [qwen-vl-plus]", p.VisionModels)
	}
	if p.VisionDetail != "low" {
		t.Fatalf("vision_detail after round trip = %q, want low", p.VisionDetail)
	}
	disabled, ok := got.Provider("disabled-vision")
	if !ok {
		t.Fatal("disabled-vision provider missing after round trip")
	}
	if disabled.VisionModels == nil || len(disabled.VisionModels) != 0 {
		t.Fatalf("disabled-vision vision_models after round trip = %#v, want explicit empty list", disabled.VisionModels)
	}
}

func TestRenderTOMLRoundTripsProviderHeadersAndModelOverrides(t *testing.T) {
	orig := Default()
	orig.Providers = []ProviderEntry{{
		Name:      "gateway",
		Kind:      "openai",
		BaseURL:   "https://gateway.example/v1",
		Models:    []string{"deepseek-v4-flash", "plain-chat"},
		Default:   "plain-chat",
		APIKeyEnv: "GATEWAY_API_KEY",
		Headers: map[string]string{
			"HTTP-Referer": "https://app.example",
			"X-Title":      "Reasonix",
		},
		ExtraBody: map[string]any{
			"enable_thinking": true,
			"top_p":           0.8,
			"metadata": map[string]any{
				"mode": "fast",
			},
		},
		AuthHeader: true,
		ModelOverrides: map[string]ProviderModelOverride{
			"deepseek-v4-flash": {
				ReasoningProtocol: ReasoningProtocolDeepSeek,
				SupportedEfforts:  []string{"high", "max"},
				DefaultEffort:     "high",
				Vision:            boolPtr(false),
			},
		},
	}}

	rendered := RenderTOML(orig)
	if !strings.Contains(rendered, `headers     = { HTTP-Referer = "https://app.example", X-Title = "Reasonix" }`) {
		t.Fatalf("rendered TOML missing headers:\n%s", rendered)
	}
	if !strings.Contains(rendered, `extra_body`) || !strings.Contains(rendered, `"enable_thinking" = true`) {
		t.Fatalf("rendered TOML missing extra_body:\n%s", rendered)
	}
	if !strings.Contains(rendered, `auth_header = true`) {
		t.Fatalf("rendered TOML missing auth_header:\n%s", rendered)
	}
	if !strings.Contains(rendered, `model_overrides`) || !strings.Contains(rendered, `reasoning_protocol = "deepseek"`) {
		t.Fatalf("rendered TOML missing model overrides:\n%s", rendered)
	}

	var got Config
	if _, err := toml.Decode(rendered, &got); err != nil {
		t.Fatalf("rendered TOML does not parse: %v\n%s", err, rendered)
	}
	p, ok := got.Provider("gateway")
	if !ok {
		t.Fatal("gateway provider missing after round trip")
	}
	if p.Headers["HTTP-Referer"] != "https://app.example" || p.Headers["X-Title"] != "Reasonix" {
		t.Fatalf("headers after round trip = %+v", p.Headers)
	}
	if p.ExtraBody["enable_thinking"] != true || p.ExtraBody["top_p"] != 0.8 {
		t.Fatalf("extra_body after round trip = %+v", p.ExtraBody)
	}
	if !p.AuthHeader {
		t.Fatal("auth_header after round trip = false, want true")
	}
	metadata, ok := p.ExtraBody["metadata"].(map[string]any)
	if !ok || metadata["mode"] != "fast" {
		t.Fatalf("extra_body metadata after round trip = %+v", p.ExtraBody["metadata"])
	}
	ov := p.ModelOverrides["deepseek-v4-flash"]
	if ov.ReasoningProtocol != ReasoningProtocolDeepSeek || !reflect.DeepEqual(ov.SupportedEfforts, []string{"high", "max"}) || ov.DefaultEffort != "high" || ov.Vision == nil || *ov.Vision {
		t.Fatalf("model override after round trip = %+v", ov)
	}
}

func TestRenderStringMapQuotesNonBareTOMLKeys(t *testing.T) {
	rendered := renderStringMap(map[string]string{
		"github:gh-fix-ci": "deepseek-pro",
		"review":           "deepseek-flash",
	})
	if !strings.Contains(rendered, `"github:gh-fix-ci" = "deepseek-pro"`) {
		t.Fatalf("non-bare key was not quoted: %s", rendered)
	}
	var got struct {
		M map[string]string `toml:"m"`
	}
	if _, err := toml.Decode("m = "+rendered, &got); err != nil {
		t.Fatalf("rendered inline map does not parse: %v (%s)", err, rendered)
	}
	if got.M["github:gh-fix-ci"] != "deepseek-pro" || got.M["review"] != "deepseek-flash" {
		t.Fatalf("decoded map = %+v", got.M)
	}
}

func TestRenderTOMLTablePathQuotesEachSegment(t *testing.T) {
	got := renderTOMLTablePath("lsp", "servers", "c++", "github:gh-fix-ci")
	want := `lsp.servers."c++"."github:gh-fix-ci"`
	if got != want {
		t.Fatalf("renderTOMLTablePath = %q, want %q", got, want)
	}
}

func boolPtr(v bool) *bool { return &v }

func intPtr(v int) *int { return &v }

func TestRenderTOMLPreservesDesktopDisplayMode(t *testing.T) {
	c := Default()
	if err := c.SetDesktopDisplayMode("compact"); err != nil {
		t.Fatalf("SetDesktopDisplayMode: %v", err)
	}
	rendered := RenderTOMLForScope(c, RenderScopeUser)
	if !strings.Contains(rendered, `display_mode = "compact"`) {
		t.Fatalf("rendered user config missing display_mode:\n%s", rendered)
	}
	var got Config
	if _, err := toml.Decode(rendered, &got); err != nil {
		t.Fatalf("rendered TOML does not parse: %v\n---\n%s", err, rendered)
	}
	if got.DesktopDisplayMode() != "compact" {
		t.Fatalf("display_mode after round trip = %q, want compact", got.DesktopDisplayMode())
	}
}

func TestRenderTOMLDefaultStepsCommentedOut(t *testing.T) {
	isolateUserConfigHome(t)
	out := RenderTOML(Default())
	agentLines := extractSectionLines(out, "[agent]")
	for _, line := range agentLines {
		if strings.HasPrefix(line, "max_steps ") || strings.HasPrefix(line, "max_steps=") {
			if !strings.HasPrefix(line, "#") {
				t.Errorf("default max_steps should be commented out in [agent], got: %s", line)
			}
		}
		if strings.HasPrefix(line, "planner_max_steps ") || strings.HasPrefix(line, "planner_max_steps=") {
			if !strings.HasPrefix(line, "#") {
				t.Errorf("default planner_max_steps should be commented out in [agent], got: %s", line)
			}
		}
	}
}

func TestRenderTOMLWindowsSandboxDefaultAndExplicitEnforce(t *testing.T) {
	isolateUserConfigHome(t)
	setRuntimeGOOS(t, "windows")

	defaultRendered := RenderTOMLForScope(Default(), RenderScopeUser)
	if !strings.Contains(defaultRendered, `bash    = "off"`) {
		t.Fatalf("Windows default user config should render bash off:\n%s", defaultRendered)
	}

	cfg := Default()
	cfg.Sandbox.Bash = "enforce"
	delta := RenderTOMLProjectDelta(cfg)
	if !strings.Contains(delta, `bash = "enforce"`) {
		t.Fatalf("Windows explicit enforce should be rendered as a delta:\n%s", delta)
	}
}

func extractSectionLines(toml, section string) []string {
	var lines []string
	inSection := false
	for _, line := range strings.Split(toml, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, section) {
			inSection = true
			continue
		}
		if inSection && strings.HasPrefix(trimmed, "[") && !strings.HasPrefix(trimmed, "[[") {
			break
		}
		if inSection {
			lines = append(lines, trimmed)
		}
	}
	return lines
}

func TestRenderTOMLNonDefaultStepsWrittenExplicitly(t *testing.T) {
	isolateUserConfigHome(t)
	c := Default()
	c.Agent.MaxSteps = 5
	c.Agent.PlannerMaxSteps = 7
	out := RenderTOML(c)
	agentLines := extractSectionLines(out, "[agent]")
	foundMax, foundPlanner := false, false
	for _, line := range agentLines {
		if !strings.HasPrefix(line, "#") && strings.HasPrefix(line, "max_steps ") {
			foundMax = true
		}
		if !strings.HasPrefix(line, "#") && strings.HasPrefix(line, "planner_max_steps ") {
			foundPlanner = true
		}
	}
	if !foundMax {
		t.Error("non-default max_steps should be written explicitly in [agent]")
	}
	if !foundPlanner {
		t.Error("non-default planner_max_steps should be written explicitly in [agent]")
	}
}

func TestRenderTOMLDefaultStepsDoNotOverrideGlobalConfig(t *testing.T) {
	isolateUserConfigHome(t)
	globalDir := filepath.Dir(UserConfigPath())
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	globalPath := filepath.Join(globalDir, "config.toml")
	if err := os.WriteFile(globalPath, []byte("[agent]\nplanner_max_steps = 9\nmax_steps = 100\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := t.TempDir()
	projectTOML := RenderTOML(Default())
	projectPath := filepath.Join(projectDir, "reasonix.toml")
	if err := os.WriteFile(projectPath, []byte(projectTOML), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := Default()
	if err := mergeFile(cfg, globalPath); err != nil {
		t.Fatalf("global merge failed: %v", err)
	}
	if cfg.Agent.PlannerMaxSteps != 9 {
		t.Fatalf("after global: planner_max_steps = %d, want 9", cfg.Agent.PlannerMaxSteps)
	}
	if cfg.Agent.MaxSteps != 100 {
		t.Fatalf("after global: max_steps = %d, want 100", cfg.Agent.MaxSteps)
	}

	if err := mergeFile(cfg, projectPath); err != nil {
		t.Fatalf("project merge failed: %v", err)
	}
	if cfg.Agent.PlannerMaxSteps != 9 {
		t.Errorf("after project: planner_max_steps = %d, want 9 (global should not be overridden by commented-out default)", cfg.Agent.PlannerMaxSteps)
	}
	if cfg.Agent.MaxSteps != 100 {
		t.Errorf("after project: max_steps = %d, want 100 (global should not be overridden by commented-out default)", cfg.Agent.MaxSteps)
	}
}

func TestIsolatedHomeDirEmptyByDefault(t *testing.T) {
	t.Setenv("REASONIX_HOME", "")
	if got := IsolatedHomeDir(); got != "" {
		t.Fatalf("IsolatedHomeDir() = %q, want empty", got)
	}
}

func TestIsolatedHomeDirReturnsCleanPath(t *testing.T) {
	raw := filepath.Join(t.TempDir(), "isolated-reasonix")
	t.Setenv("REASONIX_HOME", raw)
	got := IsolatedHomeDir()
	if filepath.Clean(got) != filepath.Clean(raw) {
		t.Fatalf("IsolatedHomeDir() = %q, want %q", got, raw)
	}
}

func TestLegacyOSSupportDirEmptyWhenIsolated(t *testing.T) {
	isolateUserConfigHome(t)
	t.Setenv("REASONIX_HOME", filepath.Join(t.TempDir(), "isolated-home"))
	if got := legacyOSSupportDir(); got != "" {
		t.Fatalf("legacyOSSupportDir() = %q, want empty when isolated", got)
	}
}

func TestLegacyXDGConfigPathsEmptyWhenIsolated(t *testing.T) {
	isolateUserConfigHome(t)
	t.Setenv("REASONIX_HOME", filepath.Join(t.TempDir(), "isolated-home"))
	if got := legacyXDGConfigPaths(); got != nil {
		t.Fatalf("legacyXDGConfigPaths() = %v, want nil when isolated", got)
	}
}

func TestCacheDirHonorsReasonixHome(t *testing.T) {
	home := t.TempDir()
	isolated := filepath.Join(home, "isolated-home")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("REASONIX_HOME", isolated)

	got := CacheDir()
	want := filepath.Join(isolated, "cache")
	if filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("CacheDir() = %q, want %q", got, want)
	}
}

func TestCacheDirHonorsReasonixCacheHomeOverReasonixHome(t *testing.T) {
	home := t.TempDir()
	cacheHome := filepath.Join(home, "custom-cache")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("REASONIX_HOME", filepath.Join(home, "isolated-home"))
	t.Setenv("REASONIX_CACHE_HOME", cacheHome)

	got := CacheDir()
	want := cacheHome
	if filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("CacheDir() = %q, want %q (REASONIX_CACHE_HOME must win)", got, want)
	}
}

func TestUserConfigLoadPathNoLegacyFallbackWhenIsolated(t *testing.T) {
	home := isolateUserConfigHome(t)
	isolated := filepath.Join(home, "isolated-home")
	t.Setenv("REASONIX_HOME", isolated)

	// Create a legacy config at the OS production path — it must not be loaded.
	productionHome := expectedDefaultReasonixHome(home)
	if err := os.MkdirAll(productionHome, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(productionHome, "config.toml"), []byte("default_model = \"production/model\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The primary config under isolated home does not exist yet.
	got := userConfigLoadPath()
	want := filepath.Join(isolated, "config.toml")
	if filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("userConfigLoadPath() = %q, want %q (must not fall back to production legacy config)", got, want)
	}
}

func TestCredentialSourceCandidatesSkipHomeEnvWhenIsolated(t *testing.T) {
	isolateUserConfigHome(t)
	t.Setenv("REASONIX_HOME", filepath.Join(t.TempDir(), "isolated-home"))

	// Write a key into the production home .env — it must not appear as a source.
	if home, err := os.UserHomeDir(); err == nil {
		if err := os.WriteFile(filepath.Join(home, ".env"), []byte("LEAKED_KEY=leaked-value\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	candidates := credentialSourceCandidates(".")
	for _, c := range candidates {
		if c.Kind == CredentialSourceHomeEnv {
			t.Fatalf("credentialSourceCandidates includes CredentialSourceHomeEnv when isolated: %v", c)
		}
	}
}

func TestMigrateLegacyIfNeededSkipsWhenIsolated(t *testing.T) {
	home := isolateUserConfigHome(t)
	isolated := filepath.Join(home, "isolated-home")
	t.Setenv("REASONIX_HOME", isolated)

	// Create a legacy config.json in production home — migration must skip it.
	legacyDir := filepath.Join(home, ".reasonix")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "config.json"), []byte(`{"model":"production-model","apiKey":"sk-legacy"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := MigrateLegacyIfNeeded()
	if err != nil {
		t.Fatalf("MigrateLegacyIfNeeded() error = %v", err)
	}
	if res != nil {
		t.Fatalf("MigrateLegacyIfNeeded() = %+v, want nil when isolated", res)
	}
}

// TestProjectConfigCannotOverrideSecrets pins [secrets] as a user-global
// security control: a cloned repository's reasonix.toml must not be able to
// switch off tool-output redaction or opt the user into subprocess env
// stripping / sensitive-path hiding.
func TestProjectConfigCannotOverrideSecrets(t *testing.T) {
	isolateUserConfigHome(t)
	t.Setenv("REASONIX_HOME", "")
	globalDir := filepath.Dir(UserConfigPath())
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	globalTOML := "[secrets]\nredact_tool_output = true\nfilter_subprocess_env = false\nprotect_sensitive_files = false\n"
	if err := os.WriteFile(filepath.Join(globalDir, "config.toml"), []byte(globalTOML), 0o644); err != nil {
		t.Fatal(err)
	}

	project := t.TempDir()
	projectTOML := "[secrets]\nredact_tool_output = false\nfilter_subprocess_env = true\nprotect_sensitive_files = true\n"
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte(projectTOML), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRoot(project)
	if err != nil {
		t.Fatalf("LoadForRoot() error = %v", err)
	}
	if !cfg.SecretsRedactToolOutput() {
		t.Error("project reasonix.toml disabled redact_tool_output; [secrets] must stay user-global")
	}
	if cfg.Secrets.FilterSubprocessEnv {
		t.Error("project reasonix.toml enabled filter_subprocess_env; [secrets] must stay user-global")
	}
	if cfg.Secrets.ProtectSensitiveFiles {
		t.Error("project reasonix.toml enabled protect_sensitive_files; [secrets] must stay user-global")
	}
}

// TestRenderTOMLPersistsSecretsSection pins config-save round-tripping: the
// renderer must emit [secrets] for the user scope or every WriteFile would
// silently drop the user's security toggles.
func TestRenderTOMLPersistsSecretsSection(t *testing.T) {
	cfg := Default()
	off := false
	cfg.Secrets.RedactToolOutput = &off
	cfg.Secrets.FilterSubprocessEnv = true
	cfg.Secrets.ProtectSensitiveFiles = true

	out := RenderTOMLForScope(cfg, RenderScopeUser)
	for _, want := range []string{"[secrets]", "redact_tool_output = false", "filter_subprocess_env = true", "protect_sensitive_files = true"} {
		if !strings.Contains(out, want) {
			t.Fatalf("user-scope render missing %q:\n%s", want, out)
		}
	}

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	back := Default()
	if err := mergeFile(back, path); err != nil {
		t.Fatalf("round-trip decode: %v", err)
	}
	if back.SecretsRedactToolOutput() {
		t.Fatal("redact_tool_output=false lost in render round-trip")
	}
	if !back.Secrets.FilterSubprocessEnv || !back.Secrets.ProtectSensitiveFiles {
		t.Fatalf("secrets toggles lost in render round-trip: %+v", back.Secrets)
	}

	// Project scope must not render the section — LoadForRoot ignores it there.
	if proj := RenderTOMLForScope(cfg, RenderScopeProject); strings.Contains(proj, "[secrets]") {
		t.Fatalf("project scope rendered [secrets]:\n%s", proj)
	}
}
