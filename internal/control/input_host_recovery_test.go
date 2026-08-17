package control

import (
	"testing"

	"reasonix/internal/agent"
)

func TestIsSyntheticUserMessageHostRecovery(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{name: "plain guidance", input: agent.HostRecoveryGuidanceToolFailedPrefix + ", continue unrelated work automatically."},
		{name: "persisted steer", input: agent.MidTurnSteerPrefix + "\n" + agent.HostRecoveryGuidanceToolFailedPrefix + ", continue unrelated work automatically."},
	}
	for _, tc := range cases {
		if !IsSyntheticUserMessage(tc.input) {
			t.Errorf("%s: IsSyntheticUserMessage() = false, want true", tc.name)
		}
	}
}
