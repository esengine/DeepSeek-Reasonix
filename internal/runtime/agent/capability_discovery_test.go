package agent

import (
	"reasonix/internal/runtime/usecap"
	"strings"
	"testing"
)

// Every id the description names must be one the examples list checks.
func TestEveryNamedExampleIsReachable(t *testing.T) {
	desc := (*usecap.UseCapabilityTool)(nil).Description()
	for _, id := range usecap.CapabilityIDExamples {
		if !strings.Contains(desc, id) {
			t.Errorf("CapabilityIDExamples lists %q but the description never names it", id)
		}
	}
}
