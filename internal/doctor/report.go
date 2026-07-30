// Package doctor collects local, redacted diagnostics for issue reports.
package doctor

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	fileencoding "reasonix/internal/fileutil/encoding"
	"reasonix/internal/netclient"
	"reasonix/internal/sandbox"
	"reasonix/internal/skill"
	"reasonix/internal/store"
)

type Options struct {
	Version string
	Config  *config.Config
}

type Report struct {
	Version    string           `json:"version"`
	OS         string           `json:"os"`
	Arch       string           `json:"arch"`
	CWD        string           `json:"cwd,omitempty"`
	Config     ConfigReport     `json:"config"`
	Providers  []ProviderReport `json:"providers"`
	Plugins    []PluginReport   `json:"plugins,omitempty"`
	LSP        LSPReport        `json:"lsp"`
	Sessions   SessionsReport   `json:"sessions"`
	Sandbox    SandboxReport    `json:"sandbox"`
	Network    NetworkReport    `json:"network"`
	Permission PermissionReport `json:"permission"`
	Warnings   []string         `json:"warnings,omitempty"`
}

type ConfigReport struct {
	SourcePath   string `json:"source_path,omitempty"`
	UserPath     string `json:"user_path,omitempty"`
	DefaultModel string `json:"default_model"`
}

type ProviderReport struct {
	Name          string   `json:"name"`
	Kind          string   `json:"kind"`
	BaseURLHost   string   `json:"base_url_host,omitempty"`
	Model         string   `json:"model,omitempty"`
	Models        []string `json:"models,omitempty"`
	APIKeyEnv     string   `json:"api_key_env,omitempty"`
	KeyPresent    bool     `json:"key_present"`
	IsDefault     bool     `json:"is_default"`
	ContextWindow int      `json:"context_window,omitempty"`
}

type PluginReport struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
	AutoStart bool   `json:"auto_start"`
	Target    string `json:"target,omitempty"`
}

type LSPReport struct {
	Enabled bool `json:"enabled"`
	Servers int  `json:"servers"`
}

type SessionsReport struct {
	Dir   string `json:"dir,omitempty"`
	Count int    `json:"count"`
	Bytes int64  `json:"bytes"`
	Error string `json:"error,omitempty"`
}

type SandboxReport struct {
	Bash       string   `json:"bash"`
	Network    bool     `json:"network"`
	WriteRoots []string `json:"write_roots,omitempty"`
	// Available is whether an OS sandbox actually backs an "enforce" request on
	// this host (Seatbelt or bubblewrap). Without it
	// "enforce" refuses bash execution instead of running unconfined.
	Available bool `json:"available"`
	// Shell is the interpreter the bash tool resolved (kind and path).
	Shell string `json:"shell,omitempty"`
	// BashConfigIgnored is set when the config file requests bash = "enforce"
	// but the platform force-resolves it to "off" (Windows, where the native
	// backend is unsupported) — the one case where Bash silently disagrees with
	// what the user wrote.
	BashConfigIgnored bool `json:"bash_config_ignored,omitempty"`
}

type NetworkReport struct {
	ProxyMode string `json:"proxy_mode"`
	Proxy     string `json:"proxy"`
	NoProxy   bool   `json:"no_proxy"`
}

type PermissionReport struct {
	Mode       string `json:"mode"`
	AllowRules int    `json:"allow_rules"`
	AskRules   int    `json:"ask_rules"`
	DenyRules  int    `json:"deny_rules"`
}

