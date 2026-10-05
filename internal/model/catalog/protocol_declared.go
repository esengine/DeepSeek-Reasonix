// Which wires an endpoint says it serves, as opposed to which one its model
// listing looks like.
package catalog

import (
	"reasonix/internal/contract/config"
	"slices"
	"strings"

	"reasonix/internal/model/openai"
)

// ProtocolsDeclaredBy reads the wires a model listing names for itself. A relay
// serving Claude behind an OpenAI-shaped listing is invisible to discovery —
// the shape says "openai", nothing says the Anthropic door is open too — and
// such a relay usually says so per model. Empty when no row declares anything.
func ProtocolsDeclaredBy(listed []openai.ListedModel) []string {
	var out []string
	for _, m := range listed {
		for _, raw := range m.Endpoints {
			// The protocol table is the vocabulary. A listing naming something
			// else is naming a wire this build does not speak, and inventing a
			// mapping for it would be guessing at a word.
			kind := strings.ToLower(strings.TrimSpace(raw))
			if slices.Contains(out, kind) {
				continue
			}
			if p, known := config.ProtocolFor(kind); known {
				out = append(out, p.Kind)
			}
		}
	}
	return protocolsInDisplayOrder(out)
}

// protocolsInDisplayOrder sorts a kind set the way a chooser should offer it,
// so a declared list and a discovered one read the same on screen.
func protocolsInDisplayOrder(kinds []string) []string {
	out := make([]string, 0, len(kinds))
	for _, p := range config.Protocols() {
		if slices.Contains(kinds, p.Kind) {
			out = append(out, p.Kind)
		}
	}
	return out
}

// mergeProtocolChoices puts the declared wires and the discovered ones into one
// list. Discovery still contributes: a relay naming only "openai" is not
// claiming its Responses route is closed, it is answering a different question.
func mergeProtocolChoices(discovered, declared []string) []string {
	if len(declared) == 0 {
		return discovered
	}
	all := slices.Clone(discovered)
	for _, k := range declared {
		if !slices.Contains(all, k) {
			all = append(all, k)
		}
	}
	return protocolsInDisplayOrder(all)
}
