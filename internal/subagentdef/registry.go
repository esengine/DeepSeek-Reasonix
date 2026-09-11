package subagentdef

import (
	"sort"
	"strings"
	"sync"
)

const (
	// ScopeCLI identifies subagent definitions bundled with the CLI binary
	// itself. These have the highest priority and override all other scopes.
	ScopeCLI = "cli"
	// ScopeProject identifies subagent definitions from the current project's
	// .reasonix directory (project-level overrides).
	ScopeProject = "project"
	// ScopeUser identifies subagent definitions from the user's home directory
	// configuration (user-level defaults).
	ScopeUser = "user"
	// ScopePlugin identifies subagent definitions provided by loaded plugins.
	ScopePlugin = "plugin"
	// ScopeBuiltin identifies built-in subagent definitions shipped with the
	// application. These have the lowest priority and serve as defaults.
	ScopeBuiltin = "builtin"
)

var scopePriority = map[string]int{
	ScopeCLI:     1,
	ScopeProject: 2,
	ScopeUser:    3,
	ScopePlugin:  4,
	ScopeBuiltin: 5,
}

// Registry is a thread-safe collection of subagent definitions with
// priority-based deduplication. When multiple definitions share the same
// name, the one from the higher-priority scope (lower scopePriority value)
// wins. Definitions are looked up case-insensitively by name.
type Registry struct {
	mu    sync.RWMutex
	defs  map[string]*SubagentDefinition
	order []string
}

// NewRegistry returns an empty subagent definition registry, ready to accept
// definitions from various sources via Add or AddAll.
func NewRegistry() *Registry {
	return &Registry{
		defs:  map[string]*SubagentDefinition{},
		order: []string{},
	}
}

// Add inserts a subagent definition into the registry. If a definition with
// the same name (case-insensitive) already exists, the one from the
// higher-priority scope is kept. Nil or invalid definitions are silently
// ignored.
func (r *Registry) Add(def *SubagentDefinition) {
	if def == nil || !def.Valid() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	key := strings.ToLower(def.Name)
	existing, ok := r.defs[key]
	if ok {
		existingPriority := scopePriority[existing.SourceScope]
		newPriority := scopePriority[def.SourceScope]
		if newPriority >= existingPriority {
			return
		}
		r.defs[key] = def
		return
	}
	r.defs[key] = def
	r.order = append(r.order, def.Name)
}

// AddAll adds all definitions from the slice to the registry, applying the
// same priority-based deduplication rules as Add.
func (r *Registry) AddAll(defs []*SubagentDefinition) {
	for _, def := range defs {
		r.Add(def)
	}
}

// Get looks up a subagent definition by name (case-insensitive). The second
// return value reports whether the definition was found.
func (r *Registry) Get(name string) (*SubagentDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.defs[strings.ToLower(strings.TrimSpace(name))]
	return def, ok
}

// List returns all registered subagent definitions sorted first by scope
// priority (CLI first, builtin last), then alphabetically by name. The
// returned slice is a new slice; modifying it does not affect the registry.
func (r *Registry) List() []*SubagentDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*SubagentDefinition, 0, len(r.order))
	for _, name := range r.order {
		def, ok := r.defs[strings.ToLower(name)]
		if ok {
			out = append(out, def)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		pi := scopePriority[out[i].SourceScope]
		pj := scopePriority[out[j].SourceScope]
		if pi != pj {
			return pi < pj
		}
		return out[i].Name < out[j].Name
	})

	return out
}

// Names returns the names of all registered subagent definitions sorted
// alphabetically.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Len returns the number of registered subagent definitions.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.defs)
}

// FindByDescription returns all subagent definitions whose name or
// description (case-insensitive) contains the query string. An empty query
// returns all definitions (same as List).
func (r *Registry) FindByDescription(query string) []*SubagentDefinition {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return r.List()
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	var matches []*SubagentDefinition
	for _, def := range r.defs {
		if strings.Contains(strings.ToLower(def.Name), query) ||
			strings.Contains(strings.ToLower(def.Description), query) {
			matches = append(matches, def)
		}
	}
	return matches
}
