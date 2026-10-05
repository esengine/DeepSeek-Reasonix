package coordinator

import (
	"testing"

	"reasonix/internal/contract/event"
)

// A sink that implements only Emit answers no audit type assertion, so the
// planner's audits would die there.
func TestPlannerSinkForwardsEveryAuditCapability(t *testing.T) {
	if missing := event.MissingCapabilities(plannerSink(event.Discard)); len(missing) > 0 {
		t.Errorf("plannerSink drops %v; every audit the planner produces dies there", missing)
	}
}
