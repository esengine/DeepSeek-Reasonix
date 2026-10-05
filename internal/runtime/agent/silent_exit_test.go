package agent

import (
	"encoding/json"
	"testing"
)

func bashArgs(t *testing.T, command string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// A search that matched nothing exits non-zero with an empty body. Reported as
// a bare error it reads as breakage, so the run tries the same search again;
// a verification or anything that may have written keeps reading as a failure.
func TestSilentExitIsAnAnswer(t *testing.T) {
	answers := []string{
		`grep -rn "missing" internal --include="*.go" | grep -v _test.go | head -20`,
		`test -f internal/runtime/agent/nothing.go`,
		`git diff --quiet`,
	}
	for _, command := range answers {
		if !silentExitIsAnAnswer("bash", bashArgs(t, command), "") {
			t.Errorf("%q: a silent non-zero exit was not read as an answer", command)
		}
	}
	failures := []struct {
		command string
		output  string
	}{
		{`go test ./internal/runtime/agent/ > /dev/null`, ""}, // a verification's status is its verdict
		{`go build ./... 2>/dev/null`, ""},                    // the host cannot prove it wrote nothing
		{`rm -rf build`, ""},
		{`grep -rn "x" internal`, "internal/runtime/agent/agent.go:1:x"}, // it printed something
	}
	for _, tc := range failures {
		if silentExitIsAnAnswer("bash", bashArgs(t, tc.command), tc.output) {
			t.Errorf("%q: a failure was excused as an answer", tc.command)
		}
	}
	if silentExitIsAnAnswer("read_file", bashArgs(t, "irrelevant"), "") {
		t.Error("a non-bash tool was read through the shell classifier")
	}
}

// A search that succeeded and found nothing used to reach the model as an empty
// result, which reads the same as a call that broke. The failing side already
// had a note for exactly this; the succeeding side is the more common one.
func TestASuccessfulSearchThatFoundNothingSaysSo(t *testing.T) {
	args := bashArgs(t, "find . -name '.mcp.json' -not -path './Library/*'")
	if got := silentSuccessDetail("bash", args, ""); got != silentExitNote {
		t.Fatalf("silentSuccessDetail = %q, want the note that says nothing matched", got)
	}
	if got := silentSuccessDetail("bash", args, "a.go\n"); got != "a.go\n" {
		t.Fatalf("output that exists was rewritten: %q", got)
	}
	// A call that may have written prints nothing all the time, and saying "no
	// match" about it would be a claim about a search that never happened.
	if got := silentSuccessDetail("bash", bashArgs(t, "npm install"), ""); got != "" {
		t.Fatalf("a possibly-writing command got the search note: %q", got)
	}
}
