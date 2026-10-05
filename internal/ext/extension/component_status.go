package extension

import "time"

// ComponentStatus is the diagnostic view of one lifecycle component.
type ComponentStatus struct {
	ID          ComponentID    `json:"id"`
	State       ComponentState `json:"state"`
	Generation  uint64         `json:"generation"`
	Epoch       string         `json:"epoch,omitempty"`
	Error       string         `json:"error,omitempty"`
	Diagnostics []string       `json:"diagnostics,omitempty"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

// RuntimeStatus is the host-facing runtime component status document for
// /doctor and UI diagnostics.
type RuntimeStatus struct {
	PublishedGeneration uint64            `json:"publishedGeneration"`
	Components          []ComponentStatus `json:"components"`
	Plan                *RuntimePlanView  `json:"plan,omitempty"`
	Receipts            []EffectReceipt   `json:"receipts,omitempty"`
}

// RuntimePlanView is a JSON-friendly projection of RuntimePlan without the
// live graph pointer.
type RuntimePlanView struct {
	FromGeneration  uint64        `json:"fromGeneration"`
	ToGeneration    uint64        `json:"toGeneration"`
	Added           []ComponentID `json:"added,omitempty"`
	Removed         []ComponentID `json:"removed,omitempty"`
	Reloaded        []ComponentID `json:"reloaded,omitempty"`
	Unchanged       []ComponentID `json:"unchanged,omitempty"`
	PrefixChanged   bool          `json:"prefixChanged"`
	ProviderChanged bool          `json:"providerChanged"`
}

// PlanView projects a plan for diagnostics APIs.
func PlanView(p *RuntimePlan) *RuntimePlanView {
	if p == nil {
		return nil
	}
	return &RuntimePlanView{
		FromGeneration:  p.FromGeneration,
		ToGeneration:    p.ToGeneration,
		Added:           append([]ComponentID(nil), p.Added...),
		Removed:         append([]ComponentID(nil), p.Removed...),
		Reloaded:        append([]ComponentID(nil), p.Reloaded...),
		Unchanged:       append([]ComponentID(nil), p.Unchanged...),
		PrefixChanged:   p.PrefixChanged,
		ProviderChanged: p.ProviderChanged,
	}
}
