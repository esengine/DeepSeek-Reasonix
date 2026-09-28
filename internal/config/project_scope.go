package config

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"reasonix/internal/permissionpreset"
)

// heldScope is what the user granted, held across the project merge. A
// project reasonix.toml arrives with a checkout, so it is untrusted input: it
// may narrow these grants and never widen them. The slices are copies because
// decoding the project file writes into the arrays the user's decode left.
type heldScope struct {
	sandbox      SandboxConfig
	permissions  PermissionsConfig
	approvalMode string
	shell        ShellConfig
	rgPath       string
	lsp          map[string]LSPServer
	browser      BrowserConfig
	network      NetworkConfig
	endpoints    heldEndpoints
	bot          BotConfig
	// Regional display preferences: a repository must not alter how spend reads.
	language, currency, displayCurrency string
}

func holdUserScope(c *Config) heldScope {
	s, p, b := c.Sandbox, c.Permissions, c.Browser
	s.AllowWrite, s.ForbidRead = slices.Clone(s.AllowWrite), slices.Clone(s.ForbidRead)
	b.ChromeArgs = slices.Clone(b.ChromeArgs)
	p.Allow, p.Ask, p.Deny = slices.Clone(p.Allow), slices.Clone(p.Ask), slices.Clone(p.Deny)
	return heldScope{
		sandbox:         s,
		permissions:     p,
		approvalMode:    c.Desktop.DefaultToolApprovalMode,
		shell:           c.Tools.Shell,
		rgPath:          c.Tools.Search.RgPath,
		lsp:             maps.Clone(c.LSP.Servers),
		browser:         b,
		network:         c.Network,
		endpoints:       holdUserEndpoints(c),
		bot:             cloneBotConfig(c.Bot),
		language:        c.Desktop.Language,
		currency:        c.Desktop.Currency,
		displayCurrency: c.Billing.DisplayCurrency,
	}
}

// IgnoredProjectReason says why a project value was not applied.
type IgnoredProjectReason string

const (
	// ProjectWidensUser: the value would loosen what the user configured.
	ProjectWidensUser IgnoredProjectReason = "widens_user_setting"
	// ProjectOutsideWorkspace: a path that resolves outside the workspace.
	ProjectOutsideWorkspace IgnoredProjectReason = "outside_workspace"
	// ProjectUserOnly: a setting only the user config may choose.
	ProjectUserOnly IgnoredProjectReason = "user_only"
	// ProjectAwaitingApproval: a program the project names that nobody approved.
	ProjectAwaitingApproval IgnoredProjectReason = "awaiting_approval"
	// ProjectApprovalUnavailable: the approval record could not be read.
	ProjectApprovalUnavailable IgnoredProjectReason = "approval_store_unavailable"
	// ProjectProgramWritable: a single program where sandboxed commands write.
	ProjectProgramWritable IgnoredProjectReason = "program_in_writable_location"
)

var ignoredProjectReasonText = map[IgnoredProjectReason]string{
	ProjectWidensUser:          "a project file may only narrow this setting; set it in your user config to change it",
	ProjectOutsideWorkspace:    "the path resolves outside this workspace",
	ProjectUserOnly:            "only your user config sets this",
	ProjectAwaitingApproval:    "it runs only after you approve it; run `reasonix trust` in this workspace",
	ProjectApprovalUnavailable: "the approval record could not be read, so it stays off",
	ProjectProgramWritable:     "it names a file in this workspace or another place sandboxed commands can write; point it at an installed program",
}

// IgnoredProjectSetting is one value a project file set that the load refused.
type IgnoredProjectSetting struct {
	Key    string
	Value  string
	Reason IgnoredProjectReason
}

type projectScopeReport struct {
	ignored  []IgnoredProjectSetting
	pending  []ProjectProgram
	admitted []ProjectProgram
}

// IgnoredProjectSettings lists the project values this load refused.
func (c *Config) IgnoredProjectSettings() []IgnoredProjectSetting {
	if c == nil {
		return nil
	}
	return slices.Clone(c.projectScope.ignored)
}

func (c *Config) ignoreProject(key, value string, reason IgnoredProjectReason) {
	c.projectScope.ignored = append(c.projectScope.ignored, IgnoredProjectSetting{Key: key, Value: value, Reason: reason})
	c.addLoadWarning(fmt.Sprintf("project config sets %s = %q; ignored: %s", key, value, ignoredProjectReasonText[reason]))
}

