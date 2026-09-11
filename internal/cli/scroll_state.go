package cli

import "strings"

// scrollFollowMode is an explicit transcript tail-follow state machine.
//
// Previously the post-Update path used a single wasAtBottom snapshot taken
// before the message was applied. Opening an approval/chooser/todo overlay
// shrinks the viewport (bottomRows grows) without any user scroll, which made
// AtBottom() flip false and permanently disabled tail-follow until the user
// manually returned to the bottom (#6430). Long sessions with frequent
// height-changing panels then looked like "scroll is broken" (#6978).
//
// Rules:
//   - Default is followTail: new content pins the viewport to the bottom.
//   - Explicit user scroll away from the bottom → userScrolled (wheel-up,
//     PgUp, scrollbar drag, Ctrl+Home, …).
//   - Returning to the bottom (wheel/page to end, empty Enter, Ctrl+End,
//     forceGotoBottom / session switch) restores followTail.
//   - Modal overlays that only change bottomRows never flip the mode.
type scrollFollowMode uint8

const (
	scrollFollowTail scrollFollowMode = iota
	scrollUserScrolled
)

// markUserScrolled records that the user intentionally left the live tail.
func (m *chatTUI) markUserScrolled() {
	m.scrollMode = scrollUserScrolled
}

// markFollowTail restores live tail-follow after the user (or a session
// switch) returns to the newest output.
func (m *chatTUI) markFollowTail() {
	m.scrollMode = scrollFollowTail
}

// shouldFollowTail reports whether new transcript content should pin the
// viewport to the bottom. forceGotoBottom always wins for one frame.
func (m chatTUI) shouldFollowTail() bool {
	return m.scrollMode == scrollFollowTail || m.forceGotoBottom
}

// modalOpen is true while a panel that steals bottom rows (or the whole main
// area) is active. Used so height-only layout changes never mark userScrolled.
func (m chatTUI) modalOpen() bool {
	if m.pendingApproval != nil || m.chooser != nil || m.rewind != nil {
		return true
	}
	if m.mcp != nil || m.clearConfirm != nil || m.mcpImport != nil {
		return true
	}
	if m.skillPick != nil || m.resumePick != nil || m.quickPick != nil || m.copyPick != nil {
		return true
	}
	return false
}

// syncScrollModeAfterGesture updates follow state from the current viewport
// position after an intentional scroll command. Landing on the bottom restores
// followTail; any other offset marks userScrolled.
func (m *chatTUI) syncScrollModeAfterGesture() {
	if m.viewport.AtBottom() {
		m.markFollowTail()
		return
	}
	m.markUserScrolled()
}

// useLegacyViewportScrollClear limits the application-level redraw workaround
// to Warp, where Bubble Tea's scroll optimization can strand stale rows. It is
// disabled on Windows even if Warp identifies itself there because Bubble Tea
// already avoids the incompatible optimization on that platform, and an extra
// asynchronous ClearScreen visibly flickers (#8090).
func useLegacyViewportScrollClear(goos string, environ []string) bool {
	if goos == "windows" {
		return false
	}
	for _, entry := range environ {
		key, value, ok := strings.Cut(entry, "=")
		if ok && key == "TERM_PROGRAM" && strings.EqualFold(strings.TrimSpace(value), "WarpTerminal") {
			return true
		}
	}
	return false
}
