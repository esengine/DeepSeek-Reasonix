package agent

import (
	"encoding/json"
	"errors"
	"testing"

	"reasonix/internal/contract/tool"
)

func TestBashDeclaredCheck(t *testing.T) {
	for _, tc := range []struct {
		args string
		want string
	}{
		{`{"command":"make test","verifies":"the suite passes"}`, "the suite passes"},
		{`{"command":"make test","verifies":"  spaced  "}`, "spaced"},
		{`{"command":"make test"}`, ""},
		{`{"command":"make test","verifies":""}`, ""},
		{`{"command":"make test","verifies":123}`, ""},
		{`not json`, ""},
	} {
		if got := bashDeclaredCheck(json.RawMessage(tc.args)); got != tc.want {
			t.Errorf("bashDeclaredCheck(%s) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

// A declared check clears the whole-command shape gate before it runs, so its
// exit status is the verdict. Without the declaration the same command is only
// as conclusive as the name table finds it, which for a bare script is not.
func TestShellVerificationVerdictHonorsDeclaration(t *testing.T) {
	const script = "python3 ledger.py"
	if got := shellVerificationVerdict(script, true, nil, nil); got != tool.ShellVerificationPassed {
		t.Errorf("declared success = %q, want %q", got, tool.ShellVerificationPassed)
	}
	if got := shellVerificationVerdict(script, true, nil, errors.New("exit 1")); got != tool.ShellVerificationFailed {
		t.Errorf("declared failure = %q, want %q", got, tool.ShellVerificationFailed)
	}
	if got := shellVerificationVerdict(script, false, nil, nil); got != tool.ShellVerificationInconclusive {
		t.Errorf("undeclared script = %q, want %q", got, tool.ShellVerificationInconclusive)
	}
}
