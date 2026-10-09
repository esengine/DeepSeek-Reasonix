package config

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// heldScope is what the user granted, held across the project merge. A
// project reasonix.toml arrives with a checkout, so it is untrusted input: it
// may narrow these grants and never widen them. The slices are copies because
// decoding the project file writes into the arrays the user's decode left.
type heldScope struct {
	sandbox      SandboxConfig
	permissions  PermissionsConfig
	approvalMode string
	autoSubmit   bool
	shell        ShellConfig
	rgPath       string
	lsp          map[string]LSPServer
	browser      BrowserConfig
	network      NetworkConfig
	endpoints    heldEndpoints
	layaPython   string
	layaLocal    bool
}

func holdUserScope(c *Config) heldScope {
	s, p := c.Sandbox, c.Permissions
	s.AllowWrite, s.ForbidRead = slices.Clone(s.AllowWrite), slices.Clone(s.ForbidRead)
	s.HostAuthorities, s.AllowedDomains, s.DeniedDomains = slices.Clone(s.HostAuthorities), slices.Clone(s.AllowedDomains), slices.Clone(s.DeniedDomains)
	p.Allow, p.Ask, p.Deny = slices.Clone(p.Allow), slices.Clone(p.Ask), slices.Clone(p.Deny)
	shell := c.Tools.Shell
	shell.Env = maps.Clone(shell.Env)
	return heldScope{
		sandbox:      s,
		permissions:  p,
		approvalMode: c.Desktop.DefaultToolApprovalMode,
		autoSubmit:   c.AutoSubmit,
		shell:        shell,
		rgPath:       c.Tools.Search.RgPath,
		lsp:          maps.Clone(c.LSP.Servers),
		browser:      c.Browser,
		network:      c.Network,
		endpoints:    holdUserEndpoints(c),
		layaPython:   c.Tools.SystemOne.Laya.Python,
		layaLocal:    c.Tools.SystemOne.Laya.Local,
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
// may have written, and gates the programs the project names. Shell env is
// the user's alone: it reaches every command the agent runs.
func (h heldScope) narrow(c *Config, r Roots, root string, projectMeta toml.MetaData) {
	ws := workspaceDir(root)
	c.Tools.Shell.Env = h.shell.Env
	warnShellEnvProblems(c, projectMeta)
	// Auto-submit commits a user's ask answers without a confirmation, so it is
	// the user's alone: a cloned repo must not turn it on.
	if c.AutoSubmit != h.autoSubmit {
		c.ignoreProject("auto_submit", fmt.Sprintf("%t", c.AutoSubmit), ProjectUserOnly)
		c.AutoSubmit = h.autoSubmit
	}
	h.narrowSandbox(c, ws)
	h.narrowPermissions(c)
	if !maps.Equal(c.Agent.RoleEfforts, h.endpoints.agent.RoleEfforts) {
		c.ignoreProject("agent.role_efforts", renderStringMap(c.Agent.RoleEfforts), ProjectUserOnly)
	}
	c.Agent.RoleEfforts = maps.Clone(h.endpoints.agent.RoleEfforts)
	if NormalizeToolApprovalMode(c.Desktop.DefaultToolApprovalMode) != NormalizeToolApprovalMode(h.approvalMode) {
		c.ignoreProject("desktop.default_tool_approval_mode", c.Desktop.DefaultToolApprovalMode, ProjectUserOnly)
	}
	c.Desktop.DefaultToolApprovalMode = h.approvalMode
	if !reflect.DeepEqual(c.Network, h.network) {
		c.ignoreProject("network", "proxy_mode="+c.Network.ProxyMode, ProjectUserOnly)
	}
	c.Network = h.network
	// The local Laya runtime starts a Python process on the host.
	if c.Tools.SystemOne.Laya.Local && !h.layaLocal {
		c.ignoreProject("tools.system_one.laya.local", "true", ProjectUserOnly)
		c.Tools.SystemOne.Laya.Local = false
	}
	grant := grantedToWorkspace(r.Home(), ws)
	c.Permissions.Allow = appendMissing(c.Permissions.Allow, grant.Allow...)
	c.Sandbox.AllowWrite = appendMissing(c.Sandbox.AllowWrite, grant.AllowWrite...)
	store := NewProjectProgramStore(r.Home())
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
	// A cloned repo must not hand itself the container daemon socket. An egress
	// allow list only narrows open network, so a repo may set one the user has
	// not, but never replace the user's; its denials only add to the user's.
	s.HostAuthorities = u.HostAuthorities
	if len(u.AllowedDomains) > 0 {
		s.AllowedDomains = u.AllowedDomains
	}
	s.DeniedDomains = appendMissing(u.DeniedDomains, s.DeniedDomains...)
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
