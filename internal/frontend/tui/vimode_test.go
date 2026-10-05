package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/eventwire"
)

// newViModel builds an idle TUI whose composer uses the vi command mode.
func newViModel(t *testing.T) (*model, *recordingKernel) {
	t.Helper()
	m, k := testModel(t)
	m.opts.CommandMode = true
	return m, k
}

func viKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

var (
	viCtrlC = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	viCtrlD = tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
)

func TestViEscEntersCommandModeAndInsertLeavesIt(t *testing.T) {
	m, _ := newViModel(t)
	if m.viInCommand() {
		t.Fatal("should start in insert mode")
	}
	press(m, "esc")
	if !m.viInCommand() {
		t.Fatal("esc should enter command mode")
	}
	// A command-mode key (i) returns to insert editing.
	m.Update(viKey('i'))
	if m.viInCommand() {
		t.Fatal("i should leave command mode")
	}
}

func TestViEscLeavingInsertStepsCaretLeft(t *testing.T) {
	m, _ := newViModel(t)
	m.composer.SetValue("abc")
	m.composer.SetCursorColumn(3) // after the last char, as after typing "abc"
	press(m, "esc")
	if !m.viInCommand() {
		t.Fatal("esc should enter command mode")
	}
	if got := m.composer.Column(); got != 2 {
		t.Fatalf("caret after esc from insert = %d, want 2 (one rune left of end)", got)
	}
	// A second Esc while already in command mode must not move the caret.
	press(m, "esc")
	if !m.viInCommand() {
		t.Fatal("esc must keep command mode")
	}
	if got := m.composer.Column(); got != 2 {
		t.Fatalf("caret after second esc = %d, want 2 (no-op)", got)
	}
}

func TestViDeleteCharX(t *testing.T) {
	m, _ := newViModel(t)
	m.composer.SetValue("abc")
	m.composer.SetCursorColumn(0)
	press(m, "esc")
	if !m.viInCommand() {
		t.Fatal("esc should enter command mode")
	}
	m.Update(viKey('x'))
	if got := m.composer.Value(); got != "bc" {
		t.Fatalf("x at col 0 = %q, want bc", got)
	}
	if !m.viInCommand() {
		t.Fatal("x must stay in command mode")
	}
	// Pressing an unrecognized command-mode key must not insert text.
	m.Update(viKey('q'))
	if got := m.composer.Value(); got != "bc" {
		t.Fatalf("command mode ignored 'q' but value = %q, want bc", got)
	}
	if !m.viInCommand() {
		t.Fatal("must remain in command mode after ignored key")
	}
}

func TestViDeleteCharAtEndOfLineNoop(t *testing.T) {
	m, _ := newViModel(t)
	m.composer.SetValue("abc")
	m.composer.SetCursorColumn(3) // caret past last char
	press(m, "esc")
	// Esc from insert steps left; move back to the true end in command mode.
	m.Update(viKey('$'))
	if got := m.composer.Column(); got != 3 {
		t.Fatalf("$ caret col = %d, want 3", got)
	}
	m.Update(viKey('x'))
	if got := m.composer.Value(); got != "abc" {
		t.Fatalf("x at end-of-line = %q, want abc unchanged", got)
	}
}

func TestViDeleteToEndD(t *testing.T) {
	m, _ := newViModel(t)
	m.composer.SetValue("hello world")
	m.composer.SetCursorColumn(11) // caret after the last char
	press(m, "esc")                // command mode, caret steps to col 10
	m.Update(viKey('0'))           // back to the start
	m.Update(viKey('l'))           // caret on 'e'
	m.Update(viKey('D'))
	if got := m.composer.Value(); got != "h" {
		t.Fatalf("D from col 1 = %q, want h", got)
	}
	if got := m.composer.Column(); got != 1 {
		t.Fatalf("caret after D = %d, want 1", got)
	}
	if !m.viInCommand() {
		t.Fatal("D must stay in command mode")
	}
}