func Collect(opts Options) Report {
	cfg := opts.Config
	var warnings []string
	if cfg == nil {
		var err error
		cfg, err = config.Load()
		if err != nil {
			warnings = append(warnings, err.Error())
			cfg = config.Default()
		}
	}
	cwd, _ := os.Getwd()
	sourcePath := config.SourcePath()
	// Settings UIs and `reasonix config` edit the user-level config, but a
	// project reasonix.toml outranks it. Users who toggle the sandbox off in
	// Settings while the project file pins [sandbox] read the no-op as "bash is
	// broken" (#5961, #6046) — surface the layering explicitly.
	if sourcePath != "" && filepath.Base(sourcePath) == "reasonix.toml" {
		if raw, err := fileencoding.ReadFileUTF8(sourcePath); err == nil && tomlHasSandboxTable(raw) {
			warnings = append(warnings, "project "+redactHome(sourcePath)+" sets [sandbox]; it overrides user-level Settings -> Sandbox for this workspace — edit the project file to change sandbox behavior here")
		}
	}
	userPath := config.UserConfigPath()
	if legacyPath := config.LegacyUserConfigPath(); userPath != "" && legacyPath != "" {
		if _, userErr := os.Stat(userPath); userErr == nil {
			if _, legacyErr := os.Stat(legacyPath); legacyErr == nil {
				warnings = append(warnings, "legacy user config exists at "+redactHome(legacyPath)+
					" but is ignored because "+redactHome(userPath)+" exists")
			}
		}
	}
	// A config that says enforce while the platform force-resolves it to off is
	// the one case where bash behavior silently disagrees with the file the user
	// edited (Windows has no OS-level Bash backend) — say it
	// out loud instead of leaving it to be discovered from unconfined commands.
	bashConfigIgnored := strings.TrimSpace(cfg.Sandbox.Bash) == "enforce" && cfg.BashMode() == "off"
	if bashConfigIgnored {
		warnings = append(warnings, `config requests [sandbox] bash = "enforce", but Windows does not provide an OS-level Bash sandbox; the setting is fixed to "off" and bash runs unconfined`)
	}
	report := Report{
		Version: opts.Version,
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
		CWD:     redactHome(cwd),
		Config: ConfigReport{
			SourcePath:   redactHome(sourcePath),
			UserPath:     redactHome(userPath),
			DefaultModel: cfg.DefaultModel,
		},
		LSP: LSPReport{
			Enabled: cfg.LSP.Enabled,
			Servers: len(cfg.LSP.Servers),
		},
		Sessions: collectSessions(config.SessionDir()),
		Sandbox: SandboxReport{
			Bash:              cfg.BashMode(),
			Network:           cfg.Sandbox.Network,
			WriteRoots:        redactHomeAll(cfg.WriteRoots()),
			Available:         sandbox.Available(),
			Shell:             resolvedShellSummary(cfg),
			BashConfigIgnored: bashConfigIgnored,
		},
		Network: NetworkReport{
			ProxyMode: cfg.NetworkProxyMode(),
			Proxy:     netclient.Summary(cfg.NetworkProxySpec()),
			NoProxy:   strings.TrimSpace(cfg.Network.NoProxy) != "",
		},
		Permission: PermissionReport{
			Mode:       cfg.Permissions.Mode,
			AllowRules: len(cfg.Permissions.Allow),
			AskRules:   len(cfg.Permissions.Ask),
			DenyRules:  len(cfg.Permissions.Deny),
		},
		Warnings: warnings,
	}
	// Skill / MCP capability health (optional diagnostics; never fail doctor).
	if skStore := skill.New(skill.Options{ProjectRoot: cwd}); skStore != nil {
		report.Warnings = append(report.Warnings, CollectSkillHealthWarnings(SkillHealthOptions{
			Skills:  skStore.List(),
			Plugins: cfg.Plugins,
		})...)
	}
	report.Sessions.Dir = redactHome(report.Sessions.Dir)
	for i := range cfg.Providers {
		p := cfg.Providers[i]
		models := p.ModelList()
		report.Providers = append(report.Providers, ProviderReport{
			Name:          p.Name,
			Kind:          p.Kind,
			BaseURLHost:   hostOnly(p.BaseURL),
			Model:         p.Model,
			Models:        models,
			APIKeyEnv:     p.APIKeyEnv,
			KeyPresent:    p.Configured(),
			IsDefault:     p.Name == cfg.DefaultModel,
			ContextWindow: p.ContextWindow,
		})
	}
	for _, p := range cfg.Plugins {
		transport := p.Type
		if transport == "" {
			transport = "stdio"
		}
		report.Plugins = append(report.Plugins, PluginReport{
			Name:      p.Name,
			Transport: transport,
			AutoStart: p.ShouldAutoStart(),
			Target:    pluginTarget(p),
		})
	}
	return report
}

