package subagentdef

import (
	"sort"
	"strings"
	"sync"
)

const (
	ScopeCLI       = "cli"
	ScopeProject   = "project"
	ScopeUser      = "user"
	ScopePlugin    = "plugin"
	ScopeBuiltin   = "builtin"
)

var scopePriority = map[string]int{
	ScopeCLI:     1,
	ScopeProject: 2,
	ScopeUser:    3,
	ScopePlugin:  4,
	ScopeBuiltin: 5,
}

type Registry struct {
	mu    sync.RWMutex
	defs  map[string]*SubagentDefinition
	order []string
}

func NewRegistry() *Registry {
	return &Registry{
		defs:  map[string]*SubagentDefinition{},
		order: []string{},
	}
}

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

func (r *Registry) AddAll(defs []*SubagentDefinition) {
	for _, def := range defs {
		r.Add(def)
	}
}

func (r *Registry) Get(name string) (*SubagentDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.defs[strings.ToLower(strings.TrimSpace(name))]
	return def, ok
}

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

func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.defs)
}

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
