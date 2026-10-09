package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/contract/eventwire"
	"reasonix/internal/frontend/termrender"
)

func fillTranscript(m *model, n int) {
	for i := range n {
		m.tr.AddNotice("info", fmt.Sprintf("row %02d", i))
	}
	m.commit()
}

// Native mouse mode hands the mouse to the terminal, so the transcript drops its
// scrollbar column and reclaims it for content.
func TestNativeMouseDropsScrollbar(t *testing.T) {
	m, _ := testModel(t)
	fillTranscript(m, 60)
	m.scr.mouseOff = true
	if got, want := m.contentWidth(), m.width; got != want {
		t.Fatalf("native-mouse content width = %d, want full width %d", got, want)
	}
	if c := m.View().Content; strings.Contains(c, "█") || strings.Contains(c, "│") {
		t.Fatalf("native mouse mode should drop the scrollbar:\n%s", c)
	}
	m.scr.mouseOff = false
	if !strings.Contains(m.View().Content, "█") {
		t.Fatal("capture-on should draw the scrollbar again")
	}
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

// Shift+PgUp/PgDn page the transcript as they page a terminal's scrollback.
func TestShiftPageKeysScrollTheTranscript(t *testing.T) {
	m, _ := testModel(t)
	fillTranscript(m, 60)
	m.View()
	press(m, "shift+pgup")
	if m.scr.follow {
		t.Fatalf("shift+pgup left the view on the tail at %d", m.scr.yoff)
	}
	press(m, "shift+pgdown")
	if !m.scr.follow {
		t.Fatalf("shift+pgdown did not return to the tail, view at %d", m.scr.yoff)
	}
	if got := m.composer.Value(); got != "" {
		t.Fatalf("a scroll key reached the composer: %q", got)
	}
}

// Shift+Insert pastes the clipboard's text, as it does in a Linux terminal.
func TestShiftInsertPastesClipboardText(t *testing.T) {
	for _, env := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		t.Setenv(env, "")
	}
	m, _ := testModel(t)
	cmd := press(m, "shift+insert")
	if cmd == nil {
		t.Fatal("shift+insert did nothing")
	}
	if _, ok := cmd().(clipTextMsg); !ok {
		t.Fatal("shift+insert did not read the clipboard's text")
	}
}

