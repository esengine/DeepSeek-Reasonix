package agent

import (
	"encoding/json"
	"errors"
	"strings"
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

// The blocked call that cost a real run four rounds had no ';', no pipe and no
// '||' — it nested a subshell, which the host cannot read at all. Naming a
// cause the command does not carry sends the model to edit the wrong thing.
func TestDeclaredCheckShapeMessageNamesTheRealShape(t *testing.T) {
	subshell := `tmp=$(mktemp -d) && cp ledger.py "$tmp/" && (cd "$tmp" && python3 ledger.py)`
	if msg := declaredCheckShapeMessage(subshell); !strings.Contains(msg, "subshell") {
		t.Errorf("subshell command got the separator message:\n%s", msg)
	}
	split := `python3 check.py; echo done`
	if msg := declaredCheckShapeMessage(split); !strings.Contains(msg, "';'") {
		t.Errorf("split command got the unreadable message:\n%s", msg)
	}
}

// The mixed-shape gate below already accepts a check the host reads out of
// PIPESTATUS; the declared-check gate refused the same shape. A run that
// declares `verifies` and pipes the suite into tail to keep 200 lines of output
// out of its context is doing the thing the declaration is for, and the host
// reads that stage's status directly rather than the pipeline's.
func TestDeclaredCheckClearsWhenTheHostReadsTheStage(t *testing.T) {
	gate := func(command string) bool {
		a := &Agent{}
		args, err := json.Marshal(map[string]string{"command": command, "verifies": "the suite passes"})
		if err != nil {
			t.Fatal(err)
		}
		plan := &toolCallPlan{evidenceName: "bash", evidenceArgs: args}
		_, blocked := a.applyShellShapeGates(t.Context(), plan)
		return blocked
	}
	for _, cmd := range []string{
		"go test ./handlers/ -run TestCreateOrder -v 2>&1 | tail -20",
		"python3 -m unittest discover -s tests 2>&1 | tail -5",
	} {
		if gate(cmd) {
			t.Errorf("blocked %q, but the host reads the verifier's own stage status", cmd)
		}
	}
	// The shapes the probe cannot answer for stay blocked: a `;` hands the
	// status on outside any pipeline, and no probe recovers it.
	for _, cmd := range []string{
		`python3 check.py; echo "exit=$?"`,
		`grep -rn foo pkgs/ > /dev/null; test $? -eq 1`,
	} {
		if !gate(cmd) {
			t.Errorf("allowed %q, but nothing here proves what the check exited with", cmd)
		}
	}
}
