package delegation

import (
	"context"
	"testing"

	"reasonix/internal/contract/event"
)

// Every sink this package hands to a child agent has to forward audits: a
// wrapper implementing only Emit answers no type assertion, so the child's
// records die there and the run reads as one that never produced them. This
// package has dug that hole twice, which is why the check is a list.
func TestChildAgentSinksForwardEveryAuditCapability(t *testing.T) {
	tracker := newSubagentProgressTracker(context.Background(), event.Discard)
	defer tracker.finish(nil, nil)

	for name, sink := range map[string]event.Sink{
		"subSinkFor":                   subSinkFor("parent", event.Discard),
		"subagentProgressTracker.wrap": tracker.wrap(),
	} {
		if missing := event.MissingCapabilities(sink); len(missing) > 0 {
			t.Errorf("%s drops %v; every audit a child produces dies there", name, missing)
		}
	}
}