// Right-click with nothing selected pastes the clipboard's text, as 1.x did and
// docs/GUIDE.md says; over a panel that hides the composer it pastes nothing.
func TestRightClickWithoutASelectionPastesClipboardText(t *testing.T) {
	for _, env := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		t.Setenv(env, "")
	}
	m, _ := testModel(t)
	m.View()
	rightClick := tea.MouseClickMsg{Button: tea.MouseRight, X: 2, Y: 0}
	_, cmd := m.Update(rightClick)
	if cmd == nil {
		t.Fatal("right-click with no selection did nothing")
	}
	if _, ok := cmd().(clipTextMsg); !ok {
		t.Fatal("right-click with no selection did not read the clipboard's text")
	}
	apply(m, eventwire.Event{Kind: "approval_request", Approval: &eventwire.Approval{ID: "ap1", Tool: "bash", Subject: "rm x"}})
	if _, cmd := m.Update(rightClick); cmd != nil {
		t.Fatal("right-click pasted while an approval card hides the composer")
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

// The code rail is drawn, not written: a selection across a fenced block copies
// the code without it, both once the answer settled and while it is live.
func TestSelectionCopiesCodeWithoutTheRail(t *testing.T) {
	for _, settle := range []bool{true, false} {
		m, _ := testModel(t)
		apply(m, eventwire.Event{Kind: "turn_started"}, eventwire.Event{Kind: "text", Text: "see\n\n```go\nfunc a() {\n\treturn\n}\n```\n\n"})
		if settle {
			apply(m, eventwire.Event{Kind: "message", Text: ""}, eventwire.Event{Kind: "turn_done"})
		}
		m.View()
		rows := m.content(m.liveLines())
		first, last := -1, -1
		for i, r := range rows {
			if strings.Contains(r, "func a()") {
				first = i
			}
			if first >= 0 && strings.Contains(r, "}") {
				last = i
			}
		}
		if first < 0 || last < 0 || !strings.Contains(rows[first], "│") {
			t.Fatalf("settle=%v: fixture drew no railed code block:\n%s", settle, strings.Join(rows, "\n"))
		}
		m.scr.sel = selection{active: true, anchor: selPos{first, 0}, head: selPos{last, m.contentWidth()}}
		got := m.selectedText()
		if strings.Contains(got, "│") || !strings.Contains(got, "func a() {") || !strings.Contains(got, "return") {
			t.Fatalf("settle=%v: copied %q", settle, got)
		}
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

// Full screen, an answer's thinking sits shut behind its marker and a click on
// the marker opens and shuts it, whether the answer streamed in or settled whole.
func TestThinkingOpensAndShutsOnAClick(t *testing.T) {
	for _, streamed := range []bool{true, false} {
		m, _ := testModel(t)
		m.tr.AddUser("go")
		evs := []eventwire.Event{{Kind: "turn_started"}, {Kind: "reasoning", Text: "weighing the two options"}}
		if streamed {
			apply(m, append(evs, eventwire.Event{Kind: "text", Text: "first block\n\nstill writ"})...)
			apply(m, eventwire.Event{Kind: "message", Text: "first block\n\nstill writing", ThoughtMs: 2000})
		} else {
			apply(m, append(evs, eventwire.Event{Kind: "message", Text: "done", ThoughtMs: 2000})...)
		}
		all := func() string { return strings.Join(m.content(nil), "\n") }
		if strings.Contains(all(), "weighing") {
			t.Fatalf("streamed=%v: thinking shown before it was opened:\n%s", streamed, all())
		}
		marker := func() int {
			for i, r := range m.content(nil) {
				if strings.Contains(r, "▸") || strings.Contains(r, "▾") {
					return i
				}
			}
			t.Fatalf("streamed=%v: no thinking marker:\n%s", streamed, all())
			return -1
		}
		press(m, "ctrl+home")
		m.View()
		m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 4, Y: marker() - m.scr.yoff})
		if !strings.Contains(all(), "weighing the two options") {
			t.Fatalf("streamed=%v: a click on the marker did not open the thinking:\n%s", streamed, all())
		}
		m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 4, Y: marker() - m.scr.yoff})
		if strings.Contains(all(), "weighing") {
			t.Fatalf("streamed=%v: a second click did not shut it:\n%s", streamed, all())
		}
	}
}

func openThinking(t *testing.T, m *model) {
	t.Helper()
	press(m, "ctrl+home")
	m.View()
	for i, r := range m.content(nil) {
		if strings.Contains(r, "▸") {
			m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 4, Y: i - m.scr.yoff})
			return
		}
	}
	t.Fatalf("no shut thinking marker:\n%s", strings.Join(m.content(nil), "\n"))
}

// Opened thinking is split into transcript rows after rendering, so each row
// has to carry its own faint style rather than inherit one that the first row
// opened and the scrollbar cell closed.
func TestOpenedThinkingIsFaintOnEveryRow(t *testing.T) {
	prev := termrender.SetColorProfile(colorprofile.ANSI256)
	t.Cleanup(func() { termrender.SetColorProfile(prev) })
	m, _ := testModel(t)
	m.tr.AddUser("go")
	apply(m, eventwire.Event{Kind: "turn_started"},
		eventwire.Event{Kind: "reasoning", Text: strings.Repeat("weigh the options ", 30) + "\nsecond thought line"},
		eventwire.Event{Kind: "message", Text: "done", ThoughtMs: 1000})
	openThinking(t, m)
	faint := termrender.Dim("x")
	on := faint[:strings.Index(faint, "x")]
	if on == "" {
		t.Fatal("the faint style renders no escape in tests; the check below would prove nothing")
	}
	var rows int
	for _, r := range m.content(nil) {
		if !strings.Contains(r, "weigh") && !strings.Contains(r, "second thought") {
			continue
		}
		rows++
		if !strings.HasPrefix(r, on) {
			t.Fatalf("an opened thinking row lost the faint style: %q", r)
		}
	}
	if rows < 3 {
		t.Fatalf("thinking wrapped to %d rows, want several", rows)
	}
}

