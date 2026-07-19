package capdiag

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"reasonix/internal/command"
	"reasonix/internal/config"
	"reasonix/internal/hook"
	"reasonix/internal/memory"
	"reasonix/internal/plugin"
	"reasonix/internal/pluginpkg"
	"reasonix/internal/secrets"
	"reasonix/internal/skill"
)

// Collect builds a capability diagnostics report. It never writes config,
// cache, state, or log files. Live MCP is opt-in via Options.Live.
func Collect(opts Options) Report {
	root := opts.Root
	if root == "" {
		if wd, err := os.Getwd(); err == nil {
			root = wd
		} else {
			root = "."
		}
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}

	home := opts.HomeDir
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = h
		}
	}
	reasonixHome := opts.ReasonixHomeDir
	if reasonixHome == "" {
		if opts.HomeDir != "" {
			reasonixHome = filepath.Join(home, ".reasonix")
		} else {
			reasonixHome = config.ReasonixHomeDir()
		}
	}

	// Read-only load: never rewrite legacy tier lines or other config on disk.
	cfg, cfgErr := config.LoadForRootReadOnly(root)
	if cfg == nil {
		cfg = config.Default()
	}

	issues := []Issue{}
	if cfgErr != nil {
		issues = append(issues, Issue{
			Severity: "error", Code: "config.load_failed", Subsystem: "config",
			Message:     "failed to load configuration: " + sanitizeErrTextWithPaths(cfgErr.Error(), root, home),
			Remediation: "Fix reasonix.toml / config.toml syntax, then re-run doctor capabilities",
		})
	}

	disp := func(p string) string { return displayPath(p, root, home) }

	instr := collectInstructions(root, home, disp)
	skillsR, skillIssues := collectSkills(root, home, reasonixHome, cfg, disp)
	cmdsR, cmdIssues := collectCommands(root, disp)
	hooksR, hookIssues := collectHooks(root, home, reasonixHome, disp)
	pluginsR, pluginIssues := collectPlugins(reasonixHome, disp)
	mcpR, mcpIssues := collectMCP(cfg, root, home, disp)

	issues = append(issues, skillIssues...)
	issues = append(issues, cmdIssues...)
	issues = append(issues, hookIssues...)
	issues = append(issues, pluginIssues...)
	issues = append(issues, mcpIssues...)

	// Runtime host merge (desktop) or live probe (CLI).
	if opts.Live {
		liveIssues := probeLiveMCP(&mcpR, cfg, root, home, opts.LiveTimeout)
		issues = append(issues, liveIssues...)
	} else if opts.RuntimeHost != nil {
		mergeRuntimeHost(&mcpR, opts.RuntimeHost, root, home, &issues)
	}

	sortIssues(issues)
	report := Report{
		SchemaVersion: SchemaVersion,
		Root:          disp(root),
		Live:          opts.Live,
		Instructions:  instr,
		Skills:        skillsR,
		Commands:      cmdsR,
		Hooks:         hooksR,
		Plugins:       pluginsR,
		MCP:           mcpR,
		Issues:        issues,
	}
	report.Summary = buildSummary(report)
	return report
}

// CollectWithRuntimeUnavailable adds mcp.runtime_unavailable when desktop
// requested session runtime but no Host is available.
func CollectWithRuntimeUnavailable(opts Options) Report {
	r := Collect(opts)
	if opts.RuntimeHost == nil && !opts.Live {
		r.Issues = append(r.Issues, Issue{
			Severity: "info", Code: "mcp.runtime_unavailable", Subsystem: "mcp",
			Message:     "no active session Host; showing static configuration only",
			Remediation: "Open or select a workspace chat tab, then refresh with session runtime enabled",
			SettingsTab: "mcp",
		})
		sortIssues(r.Issues)
		r.Summary = buildSummary(r)
	}
	return r
}