func RenderText(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "reasonix %s doctor\n", r.Version)
	fmt.Fprintf(&b, "  system       %s/%s\n", r.OS, r.Arch)
	if r.CWD != "" {
		fmt.Fprintf(&b, "  cwd          %s\n", r.CWD)
	}
	fmt.Fprintf(&b, "  config       %s\n", valueOr(r.Config.SourcePath, "not found - using defaults"))
	fmt.Fprintf(&b, "  user config  %s\n", valueOr(r.Config.UserPath, "unavailable"))
	fmt.Fprintf(&b, "  model        %s\n", valueOr(r.Config.DefaultModel, "(none)"))

	// Warnings (e.g. a config that failed to parse and fell back to defaults) go
	// up top, not buried under the full report where they read as "all fine".
	for _, w := range r.Warnings {
		fmt.Fprintf(&b, "  warning: %s\n", w)
	}

	fmt.Fprintf(&b, "\nproviders\n")
	for _, p := range r.Providers {
		key := "missing"
		if p.KeyPresent {
			key = "present"
		}
		marker := ""
		if p.IsDefault {
			marker = " default"
		}
		fmt.Fprintf(&b, "  %-16s %-8s %-24s key:%s%s\n", p.Name, p.Kind, valueOr(p.BaseURLHost, "(no host)"), key, marker)
	}

	fmt.Fprintf(&b, "\nplugins\n")
	if len(r.Plugins) == 0 {
		fmt.Fprintf(&b, "  none configured\n")
	} else {
		for _, p := range r.Plugins {
			fmt.Fprintf(&b, "  %-16s %-8s %s\n", p.Name, p.Transport, valueOr(p.Target, "(redacted)"))
		}
	}

	fmt.Fprintf(&b, "\nlsp\n")
	fmt.Fprintf(&b, "  enabled      %v\n", r.LSP.Enabled)
	fmt.Fprintf(&b, "  servers      %d configured overrides\n", r.LSP.Servers)

	fmt.Fprintf(&b, "\nsessions\n")
	fmt.Fprintf(&b, "  dir          %s\n", valueOr(r.Sessions.Dir, "unavailable"))
	fmt.Fprintf(&b, "  saved        %d\n", r.Sessions.Count)
	fmt.Fprintf(&b, "  bytes        %d\n", r.Sessions.Bytes)
	if r.Sessions.Error != "" {
		fmt.Fprintf(&b, "  warning      %s\n", r.Sessions.Error)
	}

	fmt.Fprintf(&b, "\nsandbox\n")
	bashLine := r.Sandbox.Bash
	if r.Sandbox.Bash == "enforce" && !r.Sandbox.Available {
		bashLine += " (unavailable: no OS sandbox on this host; bash execution is refused. " + sandbox.UnavailableRemediation() + ")"
	}
	if r.Sandbox.BashConfigIgnored {
		bashLine += ` (config requests "enforce", ignored: Windows has no OS-level Bash sandbox and fixes this setting to "off")`
	}
	fmt.Fprintf(&b, "  bash         %s\n", bashLine)
	if r.Sandbox.Shell != "" {
		fmt.Fprintf(&b, "  shell        %s\n", r.Sandbox.Shell)
	}
	fmt.Fprintf(&b, "  network      %v\n", r.Sandbox.Network)
	fmt.Fprintf(&b, "  write_roots  %s\n", strings.Join(r.Sandbox.WriteRoots, ", "))

	fmt.Fprintf(&b, "\nnetwork\n")
	fmt.Fprintf(&b, "  proxy_mode   %s\n", r.Network.ProxyMode)
	fmt.Fprintf(&b, "  proxy        %s\n", r.Network.Proxy)
	fmt.Fprintf(&b, "  no_proxy     %v\n", r.Network.NoProxy)

	fmt.Fprintf(&b, "\npermissions\n")
	fmt.Fprintf(&b, "  mode         %s\n", valueOr(r.Permission.Mode, "ask"))
	fmt.Fprintf(&b, "  rules        allow:%d ask:%d deny:%d\n", r.Permission.AllowRules, r.Permission.AskRules, r.Permission.DenyRules)
	return b.String()
}

func collectSessions(dir string) SessionsReport {
	r := SessionsReport{Dir: dir}
	if dir == "" {
		return r
	}
	sessions, err := agent.ListSessions(dir)
	if err != nil {
		r.Error = err.Error()
	}
	r.Count = len(sessions)
	if err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		// Transcript storage spans the .jsonl checkpoint plus the event
		// log/index; counting only checkpoints would under-report usage.
		name := filepath.Base(path)
		if !store.IsSessionTranscriptName(name) &&
			!strings.HasSuffix(name, ".events.jsonl") &&
			!strings.HasSuffix(name, ".event-index.json") {
			return nil
		}
		if info, statErr := d.Info(); statErr == nil {
			r.Bytes += info.Size()
		}
		return nil
	}); err != nil && !os.IsNotExist(err) {
		r.Error = err.Error()
	}
	return r
}

func pluginTarget(p config.PluginEntry) string {
	if p.URL != "" {
		return hostOnly(p.URL)
	}
	if p.Command == "" {
		return ""
	}
	return filepath.Base(p.Command)
}

func hostOnly(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	if port := u.Port(); port != "" {
		return u.Hostname() + ":" + port
	}
	return u.Hostname()
}

func valueOr(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// redactHome rewrites a path under the user's home directory to start with "~",
// so a shared diagnostics report doesn't carry the account name. Paths outside
// home are returned unchanged.
func redactHome(p string) string {
	if p == "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if sep := string(os.PathSeparator); strings.HasPrefix(p, home+sep) {
		return "~" + sep + p[len(home)+1:]
	}
	return p
}

func redactHomeAll(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = redactHome(p)
	}
	return out
}

// resolvedShellSummary reports which interpreter the bash tool would run
// commands under, e.g. "bash (~/bin/bash)" or "powershell (C:\...\pwsh.exe)".
func resolvedShellSummary(cfg *config.Config) string {
	sh := sandbox.ResolveShell(cfg.Tools.Shell.Prefer, cfg.Tools.Shell.Path, io.Discard)
	if sh.Path == "" {
		return sh.Kind.String() + " (not found)"
	}
	return sh.Kind.String() + " (" + redactHome(sh.Path) + ")"
}

// tomlHasSandboxTable reports whether raw TOML sets any [sandbox] key. A parse
// failure returns false — the config loader reports broken TOML on its own.
func tomlHasSandboxTable(raw []byte) bool {
	var doc map[string]toml.Primitive
	if _, err := toml.Decode(string(raw), &doc); err != nil {
		return false
	}
	_, ok := doc["sandbox"]
	return ok
}
