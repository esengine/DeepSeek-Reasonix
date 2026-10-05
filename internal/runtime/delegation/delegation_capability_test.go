package delegation

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestDelegationCountIsAgentsNotSteps pins the distinction the UI got wrong: a
// batch dispatcher reports how many contexts it opened, and a malformed or
// empty batch reports none rather than inventing one.
func TestDelegationCountIsAgentsNotSteps(t *testing.T) {
	for _, tc := range []struct {
		args string
		want int
	}{
		{`{"tasks":[{"prompt":"a"},{"prompt":"b"}]}`, 2},
		{`{"tasks":[]}`, 0},
		{`{`, 0},
	} {
		got := batchDelegation("fleet", json.RawMessage(tc.args))
		if got == nil || got.Count != tc.want {
			t.Errorf("fleet %s reported %v, want count %d", tc.args, got, tc.want)
		}
		if got != nil && !strings.EqualFold(got.Name, "fleet") {
			t.Errorf("fleet dispatch named %q", got.Name)
		}
	}
}