func buildSummary(r Report) Summary {
	s := Summary{
		Instructions: len(r.Instructions.Docs),
		Skills:       r.Skills.Winners,
		Commands:     r.Commands.Winners,
		Hooks:        len(r.Hooks.Entries),
		Plugins:      len(r.Plugins.Packages),
		MCPServers:   len(r.MCP.Servers),
	}
	for _, is := range r.Issues {
		switch is.Severity {
		case "error":
			s.Errors++
		case "warning":
			s.Warnings++
		default:
			s.Infos++
		}
	}
	return s
}

func collectInstructions(root, home string, disp func(string) string) InstructionsReport {
	userDir := config.MemoryUserDir()
	if home != "" && (userDir == "" || strings.Contains(userDir, home)) {
		// Prefer explicit test home when Reasonix home is under it.
		if custom := filepath.Join(home, ".reasonix"); custom != "" {
			if userDir == "" {
				userDir = custom
			}
		}
	}
	set := memory.Load(memory.Options{CWD: root, UserDir: userDir})
	out := InstructionsReport{Docs: []InstructionDoc{}}
	if set == nil {
		return out
	}
	for i, d := range set.Docs {
		out.Docs = append(out.Docs, InstructionDoc{
			Path:  disp(d.Path),
			Scope: string(d.Scope),
			Order: i + 1,
		})
	}
	return out
}

func collectSkills(root, home, reasonixHome string, cfg *config.Config, disp func(string) string) (AssetReport, []Issue) {
	var issues []Issue
	store := skill.New(skill.Options{
		HomeDir:         home,
		ReasonixHomeDir: reasonixHome,
		ProjectRoot:     root,
		CustomPaths:     cfg.SkillCustomPaths(),
		ExcludedPaths:   cfg.SkillExcludedPaths(),
		DisabledNames:   cfg.DisabledSkillNames(),
		MaxDepth:        cfg.SkillMaxDepth(),
		Stderr:          ioDiscard(),
	})
	insp := store.Inspect()
	rep := AssetReport{Roots: []RootInfo{}, Entries: []AssetEntry{}}
	for _, r := range insp.Roots {
		rep.Roots = append(rep.Roots, RootInfo{
			Path: disp(r.Dir), Scope: string(r.Scope), Status: string(r.Status),
		})
	}
	for _, c := range insp.Candidates {
		ent := AssetEntry{
			Name: c.Name, Description: c.Description, Scope: string(c.Scope),
			Path: disp(c.Path), Status: string(c.Status), RunAs: string(c.RunAs),
		}
		if c.WinnerPath != "" {
			ent.WinnerPath = disp(c.WinnerPath)
		}
		rep.Entries = append(rep.Entries, ent)
		switch c.Status {
		case skill.CandidateWinner:
			rep.Winners++
			if skill.MissingDescription(c.Description) {
				issues = append(issues, Issue{
					Severity: "warning", Code: "skill.missing_description", Subsystem: "skills",
					Name: c.Name, Source: disp(c.Path),
					Message:     "skill has no description frontmatter; index quality is reduced",
					Remediation: "Add a one-line description: field to the skill frontmatter",
					SettingsTab: "skills",
				})
			}
		case skill.CandidateShadowed:
			rep.Shadowed++
			issues = append(issues, Issue{
				Severity: "info", Code: "skill.shadowed", Subsystem: "skills",
				Name: c.Name, Source: disp(c.Path),
				Message:     "skill is shadowed by a higher-priority winner at " + disp(c.WinnerPath),
				Remediation: "Rename, remove, or disable the lower-priority skill if the winner is unintended",
				SettingsTab: "skills",
			})
		case skill.CandidateDisabled:
			rep.Disabled++
			issues = append(issues, Issue{
				Severity: "info", Code: "skill.disabled", Subsystem: "skills",
				Name: c.Name, Source: disp(c.Path),
				Message:     "skill is listed in disabled_skills and will not load",
				Remediation: "Remove the name from [skills].disabled_skills to re-enable",
				SettingsTab: "skills",
			})
		}
	}
	return rep, issues
}

