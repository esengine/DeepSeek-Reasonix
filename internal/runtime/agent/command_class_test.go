package agent

import "testing"

// The escalation path may only ever widen what runs by one narrow answer, so
// everything that is not a bare READONLY has to land on the refusing side.
func TestCommandClassOnlyAcceptsABareReadonly(t *testing.T) {
	accepted := []string{"READONLY", "readonly", "  ReadOnly \n"}
	for _, reply := range accepted {
		if !parseClassVerdict(reply, "READONLY") {
			t.Errorf("parseClassVerdict(%q) = false, want true", reply)
		}
	}
	refused := []string{
		"WRITES",
		"",
		"READONLY, but it depends on the flags",
		"probably READONLY",
		"READONLY WRITES",
		"I think this only reads",
		"READ", // truncated at the token cap
	}
	for _, reply := range refused {
		if parseClassVerdict(reply, "READONLY") {
			t.Errorf("parseClassVerdict(%q) = true, want false: only a bare verdict counts", reply)
		}
	}
}
