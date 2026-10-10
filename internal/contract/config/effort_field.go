package config

import "slices"

// EffortFieldForEntry is where a chosen effort level lands in this entry's
// request body, or "" when the entry's resolved reasoning protocol reshapes the
// request away from the wire's own field, or sends no effort at all.
func EffortFieldForEntry(e *ProviderEntry) string {
	if e == nil {
		return ""
	}
	p, ok := ProtocolFor(e.Kind)
	if !ok || p.EffortField == "" {
		return ""
	}
	resolved := ReasoningProtocolForEntry(e)
	if resolved == ReasoningProtocolNone {
		return ""
	}
	if len(p.EffortUnder) == 0 {
		return p.EffortField
	}
	if resolved == "" {
		if !p.EffortUnresolved || isMiniMaxEntry(e) || isZhipuEntry(e) || isLongCatEntry(e) {
			return ""
		}
		return p.EffortField
	}
	if slices.Contains(p.EffortUnder, resolved) {
		return p.EffortField
	}
	return ""
}
