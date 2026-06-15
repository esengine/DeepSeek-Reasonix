package agent_test

import (
	"reasonix/internal/agent"
	"testing"
)

func TestStripTransientBlocksActiveGoal(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"<active-goal>\nFix all bugs\n</active-goal>\n\nfix the auth bug", "fix the auth bug"},
		{"<reasoning-language>\nuse Chinese\n</reasoning-language>\n\n<active-goal>\nDo X\n</active-goal>\n\nhelp me", "help me"},
		{"<memory-update>\n- note\n</memory-update>\n\n<active-goal>\nGoal text\n</active-goal>\n\ndo it", "do it"},
		{"<active-goal>\n\nmulti-line\ngoal\n</active-goal>\n\nuser text", "user text"},
	}
	for _, tt := range tests {
		got := agent.StripTransientUserBlocks(tt.in)
		if got != tt.want {
			t.Errorf("StripTransientUserBlocks(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
