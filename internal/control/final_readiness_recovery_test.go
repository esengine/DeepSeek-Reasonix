package control

import "testing"

func TestParseFinalReadinessRecoveryCommand(t *testing.T) {
	tests := []struct {
		input string
		ok    bool
		want  string
	}{
		{input: "/continue-checks", ok: true, want: defaultContinueChecksPrompt},
		{input: "  /continue-checks rerun only package tests  ", ok: true, want: "rerun only package tests"},
		{input: "/continue-checks-now", ok: false},
		{input: "continue checks", ok: false},
	}
	for _, test := range tests {
		got, ok := ParseFinalReadinessRecoveryCommand(test.input)
		if ok != test.ok || got != test.want {
			t.Errorf("ParseFinalReadinessRecoveryCommand(%q) = (%q, %v), want (%q, %v)", test.input, got, ok, test.want, test.ok)
		}
	}
}
