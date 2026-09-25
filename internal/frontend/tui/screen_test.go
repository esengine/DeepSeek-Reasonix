package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/contract/eventwire"
)

func fillTranscript(m *model, n int) {
	for i := range n {
		m.tr.AddNotice("info", fmt.Sprintf("row %02d", i))
	}
	m.commit()
}

// The transcript follows new output until the user scrolls away, and picks
// the tail up again once they scroll back down to it.
func TestFullScreenScrollsAndFollowsTheTail(t *testing.T) {
	m, _ := testModel(t)
	fillTranscript(m, 60)
	if v := m.View(); !v.AltScreen || !strings.Contains(v.Content, "row 59") || !strings.Contains(v.Content, "█") {
		t.Fatalf("full screen should show the tail with a scrollbar:\n%s", v.Content)
	}
	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	m.tr.AddNotice("info", "row new")
	m.commit()
	if v := m.View().Content; strings.Contains(v, "row new") || m.scr.follow {
		t.Fatalf("a scrolled-back view jumped to new output:\n%s", v)
	}
	press(m, "ctrl+end")
	if v := m.View().Content; !strings.Contains(v, "row new") {
		t.Fatalf("ctrl+end did not return to the tail:\n%s", v)
	}
	press(m, "ctrl+home")
	if v := m.View().Content; !strings.Contains(v, "reasonix") && !strings.Contains(v, "row 00") {
		t.Fatalf("ctrl+home did not reach the top:\n%s", v)
	}
}

// Shift+PgUp/PgDn nudge the transcript by half a page and Shift+Up/Down by a
// single line, so the reading position can be tuned without a full page jump.
func TestShiftScrollKeysMoveTheView(t *testing.T) {
	m, _ := testModel(t)
	fillTranscript(m, 60)
	m.View()
	bottom := m.scr.yoff
	half := max(m.viewportHeight()/2, 1)
	if !m.scr.follow || bottom <= half {
		t.Fatalf("transcript should start pinned to the tail: yoff=%d follow=%v", bottom, m.scr.follow)
	}
	press(m, "shift+pgup")
	if got, want := m.scr.yoff, bottom-half; got != want {
		t.Fatalf("shift+pgup yoff = %d, want %d", got, want)
	}
	press(m, "shift+pgdn")
	if got := m.scr.yoff; got != bottom {
		t.Fatalf("shift+pgdn yoff = %d, want %d", got, bottom)
	}
	press(m, "shift+up")
	if got, want := m.scr.yoff, bottom-1; got != want {
		t.Fatalf("shift+up yoff = %d, want %d", got, want)
	}
	press(m, "shift+down")
	if got := m.scr.yoff; got != bottom {
		t.Fatalf("shift+down yoff = %d, want %d", got, bottom)
	}
}

// Dragging the thumb to the bottom of the track lands on the last page.
func TestScrollbarDragMovesTheView(t *testing.T) {
	m, _ := testModel(t)
	fillTranscript(m, 60)
	press(m, "ctrl+home")
	m.View()
	x := m.contentWidth()
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: 0})
	m.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, X: x, Y: 40})
	m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: x, Y: 40})
	if !m.scr.follow {
		t.Fatalf("thumb dragged to the end left the view at %d", m.scr.yoff)
	}
}

// A drag selects transcript text and its release copies it, without the
// padding the scrollbar column needs.
func TestDragSelectsAndCopiesTranscriptText(t *testing.T) {
	m, _ := testModel(t)
	apply(m, eventwire.Event{Kind: "notice", Level: "info", Text: "alpha beta"})
	m.View()
	y := strings.Index(strings.Join(m.content(nil), "\n"), "alpha")
	row := strings.Count(strings.Join(m.content(nil), "\n")[:y], "\n") - m.scr.yoff
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 0, Y: row})
	m.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, X: 30, Y: row})
	if got := m.selectedText(); !strings.Contains(got, "alpha beta") || strings.HasSuffix(got, " ") {
		t.Fatalf("selected %q", got)
	}
	_, cmd := m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 30, Y: row})
	if cmd == nil {
		t.Fatal("release did not copy")
	}
}

// Settled rows keep how to draw them, so a narrower window rewraps them.
func TestResizeRewrapsSettledRows(t *testing.T) {
	m, _ := testModel(t)
	m.tr.AddNotice("info", strings.Repeat("word ", 30))
	m.commit()
	wide := len(m.content(nil))
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	if narrow := len(m.content(nil)); narrow <= wide {
		t.Fatalf("rows at 40 cols = %d, at 80 = %d", narrow, wide)
	}
}

