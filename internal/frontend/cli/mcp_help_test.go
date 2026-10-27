package cli

import (
	"strings"
	"testing"
)

func TestMCPAddHelpPrintsUsage(t *testing.T) {
	var code int
	stdout, stderr := captureCLIOutput(t, func() {
		code = Run([]string{"mcp", "add", "--help"}, "test")
	})
	if code != 0 {
		t.Fatalf("mcp add --help exited %d, want 0\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "reasonix mcp add") || stderr != "" {
		t.Fatalf("mcp add --help output omitted usage or wrote to stderr\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestMCPListAndGetRejectExtraArguments(t *testing.T) {
	for _, args := range [][]string{
		{"mcp", "list", "--jsno"},
		{"mcp", "ls", "extra"},
		{"mcp", "get", "headroom", "--jsno"},
	} {
		var code int
		stdout, stderr := captureCLIOutput(t, func() {
			code = Run(args, "test")
		})
		if code != 2 || stdout != "" || stderr == "" {
			t.Fatalf("%v exited %d, want 2 with an error\nstdout:\n%s\nstderr:\n%s", args, code, stdout, stderr)
		}
	}
}
