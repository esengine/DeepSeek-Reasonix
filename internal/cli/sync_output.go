package cli

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// forceSyncOutputCmd turns on the renderer's synchronized-output (DEC mode
// 2026) wrapping for SSH sessions. bubbletea skips its capability query over
// SSH because DECRPM replies are unreliable through some gateways, which
// leaves every repaint unsynchronized across the extra transport hop and
// reads as flicker while typing (local Windows Terminal into a remote pwsh7,
// #8405). Reporting the mode the way a supporting terminal would flips the
// renderer on; terminals without 2026 ignore the per-frame sequences by spec.
// REASONIX_DISABLE_SYNC_OUTPUT=1 opts out.
func forceSyncOutputCmd() tea.Cmd {
	if !remoteClipboardSession() {
		return nil
	}
	if v := strings.TrimSpace(os.Getenv("REASONIX_DISABLE_SYNC_OUTPUT")); v != "" && v != "0" {
		return nil
	}
	return func() tea.Msg {
		return tea.ModeReportMsg{Mode: ansi.ModeSynchronizedOutput, Value: ansi.ModeReset}
	}
}
