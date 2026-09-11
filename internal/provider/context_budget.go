package provider

// Context window accounting for local admission. Unknown never assumes a
// shared prompt+completion window; Independent disables shared-window clipping.
type ContextWindowMode uint8

const (
	ContextWindowUnknown ContextWindowMode = iota
	ContextWindowShared
	ContextWindowIndependent
)

// How a provider places an output ceiling on the wire.
type OutputLimitMode uint8

const (
	OutputLimitOmitWhenSafe OutputLimitMode = iota
	OutputLimitAlways
	OutputLimitRequired
	OutputLimitUnsupported
)

// ContextBudgetPolicy is the provider-owned window and output-limit contract.
// Zero Auto/Max means the value is unknown. It never feeds compact_ratio.
type ContextBudgetPolicy struct {
	WindowMode       ContextWindowMode
	AutoOutputTokens int
	MaxOutputTokens  int
	LimitMode        OutputLimitMode
}

// ContextBudgetPolicyProvider reports the unified budget contract. Existing
// OutputBudgetProvider and SharedWindowOutputProvider implementations remain
// valid fallbacks through ResolveContextBudgetPolicy.
type ContextBudgetPolicyProvider interface {
	ContextBudgetPolicy() ContextBudgetPolicy
}

const (
	ContextBudgetSourceUnknown  = "unknown"
	ContextBudgetSourceExplicit = "explicit"
	ContextBudgetSourceOfficial = "official"
	ContextBudgetSourceOpenCode = "opencode"
	ContextBudgetSourceLearned  = "learned"
)

func (m ContextWindowMode) String() string {
	switch m {
	case ContextWindowShared:
		return "shared"
	case ContextWindowIndependent:
		return "independent"
	default:
		return "unknown"
	}
}

func (m OutputLimitMode) String() string {
	switch m {
	case OutputLimitAlways:
		return "always"
	case OutputLimitRequired:
		return "required"
	case OutputLimitUnsupported:
		return "unsupported"
	default:
		return "omit_when_safe"
	}
}

// ResolveContextBudgetPolicy prefers the new capability and otherwise maps the
// older output-budget interfaces so tests, fork wrappers, and unmigrated
// adapters keep working.
func ResolveContextBudgetPolicy(p Provider) ContextBudgetPolicy {
	if p == nil {
		return ContextBudgetPolicy{}
	}
	if owned, ok := p.(ContextBudgetPolicyProvider); ok {
		return owned.ContextBudgetPolicy()
	}
	policy := ContextBudgetPolicy{WindowMode: ContextWindowUnknown, LimitMode: OutputLimitOmitWhenSafe}
	if shared, ok := p.(SharedWindowOutputProvider); ok && shared.SharesContextWindow() {
		policy.WindowMode = ContextWindowShared
	}
	if budget, ok := p.(OutputBudgetProvider); ok {
		n := budget.OutputBudget()
		if n > 0 {
			policy.AutoOutputTokens = n
			policy.MaxOutputTokens = n
			if policy.WindowMode == ContextWindowShared {
				policy.LimitMode = OutputLimitAlways
			}
		}
	}
	return policy
}
