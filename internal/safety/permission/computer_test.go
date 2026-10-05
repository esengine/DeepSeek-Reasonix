package permission

import (
	"encoding/json"
	"testing"
)

func TestComputerGrantNamesTheApplicationAndCoversReadingAndOperatingIt(t *testing.T) {
	const notes = "com.apple.Notes"
	for _, scope := range []func(string, string) string{SessionGrantRuleForScope, RememberRuleForScope} {
		rule := scope("computer_act", notes)
		if rule != "Computer="+notes {
			t.Fatalf("grant rule = %q, want Computer=%s", rule, notes)
		}
		for _, tool := range []string{"computer_read", "computer_act"} {
			if !SessionGrantMatches(rule, tool, notes) {
				t.Errorf("%s on %s is not covered by %q", tool, notes, rule)
			}
		}
		if SessionGrantMatches(rule, "computer_act", "com.apple.Notes.helper") {
			t.Errorf("%q covered another application", rule)
		}
		if SessionGrantMatches(rule, "browser_act", notes) {
			t.Errorf("%q covered a tool that is not computer use", rule)
		}
	}
	if SessionGrantMatches("Computer", "computer_act", notes) {
		t.Fatal("a bare Computer grant answered for an application nobody approved")
	}
}

func TestComputerDecisionsAnswerOnlyToARuleNamingTheApplication(t *testing.T) {
	args, _ := json.Marshal(map[string]any{"steps": []any{}, "app": "com.apple.Notes"})
	cases := []struct {
		name   string
		policy Policy
		want   Decision
	}{
		{"auto", New("allow", nil, nil, nil), Ask},
		{"ask", New("ask", nil, nil, nil), Ask},
		{"a bare rule", New("allow", []string{"Computer", "computer_act"}, nil, nil), Ask},
		{"a glob", New("allow", []string{"Computer(com.apple.*)"}, nil, nil), Ask},
		{"the application", New("ask", []string{"Computer=com.apple.Notes"}, nil, nil), Allow},
		{"another application", New("ask", []string{"Computer=com.apple.TextEdit"}, nil, nil), Ask},
		{"a denied application", New("allow", []string{"Computer=com.apple.Notes"}, nil, []string{"Computer(com.apple.*)"}), Deny},
		{"deny mode", New("deny", nil, nil, nil), Deny},
	}
	for _, tc := range cases {
		for _, tool := range []string{"computer_read", "computer_act"} {
			if got := tc.policy.Decide(tool, false, args); got != tc.want {
				t.Errorf("%s: %s decision = %v, want %v", tc.name, tool, got, tc.want)
			}
		}
	}
}

// Taking the person's pointer is not the same thing as operating the
// application, and the grant for one does not answer for the other.
func TestTakingThePointerIsAnswearedApartFromTheApplication(t *testing.T) {
	const notes = "com.apple.Notes"
	pointer := ComputerPointerPrefix + notes
	args, _ := json.Marshal(map[string]any{"app": pointer, "steps": []any{}})
	if got := Subjects(args); len(got) != 2 || got[0] != pointer || got[1] != notes {
		t.Fatalf("subjects = %v, want the pointer subject then its application", got)
	}
	cases := []struct {
		name   string
		policy Policy
		want   Decision
	}{
		{"auto", New("allow", nil, nil, nil), Ask},
		{"the application granted", New("ask", []string{"Computer=" + notes}, nil, nil), Ask},
		{"a glob", New("allow", []string{"Computer(*)"}, nil, nil), Ask},
		// Both are answered: taking the pointer over an application is also
		// operating it, and each has to have been agreed to.
		{"the pointer subject and its application", New("allow", []string{"Computer=" + pointer, "Computer=" + notes}, nil, nil), Allow},
		{"only the pointer subject", New("allow", []string{"Computer=" + pointer}, nil, nil), Ask},
		{"the application denied", New("allow", []string{"Computer=" + pointer}, nil, []string{"Computer(" + notes + ")"}), Deny},
		{"deny mode", New("deny", nil, nil, nil), Deny},
	}
	for _, tc := range cases {
		if got := tc.policy.Decide("computer_act", false, args); got != tc.want {
			t.Errorf("%s: decision = %v, want %v", tc.name, got, tc.want)
		}
	}
}
