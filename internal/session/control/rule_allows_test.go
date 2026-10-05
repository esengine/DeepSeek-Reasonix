package control

import (
	"encoding/json"
	"testing"

	"reasonix/internal/safety/permission"
)

func bashCall(t *testing.T, command string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func allowRule(t *testing.T, s string) permission.Rule {
	t.Helper()
	r, ok := permission.ParseRule(s)
	if !ok {
		t.Fatalf("ParseRule(%q) failed", s)
	}
	return r
}

// A rule the user saved answers for the call, so the host never has to ask a
// model about a command a person already decided.
func TestRuleAllowsHonorsASavedRule(t *testing.T) {
	gate := NewSharedHeadlessGate(permission.Policy{Allow: []permission.Rule{allowRule(t, "bash(sed:*)")}}, "ask")
	if !gate.RuleAllows("bash", bashCall(t, "sed -n '1,5p' out.txt"), false) {
		t.Fatal("a saved allow rule did not answer for the call")
	}
	if gate.RuleAllows("bash", bashCall(t, "rm -rf build"), false) {
		t.Fatal("a call outside every rule was reported as already allowed")
	}
}

// The fallback mode is a posture, not an answer: auto-approving writers must
// not read as "the user decided this one".
func TestRuleAllowsIgnoresTheFallbackMode(t *testing.T) {
	gate := NewSharedHeadlessGate(permission.Policy{Mode: permission.Allow}, "auto")
	if gate.RuleAllows("bash", bashCall(t, "jq .name pkg.json"), false) {
		t.Fatal("auto mode stood in for a user decision")
	}
}
