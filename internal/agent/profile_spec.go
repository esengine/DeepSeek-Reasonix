package agent

import (
	"fmt"
	"strings"

	"reasonix/internal/tool"
)

// ProfileDefinition is the resolved, runtime-facing shape of a runAs=subagent
// skill used by task/fleet/run_skill. Profile names are resolved at call time
// from the Skill store and must never be written into tool schemas or the
// parent system prompt (prompt-cache stability).
type ProfileDefinition struct {
	Name         string
	Body         string
	AllowedTools []string
	Model        string
	Effort       string
	ReadOnly     bool
	// Invocation is "auto" or "manual". Explicit profile= on task/fleet may
	// call manual profiles; automatic discovery still respects the index.
	Invocation string
	// NamedBuiltin is true for the built-in explore/research/review/
	// security-review profiles. Their body is still the full system prompt
	// (no implicit concise default), matching custom profiles.
	NamedBuiltin bool
}

// ProfileLookup resolves a profile by exact skill name. Implementations read
// from the live Skill store; a nil lookup means profile= is unavailable.
type ProfileLookup func(name string) (ProfileDefinition, bool)

// ProfileExecSpec is the unified execution specification shared by task,
// fleet items, and run_skill profile runs. Call sites build a spec, then hand
// it to TaskTool.RunProfileSpec so runners cannot drift.
type ProfileExecSpec struct {
	// Kind is the transcript kind: "task", "skill", or "fleet".
	Kind string
	// Name is the transcript / display name (profile name or "task").
	Name string
	// Profile is the optional profile skill name (empty for ordinary task).
	Profile string
	// Prompt is the user task text for the child agent.
	Prompt string
	// Description is an optional short UI label.
	Description string
	// SystemPrompt is the full child system prompt. When UseProfilePrompt is
	// true this is exactly the profile body (no DefaultTaskSystemPrompt).
	SystemPrompt string
	// UseProfilePrompt marks custom/named-builtin profile system prompts so
	// hosts do not append the ordinary concise task default.
	UseProfilePrompt bool
	// ReadOnly forces the read-only registry even when the profile can write.
	ReadOnly bool
	// CallTools is the optional per-call tools whitelist.
	CallTools []string
	// ProfileTools is the profile frontmatter allowed-tools list.
	ProfileTools []string
	// Model/Effort are the already-resolved effective values for this run
	// (after config override → call params → frontmatter → global → parent).
	Model  string
	Effort string
	// WritePaths is the normalized write claim (empty for read-only).
	WritePaths WritePathSet
	// MaxSteps is the optional per-call step budget (0 = default).
	MaxSteps int
	// ContinueFrom / ForkFrom are transcript continuation refs (writer path).
	ContinueFrom string
	ForkFrom     string
	// RunInBackground starts a jobs.Manager background job.
	RunInBackground bool
	// Nested marks nested sub-agent acquires (fail-fast on concurrency limits).
	Nested bool
}

// ResolveProfileDefinition looks up a profile and enforces the runAs=subagent
// contract. Explicit names may invoke invocation=manual profiles.
func ResolveProfileDefinition(lookup ProfileLookup, name string) (ProfileDefinition, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return ProfileDefinition{}, fmt.Errorf("profile name is required")
	}
	if lookup == nil {
		return ProfileDefinition{}, fmt.Errorf("profile resolution is not configured in this session")
	}
	def, ok := lookup(name)
	if !ok {
		return ProfileDefinition{}, fmt.Errorf("unknown profile %q", name)
	}
	if strings.TrimSpace(def.Name) == "" {
		def.Name = name
	}
	return def, nil
}

// IntersectToolLists returns the intersection of profile tools and call tools.
// Call parameters may only narrow permissions, never expand them.
//
// Rules:
//   - both empty → nil (meaning "all tools allowed by the registry builder")
//   - profile empty, call set → call list
//   - call empty, profile set → profile list
//   - both set → expand patterns against parent, then intersect; empty
//     intersection is an error
func IntersectToolLists(parent *tool.Registry, profileTools, callTools []string) ([]string, error) {
	profileTools = cleanToolList(profileTools)
	callTools = cleanToolList(callTools)
	if len(profileTools) == 0 {
		return callTools, nil
	}
	if len(callTools) == 0 {
		return profileTools, nil
	}
	// Imported profiles support wildcard tool names. Resolve both sides against
	// the same live registry before comparing them so a profile pattern can be
	// narrowed by a concrete call tool (and vice versa).
	if parent != nil {
		profileTools = expandToolPatterns(parent, profileTools)
		callTools = expandToolPatterns(parent, callTools)
	}
	allowed := map[string]bool{}
	for _, t := range profileTools {
		allowed[t] = true
	}
	var out []string
	seen := map[string]bool{}
	for _, t := range callTools {
		if !allowed[t] || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("tools intersection is empty: call tools are not within the profile allowlist")
	}
	return out, nil
}

// ResolveModelEffort applies the fixed priority:
// profile persistent config → call params → profile frontmatter → global
// subagent default. Empty results leave identity resolution to the parent.
func ResolveModelEffort(configModel, configEffort, callModel, callEffort, profileModel, profileEffort, globalModel, globalEffort string) (model, effort string) {
	model = firstNonBlank(
		strings.TrimSpace(configModel),
		strings.TrimSpace(callModel),
		strings.TrimSpace(profileModel),
		strings.TrimSpace(globalModel),
	)
	effort = firstNonBlank(
		strings.TrimSpace(configEffort),
		strings.TrimSpace(callEffort),
		strings.TrimSpace(profileEffort),
		strings.TrimSpace(globalEffort),
	)
	return model, effort
}

func firstNonBlank(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func cleanToolList(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	out := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// NamedBuiltinProfile reports whether name is a built-in named subagent profile.
func NamedBuiltinProfile(name string) bool {
	switch strings.TrimSpace(name) {
	case "explore", "research", "review", "security-review", "security_review":
		return true
	default:
		return false
	}
}