// The first chunk of a streamed answer is drawn before the answer settles;
// its marker has to show the thinking and elapsed time the answer settled with.
func TestStreamedThinkingOpensAsItSettled(t *testing.T) {
	m, _ := testModel(t)
	m.tr.AddUser("go")
	apply(m, eventwire.Event{Kind: "turn_started"}, eventwire.Event{Kind: "reasoning", Text: "early "},
		eventwire.Event{Kind: "text", Text: "first block\n\nstill writ"})
	apply(m, eventwire.Event{Kind: "reasoning", Text: "late"})
	apply(m, eventwire.Event{Kind: "message", Text: "first block\n\nstill writing", Reasoning: "early late", ThoughtMs: 4000})
	openThinking(t, m)
	all := strings.Join(m.content(nil), "\n")
	if !strings.Contains(all, "early late") {
		t.Fatalf("opened thinking is missing what arrived after the text started:\n%s", all)
	}
	if !strings.Contains(ansi.Strip(all), "▾ thought for 4s") {
		t.Fatalf("marker does not show the settled elapsed time:\n%s", ansi.Strip(all))
	}
}

// Ctrl+O opens the newest thinking from the keyboard, for a terminal whose
// mouse is handed back to it.
func TestCtrlOOpensTheLatestThinking(t *testing.T) {
	m, _ := testModel(t)
	m.scr.mouseOff = true
	m.tr.AddUser("go")
	apply(m, eventwire.Event{Kind: "turn_started"}, eventwire.Event{Kind: "reasoning", Text: "pondering"},
		eventwire.Event{Kind: "message", Text: "done", ThoughtMs: 1000})
	all := func() string { return strings.Join(m.content(nil), "\n") }
	press(m, "ctrl+o")
	if !strings.Contains(all(), "pondering") {
		t.Fatalf("ctrl+o did not open the thinking:\n%s", all())
	}
	press(m, "ctrl+o")
	if strings.Contains(all(), "pondering") {
		t.Fatalf("a second ctrl+o did not shut it:\n%s", all())
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

// clusterSamples holds a grapheme of every kind a per-rune terminal may fold
// to a width of its own: variation selectors, ZWJ sequences, skin tones,
// flags, keycaps and tag sequences.
var clusterSamples = []string{
	"变体选择符 ⚠\ufe0f 警告 ❤\ufe0f 心 ✔\ufe0f 勾 ☀\ufe0f 晴",
	"组合 👨\u200d👩\u200d👧 家庭 🏳\ufe0f\u200d🌈 彩虹 👩🏽\u200d💻 程序员 ❤\ufe0f\u200d🔥 🐻\u200d❄\ufe0f " + strings.Repeat("中", 20),
	"肤色 👍🏻 👋🏿 旗帜 🇨🇳 🇺🇸 🇯🇵 键帽 1\ufe0f\u20e3 #\ufe0f\u20e3 标签 🏴\U000e0067\U000e0062\U000e0073\U000e0063\U000e0074\U000e007f",
	strings.Repeat("⚠\ufe0f", 50),
	strings.Repeat("👨\u200d👩\u200d👧🇨🇳", 30),
}

// Every transcript row ends in the scrollbar at the last column as the
// renderer counts it: per rune until the terminal reports or is measured to
// draw grapheme clusters, per cluster after. Per rune, no row carries a rune
// the terminal could fold into a cluster of its own width.
func TestScrollbarHoldsItsColumnUnderEitherWidthCount(t *testing.T) {
	for _, report := range []tea.ModeReportMsg{
		{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeReset},
		clusterReport,
	} {
		m, _ := testModel(t)
		for _, s := range clusterSamples {
			m.tr.AddNotice("info", s)
		}
		args, _ := json.Marshal(map[string]string{"command": "printf '" + strings.Join(clusterSamples, " ") + "' > out.txt"})
		apply(m, eventwire.Event{Kind: "tool_result", Tool: &eventwire.Tool{ID: "c9", Name: "bash", Args: string(args), Output: clusterSamples[1]}})
		fillTranscript(m, 40)
		check := func(cells ansi.Method) {
			t.Helper()
			for range 30 {
				m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
			}
			for scrolled := 0; scrolled < 40; scrolled += wheelRows {
				rows := strings.Split(m.View().Content, "\n")[:m.viewportHeight()]
				for i, r := range rows {
					plain := ansi.Strip(r)
					if w := cells.StringWidth(plain); w != m.width || !strings.HasSuffix(plain, "│") && !strings.HasSuffix(plain, "█") {
						t.Fatalf("method %v row %d is %d cells wide, want %d ending in the scrollbar: %q", cells, i, w, m.width, plain)
					}
					if cells == ansi.WcWidth && strings.ContainsFunc(plain, foldsIntoCluster) {
						t.Fatalf("per-rune row %d still carries a foldable rune: %q", i, plain)
					}
				}
				m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
			}
		}
		check(ansi.WcWidth)
		m.Update(report)
		check(ansi.GraphemeWidth)
		press(m, "ctrl+home")
		if c := m.View().Content; !strings.Contains(c, "👨\u200d👩\u200d👧") && !strings.Contains(c, "🇨🇳") {
			t.Fatalf("a terminal drawing clusters should get them unchanged:\n%s", c)
		}
	}
}

// Stand-ins change what is drawn, never what a selection copies.
func TestSelectionCopiesTheGraphemesNotTheirStandIns(t *testing.T) {
	m, _ := testModel(t)
	const text = "flag 🇨🇳 family 👨\u200d👩\u200d👧 key 1\ufe0f\u20e3 tone 👍🏻"
	apply(m, eventwire.Event{Kind: "notice", Level: "info", Text: text})
	if c := m.View().Content; strings.Contains(c, "🇨🇳") || !strings.Contains(c, "CN") {
		t.Fatalf("a per-rune terminal should be drawn the stand-ins:\n%s", c)
	}
	joined := strings.Join(m.content(nil), "\n")
	row := strings.Count(joined[:strings.Index(joined, "flag")], "\n") - m.scr.yoff
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 0, Y: row})
	m.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, X: 70, Y: row})
	if got := m.selectedText(); !strings.Contains(got, text) {
		t.Fatalf("selected %q, want the original %q", got, text)
	}
}

