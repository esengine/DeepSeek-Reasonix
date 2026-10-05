package event

import (
	"maps"
	"reflect"
	"slices"
)

// capabilityContracts is the single authority on the optional capabilities a
// wrapper must forward. Delivery is by type assertion, so a wrapper missing one
// silently drops that channel for every sink behind it. Register new ones here.
var capabilityContracts = map[string]reflect.Type{
	"CompletionReport":  reflect.TypeFor[CompletionReportAuditSink](),
	"ContractShadow":    reflect.TypeFor[ContractShadowAuditSink](),
	"DelegationAudit":   reflect.TypeFor[DelegationAuditSink](),
	"EvidenceBundle":    reflect.TypeFor[EvidenceBundleAuditSink](),
	"MemoryRecall":      reflect.TypeFor[MemoryRecallSink](),
	"OutcomeProgress":   reflect.TypeFor[OutcomeProgressSink](),
	"ProjectCheckProbe": reflect.TypeFor[ProjectCheckProbeSink](),
	"ProtocolRecovery":  reflect.TypeFor[ProtocolRecoveryAuditSink](),
	"ReadinessAudit":    reflect.TypeFor[ReadinessAuditSink](),
	"RunBudget":         reflect.TypeFor[RunBudgetSink](),
	"SubagentHandoff":   reflect.TypeFor[SubagentHandoffSink](),
	"TurnCompletion":    reflect.TypeFor[TurnCompletionSink](),
	"VerificationDrift": reflect.TypeFor[VerificationContractDriftSink](),
	"WorkspaceMutation": reflect.TypeFor[WorkspaceMutationSink](),
}

// CapabilityNames lists every optional capability in stable order.
func CapabilityNames() []string {
	out := slices.Sorted(maps.Keys(capabilityContracts))
	return out
}

// MissingCapabilities names the optional capabilities s does not implement.
// A pass-through wrapper must report none: anything it omits is an audit
// channel that dies at that link.
func MissingCapabilities(s Sink) []string {
	if s == nil {
		return CapabilityNames()
	}
	t := reflect.TypeOf(s)
	var out []string
	for _, name := range CapabilityNames() {
		if !t.Implements(capabilityContracts[name]) {
			out = append(out, name)
		}
	}
	return out
}