// TestViDeleteToEndOnlyCurrentLine: D stops at the end of the caret's line and
// leaves the following lines untouched.
func TestViDeleteToEndOnlyCurrentLine(t *testing.T) {
	m, _ := newViModel(t)
	m.composer.SetValue("first\nsecond")
	press(m, "esc")      // command mode; SetValue left the caret on the last line
	m.Update(viKey('k')) // up to the first line
	m.Update(viKey('0'))
	m.Update(viKey('l')) // caret on 'i' of "first"
	m.Update(viKey('D'))
	if got := m.composer.Value(); got != "f\nsecond" {
		t.Fatalf("D on the first line = %q, want f\\nsecond", got)
	}
}

func TestViDeleteToEndAtEndOfLineNoop(t *testing.T) {
	m, _ := newViModel(t)
	m.composer.SetValue("abc")
	m.composer.SetCursorColumn(3)
	press(m, "esc")
	m.Update(viKey('$')) // caret past the last char
	m.Update(viKey('D'))
	if got := m.composer.Value(); got != "abc" {
		t.Fatalf("D at end-of-line = %q, want abc unchanged", got)
	}
}

func TestViInsertAfterLastChar(t *testing.T) {
	m, _ := newViModel(t)
	m.composer.SetValue("abc")
	m.composer.SetCursorColumn(1) // caret after 'a'
	press(m, "esc")
	m.Update(viKey('A')) // append after last char
	if m.viInCommand() {
		t.Fatal("A should enter insert mode")
	}
	if got := m.composer.Column(); got != 3 {
		t.Fatalf("A caret col = %d, want 3 (end of 'abc')", got)
	}
	m.Update(viKey('d'))
	if got := m.composer.Value(); got != "abcd" {
		t.Fatalf("typing after A = %q, want abcd", got)
	}
}

func TestViInsertBeforeFirstChar(t *testing.T) {
	m, _ := newViModel(t)
	m.composer.SetValue("bc")
	m.composer.SetCursorColumn(2) // caret at end
	press(m, "esc")
	m.Update(viKey('I')) // insert before first char
	if m.viInCommand() {
		t.Fatal("I should enter insert mode")
	}
	m.Update(viKey('a'))
	if got := m.composer.Value(); got != "abc" {
		t.Fatalf("typing after I = %q, want abc", got)
	}
}

// TestViFooterModeMarker: the footer names the composer's input mode while vi
// mode is on, INSERT in insert mode and NORMAL once Esc enters command mode.
func TestViFooterModeMarker(t *testing.T) {
	m, _ := newViModel(t)
	if got := strings.Join(m.statusBlock(), "\n"); !strings.Contains(got, "INSERT") {
		t.Fatalf("insert-mode footer lacks the INSERT marker:\n%s", got)
	}
	press(m, "esc")
	if got := strings.Join(m.statusBlock(), "\n"); !strings.Contains(got, "NORMAL") {
		t.Fatalf("command-mode footer lacks the NORMAL marker:\n%s", got)
	}
}

// TestViFooterModeMarkerAbsentWithoutViMode: with vi mode off the footer carries
// no mode marker, leaving the first row exactly as before.
func TestViFooterModeMarkerAbsentWithoutViMode(t *testing.T) {
	m, _ := testModel(t)
	got := strings.Join(m.statusBlock(), "\n")
	if strings.Contains(got, "NORMAL") || strings.Contains(got, "INSERT") {
		t.Fatalf("non-vi footer carries a vi mode marker:\n%s", got)
	}
}

// TestViCommandModeConsumesMultiByteText: a CJK/IME character is one key, not
// one byte, so command mode must consume it instead of inserting it.
func TestViCommandModeConsumesMultiByteText(t *testing.T) {
	m, _ := newViModel(t)
	m.Update(viKey('中'))
	if got := m.composer.Value(); got != "中" {
		t.Fatalf("insert mode dropped the character: %q", got)
	}
	m.composer.Reset()
	press(m, "esc") // command mode
	m.Update(viKey('中'))
	if got := m.composer.Value(); got != "" {
		t.Fatalf("command mode inserted a multi-byte character: %q", got)
	}
	if !m.viInCommand() {
		t.Fatal("a multi-byte character left command mode")
	}
}

