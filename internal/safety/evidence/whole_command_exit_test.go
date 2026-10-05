package evidence

import "testing"

// A declared check is read off its exit status, so the status has to answer for
// the whole line: `;` hands it to the last statement, a pipe to the last stage,
// `||` to whichever side ran. Only a single command or an `&&` chain qualifies.
func TestWholeCommandExitConclusive(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    bool
	}{
		{"python3 ledger.py", true},
		{"make test", true},
		{"python3 ledger.py && cat balances.json", true},
		{"cd /tmp && python3 x.py", true},
		{"python3 check.py; echo ok", false},
		{"pytest -q | tail -15", false},
		{"python3 -m pytest tests/ -q 2>&1 | tail -20", false},
		{"go test ./... || true", false},
		{"python3 x.py &", false},
		{"", false},
	} {
		if got := WholeCommandExitConclusive(tc.command); got != tc.want {
			t.Errorf("WholeCommandExitConclusive(%q) = %v, want %v", tc.command, got, tc.want)
		}
	}
}
