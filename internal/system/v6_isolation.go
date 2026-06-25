package system

import "strings"

type V6Isolation struct{}

var allowedV6DiagnosticSignals = map[string]struct{}{
	"LayerCollapseReport":    {},
	"CausalFieldSuggestion":  {},
	"ArchitectureSuggestion": {},
}

// Validate returns true only for v6-pre signals that are strictly diagnostic.
// v6 diagnostics may describe future architecture work, but they must not
// influence runtime execution in the v5 stable release.
func (V6Isolation) Validate(signal string) bool {
	_, allowed := allowedV6DiagnosticSignals[strings.TrimSpace(signal)]
	return allowed
}
