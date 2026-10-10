package tool

import (
	"encoding/json"
	"strings"

	"reasonix/internal/contract/provider"
)

// ReplacePrefix swaps one name-prefixed namespace for the tools given, in a
// single lock hold: a request composed between a RemovePrefix and its Add would
// be built from a server that appears to have no tools. The admit ceiling and
// suspended prefixes bind as they do in Add; a name outside prefix is ignored.
// Reports how many tools the namespace holds afterwards.
func (r *Registry) ReplacePrefix(prefix string, tools []Tool) int {
	if r == nil || prefix == "" {
		return 0
	}
	// admit may call back into a tool that takes its own lock, which a registry
	// swap must not be holding this one while it waits for.
	r.mu.RLock()
	admit := r.admit
	r.mu.RUnlock()

	type entry struct {
		name  string
		tool  Tool
		canon json.RawMessage
	}
	admitted := make([]entry, 0, len(tools))
	for _, t := range tools {
		if t == nil {
			continue
		}
		name := t.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if admit != nil && !admit(t) {
			continue
		}
		admitted = append(admitted, entry{name: name, tool: t, canon: provider.CanonicalizeSchema(t.Schema())})
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	kept := r.order[:0]
	removed := 0
	for _, name := range r.order {
		if strings.HasPrefix(name, prefix) {
			delete(r.tools, name)
			delete(r.canon, name)
			removed++
			continue
		}
		kept = append(kept, name)
	}
	r.order = kept
	registered := 0
	for _, e := range admitted {
		if r.suspendedLocked(e.name) {
			continue
		}
		if _, ok := r.tools[e.name]; !ok {
			r.order = append(r.order, e.name)
		}
		r.tools[e.name] = e.tool
		r.canon[e.name] = e.canon
		registered++
	}
	if removed > 0 || registered > 0 {
		r.schemaRev.Add(1)
	}
	return registered
}

// suspendedLocked reports whether name falls under a prefix this session turned
// off. Callers hold r.mu.
func (r *Registry) suspendedLocked(name string) bool {
	for prefix := range r.suspended {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
