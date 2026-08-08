package skill

import (
	"strings"
	"testing"
)

func TestBuiltinEvolveIsInlineSkill(t *testing.T) {
	st := New(Options{HomeDir: t.TempDir()})
	sk, ok := st.Read("evolve")
	if !ok {
		t.Fatal("built-in evolve skill not found")
	}
	if sk.Scope != ScopeBuiltin || sk.RunAs != RunInline {
		t.Errorf("evolve should be a builtin inline skill, got scope=%s runAs=%s", sk.Scope, sk.RunAs)
	}
	if _, listed := find(st.List(), "evolve"); !listed {
		t.Error("evolve should appear in List() so it reaches the slash menu")
	}
	body := sk.Body
	for _, want := range []string{
		"history",
		"L0",
		"L1",
		"session_path",
		"message_index",
		"do NOT call remember",
		"next session",
		"**Why:**",
		"**How to apply:**",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("evolve body missing %q", want)
		}
	}
	if !strings.Contains(sk.Description, "L0") {
		t.Errorf("description should mention L0 default: %q", sk.Description)
	}
	if len(sk.Triggers) == 0 {
		t.Error("evolve should declare triggers")
	}
}
