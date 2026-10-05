package delegation

import (
	"context"
	"reasonix/internal/runtime/agent"
	"testing"
)

// A sub-agent's "user turns" are the parent's instructions, and nothing else in
// the child transcript records them — so the protection has to travel down the
// one construction point sub-agents share. This pins the inheritance rather than
// the mechanism, which compact_partition_test.go already covers.
func TestSubagentOptionsInheritUserTurnRetention(t *testing.T) {
	parent := &TaskTool{keepPolicy: agent.KeepErrors | agent.KeepUserMarked, recentKeep: 2, compactRatio: 0.85}
	opts := parent.subagentOptions(context.Background(), 8, nil, 32_000, 1, "", nil)
	if opts.KeepPolicy != agent.KeepErrors|agent.KeepUserMarked {
		t.Fatalf("child KeepPolicy = %v, want the parent's", opts.KeepPolicy)
	}
	if opts.ContextWindow != 32_000 {
		t.Fatalf("child ContextWindow = %d, want the resolved sub-session window", opts.ContextWindow)
	}

}