// TestViEscDoesNotDenyApproval: a vi user hitting Esc out of habit must not
// reject a waiting call; the ask card is guarded the same way.
func TestViEscDoesNotDenyApproval(t *testing.T) {
	m, k := newViModel(t)
	apply(m, eventwire.Event{Kind: "approval_request", Approval: &eventwire.Approval{ID: "ap1", Tool: "bash", Subject: "rm x"}})
	if m.tr.OpenPrompt() == nil {
		t.Fatal("setup: expected an open approval")
	}
	run(m, press(m, "esc"))
	if m.tr.OpenPrompt() == nil {
		t.Fatal("vi mode: Esc must not decide the approval")
	}
	if calls := strings.Join(k.seen(), "\n"); strings.Contains(calls, "POST /approve") {
		t.Fatal("vi mode: Esc rejected the approval")
	}
}

func TestEscStillDeniesApprovalWhenNotVi(t *testing.T) {
	m, k := testModel(t)
	apply(m, eventwire.Event{Kind: "approval_request", Approval: &eventwire.Approval{ID: "ap1", Tool: "bash", Subject: "rm x"}})
	run(m, press(m, "esc"))
	if m.tr.OpenPrompt() != nil {
		t.Fatal("non-vi: Esc must still decide the approval")
	}
	if calls := strings.Join(k.seen(), "\n"); !strings.Contains(calls, "POST /approve") {
		t.Fatalf("non-vi: Esc did not decide the approval:\n%s", calls)
	}
}

func TestViCtrlDQuitOnlyInInsertModeEmptyPrompt(t *testing.T) {
	// Insert mode, truly empty prompt: quits.
	m, _ := newViModel(t)
	_, cmd := m.Update(viCtrlD)
	if cmd == nil {
		t.Fatal("^D in insert mode on empty prompt should request shutdown")
	}
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Fatal("^D on an empty prompt did not request shutdown")
	}

	// Insert mode but with content (even a single space): no quit.
	m, _ = newViModel(t)
	m.composer.SetValue(" ")
	if _, cmd := m.Update(viCtrlD); cmd != nil {
		t.Fatal("^D with a space must not quit in vi mode")
	}

	// Command mode on the empty prompt: no quit.
	m, _ = newViModel(t)
	press(m, "esc")
	if _, cmd := m.Update(viCtrlD); cmd != nil {
		t.Fatal("^D in command mode must not quit")
	}
}

func TestViCtrlCLeavesCommandModeButNeverQuits(t *testing.T) {
	m, _ := newViModel(t)
	press(m, "esc")
	if !m.viInCommand() {
		t.Fatal("setup: expected command mode")
	}
	_, cmd := m.Update(viCtrlC)
	if m.viInCommand() {
		t.Fatal("^C should leave command mode back into insert")
	}
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("^C on idle must not quit")
		}
	}
}

// TestViCtrlCSavesDraftToHistoryAndClears: pressing ^C at the prompt with typed
// text must save the draft to the cmdline history and clear the prompt, so an
// accidental interrupt never loses it.
func TestViCtrlCSavesDraftToHistoryAndClears(t *testing.T) {
	m, _ := newViModel(t)
	m.composer.SetValue("half-written draft")

	m.Update(viCtrlC)

	if got := m.composer.Value(); got != "" {
		t.Fatalf("prompt after ^C = %q, want empty", got)
	}
	if len(m.history) == 0 || m.history[len(m.history)-1] != "half-written draft" {
		t.Fatalf("draft not saved to cmdline history: %v", m.history)
	}
}