func collectCommands(root string, disp func(string) string) (AssetReport, []Issue) {
	var issues []Issue
	dirs := config.CommandDirsForRoot(root)
	insp := command.Inspect(dirs...)
	rep := AssetReport{Roots: []RootInfo{}, Entries: []AssetEntry{}}
	for _, r := range insp.Roots {
		rep.Roots = append(rep.Roots, RootInfo{Path: disp(r.Dir), Status: r.Status})
	}
	for _, c := range insp.Candidates {
		ent := AssetEntry{
			Name: c.Name, Description: c.Description, Path: disp(c.Path), Status: string(c.Status),
		}
		if c.WinnerPath != "" {
			ent.WinnerPath = disp(c.WinnerPath)
		}
		if c.Error != "" {
			ent.Error = sanitizeErrText(c.Error)
		}
		rep.Entries = append(rep.Entries, ent)
		switch c.Status {
		case command.CandidateWinner:
			rep.Winners++
		case command.CandidateShadowed:
			rep.Shadowed++
			issues = append(issues, Issue{
				Severity: "info", Code: "command.shadowed", Subsystem: "commands",
				Name: c.Name, Source: disp(c.Path),
				Message:     "command is overridden by later directory winner at " + disp(c.WinnerPath),
				Remediation: "Remove or rename the earlier command file if the override is unintended",
			})
		case command.CandidateError:
			rep.ParseErrors++
			issues = append(issues, Issue{
				Severity: "error", Code: "command.read_failed", Subsystem: "commands",
				Name: c.Name, Source: disp(c.Path),
				Message:     "failed to read or parse command file",
				Remediation: "Fix file permissions/encoding or remove the broken file",
			})
		}
	}
	return rep, issues
}

func collectHooks(root, home, reasonixHome string, disp func(string) string) (HookReport, []Issue) {
	var issues []Issue
	// Prefer explicit home for trust/settings when tests isolate HOME.
	homeDir := home
	if reasonixHome != "" && home == "" {
		homeDir = filepath.Dir(reasonixHome)
	}
	trusted := hook.IsTrusted(root, homeDir)
	insp := hook.Inspect(hook.LoadOptions{
		ProjectRoot: root,
		HomeDir:     homeDir,
		Trusted:     trusted,
	})
	rep := HookReport{
		TrustedProject: insp.TrustedProject || trusted,
		ProjectDefines: insp.ProjectDefines,
		Sources:        []HookSource{},
		Entries:        []HookEntry{},
	}
	for _, s := range insp.Sources {
		rep.Sources = append(rep.Sources, HookSource{
			Scope: string(s.Scope), Path: disp(s.Path), Status: s.Status,
			HookCount: s.HookCount, ParseError: sanitizeErrText(s.ParseError),
		})
		if s.Status == "malformed" {
			issues = append(issues, Issue{
				Severity: "error", Code: "hook.malformed_settings", Subsystem: "hooks",
				Source:      disp(s.Path),
				Message:     "hooks settings JSON is malformed",
				Remediation: "Fix JSON syntax in the settings file",
				SettingsTab: "hooks",
			})
		}
		if s.Status == "untrusted_skipped" && insp.ProjectDefines {
			issues = append(issues, Issue{
				Severity: "warning", Code: "hook.untrusted_project", Subsystem: "hooks",
				Source:      disp(s.Path),
				Message:     "project defines hooks but the workspace is not trusted",
				Remediation: "Trust this project in Settings → Hooks (or CLI trust flow) before project hooks run",
				SettingsTab: "hooks",
			})
		}
	}
	for _, e := range insp.Entries {
		rep.Entries = append(rep.Entries, HookEntry{
			Event: string(e.Event), Match: e.Match, Command: redactCommandDisplay(e.Command, root, home),
			ContextFile: disp(e.ContextFile), Description: e.Description, TimeoutMS: e.Timeout,
			Scope: string(e.Scope), Source: disp(e.Source), Blocking: hook.IsBlocking(e.Event),
		})
		if strings.TrimSpace(e.Command) == "" && strings.TrimSpace(e.ContextFile) == "" {
			issues = append(issues, Issue{
				Severity: "error", Code: "hook.missing_command", Subsystem: "hooks",
				Name: string(e.Event), Source: disp(e.Source),
				Message:     "hook entry has neither command nor contextFile",
				Remediation: "Set command or contextFile for the hook entry",
				SettingsTab: "hooks",
			})
		}
		if e.ContextFile != "" {
			if _, err := os.Stat(e.ContextFile); err != nil {
				issues = append(issues, Issue{
					Severity: "error", Code: "hook.missing_context_file", Subsystem: "hooks",
					Name: string(e.Event), Source: disp(e.ContextFile),
					Message:     "hook contextFile does not exist",
					Remediation: "Create the context file or fix the path in the hook entry",
					SettingsTab: "hooks",
				})
			}
		}
		if msg := hook.ValidateMatcher(e.Match); msg != "" {
			issues = append(issues, Issue{
				Severity: "error", Code: "hook.invalid_matcher", Subsystem: "hooks",
				Name: string(e.Event), Source: disp(e.Source),
				Message:     msg,
				Remediation: "Use an anchored regex (or empty/*); remember matchers are fully anchored",
				SettingsTab: "hooks",
			})
		}
		if !hook.IsKnownEvent(string(e.Event)) {
			issues = append(issues, Issue{
				Severity: "warning", Code: "hook.unknown_event", Subsystem: "hooks",
				Name: string(e.Event), Source: disp(e.Source),
				Message:     "hook event is not one of the 11 supported events",
				Remediation: "Use a supported event name from the hooks documentation",
				SettingsTab: "hooks",
			})
		}
	}
	return rep, issues
}

