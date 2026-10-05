package control

import "testing"

func TestRuntimeSelectionMatchesCanonicalModelAndEffort(t *testing.T) {
	ctrl := New(Options{ModelRef: " provider/model ", Effort: " HIGH ", ProviderFingerprint: "provider-fp"})
	target := func(modelRef, effort, fingerprint string) RuntimeSelection {
		return RuntimeSelection{ModelRef: modelRef, Effort: effort, ProviderFingerprint: fingerprint}
	}

	if !ctrl.MatchesRuntimeSelection(target("provider/model", "high", "provider-fp")) {
		t.Fatal("canonical model and normalized effort should match the running selection")
	}
	if ctrl.MatchesRuntimeSelection(target("other/model", "high", "provider-fp")) {
		t.Fatal("a different model must not match the running selection")
	}
	if ctrl.MatchesRuntimeSelection(target("provider/model", "low", "provider-fp")) {
		t.Fatal("a different effort must not match the running selection")
	}
	if ctrl.MatchesRuntimeSelection(target("provider/model", "high", "other-fp")) {
		t.Fatal("a different provider build must not match the running selection")
	}
	if ctrl.MatchesRuntimeSelection(target("provider/model", "high", "")) {
		t.Fatal("an unknown provider build must fail closed")
	}
}

func TestRuntimeSelectionTreatsEmptyAndAutoEffortAlike(t *testing.T) {
	ctrl := New(Options{ModelRef: "provider/model", ProviderFingerprint: "provider-fp"})

	if !ctrl.MatchesRuntimeSelection(RuntimeSelection{ModelRef: "provider/model", Effort: "auto", ProviderFingerprint: "provider-fp"}) {
		t.Fatal("auto should match the default empty effort selection")
	}
	if !ctrl.MatchesRuntimeSelection(RuntimeSelection{ModelRef: "provider/model", Effort: "", ProviderFingerprint: "provider-fp"}) {
		t.Fatal("empty effort should match the default auto selection")
	}
}