// TestViCtrlCSavesTrimmedDraftAndLeavesShellMode: the saved draft follows
// send()'s rule (trimmed, whitespace-only skipped), and clearing the prompt also
// leaves shell mode, as send() does.
func TestViCtrlCSavesTrimmedDraftAndLeavesShellMode(t *testing.T) {
	m, _ := newViModel(t)
	m.shell = true
	m.composer.SetValue("  ls -la  ")

	m.Update(viCtrlC)

	if got := m.composer.Value(); got != "" {
		t.Fatalf("prompt after ^C = %q, want empty", got)
	}
	if len(m.history) != 1 || m.history[0] != "ls -la" {
		t.Fatalf("history = %v, want [ls -la]", m.history)
	}
	if m.shell {
		t.Fatal("^C cleared the prompt but left shell mode on")
	}
}

// TestViCtrlCClearsWhitespaceOnlyDraftWithoutSaving: a whitespace-only prompt
// is empty to send(), so ^C clears it as the default mode does but saves
// nothing to the cmdline history.
func TestViCtrlCClearsWhitespaceOnlyDraftWithoutSaving(t *testing.T) {
	m, _ := newViModel(t)
	m.composer.SetValue("   ")

	m.Update(viCtrlC)

	if len(m.history) != 0 {
		t.Fatalf("whitespace-only draft was saved: %v", m.history)
	}
	if got := m.composer.Value(); got != "" {
		t.Fatalf("whitespace-only prompt was not cleared: %q", got)
	}
}

// TestThinkingHintNamesTheInterruptKeyPerMode: the thinking line names the key
// that actually cancels — Esc by default, Ctrl+C in vi mode, where Esc enters
// command mode instead.
func TestThinkingHintNamesTheInterruptKeyPerMode(t *testing.T) {
	frame := spinFrames[0]

	plain, _ := testModel(t)
	apply(plain, eventwire.Event{Kind: "turn_started"})
	plain.runSince = time.Now()
	if got, want := ansi.Strip(plain.workingLine()), fmt.Sprintf("  "+i18n.M.ChatStatusThinkingFmt, frame, 0, "Esc"); got != want {
		t.Fatalf("default thinking line = %q, want %q", got, want)
	}

	vi, _ := newViModel(t)
	apply(vi, eventwire.Event{Kind: "turn_started"})
	vi.runSince = time.Now()
	if got, want := ansi.Strip(vi.workingLine()), fmt.Sprintf("  "+i18n.M.ChatStatusThinkingFmt, frame, 0, "Ctrl+C"); got != want {
		t.Fatalf("vi thinking line = %q, want %q", got, want)
	}
}

// TestCancellingHintOmitsExitKeyInViMode: vi mode consumes Ctrl+C to re-cancel
// instead of exiting, so the cancelling line drops the "Ctrl+C exits" clause
// there and keeps it for the default mode.
func TestCancellingHintOmitsExitKeyInViMode(t *testing.T) {
	frame := spinFrames[0]

	vi, _ := newViModel(t)
	apply(vi, eventwire.Event{Kind: "turn_started"})
	vi.cancelling, vi.runSince = true, time.Now()
	if got, want := ansi.Strip(vi.workingLine()), fmt.Sprintf("  "+i18n.M.ChatStatusCancellingViFmt, frame, 0); got != want {
		t.Fatalf("vi cancelling line = %q, want %q", got, want)
	}

	plain, _ := testModel(t)
	apply(plain, eventwire.Event{Kind: "turn_started"})
	plain.cancelling, plain.runSince = true, time.Now()
	if got, want := ansi.Strip(plain.workingLine()), fmt.Sprintf("  "+i18n.M.ChatStatusCancellingFmt, frame, 0); got != want {
		t.Fatalf("non-vi cancelling line = %q, want %q", got, want)
	}
}

