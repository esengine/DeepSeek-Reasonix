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

// A verdict has to be stable: the same segment feeds the evidence ledger, which
// is audited, so asking twice must not be able to answer differently.
func TestCommandClassCacheAnswersOnce(t *testing.T) {
	var c commandClassCache
	if _, ok := c.get("jq .name pkg.json"); ok {
		t.Fatal("empty cache reported a verdict")
	}
	c.put("jq .name pkg.json", true)
	got, ok := c.get("jq .name pkg.json")
	if !ok || !got {
		t.Fatalf("cache returned (%v, %v), want the stored verdict", got, ok)
	}
	c.put("rm -rf build", false)
	if got, ok := c.get("rm -rf build"); !ok || got {
		t.Fatalf("cache returned (%v, %v) for a writing command", got, ok)
	}
}

// Without a provider there is nothing to escalate to, and the answer must stay
// on the conservative side rather than defaulting to permissive.
func TestSegmentIsReadOnlyWithoutProviderRefuses(t *testing.T) {
	var a Agent
	if a.segmentIsReadOnly(t.Context(), "jq .name pkg.json") {
		t.Fatal("classified read-only with no provider to ask")
	}
	if (&Agent{}).segmentIsReadOnly(t.Context(), "") {
		t.Fatal("classified an empty segment as read-only")
	}
}