// narrow applies the project-only-narrows rule to everything the project merge
// may have written, and gates the programs the project names.
func (h heldScope) narrow(c *Config, root string) {
	ws := workspaceDir(root)
	c.Desktop.Language, c.Desktop.Currency, c.Billing.DisplayCurrency = h.language, h.currency, h.displayCurrency
	h.narrowSandbox(c, ws)
	h.narrowPermissions(c)
	if permissionpreset.NormalizeDefault(c.Desktop.DefaultToolApprovalMode) != permissionpreset.NormalizeDefault(h.approvalMode) {
		c.ignoreProject("desktop.default_tool_approval_mode", c.Desktop.DefaultToolApprovalMode, ProjectUserOnly)
	}
	c.Desktop.DefaultToolApprovalMode = h.approvalMode
	if !reflect.DeepEqual(c.Network, h.network) {
		c.ignoreProject("network", "proxy_mode="+c.Network.ProxyMode, ProjectUserOnly)
	}
	c.Network = h.network
	// [bot] opens the machine to chat accounts and routes their messages into
	// sessions; only the user's own config may say which.
	if !reflect.DeepEqual(c.Bot, h.bot) {
		c.ignoreProject("bot", "", ProjectUserOnly)
	}
	c.Bot = h.bot
	home := reasonixHomeDir()
	grant := grantedToWorkspace(home, ws)
	c.Permissions.Allow = appendMissing(c.Permissions.Allow, grant.Allow...)
	c.Sandbox.AllowWrite = appendMissing(c.Sandbox.AllowWrite, grant.AllowWrite...)
	store := NewProjectProgramStore(home)
	h.gatePrograms(c, store, ws)
	h.endpoints.gateEndpoints(c, store, ws)
}

func (h heldScope) narrowSandbox(c *Config, ws string) {
	s, u := &c.Sandbox, h.sandbox
	if bashJails(u.Bash) && !bashJails(s.Bash) {
		c.ignoreProject("sandbox.bash", s.Bash, ProjectWidensUser)
		s.Bash = u.Bash
	}
	if s.Network && !u.Network {
		c.ignoreProject("sandbox.network", "true", ProjectWidensUser)
		s.Network = false
	}
	s.ForbidRead = appendMissing(u.ForbidRead, s.ForbidRead...)
	allow := slices.Clone(u.AllowWrite)
	for _, entry := range s.AllowWrite {
		if slices.Contains(u.AllowWrite, entry) {
			continue
		}
		if dir, ok := c.workspacePath(ws, entry); ok {
			allow = appendMissing(allow, dir)
		} else {
			c.ignoreProject("sandbox.allow_write", entry, ProjectOutsideWorkspace)
		}
	}
	s.AllowWrite = allow
	if s.WorkspaceRoot != u.WorkspaceRoot {
		if dir, ok := c.workspacePath(ws, s.WorkspaceRoot); ok {
			s.WorkspaceRoot = dir
		} else {
			c.ignoreProject("sandbox.workspace_root", s.WorkspaceRoot, ProjectOutsideWorkspace)
			s.WorkspaceRoot = u.WorkspaceRoot
		}
	}
}

func (h heldScope) narrowPermissions(c *Config) {
	p, u := &c.Permissions, h.permissions
	if normalizedMode(p.Mode) != normalizedMode(u.Mode) {
		c.ignoreProject("permissions.mode", p.Mode, ProjectUserOnly)
	}
	if p.AllowDynamicBash && !u.AllowDynamicBash {
		c.ignoreProject("permissions.allow_dynamic_bash", "true", ProjectUserOnly)
	}
	for _, rule := range p.Allow {
		if !slices.Contains(u.Allow, rule) {
			c.ignoreProject("permissions.allow", rule, ProjectUserOnly)
		}
	}
	p.Mode, p.AllowDynamicBash, p.Allow = u.Mode, u.AllowDynamicBash, u.Allow
	p.Ask = appendMissing(u.Ask, p.Ask...)
	p.Deny = appendMissing(u.Deny, p.Deny...)
}

// bashJails mirrors BashModeForGOOS off Windows: only an explicit "off" runs
// bash unconfined, so every other spelling counts as the jail.
func bashJails(mode string) bool { return strings.TrimSpace(mode) != "off" }

func normalizedMode(mode string) string { return strings.ToLower(strings.TrimSpace(mode)) }

// workspacePath resolves a project-declared path the way the confiner will and
// reports whether it stays inside ws. The resolved form is what gets stored, so
// a link swapped in after this check cannot move what was approved.
func (c *Config) workspacePath(ws, path string) (string, bool) {
	path = strings.TrimSpace(c.expandSandboxPath(path))
	if path == "" || ws == "" || strings.Contains(path, "${") {
		return "", false
	}
	if filepath.VolumeName(path) == "" && !os.IsPathSeparator(path[0]) {
		path = filepath.Join(ws, path)
	} else if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	resolved, err := evalSymlinksAllowMissing(path)
	if err != nil {
		return "", false
	}
	return resolved, pathWithinRoot(ws, resolved)
}

// expandSandboxPath expands ${VAR} from the process environment alone: a
// workspace .env must not steer where writes land or which reads are refused,
// and a project path still holding "${" is refused rather than expanded again.
func (c *Config) expandSandboxPath(path string) string {
	return ExpandVars(path)
}

// workspaceDir is root absolute and symlink-free, or "" when it cannot be
// resolved; an unresolvable workspace contains nothing.
func workspaceDir(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		return ""
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return ""
	}
	if info, err := os.Stat(real); err != nil || !info.IsDir() {
		return ""
	}
	return filepath.Clean(real)
}

func appendMissing(base []string, extra ...string) []string {
	out := slices.Clone(base)
	for _, v := range extra {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// cloneBotConfig deep-copies b: the project decode writes into the maps and
// slices the user's decode left behind.
func cloneBotConfig(b BotConfig) BotConfig {
	data, err := json.Marshal(b)
	if err != nil {
		return BotConfig{}
	}
	var out BotConfig
	if json.Unmarshal(data, &out) != nil {
		return BotConfig{}
	}
	return out
}