// TestViFooterHintsDropEscOnCards: the tool-approval, ask and plan card hints
// name Esc in the default mode, where Esc answers them; in vi mode Esc is
// ignored there, so the hint drops it (the ask card names Ctrl+C instead).
func TestViFooterHintsDropEscOnCards(t *testing.T) {
	tool := eventwire.Event{Kind: "approval_request", Approval: &eventwire.Approval{ID: "ap1", Tool: "bash", Subject: "rm x"}}
	plan := eventwire.Event{Kind: "approval_request", Approval: &eventwire.Approval{ID: "ap2", Kind: "plan"}}
	for _, tc := range []struct {
		name string
		ev   eventwire.Event
	}{
		{"tool approval", tool},
		{"plan approval", plan},
		{"ask", askEvent()},
	} {
		plain, _ := testModel(t)
		apply(plain, tc.ev)
		if got := ansi.Strip(plain.stateText()); !strings.Contains(got, "Esc") {
			t.Fatalf("%s: default footer hint should name Esc:\n%s", tc.name, got)
		}

		vi, _ := newViModel(t)
		apply(vi, tc.ev)
		if got := ansi.Strip(vi.stateText()); strings.Contains(got, "Esc") {
			t.Fatalf("%s: vi footer hint still names Esc:\n%s", tc.name, got)
		}
	}
}

func TestViAskIgnoresEsc(t *testing.T) {
	// vi mode: Esc must not dismiss the ask card.
	m, _ := newViModel(t)
	apply(m, askEvent())
	if m.tr.OpenPrompt() == nil {
		t.Fatal("setup: expected an open ask")
	}
	press(m, "esc")
	if m.tr.OpenPrompt() == nil {
		t.Fatal("vi mode: Esc must not dismiss the ask card")
	}

	// vi mode: Esc while typing an answer must not back out / discard.
	m2, _ := newViModel(t)
	apply(m2, askEvent())
	m2.openAsk(m2.tr.OpenPrompt())
	m2.ask.entry = entryAnswer
	m2.composer.SetValue("draft")
	press(m2, "esc")
	if m2.ask == nil || !m2.ask.entering() {
		t.Fatal("vi mode: Esc while typing an ask answer must not back out")
	}
	if got := m2.composer.Value(); got != "draft" {
		t.Fatalf("vi mode: Esc discarded the draft, value = %q, want draft", got)
	}
}

// TestViCtrlCCancelsWhileTypingAnAskAnswer: Esc is ignored while an ask answer
// is being typed, so ^C must still cancel the running turn rather than being
// swallowed by the composer.
func TestViCtrlCCancelsWhileTypingAnAskAnswer(t *testing.T) {
	m, k := newViModel(t)
	apply(m, eventwire.Event{Kind: "turn_started"})
	apply(m, askEvent())
	m.openAsk(m.tr.OpenPrompt())
	m.ask.entry = entryAnswer
	run(m, press(m, "ctrl+c"))
	if calls := strings.Join(k.seen(), "\n"); !strings.Contains(calls, "POST /cancel") {
		t.Fatalf("^C while typing an ask answer did not cancel:\n%s", calls)
	}
}

func TestAskEscStillDismissesWhenNotVi(t *testing.T) {
	m, _ := testModel(t) // commandmode empty → not vi
	if m.viActive() {
		t.Fatal("setup: expected non-vi mode")
	}
	apply(m, askEvent())
	run(m, press(m, "esc"))
	if m.tr.OpenPrompt() != nil {
		t.Fatal("non-vi: Esc must still dismiss the ask card")
	}
}

// TestViEscEntersCommandModeWhileTurnRuns keeps vi mode's Esc a pure mode
// switch: while a turn runs, Esc enters command mode and never cancels — only
// ^C interrupts.
func TestViEscEntersCommandModeWhileTurnRuns(t *testing.T) {
	m, k := newViModel(t)
	apply(m, eventwire.Event{Kind: "turn_started"})
	if !m.tr.Running {
		t.Fatal("setup: expected a running turn")
	}
	press(m, "esc")
	if !m.tr.Running {
		t.Fatal("vi mode: Esc must not cancel a running turn")
	}
	if !m.viInCommand() {
		t.Fatal("vi mode: Esc while a turn runs should enter command mode")
	}
	if calls := strings.Join(k.seen(), "\n"); strings.Contains(calls, "POST /cancel") {
		t.Fatal("vi mode: Esc cancelled the turn")
	}
}
