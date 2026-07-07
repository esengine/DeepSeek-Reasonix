package builtin

import (
	"os"
	"testing"

	"reasonix/internal/sandbox"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == sandbox.WindowsHelperCommand {
		os.Exit(sandbox.RunWindowsSandboxHelper(os.Args[2:], os.Stdin, os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}
