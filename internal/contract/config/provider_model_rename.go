// provider_model_rename.go — pointing one entry's model id at another.
package config

import "slices"

// renameProviderModel replaces a model id throughout one provider entry: the
// fields that name it and the maps keyed by it. A price or an effort list left
// under the old key would stop answering for the model that replaced it, which
// reads downstream as an edited setting reverting to its default.
func renameProviderModel(p *ProviderEntry, from, to string) bool {
	if p == nil || from == "" || to == "" || from == to {
		return false
	}
	changed := false
	if p.Model == from {
		p.Model, changed = to, true
	}
	if p.Default == from {
		p.Default, changed = to, true
	}
	for _, list := range []*[]string{&p.Models, &p.VisionModels} {
		if renamed, ok := renameInModelList(*list, from, to); ok {
			*list, changed = renamed, true
		}
	}
	if renameMapKey(p.Prices, from, to) {
		changed = true
	}
	if renameMapKey(p.ModelOverrides, from, to) {
		changed = true
	}
	return changed
}

// renameInModelList dedupes as it renames: an entry naming both ids would
// otherwise come out listing the surviving model twice.
func renameInModelList(list []string, from, to string) ([]string, bool) {
	if !slices.Contains(list, from) {
		return list, false
	}
	out := make([]string, 0, len(list))
	for _, model := range list {
		if model == from {
			model = to
		}
		if !slices.Contains(out, model) {
			out = append(out, model)
		}
	}
	return out, true
}

// renameMapKey moves one key's value. A value already under the new key wins:
// it was chosen for the model that survives, the other for the one that did not.
func renameMapKey[V any](m map[string]V, from, to string) bool {
	v, ok := m[from]
	if !ok {
		return false
	}
	if _, taken := m[to]; !taken {
		m[to] = v
	}
	delete(m, from)
	return true
}
