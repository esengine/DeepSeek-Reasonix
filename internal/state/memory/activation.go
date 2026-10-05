// Activation: whether a fact's body rides the stable session prefix (pinned)
// or stays retrieval-only (relevant), and the legacy-aware default resolution.
package memory

import "strings"

// Activation controls how a fact reaches the model, orthogonal to Scope:
// scope says where a fact may be used, activation says whether its body rides
// the stable prefix every session (pinned) or is retrieval-only (relevant).
type Activation string

const (
	ActivationRelevant Activation = "relevant" // retrieval-only: index + recall
	ActivationPinned   Activation = "pinned"   // body loads into the stable session prefix
)

// NormalizeActivation validates a persisted or requested activation. Empty and
// unknown values return "" (unset) so ResolveActivation can apply the
// legacy-aware default instead of silently inventing an explicit choice.
func NormalizeActivation(s string) Activation {
	switch Activation(strings.ToLower(strings.TrimSpace(s))) {
	case ActivationRelevant:
		return ActivationRelevant
	case ActivationPinned:
		return ActivationPinned
	}
	return ""
}

// ResolveActivation defaults an unset activation. Legacy global user/feedback
// facts predate the field and were always loaded as stable guidance, so they
// resolve to pinned until a rewrite records an explicit choice; everything
// else is relevant.
func ResolveActivation(m Memory) Activation {
	if a := NormalizeActivation(string(m.Activation)); a != "" {
		return a
	}
	if NormalizeFactScope(string(m.Scope)) == FactScopeGlobal {
		if t := NormalizeType(string(m.Type)); t == TypeUser || t == TypeFeedback {
			return ActivationPinned
		}
	}
	return ActivationRelevant
}