func collectPlugins(reasonixHome string, disp func(string) string) (PluginPackageReport, []Issue) {
	var issues []Issue
	rep := PluginPackageReport{
		StatePath: disp(filepath.Join(reasonixHome, pluginpkg.StateFilename)),
		Packages:  []PluginPackageInfo{},
	}
	st, err := pluginpkg.LoadState(reasonixHome)
	if err != nil {
		issues = append(issues, Issue{
			Severity: "error", Code: "plugin.state_read_failed", Subsystem: "plugins",
			Source:      rep.StatePath,
			Message:     "failed to read plugin-packages state",
			Remediation: "Ensure Reasonix home is readable or reinstall packages",
			SettingsTab: "plugins",
		})
		return rep, issues
	}
	// Stable order by name.
	sort.SliceStable(st.Plugins, func(i, j int) bool {
		return st.Plugins[i].Name < st.Plugins[j].Name
	})
	for _, p := range st.Plugins {
		info := PluginPackageInfo{
			Name: p.Name, Enabled: p.Enabled, Version: p.Version,
			Root:         disp(pluginpkg.ResolveRoot(reasonixHome, p.Root)),
			ManifestKind: p.ManifestKind, Status: "ok",
		}
		root := pluginpkg.ResolveRoot(reasonixHome, p.Root)
		if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
			info.Status = "missing_root"
			issues = append(issues, Issue{
				Severity: "error", Code: "plugin.missing_root", Subsystem: "plugins",
				Name: p.Name, Source: disp(root),
				Message:     "plugin package root directory is missing",
				Remediation: "Reinstall the plugin package or remove it from plugin-packages.json",
				SettingsTab: "plugins",
			})
			rep.Packages = append(rep.Packages, info)
			continue
		}
		pkg, warnings, perr := pluginpkg.ParseDir(root)
		if perr != nil {
			info.Status = "invalid_manifest"
			issues = append(issues, Issue{
				Severity: "error", Code: "plugin.invalid_manifest", Subsystem: "plugins",
				Name: p.Name, Source: disp(root),
				Message:     "plugin package manifest is invalid: " + sanitizeErr(perr),
				Remediation: "Fix reasonix-plugin.json / Codex / Claude plugin manifest",
				SettingsTab: "plugins",
			})
			rep.Packages = append(rep.Packages, info)
			continue
		}
		sk, commands, hk, mcp := pkg.CapabilityCounts()
		info.Skills, info.Commands, info.Hooks, info.MCPServers = sk, commands, hk, mcp
		if p.ManifestKind == "" {
			info.ManifestKind = pkg.ManifestKind
		}
		for _, w := range warnings {
			info.Warnings = append(info.Warnings, w)
			issues = append(issues, Issue{
				Severity: "warning", Code: "plugin.compatibility", Subsystem: "plugins",
				Name: p.Name, Source: disp(root),
				Message:     w,
				Remediation: "Review plugin package documentation for unsupported Claude/Codex features",
				SettingsTab: "plugins",
			})
		}
		if !p.Enabled {
			info.Status = "disabled"
		}
		rep.Packages = append(rep.Packages, info)
	}
	return rep, issues
}