// With native mouse selection there is no scrollbar, but the bottom region
// still has to fit: a per-rune terminal is drawn the stand-ins either way.
func TestNativeSelectionModeStillDrawsTheStandIns(t *testing.T) {
	m, _ := testModel(t)
	m.scr.mouseOff = true
	apply(m, eventwire.Event{Kind: "notice", Level: "info", Text: "flag 🇨🇳 family 👨\u200d👩\u200d👧"})
	if c := m.View().Content; strings.ContainsFunc(c, foldsIntoCluster) || !strings.Contains(c, "CN") {
		t.Fatalf("a per-rune terminal should get the stand-ins in every mode:\n%s", c)
	}
}

func TestSplitClustersKeepsThePerRuneCount(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"👨\u200d👩\u200d👧", "👨👩👧"},
		{"🏳\ufe0f\u200d🌈", " 🌈"},
		{"🏳\ufe0f 白旗 🖥", "🏳 白旗 🖥"},
		{"👩🏽\u200d💻", "👩  💻"},
		{"🇨🇳 🇺🇸", "CN US"},
		{"1\ufe0f\u20e3 #\ufe0f\u20e3", "1 #"},
		{"🏴\U000e0067\U000e0062\U000e007f", "🏴"},
		{"⚠\ufe0f \x1b[1m🇯🇵\x1b[m", "⚠ \x1b[1mJP\x1b[m"},
		{"plain 中文", "plain 中文"},
	} {
		got := splitClusters(c.in)
		if got != c.want {
			t.Fatalf("splitClusters(%q) = %q, want %q", c.in, got, c.want)
		}
		if a, b := ansi.WcWidth.StringWidth(c.in), ansi.WcWidth.StringWidth(got); a != b {
			t.Fatalf("splitClusters(%q) changed the per-rune count %d -> %d", c.in, a, b)
		}
	}
}

