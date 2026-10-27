package config

import (
	"errors"
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
	attachments    AttachmentsConfig
	sandbox        SandboxConfig
	permissions    PermissionsConfig
	approvalMode   string
	updateMode     string
	autoSubmit     bool
	writeLease     string
	planApproval   string
	defaultMode    string
	toolsDisabled  []string
	limits         agentLimits
	goalAutoRetry  int
	subagentClaims string
	hostVerif      string
	shell          ShellConfig
	rgPath         string
	lsp            map[string]LSPServer
	browser        BrowserConfig
	network        NetworkConfig
	endpoints      heldEndpoints
	layaPython     string
	layaLocal      bool
	// rememberDevices is the user's [serve] remember_paired_devices. A project
	// may not set it: a cloned repo must not be able to make a phone the person
	// paired with stay trusted longer than they chose.
	rememberDevices bool
	// turnNarration is the user's [agent] turn_narration. It adds output tokens
	// to every turn, so a cloned repo must not turn it on.
	turnNarration string
	persona       PersonaConfig
	peers         PeersConfig
	observability ObservabilityConfig
	mods          ModsConfig
	// mcpResources decides whether a server's resources are readable by the model.
	mcpResources bool
	presentFiles bool
	// webSearchBackends and searxngURL decide where the user's queries go, so
	// only the user file names them.
	webSearchBackends []string
	searxngURL        string
	// webSearchPaid is the user's agreement to be charged; a cloned repo cannot
	// give it on their behalf.
	webSearchPaid []string
	webFetch      []string
	webFetchPaid  []string
	// videoNativeMax lowers what is sent to a vendor, so only the user sets it.
	videoNativeMax int64
	// videoURLPass has a vendor visit an address, so only the user turns it on.
	videoURLPass bool
}