func collectMCP(cfg *config.Config, root, home string, disp func(string) string) (MCPReport, []Issue) {
	var issues []Issue
	rep := MCPReport{Servers: []MCPServerInfo{}}
	if cfg == nil {
		return rep, issues
	}
	// Stable order by name.
	entries := append([]config.PluginEntry(nil), cfg.Plugins...)
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
	for _, p := range entries {
		info := MCPServerInfo{
			Name:        p.Name,
			Transport:   transportOf(p.Type),
			StartIntent: "automatic",
			EnvKeys:     sortedKeys(p.Env),
			HeaderKeys:  sortedKeys(p.Headers),
		}
		if !p.ShouldAutoStart() {
			info.StartIntent = "off"
		}
		if owner, ok := cfg.PluginPackageOwner(p.Name); ok {
			info.PackageOwner = owner
			info.Source = "plugin_package"
		} else {
			info.Source = guessMCPSource(root, p.Name)
		}
		if info.Transport == "stdio" {
			info.Command = redactCommandDisplay(p.Command, root, home)
		} else {
			info.URLHost = urlHostOnly(p.URL)
		}
		if !isValidTransport(p.Type) && strings.TrimSpace(p.Type) != "" {
			issues = append(issues, Issue{
				Severity: "error", Code: "mcp.invalid_transport", Subsystem: "mcp",
				Name: p.Name, Source: info.Source,
				Message:     "unsupported MCP transport " + p.Type,
				Remediation: "Use type stdio, http, or sse",
				SettingsTab: "mcp",
			})
		}
		if transportOf(p.Type) == "stdio" {
			if strings.TrimSpace(p.Command) == "" {
				issues = append(issues, Issue{
					Severity: "error", Code: "mcp.missing_command", Subsystem: "mcp",
					Name: p.Name, Source: info.Source,
					Message:     "stdio MCP server has empty command",
					Remediation: "Set command (and optional args) for the server",
					SettingsTab: "mcp",
				})
			} else if !commandExists(p.Command) {
				// Static LookPath cannot mirror GUI/login-shell PATH enrichment used
				// at runtime; treat as warning so diagnostics do not hard-fail a
				// command that may still start under the real session environment.
				issues = append(issues, Issue{
					Severity: "warning", Code: "mcp.command_not_found", Subsystem: "mcp",
					Name: p.Name, Source: info.Source,
					Message:     "MCP command is not found via static PATH lookup (GUI/login-shell PATH may still resolve it at runtime)",
					Remediation: "Use an absolute command path, set PATH in the server env, or verify with session runtime / --live",
					SettingsTab: "mcp",
				})
			}
		} else if strings.TrimSpace(p.URL) == "" {
			issues = append(issues, Issue{
				Severity: "error", Code: "mcp.missing_url", Subsystem: "mcp",
				Name: p.Name, Source: info.Source,
				Message:     "remote MCP server has empty url",
				Remediation: "Set a valid http(s) URL for the server",
				SettingsTab: "mcp",
			})
		}
		rep.Servers = append(rep.Servers, info)
	}
	return rep, issues
}

