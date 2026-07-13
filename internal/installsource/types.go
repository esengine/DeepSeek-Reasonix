package installsource

import (
	"encoding/json"

	"reasonix/internal/config"
)

// request mirrors the public schema. Fields stay exported because Execute
// unmarshals directly into this struct; the struct comment is the source of
// truth for the field semantics surfaced to the model.
type request struct {
	Op        string            `json:"op"`        // "install" (default) | "uninstall"
	Source    string            `json:"source"`    // required for install; ignored for uninstall
	Kind      string            `json:"kind"`      // auto|skill|mcp|plugin (install only)
	Apply     bool              `json:"apply"`     // install only: actually write/connect
	Scope     string            `json:"scope"`     // project|global
	Mode      string            `json:"mode"`      // auto|copy|link|register (skill install)
	Name      string            `json:"name"`      // override the discovered name
	Transport string            `json:"transport"` // auto|stdio|http|sse
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env"`
	Headers   map[string]string `json:"headers"`
	Tier      string            `json:"tier"`
	Replace   bool              `json:"replace"` // overwrite existing entries
	Strict    *bool             `json:"strict"`  // nil -> true; require skill frontmatter
	// PlanID is echoed back on a confirm-apply call so the host can refuse
	// to apply a plan that does not match the one it approved.
	PlanID string `json:"planId"`

	scopeExplicit bool
}

// response is the JSON shape returned to the model. Status is one of
// "planned" (apply=false, no writes), "done" (all actions succeeded),
// "partial" (some actions succeeded), "failed" (no actions succeeded), or
// "blocked" (no plan could be produced). The `Kind` field is preserved for
// back-compat with callers that only handle a single capability; `Kinds`
// reports per-kind counts.
type response struct {
	OK       bool      `json:"ok"`
	Status   string    `json:"status"`
	Op       string    `json:"op"`
	Applied  bool      `json:"applied"`
	Source   string    `json:"source"`
	Name     string    `json:"name,omitempty"`
	Kind     string    `json:"kind"`            // legacy single kind, "" when no plan
	Kinds    kindTally `json:"kinds,omitempty"` // counts per kind
	Scope    string    `json:"scope"`
	Mode     string    `json:"mode"`
	PlanID   string    `json:"planId,omitempty"`
	Actions  []action  `json:"actions"`
	Warnings []string  `json:"warnings,omitempty"`
	Next     string    `json:"next,omitempty"`
}

// kindTally reports per-kind counts. It is a struct (not a map) so the JSON
// shape stays stable: missing fields read as zero, and we can add new kinds
// without breaking old clients.
type kindTally struct {
	Skill  int `json:"skill"`
	MCP    int `json:"mcp"`
	Plugin int `json:"plugin"`
}

// action is the per-install-step DTO. The Kind/Action pair drives the apply
// dispatcher; RiskLevel/RiskReasons help the calling skill decide whether to
// ask the user before apply=true.
type action struct {
	Kind          string            `json:"kind"`      // "skill" | "mcp" | "plugin"
	Action        string            `json:"action"`    // copy_skill|link_skill|register_skill_root|install_mcp_server|remove_skill|remove_skill_root|remove_mcp_server
	Status        string            `json:"status"`    // planned|done|failed
	RiskLevel     RiskLevel         `json:"riskLevel"` // low|medium|high
	RiskReasons   []string          `json:"riskReasons,omitempty"`
	Name          string            `json:"name,omitempty"`
	Source        string            `json:"source,omitempty"`
	Target        string            `json:"target,omitempty"`
	ConfigPath    string            `json:"configPath,omitempty"`
	Scope         string            `json:"scope,omitempty"`
	Mode          string            `json:"mode,omitempty"`
	Transport     string            `json:"transport,omitempty"`
	URL           string            `json:"url,omitempty"`
	Command       string            `json:"command,omitempty"`
	Args          []string          `json:"args,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	Skills        []string          `json:"skills,omitempty"`
	SkillCount    int               `json:"skillCount,omitempty"`
	Commands      []string          `json:"commands,omitempty"`
	CommandCount  int               `json:"commandCount,omitempty"`
	Commit        string            `json:"commit,omitempty"`        // resolved git snapshot the plan describes; apply pins to it
	Layout        string            `json:"layout,omitempty"`        // canonical_dir|flat_compat|registered_root
	InstallRoot   string            `json:"installRoot,omitempty"`   // skills root or registered custom root
	CanonicalPath string            `json:"canonicalPath,omitempty"` // <skill-name>/SKILL.md when applicable
	Discoverable  bool              `json:"discoverable,omitempty"`  // Store.Read can load it after apply/register
	Indexed       bool              `json:"indexed,omitempty"`       // Store.List includes it for the skills index
	ToolCount     int               `json:"toolCount,omitempty"`
	HookCount     int               `json:"hookCount,omitempty"`
	ManifestKind  string            `json:"manifestKind,omitempty"`
	Version       string            `json:"version,omitempty"`
	Warnings      []string          `json:"warnings,omitempty"`
	Error         string            `json:"error,omitempty"`
	Next          string            `json:"next,omitempty"`

	// Internal state used by apply. Stripped by publicActions before
	// serializing to JSON.
	entry      config.PluginEntry
	skill      skillCandidate
	disconnect func() // optional MCP rollback; nil when not connected
	// preparedRoot lets a multi-plugin marketplace apply reuse the exact clone
	// that produced its approved plan instead of cloning the same repository
	// once per plugin. cleanup is attached to one action and runs after all
	// actions finish.
	preparedRoot string
	cleanup      func()
}

func actionPlanKey(a action) string {
	return a.Kind + ":" + a.Name + ":" + a.Action + ":" + a.Source + ":" + a.Target + ":" + a.ConfigPath
}

// skillCandidate is a parsed skill file/directory ready to install. The
// caller decides whether to copy, link, or register the source path.
type skillCandidate struct {
	Name        string
	Description string
	SourcePath  string
	RootPath    string
	IsDir       bool
	Content     string
}

// summarizeKind returns a one-word kind label for callers that only want the
// dominant capability. "mixed" is returned when the plan contains both
// skills and MCP servers; counts live in response.Kinds.
func summarizeKind(actions []action) string {
	seen := map[string]bool{}
	for _, a := range actions {
		seen[a.Kind] = true
	}
	if len(seen) > 1 {
		return "mixed"
	}
	for k := range seen {
		return k
	}
	return ""
}

// publicActions strips unexported state from each action so the JSON
// response does not leak internal config types.
func publicActions(in []action) []action {
	out := make([]action, len(in))
	for i := range in {
		out[i] = in[i]
		out[i].entry = config.PluginEntry{}
		out[i].skill = skillCandidate{}
		out[i].preparedRoot = ""
		out[i].cleanup = nil
	}
	return out
}

func marshalJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
