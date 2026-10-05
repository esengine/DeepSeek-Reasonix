package extension

// Scope records which tier contributed a capability. The wire names are
// stable: they appear in manifests, diagnostics, and conflict reports, so a
// rename is a protocol break.
type Scope string

const (
	// ScopeBuiltin is compiled into the binary (built-in tools, shipped
	// skills). It is the lowest tier: anything user-provided shadows it.
	ScopeBuiltin Scope = "builtin"
	// ScopeGlobal is user-level configuration under the Reasonix home dir.
	ScopeGlobal Scope = "global"
	// ScopeProject is configuration rooted at the current project. It is the
	// highest tier, matching the existing skill and command discovery where
	// project roots always win.
	ScopeProject Scope = "project"
	// ScopePlugin is contributed by an installed plugin package. Plugins sit
	// below project config but above global user config: a project may
	// override a plugin, a plugin may override a user default.
	ScopePlugin Scope = "plugin"
	// ScopeClaudeCompat marks contributions imported from Claude conventions
	// (.claude dirs, Claude-style settings). Compat tiers rank just below
	// their native project counterpart so imported config never silently
	// overrides native Reasonix config.
	ScopeClaudeCompat Scope = "claude_compat"
	// ScopeCodexCompat marks contributions imported from Codex conventions.
	ScopeCodexCompat Scope = "codex_compat"
)

// tierRank orders the scopes for shadowing: a higher rank wins the same
// canonical ID. Compat scopes sit between project and plugin because imported
// project-adjacent config (.claude under a project root) is closer to project
// intent than to an installed package, but must never outrank native config.
// Unknown scopes rank below builtin so they lose every shadow race instead of
// accidentally winning one.
func tierRank(s Scope) int {
	switch s {
	case ScopeProject:
		return 60
	case ScopeClaudeCompat:
		return 50
	case ScopeCodexCompat:
		return 40
	case ScopePlugin:
		return 30
	case ScopeGlobal:
		return 20
	case ScopeBuiltin:
		return 10
	default:
		return 0
	}
}

// knownScope reports whether s is one of the declared wire values. The builder
// rejects unknown scopes at validation time; silently accepting a typo'd scope
// would rank it 0 and change shadowing outcomes invisibly.
func knownScope(s Scope) bool {
	switch s {
	case ScopeBuiltin, ScopeGlobal, ScopeProject, ScopePlugin, ScopeClaudeCompat, ScopeCodexCompat:
		return true
	default:
		return false
	}
}

// ContributionSource is the provenance of one contribution. It answers "who
// put this here" for diagnostics and conflict reports, and its Scope drives
// shadow resolution.
type ContributionSource struct {
	// PluginID is the installed plugin package name when the contribution came
	// from one; empty for non-plugin sources.
	PluginID string
	// Version is the contributor's version (plugin package version, contract
	// version). Optional; empty means unspecified.
	Version string
	// Origin is a short stable label for the discovery path: "builtin",
	// "user", "project", "plugin", a manifest path, and so on.
	Origin string
	// Scope is the shadowing tier.
	Scope Scope
	// Priority is provenance-level priority metadata carried for display and
	// tie-breaking. Per-contribution priority lives on Contribution.Priority;
	// the two are deliberately separate so a source descriptor can be shared
	// by contributions with different priorities.
	Priority int
	// Path is the file or directory the contribution was loaded from, when one
	// exists. Diagnostic only; it never influences winner rules.
	Path string
}

// key identifies a source for conflict detection. Two contributions share a
// source only when scope, plugin, and origin all agree — path is excluded so
// several files discovered by the same root count as one source (mirroring
// today's first-root-wins behavior inside a single discovery pass).
func (s ContributionSource) key() string {
	return string(s.Scope) + "|" + s.PluginID + "|" + s.Origin
}

// label renders the source for error messages, preferring the plugin identity
// because cross-plugin conflicts are the ones users must act on.
func (s ContributionSource) label() string {
	if s.PluginID != "" {
		return "plugin " + s.PluginID
	}
	if s.Origin != "" {
		return string(s.Scope) + " (" + s.Origin + ")"
	}
	return string(s.Scope)
}
