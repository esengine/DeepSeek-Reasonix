package event

import (
	"reflect"
)

// capabilityContracts is the single authority on the optional capabilities a
// wrapper must forward. Delivery is by type assertion, so a wrapper missing one
// silently drops that channel for every sink behind it. Register new ones here.
var capabilityContracts = map[string]reflect.Type{
	"DelegationAudit":   reflect.TypeFor[DelegationAuditSink](),
	"MemoryRecall":      reflect.TypeFor[MemoryRecallSink](),
	"RunBudget":         reflect.TypeFor[RunBudgetSink](),
	"TurnCompletion":    reflect.TypeFor[TurnCompletionSink](),
	"UsageQuote":        reflect.TypeFor[UsageQuoter](),
	"WorkspaceMutation": reflect.TypeFor[WorkspaceMutationSink](),
	"UsageScope":        reflect.TypeFor[UsageScopeSource](),
}