func guessMCPSource(root, name string) string {
	// Best-effort: check project .mcp.json presence of the name without loading secrets.
	mcpPath := filepath.Join(root, ".mcp.json")
	if b, err := os.ReadFile(mcpPath); err == nil {
		// Cheap substring check; false positives are acceptable for diagnostics.
		if strings.Contains(string(b), `"`+name+`"`) {
			return "mcp_json"
		}
	}
	return "toml"
}

func commandExists(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return false
	}
	if filepath.IsAbs(cmd) {
		st, err := os.Stat(cmd)
		return err == nil && !st.IsDir()
	}
	// Relative path or PATH lookup — use LookPath.
	if _, err := lookPath(cmd); err == nil {
		return true
	}
	// Relative workspace path.
	if st, err := os.Stat(cmd); err == nil && !st.IsDir() {
		return true
	}
	return false
}

func mergeRuntimeHost(rep *MCPReport, host *plugin.Host, root, home string, issues *[]Issue) {
	if host == nil {
		return
	}
	byName := map[string]int{}
	for i, s := range rep.Servers {
		byName[s.Name] = i
	}
	for _, s := range host.Servers() {
		tools := make([]MCPToolInfo, 0, len(s.ToolList))
		for _, t := range s.ToolList {
			tools = append(tools, MCPToolInfo{Name: t.Name, ReadOnlyHint: t.ReadOnlyHint, DestructiveHint: t.DestructiveHint})
		}
		if i, ok := byName[s.Name]; ok {
			rep.Servers[i].RuntimeStatus = "connected"
			rep.Servers[i].ToolCount = s.Tools
			rep.Servers[i].Tools = tools
			// Only warn when the server advertised a tools capability but listed none.
			if s.HasTools && s.Tools == 0 {
				*issues = append(*issues, Issue{
					Severity: "warning", Code: "mcp.no_tools", Subsystem: "mcp",
					Name: s.Name, Message: "MCP server connected but exposes no tools",
					Remediation: "Check server configuration and authentication",
					SettingsTab: "mcp",
				})
			}
		} else {
			rep.Servers = append(rep.Servers, MCPServerInfo{
				Name: s.Name, Transport: s.Transport, RuntimeStatus: "connected",
				ToolCount: s.Tools, Tools: tools, StartIntent: "automatic",
			})
		}
	}
	for _, f := range host.Failures() {
		errText := sanitizeErrTextWithPaths(f.Error, root, home)
		if i, ok := byName[f.Name]; ok {
			rep.Servers[i].RuntimeStatus = "failed"
			rep.Servers[i].Error = errText
		}
		*issues = append(*issues, Issue{
			Severity: "error", Code: "mcp.start_failed", Subsystem: "mcp",
			Name: f.Name, Message: "MCP server failed in the current session: " + errText,
			Remediation: "Inspect server logs, command/URL, and authentication; retry from Settings → MCP",
			SettingsTab: "mcp",
		})
	}
	for _, name := range host.ConnectingServers() {
		if i, ok := byName[name]; ok && rep.Servers[i].RuntimeStatus == "" {
			rep.Servers[i].RuntimeStatus = "deferred"
		}
	}
	// Mark auto_start=off with empty runtime as disabled.
	for i := range rep.Servers {
		if rep.Servers[i].RuntimeStatus == "" && rep.Servers[i].StartIntent == "off" {
			rep.Servers[i].RuntimeStatus = "disabled"
		}
	}
}

func sortIssues(issues []Issue) {
	sev := map[string]int{"error": 0, "warning": 1, "info": 2}
	sort.SliceStable(issues, func(i, j int) bool {
		if sev[issues[i].Severity] != sev[issues[j].Severity] {
			return sev[issues[i].Severity] < sev[issues[j].Severity]
		}
		if issues[i].Code != issues[j].Code {
			return issues[i].Code < issues[j].Code
		}
		if issues[i].Name != issues[j].Name {
			return issues[i].Name < issues[j].Name
		}
		return issues[i].Source < issues[j].Source
	})
}

func sanitizeErr(err error) string {
	if err == nil {
		return ""
	}
	return sanitizeErrText(err.Error())
}

