package agent

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

// ProfileDefinition is the delegation-facing narrowing of a stored Skill: what
// the worker is, never what one call wants of it. A field belongs here only if
// its value follows from the worker's identity; allowed-tools and read-only are
// ceilings, not grants. Profile names resolve at call time and must never enter
// tool schemas or the parent system prompt (prompt-cache stability).
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

// ProfileFromSkill narrows a stored Skill to the fields delegation may see.
// Routing metadata (triggers, auto-use, cost, freshness) stays behind: it
// decides when a worker is chosen, not how that worker thinks, and admitting it
// here is the first step from a profile toward a workflow language.
func ProfileFromSkill(sk skill.Skill) ProfileDefinition {
	return ProfileDefinition{
		Name:         sk.Name,
		Body:         sk.Body,
		AllowedTools: sk.AllowedTools,
		Model:        sk.Model,
		Effort:       sk.Effort,
		ReadOnly:     sk.ReadOnly,
		Invocation:   sk.Invocation,
		NamedBuiltin: NamedBuiltinProfile(sk.Name),
	}
}

// ProfileExecSpec is the unified execution specification shared by task,
// fleet items, and run_skill profile runs. Call sites build a spec, then hand
// it to TaskTool.RunProfileSpec so runners cannot drift. Its members are the
// delegation boundary: place a new field in the member that decides its value,
// never in whichever one is closest to the call site.
type ProfileExecSpec struct {
	Task    TaskSpec
	Worker  WorkerSpec
	Grant   CapabilityGrant
	Context ContextRequest
	Sched   SchedulerPolicy
}

// TaskSpec is what one delegated run must accomplish. Every field is decided
// per call by the delegating parent, never by the worker's identity.
type TaskSpec struct {
	// Objective is the task text handed to the child agent.
	Objective string
	// Description is an optional short UI label.
	Description string
}

// WorkerSpec is who carries the run out: the resolved profile identity and the
// provider runtime it thinks with. Fields here follow from the worker chosen,
// not from what this particular call asks for.
type WorkerSpec struct {
	// Kind is the transcript kind: "task", "skill", or "fleet".
	Kind string
	// Name is the transcript / display name (profile name or "task").
	Name string
	// Profile is the optional profile skill name (empty for ordinary task).
	Profile string
	// SystemPrompt is the full child system prompt.
	SystemPrompt string
	// UseProfilePrompt marks a profile body used verbatim, with no task default.
	UseProfilePrompt bool
	// Model/Effort are the already-resolved effective values for this run
	// (after config override → call params → frontmatter → global → parent).
	Model  string
	Effort string
}

// CapabilityGrant is what the run may touch. Profile frontmatter supplies a
// ceiling and call arguments may only narrow it (see IntersectToolLists), so
// the effective grant is always the intersection of the two.
type CapabilityGrant struct {
	// ReadOnly forces the read-only registry even when the profile can write.
	ReadOnly bool
	// AllowNoTools lets the parallel-research path run a child with no tools.
	AllowNoTools bool
	// CallTools is the optional per-call tools whitelist.
	CallTools []string
	// ProfileTools is the profile frontmatter allowed-tools ceiling.
	ProfileTools []string
	// WritePaths is the normalized write claim (empty for read-only).
	WritePaths WritePathSet
}

// ContextRequest is the context a child starts from, as opposed to the task it
// is given. Today that is only a prior transcript to resume.
type ContextRequest struct {
	// ContinueFrom / ForkFrom are transcript continuation refs (writer path).
	ContinueFrom string
	ForkFrom     string
	// Ephemeral forces a non-persisted transcript for entry points that promise
	// no durable host side effects, such as read_only_task.
	Ephemeral bool
}

// SchedulerPolicy is when and how the run executes. It never changes what the
// child is asked to do or what it is allowed to touch.
type SchedulerPolicy struct {
	// MaxSteps is the optional per-call step budget (0 = default).
	MaxSteps int
	// RunInBackground starts a jobs.Manager background job.
	RunInBackground bool
	// BackgroundWriter marks work already hosted by a parent background job
	// (for example fleet). It participates in checkpoint writer exclusion
	// without spawning a second nested job.
	BackgroundWriter bool
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

// parentSession returns the owning session, or empty when the caller asked for
// an ephemeral run so the store never persists a transcript for it.
func (c ContextRequest) parentSession(ctx context.Context) string {
	if c.Ephemeral {
		return ""
	}
	return ParentSession(ctx)
}
