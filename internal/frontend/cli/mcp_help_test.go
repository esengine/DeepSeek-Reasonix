package cli

import (
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
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

func TestMCPHelpFlagPrintsUsageForEverySubcommandAndRunsNothing(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("REASONIX_HOME", home)
	for _, sub := range []string{"list", "ls", "add", "get", "remove", "rm", "enable", "disable", "retry", "connect", "trust", "auth", "authorize", "update", "import", "browse", "search", "install"} {
		for _, flag := range []string{"--help", "-h"} {
			var code int
			stdout, stderr := captureCLIOutput(t, func() {
				code = Run([]string{"mcp", sub, flag}, "test")
			})
			if code != 0 || !strings.Contains(stdout, "reasonix mcp") || stderr != "" || strings.Contains(stdout, "imported") {
				t.Errorf("mcp %s %s: exit %d, want 0 with usage only\nstdout:\n%s\nstderr:\n%s", sub, flag, code, stdout, stderr)
			}
		}
	}
}

func TestMCPImportRejectsArgumentsInsteadOfImporting(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("REASONIX_HOME", home)
	var code int
	stdout, stderr := captureCLIOutput(t, func() {
		code = Run([]string{"mcp", "import", "--nope"}, "test")
	})
	if code != 2 || stdout != "" || stderr == "" {
		t.Fatalf("mcp import --nope exited %d, want 2 with an error\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
}
