package tool

import "maps"

// Restrict fixes the ceiling on what the registry may hold. Tools already
// registered that admit rejects are removed, and every later Add is checked
// against it. admit runs outside the registry lock: a lazy tool's own methods
// may take a lock that a registry swap holds while it waits for this one.
func (r *Registry) Restrict(admit func(Tool) bool) {
	if r == nil || admit == nil {
		return
	}
	r.mu.Lock()
	r.admit = admit
	held := maps.Clone(r.tools)
	r.mu.Unlock()
	rejected := map[string]bool{}
	for name, t := range held {
		if t != nil && !admit(t) {
			rejected[name] = true
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	kept := r.order[:0]
	for _, name := range r.order {
		if rejected[name] {
			delete(r.tools, name)
			delete(r.canon, name)
			continue
		}
		kept = append(kept, name)
	}
	r.order = kept
	r.schemaRev.Add(1)
}
