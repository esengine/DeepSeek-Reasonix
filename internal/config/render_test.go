package config

import (
	"strconv"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// TestRenderTOMLRoundTrips ensures the annotated TOML we emit parses back into
// an equivalent config — i.e. the wizard never writes a file it can't read.
func TestRenderTOMLRoundTrips(t *testing.T) {
	orig := Default()
	orig.DefaultModel = "mimo-pro"
	orig.Language = "zh"
	orig.UI.Theme = "light"
	orig.UI.ThemeStyle = "glacier"
	orig.UI.ShortcutLayout = "desktop"
	orig.Desktop.Language = "en"
	orig.Desktop.Theme = "dark"
	orig.Desktop.ThemeStyle = "graphite"
	orig.Desktop.CloseBehavior = "background"
	orig.Desktop.CheckUpdates = boolPtr(false)
	orig.Desktop.Telemetry = boolPtr(false)
	orig.Notifications.Enabled = true
	orig.Notifications.TurnDone = true
	orig.Notifications.ApprovalRequest = true
	orig.Notifications.AskRequest = true
	orig.Agent.AutoPlanClassifier = "deepseek-flash"
	orig.Agent.SubagentModel = "mimo-pro"
	orig.Agent.SubagentModels = map[string]string{"review": "deepseek-pro"}
	orig.Tools.BashTimeoutSeconds = intPtr(900)
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
	orig.Skills.Paths = []string{"~/my-skills", "../shared/skills"}
	orig.Skills.ExcludedPaths = []string{"~/.agents/skills"}
	orig.Skills.DisabledSkills = []string{"review", "explore"}
	orig.Skills.MaxDepth = 2
	orig.Codegraph = CodegraphConfig{Enabled: true, AutoInstall: false, Path: "/opt/codegraph", Tier: "background"}
	orig.Bot.Connections = []BotConnectionConfig{{
		ID:            "feishu-lark",
		Provider:      "feishu",
		Domain:        "lark",
		Label:         "Lark",
		Enabled:       true,
		Status:        "connected",
		Model:         "deepseek-pro",
		WorkspaceRoot: "/tmp/reasonix-bot",
		Credential:    BotConnectionCredential{AppID: "cli_lark", AppSecretEnv: "LARK_BOT_APP_SECRET"},
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
		{Name: "stripe", Type: "http", URL: "https://mcp.stripe.com", Headers: map[string]string{"Authorization": "Bearer x"}, AutoStart: boolPtr(false), Tier: "background"},
	}
	mm, _ := orig.Provider("mimo-pro")
	mm.BaseURL = "http://localhost:8000/v1"
	mm.ReasoningProtocol = "openai"
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
	if got.ConfigVersion != 2 {
		t.Errorf("config_version = %d, want 2", got.ConfigVersion)
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
	if got.Desktop.Language != "en" {
		t.Errorf("desktop.language = %q, want en", got.Desktop.Language)
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
	if got.Agent.SoftCompactRatio != orig.Agent.SoftCompactRatio {
		t.Errorf("soft_compact_ratio = %v, want %v", got.Agent.SoftCompactRatio, orig.Agent.SoftCompactRatio)
	}
	if got.Agent.CompactRatio != orig.Agent.CompactRatio {
		t.Errorf("compact_ratio = %v, want %v", got.Agent.CompactRatio, orig.Agent.CompactRatio)
	}
	if got.Agent.CompactForceRatio != orig.Agent.CompactForceRatio {
		t.Errorf("compact_force_ratio = %v, want %v", got.Agent.CompactForceRatio, orig.Agent.CompactForceRatio)
	}
	if got.Agent.SystemPrompt != orig.Agent.SystemPrompt {
		t.Errorf("system_prompt mismatch:\n got %q\nwant %q", got.Agent.SystemPrompt, orig.Agent.SystemPrompt)
	}
	if !got.Codegraph.Enabled {
		t.Error("codegraph.enabled = false, want true")
	}
	if got.Codegraph.AutoInstall {
		t.Error("codegraph.auto_install = true, want false")
	}
	if got.Codegraph.Path != "/opt/codegraph" {
		t.Errorf("codegraph.path = %q, want /opt/codegraph", got.Codegraph.Path)
	}
	if got.Codegraph.Tier != "" {
		t.Errorf("codegraph.tier = %q, want migrated empty", got.Codegraph.Tier)
	}
	if !got.LSP.Enabled {
		t.Error("lsp.enabled = false, want true")
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
	if got.Tools.BashTimeoutSeconds == nil || *got.Tools.BashTimeoutSeconds != 900 {
		t.Errorf("tools.bash_timeout_seconds = %v, want 900", got.Tools.BashTimeoutSeconds)
	}
	if g, _ := got.Provider("mimo-pro"); g == nil || g.BaseURL != "http://localhost:8000/v1" || g.ReasoningProtocol != "openai" {
		t.Errorf("mimo-pro base_url not preserved: %+v", g)
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

func TestScopedRenderPreservesLSPConfig(t *testing.T) {
	const src = `
config_version = 2
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
	c.Desktop.CheckUpdates = boolPtr(false)

	user := RenderTOMLForScope(c, RenderScopeUser)
	for _, want := range []string{"config_version = 2", "[desktop]", `theme = "dark"`, `close_behavior = "background"`, `check_updates = false`, "[notifications]"} {
		if !strings.Contains(user, want) {
			t.Fatalf("user render missing %q:\n%s", want, user)
		}
	}

	project := RenderTOMLForScope(c, RenderScopeProject)
	for _, forbidden := range []string{"[desktop]", "[notifications]", "close_behavior =", "check_updates ="} {
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

func boolPtr(v bool) *bool { return &v }

func intPtr(v int) *int { return &v }
