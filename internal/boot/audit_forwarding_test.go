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
	required := reflect.TypeFor[event.OptionalSinkCapabilities]()
	for i := range required.NumMethod() {
		method := required.Method(i)
		if _, ok := fwd.MethodByName(method.Name); !ok {
			t.Errorf("event.AuditForwarder does not forward %s; every embedder loses it", method.Name)
		}
	}
}

// A wrapper that forwards one audit but forgets another drops its data
// silently, with the tests still green. Pure forwarders should embed
// event.AuditForwarder instead of repeating the capability methods.
func TestAuditCapabilitiesAreForwardedByEveryWrapper(t *testing.T) {
	required := reflect.TypeFor[event.OptionalSinkCapabilities]()
	wrappers := []event.Sink{
		event.Sync(event.Discard),
		event.Coalesce(event.Discard, time.Millisecond),
		event.NewCostQuoteSink(event.Discard, nil),
		control.NewGoalUsageTee(event.Discard),
		notify.NewSink(event.Discard, nil, config.NotificationsConfig{}),
		&trajectory.Recorder{},
		&stats.Recorder{},
	}
	for _, w := range wrappers {
		wt := reflect.TypeOf(w)
		for i := range required.NumMethod() {
			method := required.Method(i)
			if _, ok := wt.MethodByName(method.Name); !ok {
				t.Errorf("%s does not forward %s: the host signal dies at this wrapper", wt, method.Name)
			}
		}
	}
}
