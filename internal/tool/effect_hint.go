package tool

import "encoding/json"

// EffectHint is a host-only, per-call effect preview. It is never serialized
// into a provider tool schema and must not change registration order.
type EffectHint struct {
	Known        bool
	ReadOnly     bool
	Destructive  bool
	Privileged   bool
	UsesNetwork  bool
	ExecutesCode bool
	Targets      []string
}

// EffectHintProvider is an optional host-only Tool capability. Tools that omit
// it are classified from parsed arguments, receipts, and static ReadOnly().
type EffectHintProvider interface {
	EffectHint(args json.RawMessage) EffectHint
}
