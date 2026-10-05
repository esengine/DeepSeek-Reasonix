package boot

import (
	"reflect"
	"testing"
	"time"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/platform/notify"
	"reasonix/internal/session/control"
	"reasonix/internal/state/stats"
	"reasonix/internal/state/trajectory"
)

// A wrapper that forwards one audit but forgets another drops its data
// silently, with the tests still green. Pure forwarders should embed
// event.AuditForwarder instead of repeating the nine methods.
func TestAuditCapabilitiesAreForwardedByEveryWrapper(t *testing.T) {
	reference := reflect.TypeFor[event.CompletionReportAuditSink]()
	required := map[string]reflect.Type{
		"DelegationAuditSink":       reflect.TypeFor[event.DelegationAuditSink](),
		"ReadinessAuditSink":        reflect.TypeFor[event.ReadinessAuditSink](),
		"ContractShadowAuditSink":   reflect.TypeFor[event.ContractShadowAuditSink](),
		"EvidenceBundleAuditSink":   reflect.TypeFor[event.EvidenceBundleAuditSink](),
		"MemoryRecallSink":          reflect.TypeFor[event.MemoryRecallSink](),
		"OutcomeProgressSink":       reflect.TypeFor[event.OutcomeProgressSink](),
		"ProtocolRecoveryAuditSink": reflect.TypeFor[event.ProtocolRecoveryAuditSink](),
		"TurnCompletionSink":        reflect.TypeFor[event.TurnCompletionSink](),
		"SubagentHandoffSink":       reflect.TypeFor[event.SubagentHandoffSink](),
	}
	wrappers := []event.Sink{
		event.Sync(event.Discard),
		event.Coalesce(event.Discard, time.Millisecond),
		control.NewGoalUsageTee(event.Discard),
		notify.NewSink(event.Discard, nil, i18n.English, notify.NewSettings(config.NotificationsConfig{})),
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
