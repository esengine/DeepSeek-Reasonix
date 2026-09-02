package cli

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestForceSyncOutputOnlyOverSSH pins the #8405 workaround: synchronized
// output is forced on for SSH sessions (where bubbletea skips its capability
// query) and stays off locally unless the terminal itself reports support.
func TestForceSyncOutputOnlyOverSSH(t *testing.T) {
	for _, k := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY", "REASONIX_DISABLE_SYNC_OUTPUT"} {
		t.Setenv(k, "")
	}
	if cmd := forceSyncOutputCmd(); cmd != nil {
		t.Fatal("local sessions must not force synchronized output")
	}
	t.Setenv("SSH_TTY", "/dev/pts/3")
	cmd := forceSyncOutputCmd()
	if cmd == nil {
		t.Fatal("SSH sessions must force synchronized output")
	}
	msg := cmd()
	report, ok := msg.(tea.ModeReportMsg)
	if !ok || report.Mode != ansi.ModeSynchronizedOutput || report.Value != ansi.ModeReset {
		t.Fatalf("forceSyncOutputCmd() = %#v, want a 2026 mode report with reset value", msg)
	}
	t.Setenv("REASONIX_DISABLE_SYNC_OUTPUT", "1")
	if cmd := forceSyncOutputCmd(); cmd != nil {
		t.Fatal("REASONIX_DISABLE_SYNC_OUTPUT=1 must opt out")
	}
}