func TestCursorColumnReadsThePositionReport(t *testing.T) {
	for in, want := range map[string]int{
		"\x1b[12;3R":              3,
		"junk\x1b[?1;2c\x1b[1;2R": 2,
		"\x1b[4;R":                0,
		"\x1b[5":                  0,
	} {
		got, ok := cursorColumn(in)
		if got != want || ok != (want > 0) {
			t.Fatalf("cursorColumn(%q) = %d, %v; want %d", in, got, ok, want)
		}
	}
}

// A table measured the renderer's way keeps its column lines in one column
// on a per-rune terminal, whatever its cells hold.
func TestTableColumnsLineUpUnderThePerRuneCount(t *testing.T) {
	m, _ := testModel(t)
	m.tr.AddUser("go")
	apply(m, eventwire.Event{Kind: "turn_started"})
	apply(m, eventwire.Event{Kind: "text", Text: "| 名称 | 状态 | 说明 |\n|---|---|---|\n| 解析器 | ✅ 通过 | 全角「测试」 |\n| 渲染器 | ⚠\ufe0f 警告 | 👨\u200d👩\u200d👧 🇨🇳 |\n| 键帽 | 1\ufe0f\u20e3 | 👍🏽 肤色 |\n"})
	apply(m, eventwire.Event{Kind: "message", Text: ""}, eventwire.Event{Kind: "turn_done"})
	var cols []string
	for _, r := range strings.Split(m.View().Content, "\n")[:m.viewportHeight()] {
		plain := strings.TrimSuffix(strings.TrimRight(ansi.Strip(r), " │█"), " ")
		if !strings.Contains(plain, "│") {
			continue
		}
		var at []string
		for _, part := range strings.Split(plain, "│")[:strings.Count(plain, "│")] {
			at = append(at, fmt.Sprint(ansi.WcWidth.StringWidth(part)))
		}
		cols = append(cols, strings.Join(at, ","))
	}
	if len(cols) < 4 {
		t.Fatalf("table rows not found: %q", cols)
	}
	for _, c := range cols[1:] {
		if c != cols[0] {
			t.Fatalf("column lines moved between rows: %q", cols)
		}
	}
}

// A tool call waiting on approval draws its one row at the transcript's width,
// so its closing parenthesis is not wrapped onto a row of its own.
func TestPendingToolRowFitsTheTranscript(t *testing.T) {
	m, _ := testModel(t)
	args, _ := json.Marshal(map[string]string{"command": "printf '" + strings.Repeat("写入文件 ", 30) + "' > out.txt"})
	apply(m, eventwire.Event{Kind: "turn_started"}, eventwire.Event{Kind: "tool_dispatch", Tool: &eventwire.Tool{ID: "c1", Name: "bash", Args: string(args)}})
	for _, r := range m.content(m.liveLines()) {
		if plain := strings.TrimSpace(ansi.Strip(r)); plain == ")" {
			t.Fatalf("the pending tool row wrapped its closing parenthesis:\n%s", strings.Join(m.content(m.liveLines()), "\n"))
		}
	}
}
