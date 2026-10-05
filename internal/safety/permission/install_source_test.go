package permission

import (
	"encoding/json"
	"testing"
)

func ticketArgs(t *testing.T, planID string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"source": "x", "apply": true, "planId": planID})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// The grade rides in front of the plan hash, so the fallback mode covers the
// two lower ones and stops at the one that runs code the user did not write.
func TestPlanTicketGradeDecidesWhatABlanketAllowCovers(t *testing.T) {
	blanket := New("allow", nil, nil, nil)
	for _, ticket := range []string{"low:sha256:abc", "medium:sha256:abc"} {
		if got := blanket.Decide("install_source", false, ticketArgs(t, ticket)); got != Allow {
			t.Errorf("Decide(%q) = %v, want Allow", ticket, got)
		}
	}
	if got := blanket.Decide("install_source", false, ticketArgs(t, "high:sha256:abc")); got != Ask {
		t.Errorf("Decide(high) = %v, want Ask even under a blanket allow", got)
	}
}

// Deny still wins: the escape hatch for a high ticket is an allow rule, never a
// weaker posture, and nothing here can talk past an explicit refusal.
func TestPlanTicketRulesKeepPrecedence(t *testing.T) {
	denied := New("allow", []string{"install_source(high:*)"}, nil, []string{"install_source"})
	if got := denied.Decide("install_source", false, ticketArgs(t, "high:sha256:abc")); got != Deny {
		t.Errorf("Decide with a deny rule = %v, want Deny", got)
	}
	asked := New("allow", nil, []string{"install_source(low:*)"}, nil)
	if got := asked.Decide("install_source", false, ticketArgs(t, "low:sha256:abc")); got != Ask {
		t.Errorf("Decide with an ask rule on a low ticket = %v, want Ask", got)
	}
}

// Uninstall carries no ticket and gets no special grade: it takes a capability
// away rather than granting one, and the package it removes can be installed
// again. That makes it an ordinary write, decided by the fallback mode.
func TestUninstallIsAnOrdinaryWrite(t *testing.T) {
	raw, err := json.Marshal(map[string]any{"op": "uninstall", "name": "pack"})
	if err != nil {
		t.Fatal(err)
	}
	if got := New("allow", nil, nil, nil).Decide("install_source", false, raw); got != Allow {
		t.Errorf("uninstall under allow = %v, want Allow", got)
	}
	if got := New("ask", nil, nil, nil).Decide("install_source", false, raw); got != Ask {
		t.Errorf("uninstall under ask = %v, want Ask", got)
	}
	if InstallSourceIsPlanOnly(raw) {
		t.Error("uninstall was read as a preview; it has none")
	}
}
