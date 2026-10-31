package shellparse

import "testing"

func TestOnlyParsedExplicitVariablesAreSelected(t *testing.T) {
	for _, tc := range []struct {
		command  string
		selected bool
	}{
		{`echo "$REASONIX_SECRET_KEY"`, true},
		{`echo ${REASONIX_SECRET_KEY:-empty}`, true},
		{`echo '$REASONIX_SECRET_KEY'`, false},
		{`echo \$REASONIX_SECRET_KEY`, false},
		{`name=REASONIX_SECRET_KEY; echo "${!name}"`, false},
		{`echo "${!REASONIX_SECRET_KEY}"`, false},
		{`env`, false},
		{`echo "$REASONIX_SECRET_KEY`, false},
	} {
		if got := ExpandedVariables(tc.command)["REASONIX_SECRET_KEY"]; got != tc.selected {
			t.Errorf("%q selected=%v want=%v", tc.command, got, tc.selected)
		}
	}
}
