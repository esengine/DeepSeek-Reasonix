package control

import "strings"

// RuntimeSelection is the immutable provider identity a controller was built
// with: canonical model ref, resolved effective effort, and the provider build
// fingerprint. A different identity is what requires a rebuild.
type RuntimeSelection struct {
	ModelRef            string
	Effort              string
	ProviderFingerprint string
}

// RuntimeSelectionMatcher is the controller capability frontends use to skip a
// rebuild when a resolved target is already the running generation.
type RuntimeSelectionMatcher interface {
	MatchesRuntimeSelection(RuntimeSelection) bool
}

// RuntimeSelection returns the immutable provider identity of this
// controller generation.
func (c *Controller) RuntimeSelection() RuntimeSelection {
	if c == nil {
		return RuntimeSelection{}
	}
	return RuntimeSelection{
		ModelRef:            strings.TrimSpace(c.modelRef),
		Effort:              normalizeRuntimeEffort(c.effort),
		ProviderFingerprint: strings.TrimSpace(c.providerFingerprint),
	}
}

// MatchesRuntimeSelection reports whether target names the running controller.
// The caller passes a canonical model ref, resolved effort, and provider
// fingerprint; this is the shared no-op comparison for ACP, serve, and CLI.
func (c *Controller) MatchesRuntimeSelection(target RuntimeSelection) bool {
	current := c.RuntimeSelection()
	target.ModelRef = strings.TrimSpace(target.ModelRef)
	target.Effort = normalizeRuntimeEffort(target.Effort)
	target.ProviderFingerprint = strings.TrimSpace(target.ProviderFingerprint)
	if current.ModelRef == "" || current.ProviderFingerprint == "" || target.ModelRef == "" || target.ProviderFingerprint == "" {
		return false
	}
	return current.ModelRef == target.ModelRef &&
		current.Effort == target.Effort &&
		current.ProviderFingerprint == target.ProviderFingerprint
}

var _ RuntimeSelectionMatcher = (*Controller)(nil)

func normalizeRuntimeEffort(effort string) string {
	effort = strings.ToLower(strings.TrimSpace(effort))
	if effort == "" || effort == "auto" {
		return "auto"
	}
	return effort
}
