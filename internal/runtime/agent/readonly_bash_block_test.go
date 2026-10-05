package agent

import (
	"strings"
	"testing"
)

// A refusal that does not name the disqualifying shape makes the sub-agent
// rewrite whatever it guesses is at fault, which is how one block becomes three.
func TestReadOnlyBashBlockNamesTheShape(t *testing.T) {
	cases := []struct {
		command string
		want    string
	}{
		{`python3 -c "print(1)"`, "inline interpreter code"},
		{"grep -c foo $(ls)", "nests another command"},
		{`grep "unclosed internal/`, "could not read"},
		{"rm -rf build", "read-only command set"},
	}
	for _, c := range cases {
		got := readOnlyBashBlockReason(c.command)
		if !strings.Contains(got, c.want) {
			t.Errorf("readOnlyBashBlockReason(%q) = %q, want it to name %q", c.command, got, c.want)
		}
		if !strings.Contains(got, "read_file") {
			t.Errorf("readOnlyBashBlockReason(%q) = %q, want a usable alternative named", c.command, got)
		}
	}
}