// sanitizeErrText redacts secrets and machine-local identity from diagnostic
// strings. Prefer sanitizeErrTextWithPaths when workspace/home are known.
func sanitizeErrText(s string) string {
	return sanitizeErrTextWithPaths(s, "", "")
}

func sanitizeErrTextWithPaths(s, workspace, home string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	// Collapse whitespace so multi-line stderr is one line.
	s = strings.Join(strings.Fields(s), " ")

	// Strip URL query/fragment early.
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		// Only cut when it looks like a URL fragment, not ordinary prose "?".
		prefix := s[:i]
		if strings.Contains(prefix, "://") || strings.Contains(strings.ToLower(prefix), "http") {
			s = prefix
		}
	}

	// PATH=... (often embedded in stdio resolve errors; not a credential, so
	// the shared redactor below leaves it alone).
	s = redactKeyValue(s, "PATH=")
	s = redactKeyValue(s, "path=")

	// Transport errors embed arbitrary HTTP bodies and stdio stderr, so run
	// the product-wide credential recognizer (KEY=value and JSON
	// "key":"value" forms, Authorization schemes, Cookie/Set-Cookie values,
	// Bearer/JWT/vendor token shapes) instead of a second, narrower list.
	s = secrets.Redact(s)

	// The shared Bearer pattern only masks tokens of 16+ chars; diagnostics
	// text can afford to redact shorter bearer tokens too.
	s = redactBearer(s)

	// Absolute paths: rewrite with displayPath when possible.
	s = redactAbsolutePaths(s, workspace, home)

	// Cap length after redaction.
	const max = 400
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

func redactKeyValue(s, key string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, key)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		b.WriteString(key)
		b.WriteString("<redacted>")
		rest := s[i+len(key):]
		end := len(rest)
		if j := strings.IndexAny(rest, " \t\n\r;,"); j >= 0 {
			end = j
		}
		s = rest[end:]
	}
}

func redactBearer(s string) string {
	var b strings.Builder
	lower := strings.ToLower(s)
	const needle = "bearer "
	for {
		i := strings.Index(lower, needle)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		b.WriteString("Bearer <redacted>")
		rest := s[i+len(needle):]
		end := len(rest)
		if j := strings.IndexAny(rest, " \t\n\r;,\"'"); j >= 0 {
			end = j
		}
		s = rest[end:]
		lower = strings.ToLower(s)
	}
}

func redactAbsolutePaths(s, workspace, home string) string {
	// Walk for POSIX and Windows absolute path-like tokens.
	var b strings.Builder
	i := 0
	for i < len(s) {
		// Find candidate start: / or X:\
		start := -1
		if s[i] == '/' {
			start = i
		} else if i+2 < len(s) && ((s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z')) && s[i+1] == ':' && (s[i+2] == '\\' || s[i+2] == '/') {
			start = i
		}
		if start < 0 {
			b.WriteByte(s[i])
			i++
			continue
		}
		j := start + 1
		for j < len(s) {
			c := s[j]
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '"' || c == '\'' || c == ',' || c == ';' || c == ')' || c == ']' {
				break
			}
			j++
		}
		token := s[start:j]
		// Only rewrite if it looks like a path with a directory separator beyond root.
		if strings.ContainsAny(token, `/\`) && len(token) > 1 {
			b.WriteString(displayPath(token, workspace, home))
		} else {
			b.WriteString(token)
		}
		i = j
	}
	return b.String()
}

func redactCommandDisplay(cmd, root, home string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}
	// Only show the command token, redacted if it looks like a path.
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return ""
	}
	return displayPath(fields[0], root, home)
}

// ioDiscard avoids importing io in every call site for skill.Options.Stderr.
func ioDiscard() *discardWriter { return &discardWriter{} }

type discardWriter struct{}

func (d *discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// DefaultLiveTimeout is used when --live is set without --timeout.
const DefaultLiveTimeout = 5 * time.Second

// MinLiveTimeout / MaxLiveTimeout bound --timeout.
const (
	MinLiveTimeout = 1 * time.Second
	MaxLiveTimeout = 60 * time.Second
)