func TestInlineWritesToTheTerminalScrollback(t *testing.T) {
	m, _ := testModel(t)
	m.scr = nil
	m.tr.AddNotice("info", "printed")
	if cmd := m.commit(); cmd == nil {
		t.Fatal("inline commit printed nothing")
	}
	if m.View().AltScreen {
		t.Fatal("inline mode took the full screen")
	}
}

func shellRow(lines int) eventwire.Event {
	out := make([]string, lines)
	for i := range out {
		out[i] = fmt.Sprintf("out %03d", i)
	}
	return eventwire.Event{Kind: "tool_result", Tool: &eventwire.Tool{ID: "c1", Name: "bash", Args: `{"command":"seq"}`, Output: strings.Join(out, "\n")}}
}

// A long shell output shows its preview and opens with Ctrl+B or a click on
// its "more lines" row, as 1.x's did.
func TestShellOutputOpensAndShuts(t *testing.T) {
	m, _ := testModel(t)
	apply(m, eventwire.Event{Kind: "tool_dispatch", Tool: &eventwire.Tool{ID: "c1", Name: "bash", Args: `{"command":"seq"}`}}, shellRow(30))
	all := func() string { return strings.Join(m.content(nil), "\n") }
	if !strings.Contains(all(), "20 more lines (Ctrl+B)") || strings.Contains(all(), "out 029") {
		t.Fatalf("preview wrong:\n%s", all())
	}
	m.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	if !strings.Contains(all(), "out 029") {
		t.Fatalf("ctrl+b did not open the output:\n%s", all())
	}
	m.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	rows := m.content(nil)
	press(m, "ctrl+home")
	m.View()
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 4, Y: len(rows) - 1 - m.scr.yoff})
	if !strings.Contains(all(), "out 029") {
		t.Fatalf("a click on the hint row did not open the output:\n%s", all())
	}
}

// A selection dragged against the bottom edge keeps scrolling the transcript.
func TestSelectionAtTheEdgeScrolls(t *testing.T) {
	m, _ := testModel(t)
	fillTranscript(m, 60)
	press(m, "ctrl+home")
	m.View()
	h := m.viewportHeight()
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 2, Y: 1})
	_, cmd := m.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, X: 2, Y: h - 1})
	if cmd == nil {
		t.Fatal("holding the bottom edge did not start scrolling")
	}
	m.Update(edgeMsg{})
	m.Update(edgeMsg{})
	if m.scr.yoff != 2 || m.scr.sel.head.line != 2+h-1 {
		t.Fatalf("yoff = %d, head = %+v", m.scr.yoff, m.scr.sel.head)
	}
}

func TestPiecesFitTheRowsAboveTheFrame(t *testing.T) {
	var lines []string
	for i := range 25 {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	lines[3] = strings.Repeat("x", 25)
	got := pieces(strings.Join(lines, "\n"), 10, 10)
	if strings.Join(got, "\n") != strings.Join(lines, "\n") {
		t.Fatal("pieces lost or reordered text")
	}
	for _, p := range got {
		rows := 0
		for l := range strings.SplitSeq(p, "\n") {
			rows += 1 + len(l)/10
		}
		if rows > 10 {
			t.Fatalf("a piece of %d rows over a room of 10:\n%s", rows, p)
		}
	}
}

// Capture starts off over SSH and wherever 1.x's REASONIX_DISABLE_MOUSE says
// so; 0 captures even over SSH.
func TestMouseCaptureStartsFromTheEnvironment(t *testing.T) {
	for _, tc := range []struct {
		env, ssh string
		off      bool
	}{
		{"", "", false},
		{"", "host 22 client 1234", true},
		{"1", "", true},
		{"0", "host 22 client 1234", false},
	} {
		t.Setenv("REASONIX_DISABLE_MOUSE", tc.env)
		t.Setenv("SSH_CONNECTION", tc.ssh)
		t.Setenv("SSH_CLIENT", "")
		t.Setenv("SSH_TTY", "")
		m := newModel(context.Background(), Options{})
		if m.scr.mouseOff != tc.off {
			t.Errorf("REASONIX_DISABLE_MOUSE=%q SSH_CONNECTION=%q: mouseOff = %v, want %v", tc.env, tc.ssh, m.scr.mouseOff, tc.off)
		}
	}
}
