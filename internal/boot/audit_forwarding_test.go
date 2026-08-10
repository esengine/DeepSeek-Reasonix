package boot

import (
	"reflect"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/notify"
	"reasonix/internal/stats"
	"reasonix/internal/trajectory"
)

// The shared forwarder is why a new capability no longer has to be repeated at
// every layer, so it must itself be complete.
func TestAuditForwarderCoversEveryCapability(t *testing.T) {
	fwd := reflect.TypeFor[event.AuditForwarder]()
	for name, want := range map[string]reflect.Type{
		"DelegationAuditSink":       reflect.TypeFor[event.DelegationAuditSink](),
		"ReadinessAuditSink":        reflect.TypeFor[event.ReadinessAuditSink](),
		"ContractShadowAuditSink":   reflect.TypeFor[event.ContractShadowAuditSink](),
		"CompletionReportAuditSink": reflect.TypeFor[event.CompletionReportAuditSink](),
		"MemoryRecallSink":          reflect.TypeFor[event.MemoryRecallSink](),
		"DelegationAdmissionSink":   reflect.TypeFor[event.DelegationAdmissionSink](),
		"OutcomeProgressSink":       reflect.TypeFor[event.OutcomeProgressSink](),
		"ProtocolRecoveryAuditSink": reflect.TypeFor[event.ProtocolRecoveryAuditSink](),
		"TurnCompletionSink":        reflect.TypeFor[event.TurnCompletionSink](),
	} {
		if !fwd.Implements(want) {
			t.Errorf("event.AuditForwarder does not forward %s; every embedder loses it", name)
		}
	}
}

// A wrapper that forwards one audit but forgets another drops its data
// silently, with the tests still green. Pure forwarders should embed
// event.AuditForwarder instead of repeating the nine methods.
func TestAuditCapabilitiesAreForwardedByEveryWrapper(t *testing.T) {
	reference := reflect.TypeFor[event.CompletionReportAuditSink]()
	required := map[string]reflect.Type{
		"DelegationAuditSink":       reflect.TypeFor[event.DelegationAuditSink](),
		"ReadinessAuditSink":        reflect.TypeFor[event.ReadinessAuditSink](),
		"ContractShadowAuditSink":   reflect.TypeFor[event.ContractShadowAuditSink](),
		"MemoryRecallSink":          reflect.TypeFor[event.MemoryRecallSink](),
		"DelegationAdmissionSink":   reflect.TypeFor[event.DelegationAdmissionSink](),
		"OutcomeProgressSink":       reflect.TypeFor[event.OutcomeProgressSink](),
		"ProtocolRecoveryAuditSink": reflect.TypeFor[event.ProtocolRecoveryAuditSink](),
		"TurnCompletionSink":        reflect.TypeFor[event.TurnCompletionSink](),
	}
	wrappers := []event.Sink{
		event.Sync(event.Discard),
		event.Coalesce(event.Discard, time.Millisecond),
		control.NewGoalUsageTee(event.Discard),
		notify.NewSink(event.Discard, nil, config.NotificationsConfig{}),
		&trajectory.Recorder{},
		&stats.Recorder{},
	}
	checked := 0
	for _, w := range wrappers {
		wt := reflect.TypeOf(w)
		if !wt.Implements(reference) {
			continue
		}
		checked++
		for name, want := range required {
			if !wt.Implements(want) {
				t.Errorf("%s forwards CompletionReportAudit but not %s: the audit dies at this wrapper", wt, name)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no wrapper was checked; the reference capability must have moved")
	}
}
