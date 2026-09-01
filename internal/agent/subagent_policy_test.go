package agent

import (
	"strings"
	"testing"
)

func TestNormalizeSubagentPolicy(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want SubagentPolicy
		ok   bool
	}{
		{"", SubagentPolicyLight, true},
		{"light", SubagentPolicyLight, true},
		{"  LIGHT  ", SubagentPolicyLight, true},
		{"balanced", SubagentPolicyBalanced, true},
		{"aggressive", SubagentPolicyAggressive, true},
		{"full", SubagentPolicyAggressive, true},
		{"turbo", "", false},
		{"", SubagentPolicyLight, true},
	} {
		got, err := NormalizeSubagentPolicy(tc.in)
		if (err == nil) != tc.ok {
			t.Fatalf("NormalizeSubagentPolicy(%q) err=%v, want ok=%v", tc.in, err, tc.ok)
		}
		if err == nil && got != tc.want {
			t.Fatalf("NormalizeSubagentPolicy(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestSubagentPolicyGuidance(t *testing.T) {
	if got := SubagentPolicyGuidance(SubagentPolicyLight); got != "" {
		t.Fatalf("light must inject nothing, got %q", got)
	}
	for _, p := range []SubagentPolicy{SubagentPolicyBalanced, SubagentPolicyAggressive} {
		got := SubagentPolicyGuidance(p)
		if !strings.HasPrefix(got, "<subagent-policy>"+string(p)+"\n") {
			t.Fatalf("%s guidance must open with its own block tag", p)
		}
		// 并行度提示必须解耦（不在 prompt 层）
		if strings.Contains(got, "parallel") && p == SubagentPolicyBalanced {
			t.Fatalf("balanced must not guide parallelism")
		}
		if p == SubagentPolicyAggressive && !strings.Contains(got, "30 seconds") {
			t.Fatalf("aggressive must carry the cost thresholds")
		}
	}
}

func TestStripTransientUserBlocksSubagentPolicy(t *testing.T) {
	content := "<subagent-policy>balanced\nDelegation guidance for this turn:\n- Delegate ...\n</subagent-policy>\n\nuser question"
	stripped := StripTransientUserBlocks(content)
	if strings.Contains(stripped, "subagent-policy") || strings.Contains(stripped, "Delegation guidance") {
		t.Fatalf("transient block must be stripped, got %q", stripped)
	}
	if !strings.Contains(stripped, "user question") {
		t.Fatalf("user text must survive, got %q", stripped)
	}
}