func holdUserScope(c *Config) heldScope {
	s, p := c.Sandbox, c.Permissions
	s.AllowWrite, s.ForbidRead = slices.Clone(s.AllowWrite), slices.Clone(s.ForbidRead)
	s.HostAuthorities, s.AllowedDomains, s.DeniedDomains = slices.Clone(s.HostAuthorities), slices.Clone(s.AllowedDomains), slices.Clone(s.DeniedDomains)
	s.AllowDevices = slices.Clone(s.AllowDevices)
	p.Allow, p.Ask, p.Deny = slices.Clone(p.Allow), slices.Clone(p.Ask), slices.Clone(p.Deny)
	shell := c.Tools.Shell
	shell.Env = maps.Clone(shell.Env)
	shell.Args = slices.Clone(shell.Args)
	return heldScope{
		attachments:     cloneAttachments(c.Attachments),
		sandbox:         s,
		permissions:     p,
		approvalMode:    c.Desktop.DefaultToolApprovalMode,
		updateMode:      c.DesktopUpdateMode(),
		autoSubmit:      c.AutoSubmit,
		writeLease:      c.Agent.WriteLease,
		planApproval:    c.Agent.PlanApproval,
		defaultMode:     c.Agent.DefaultMode,
		toolsDisabled:   c.DisabledTools(),
		limits:          holdAgentLimits(c),
		goalAutoRetry:   c.Agent.GoalAutoRetry,
		subagentClaims:  c.Agent.SubagentClaims,
		hostVerif:       c.Agent.HostVerification,
		shell:           shell,
		rgPath:          c.Tools.Search.RgPath,
		lsp:             maps.Clone(c.LSP.Servers),
		browser:         c.Browser,
		network:         c.Network,
		endpoints:       holdUserEndpoints(c),
		layaPython:      c.Tools.SystemOne.Laya.Python,
		layaLocal:       c.Tools.SystemOne.Laya.Local,
		rememberDevices: c.Serve.RememberPairedDevices,
		turnNarration:   c.TurnNarration(),
		persona:         clonePersona(c.Agent.Persona),
		peers:           clonePeers(c.Peers),
		observability:   c.Observability,
		mods:            c.Mods,
		mcpResources:    c.Tools.MCPResources,
		presentFiles:    c.Tools.PresentFiles,

		webSearchBackends: slices.Clone(c.Tools.WebSearchBackends),
		searxngURL:        c.Tools.SearxngURL,
		webSearchPaid:     slices.Clone(c.Tools.WebSearchPaidAllowed),
		webFetch:          slices.Clone(c.Tools.WebFetchBackends),
		webFetchPaid:      slices.Clone(c.Tools.WebFetchPaidAllowed),
		videoNativeMax:    c.Tools.VideoNativeMaxBytes,
		videoURLPass:      c.Tools.VideoURLPassthrough,
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
	// ProjectRootRefused: an extra folder the user granted that no longer passes
	// vetting where it resolves now.
	ProjectRootRefused IgnoredProjectReason = "extra_root_refused"
	// ProjectProgramWritable: a single program where sandboxed commands write.
	ProjectProgramWritable IgnoredProjectReason = "program_in_writable_location"
)

var ignoredProjectReasonText = map[IgnoredProjectReason]string{
	ProjectWidensUser:          "a project file may only narrow this setting; set it in your user config to change it",
	ProjectOutsideWorkspace:    "the path resolves outside this workspace",
	ProjectUserOnly:            "only your user config sets this",
	ProjectAwaitingApproval:    "it runs only after you approve it; run `reasonix trust` in this workspace",
	ProjectApprovalUnavailable: "the approval record could not be read, so it stays off",
	ProjectRootRefused:         "the granted folder now resolves somewhere that cannot take writes; grant it again if it is still wanted",
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
	// extraRoots are the granted folders that passed vetting at this load.
	extraRoots []string
}

// ExtraRoots lists the user-granted extra workspace folders as vetted at this
// load, each under the path it resolves to now.
func (c *Config) ExtraRoots() []string {
	if c == nil {
		return nil
	}
	return slices.Clone(c.projectScope.extraRoots)
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
	if !reflect.DeepEqual(c.Attachments, h.attachments) {
		c.ignoreProject("attachments.max_stream_bytes", fmt.Sprint(c.AttachmentStreamLimit().Bytes), ProjectUserOnly)
	}
	c.Attachments = cloneAttachments(h.attachments)
	ws := workspaceDir(root)
	h.narrowPromptFile(c, r, ws)
	c.Tools.Shell.Env = h.shell.Env
	c.Tools.Shell.Args = h.shell.Args
	warnShellEnvProblems(c, projectMeta)
	// Auto-submit commits a user's ask answers without a confirmation, so it is
	// the user's alone: a cloned repo must not turn it on.
	if c.AutoSubmit != h.autoSubmit {
		c.ignoreProject("auto_submit", fmt.Sprintf("%t", c.AutoSubmit), ProjectUserOnly)
		c.AutoSubmit = h.autoSubmit
	}
	// Messages between sessions put other sessions' text in front of this one's
	// model, so enabling them is the user's alone.
	h.narrowPeers(c)
	h.narrowObservability(c)
	h.narrowMods(c)
	// Letting the model read what a server publishes widens what a third party
	// can put in front of it, so a cloned repo must not turn it on.
	if c.Tools.MCPResources != h.mcpResources {
		c.ignoreProject("tools.mcp_resources", fmt.Sprintf("%t", c.Tools.MCPResources), ProjectUserOnly)
		c.Tools.MCPResources = h.mcpResources
	}
	if c.Tools.PresentFiles != h.presentFiles {
		c.ignoreProject("tools.present_files", fmt.Sprintf("%t", c.Tools.PresentFiles), ProjectUserOnly)
		c.Tools.PresentFiles = h.presentFiles
	}
	c.keepUserSearchBackends(h)
	if c.Tools.VideoURLPassthrough != h.videoURLPass {
		c.ignoreProject("tools.video_url_passthrough", fmt.Sprintf("%t", c.Tools.VideoURLPassthrough), ProjectUserOnly)
		c.Tools.VideoURLPassthrough = h.videoURLPass
	}
	if c.Tools.VideoNativeMaxBytes != h.videoNativeMax {
		c.ignoreProject("tools.video_native_max_bytes", fmt.Sprint(c.Tools.VideoNativeMaxBytes), ProjectUserOnly)
		c.Tools.VideoNativeMaxBytes = h.videoNativeMax
	}
	// The cross-session write lease protects other sessions from this one, so a
	// cloned repo must not relax it, nor tighten what the user chose.
	if c.Agent.GoalAutoRetry != h.goalAutoRetry {
		c.ignoreProject("agent.goal_auto_retry", fmt.Sprint(c.Agent.GoalAutoRetry), ProjectUserOnly)
		c.Agent.GoalAutoRetry = h.goalAutoRetry
	}
	h.narrowAgentLimits(c)
	// A project can remove tools for its own sessions and never take one back.
	c.Tools.Disabled = append(slices.Clone(h.toolsDisabled), c.Tools.Disabled...)
	c.Tools.Disabled = c.DisabledTools()
	if c.Agent.PlanApproval != h.planApproval {
		c.ignoreProject("agent.plan_approval", c.Agent.PlanApproval, ProjectUserOnly)
		c.Agent.PlanApproval = h.planApproval
	}
	if c.Agent.DefaultMode != h.defaultMode {
		c.ignoreProject("agent.default_mode", c.Agent.DefaultMode, ProjectUserOnly)
		c.Agent.DefaultMode = h.defaultMode
	}
	if c.Agent.WriteLease != h.writeLease {
		c.ignoreProject("agent.write_lease", c.Agent.WriteLease, ProjectUserOnly)
		c.Agent.WriteLease = h.writeLease
	}
	// How a sub-agent's write claim is held decides whether another writer can
	// work beside it, so a cloned repo cannot loosen it either.
	if c.Agent.SubagentClaims != h.subagentClaims {
		c.ignoreProject("agent.subagent_claims", c.Agent.SubagentClaims, ProjectUserOnly)
		c.Agent.SubagentClaims = h.subagentClaims
	}
	// What the host asks of a turn before it may finish is the user's own call,
	// so a cloned repo can neither switch it off nor undo the user's choice.
	if c.Agent.HostVerification != h.hostVerif {
		c.ignoreProject("agent.host_verification", c.Agent.HostVerification, ProjectUserOnly)
		c.Agent.HostVerification = h.hostVerif
	}
	// Keeping paired phones across restarts is the user's alone: it widens how
	// long a device stays trusted, which is not a clone's to give away.
	if c.Serve.RememberPairedDevices != h.rememberDevices {
		c.ignoreProject("serve.remember_paired_devices", fmt.Sprintf("%t", c.Serve.RememberPairedDevices), ProjectUserOnly)
	}
	c.Serve.RememberPairedDevices = h.rememberDevices
	if c.TurnNarration() != h.turnNarration {
		c.ignoreProject("agent.turn_narration", c.TurnNarration(), ProjectUserOnly)
	}
	c.Agent.TurnNarration = h.turnNarration
	if !reflect.DeepEqual(c.Agent.Persona, h.persona) {
		c.ignoreProject("agent.persona", "", ProjectUserOnly)
	}
	c.Agent.Persona = clonePersona(h.persona)
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
	if c.DesktopUpdateMode() != h.updateMode {
		c.ignoreProject("desktop.update_mode", c.Desktop.UpdateMode, ProjectUserOnly)
	}
	c.Desktop.UpdateMode = h.updateMode
	if !reflect.DeepEqual(c.Network, h.network) {
		c.ignoreProject("network", "proxy_mode="+c.Network.ProxyMode, ProjectUserOnly)
	}
	c.Network = h.network
	// The local Laya runtime starts a Python process on the host.
	if c.Tools.SystemOne.Laya.Local && !h.layaLocal {
		c.ignoreProject("tools.system_one.laya.local", "true", ProjectUserOnly)
		c.Tools.SystemOne.Laya.Local = false
	}
	c.applyWorkspaceGrants(r, ws)
	store := NewProjectProgramStore(r.Home())
	h.gatePrograms(c, store, ws)
	// Launcher arguments belong to the program the user chose them for.
	if c.Tools.Shell.Path != h.shell.Path {
		c.Tools.Shell.Args = nil
	}
	h.endpoints.gateEndpoints(c, store, ws)
}

// applyWorkspaceGrants adds what the user already granted this workspace, and
// refuses a granted root that no longer resolves to itself.
func (c *Config) applyWorkspaceGrants(r Roots, ws string) {
	grant := grantedToWorkspace(r.Home(), ws)
	c.Permissions.Allow = appendMissing(c.Permissions.Allow, grant.Allow...)
	for _, entry := range grant.AllowWrite {
		if !filepath.IsAbs(entry) {
			c.Sandbox.AllowWrite = appendMissing(c.Sandbox.AllowWrite, entry)
			continue
		}
		resolved, err := c.VetExtraRoot(ws, entry, c.projectScope.extraRoots)
		if err != nil {
			var refusal *WorkspaceRootError
			code := "unreadable"
			if errors.As(err, &refusal) {
				code = string(refusal.Code)
			}
			c.ignoreProject("workspace_roots["+code+"]", entry, ProjectRootRefused)
			continue
		}
		if !samePath(resolved, entry) {
			c.ignoreProject("workspace_roots[moved_to "+resolved+"]", entry, ProjectRootRefused)
			continue
		}
		c.projectScope.extraRoots = appendMissing(c.projectScope.extraRoots, resolved)
	}
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
	// Turning it on edits ACLs on the user's disk, which a cloned repo must not cause.
	if s.WindowsIntegrity != u.WindowsIntegrity {
		c.ignoreProject("sandbox.windows_integrity", fmt.Sprint(s.WindowsIntegrity), ProjectUserOnly)
		s.WindowsIntegrity = u.WindowsIntegrity
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
	if s.EgressUnlisted != u.EgressUnlisted {
		c.ignoreProject("sandbox.egress_unlisted", s.EgressUnlisted, ProjectUserOnly)
	}
	s.HostAuthorities, s.AllowDevices, s.EgressUnlisted = u.HostAuthorities, u.AllowDevices, u.EgressUnlisted
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
	if p.AskOnExternalOrigin != u.AskOnExternalOrigin {
		c.ignoreProject("permissions.ask_on_external_origin", fmt.Sprint(p.AskOnExternalOrigin), ProjectUserOnly)
	}
	p.Mode, p.AllowDynamicBash, p.Allow, p.AskOnExternalOrigin = u.Mode, u.AllowDynamicBash, u.Allow, u.AskOnExternalOrigin
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

// keepUserSearchBackends restores the user's search backends over a project
// file's: they decide where the user's queries are sent.
func (c *Config) keepUserSearchBackends(h heldScope) {
	if !slices.Equal(c.Tools.WebSearchBackends, h.webSearchBackends) {
		c.ignoreProject("tools.web_search_backends", strings.Join(c.Tools.WebSearchBackends, ","), ProjectUserOnly)
		c.Tools.WebSearchBackends = slices.Clone(h.webSearchBackends)
	}
	if c.Tools.SearxngURL != h.searxngURL {
		c.ignoreProject("tools.searxng_url", c.Tools.SearxngURL, ProjectUserOnly)
		c.Tools.SearxngURL = h.searxngURL
	}
	if !slices.Equal(c.Tools.WebSearchPaidAllowed, h.webSearchPaid) {
		c.ignoreProject("tools.web_search_paid_allowed", strings.Join(c.Tools.WebSearchPaidAllowed, ","), ProjectUserOnly)
		c.Tools.WebSearchPaidAllowed = slices.Clone(h.webSearchPaid)
	}
	if !slices.Equal(c.Tools.WebFetchBackends, h.webFetch) {
		c.ignoreProject("tools.web_fetch_backends", strings.Join(c.Tools.WebFetchBackends, ","), ProjectUserOnly)
		c.Tools.WebFetchBackends = slices.Clone(h.webFetch)
	}
	if !slices.Equal(c.Tools.WebFetchPaidAllowed, h.webFetchPaid) {
		c.ignoreProject("tools.web_fetch_paid_allowed", strings.Join(c.Tools.WebFetchPaidAllowed, ","), ProjectUserOnly)
		c.Tools.WebFetchPaidAllowed = slices.Clone(h.webFetchPaid)
	}
}

// narrowPromptFile keeps a project's own system_prompt_file out of an untrusted
// folder: it replaces the base prompt the session runs under, so it is chosen by
// the person who trusts the folder, not by whoever wrote the checkout.
func (h heldScope) narrowPromptFile(c *Config, r Roots, ws string) {
	if c.systemPromptFileSource != promptFileSourceProject {
		return
	}
	if trust, _ := NewProjectGrantStore(r.Home()).Trust(ws); trust == WorkspaceTrusted {
		return
	}
	c.ignoreProject("agent.system_prompt_file", c.Agent.SystemPromptFile, ProjectAwaitingApproval)
	c.Agent.SystemPromptFile = h.endpoints.agent.SystemPromptFile
	c.systemPromptFileSource = promptFileSourceUnknown
	if c.Agent.SystemPromptFile != "" {
		c.systemPromptFileSource = promptFileSourceUser
	}
}
