package tui

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/contract/eventwire"
)

func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	switch msg := cmd().(type) {
	case tea.QuitMsg:
		return true
	case tea.BatchMsg:
		return slices.ContainsFunc(msg, quits)
	default:
		if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeFor[tea.Cmd]() {
			for i := range v.Len() {
				if quits(v.Index(i).Interface().(tea.Cmd)) {
					return true
				}
			}
		}
	}
	return false
}

func startTurn(m *model) { apply(m, eventwire.Event{Kind: "turn_started"}) }

func TestSlashQuitAndExitQuitWithoutReachingTheModel(t *testing.T) {
	for _, line := range []string{"/quit", "/exit"} {
		t.Run(line, func(t *testing.T) {
			m, k := testModel(t)
			typeText(m, line)
			if !quits(press(m, "enter")) {
				t.Fatalf("%q did not quit", line)
			}
			if calledWith(k, "POST /submit") || calledWith(k, "POST /inbox/items") {
				t.Fatalf("%q reached the kernel:\n%s", line, strings.Join(k.seen(), "\n"))
			}
		})
	}
}

func TestSlashQuitQuitsWhileATurnRuns(t *testing.T) {
	m, k := testModel(t)
	startTurn(m)
	typeText(m, "/quit")
	if !quits(press(m, "enter")) {
		t.Fatal("/quit while a turn runs did not quit")
	}
	if calledWith(k, "POST /inbox/items") {
		t.Fatalf("/quit was queued as a follow-up:\n%s", strings.Join(k.seen(), "\n"))
	}
}

func TestBareExitWordsStayPlainPrompts(t *testing.T) {
	for _, line := range []string{"exit", "quit", ":q"} {
		t.Run(line, func(t *testing.T) {
			m, k := testModel(t)
			typeText(m, line)
			if quits(press(m, "enter")) {
				t.Fatalf("%q quit; wording must not decide that", line)
			}
			if !calledWith(k, "POST /submit") {
				t.Fatalf("%q was not sent as a prompt", line)
			}
		})
	}
}

func TestTabAcceptsTheOnlyCompletion(t *testing.T) {
	m, _ := testModel(t)
	menuFor(m, "/ze", Completion{Kind: "slash", To: 3, Items: []CompletionItem{{Label: "/zebra", Insert: "/zebra"}}})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if got := m.composer.Value(); got != "/zebra" {
		t.Fatalf("composer after Tab = %q, want /zebra", got)
	}
	if m.menu != nil {
		t.Fatal("the menu stayed open after Tab accepted")
	}
}

func TestTabAcceptsTheHighlightedRowNotTheNextOne(t *testing.T) {
	m, _ := testModel(t)
	menuFor(m, "/z", Completion{Kind: "slash", To: 2, Items: []CompletionItem{
		{Label: "/zebra", Insert: "/zebra"}, {Label: "/zulu", Insert: "/zulu"}, {Label: "/zeta", Insert: "/zeta"},
	}})
	press(m, "down")
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if got := m.composer.Value(); got != "/zulu" {
		t.Fatalf("composer after Down, Tab = %q, want /zulu", got)
	}
}

func TestDownAndUpStillMoveTheHighlight(t *testing.T) {
	m, _ := testModel(t)
	menuFor(m, "/z", Completion{Kind: "slash", To: 2, Items: []CompletionItem{
		{Label: "/zebra", Insert: "/zebra"}, {Label: "/zulu", Insert: "/zulu"}, {Label: "/zeta", Insert: "/zeta"},
	}})
	press(m, "down")
	press(m, "down")
	if m.menu.sel != 2 {
		t.Fatalf("highlight after two Downs = %d, want 2", m.menu.sel)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.menu.sel != 1 {
		t.Fatalf("highlight after Up = %d, want 1", m.menu.sel)
	}
	if m.composer.Value() != "/z" {
		t.Fatalf("moving the highlight changed the composer: %q", m.composer.Value())
	}
}

func TestTabAcceptsAFileMentionAndDescendsIntoADirectory(t *testing.T) {
	m, _ := testModel(t)
	menuFor(m, "see @in", Completion{Kind: "file", From: 4, To: 7, Items: []CompletionItem{
		{Label: "internal/", Insert: "@internal/", Descend: true},
	}})
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if got := m.composer.Value(); got != "see @internal/" {
		t.Fatalf("composer after Tab = %q, want see @internal/", got)
	}
	if cmd == nil {
		t.Fatal("a directory did not ask for the next level")
	}
}

func TestEnterStillAcceptsTheHighlightedCompletion(t *testing.T) {
	m, _ := testModel(t)
	menuFor(m, "/z", Completion{Kind: "slash", To: 2, Items: []CompletionItem{
		{Label: "/zebra", Insert: "/zebra"}, {Label: "/zulu", Insert: "/zulu"},
	}})
	press(m, "down")
	press(m, "enter")
	if got := m.composer.Value(); got != "/zulu" {
		t.Fatalf("composer after Down, Enter = %q, want /zulu", got)
	}
}

func TestTabWithoutAMenuStillAsksTheKernel(t *testing.T) {
	m, k := testModel(t)
	typeText(m, "see @in")
	m.menu = nil
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	run(m, cmd)
	if !calledWith(k, "GET /complete") {
		t.Fatalf("Tab without a menu did not ask for completions:\n%s", strings.Join(k.seen(), "\n"))
	}
}

func TestSecondCtrlCWhileStoppingQuits(t *testing.T) {
	m, _ := testModel(t)
	startTurn(m)
	if quits(press(m, "ctrl+c")) {
		t.Fatal("the first ctrl+c quit")
	}
	if !m.cancelling {
		t.Fatal("the first ctrl+c did not start stopping the turn")
	}
	if !quits(press(m, "ctrl+c")) {
		t.Fatal("the second ctrl+c while stopping did not quit")
	}
}

func TestFirstCtrlCCancelsOnlyOnce(t *testing.T) {
	m, k := testModel(t)
	startTurn(m)
	run(m, press(m, "ctrl+c"))
	n := 0
	for _, c := range k.seen() {
		if strings.HasPrefix(c, "POST /cancel") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("cancel calls after one ctrl+c = %d, want 1", n)
	}
}

func TestEscThenCtrlCWhileStoppingQuits(t *testing.T) {
	m, _ := testModel(t)
	startTurn(m)
	if quits(press(m, "esc")) {
		t.Fatal("esc quit")
	}
	if !quits(press(m, "ctrl+c")) {
		t.Fatal("ctrl+c after an esc cancel did not quit")
	}
}

func TestSecondEscWhileStoppingDoesNotQuit(t *testing.T) {
	m, _ := testModel(t)
	startTurn(m)
	press(m, "esc")
	if quits(press(m, "esc")) {
		t.Fatal("a second esc quit")
	}
}

func TestViSecondCtrlCWhileStoppingQuits(t *testing.T) {
	m, _ := newViModel(t)
	startTurn(m)
	if quits(press(m, "ctrl+c")) {
		t.Fatal("the first ctrl+c quit")
	}
	if !quits(press(m, "ctrl+c")) {
		t.Fatal("the second ctrl+c while stopping did not quit in vi mode")
	}
}

func TestCtrlCAfterTheTurnEndedNeedsTwoPressesAgain(t *testing.T) {
	m, _ := testModel(t)
	startTurn(m)
	press(m, "ctrl+c")
	m.Update(updateMsg{us: []Update{{Event: eventwire.Event{Kind: "turn_done"}}}, ok: true})
	if m.tr.Running || m.cancelling {
		t.Fatalf("setup: running=%v cancelling=%v", m.tr.Running, m.cancelling)
	}
	if quits(press(m, "ctrl+c")) {
		t.Fatal("an idle first ctrl+c quit after a cancelled turn")
	}
}

func TestCtrlDQuitsOnlyIdleAndEmpty(t *testing.T) {
	m, _ := testModel(t)
	if !quits(m.keyCmd(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})) {
		t.Fatal("ctrl+d on an idle empty composer did not quit")
	}
	m, _ = testModel(t)
	startTurn(m)
	if quits(m.keyCmd(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})) {
		t.Fatal("ctrl+d quit while a turn runs")
	}
}

func TestDoubleEscOnAnIdleEmptyComposerStillOpensRewind(t *testing.T) {
	m, _ := testModel(t)
	press(m, "esc")
	run(m, press(m, "esc"))
	if m.rewind == nil {
		t.Fatal("double esc did not open the rewind picker")
	}
}

func (m *model) keyCmd(msg tea.KeyPressMsg) tea.Cmd {
	_, cmd := m.Update(msg)
	return cmd
}

var (
	ctrlN = tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}
	ctrlP = tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}
)

// 1.x moved through a list with Ctrl+N / Ctrl+P as well as the arrows, and
// both lines' docs say so.
func TestCtrlNAndCtrlPMoveTheMenuHighlight(t *testing.T) {
	m, _ := testModel(t)
	menuFor(m, "/z", Completion{Kind: "slash", To: 2, Items: []CompletionItem{
		{Label: "/zebra", Insert: "/zebra"}, {Label: "/zulu", Insert: "/zulu"}, {Label: "/zeta", Insert: "/zeta"},
	}})
	m.Update(ctrlN)
	m.Update(ctrlN)
	if m.menu == nil || m.menu.sel != 2 {
		t.Fatalf("menu after two Ctrl+N = %+v, want the third row", m.menu)
	}
	m.Update(ctrlP)
	if m.menu.sel != 1 {
		t.Fatalf("highlight after Ctrl+P = %d, want 1", m.menu.sel)
	}
	if m.composer.Value() != "/z" {
		t.Fatalf("moving the highlight changed the composer: %q", m.composer.Value())
	}
}

func TestCtrlNAndCtrlPMoveThePickerSelection(t *testing.T) {
	m, _ := testModel(t)
	run(m, m.openPicker())
	if m.picker == nil || m.picker.sel != 1 || len(m.picker.shown()) < 3 {
		t.Fatalf("picker = %+v, want it on the second of at least three rows", m.picker)
	}
	m.Update(ctrlN)
	if m.picker.sel != 2 {
		t.Fatalf("selection after Ctrl+N = %d, want 2", m.picker.sel)
	}
	m.Update(ctrlP)
	m.Update(ctrlP)
	if m.picker.sel != 0 || m.picker.query != "" {
		t.Fatalf("picker after two Ctrl+P = sel %d, query %q; want 0 and no filter", m.picker.sel, m.picker.query)
	}
}

// Ctrl+Enter steers a running turn, as 1.x did and docs/GUIDE.md says; idle it
// sends nothing, so a press meant as a newline never submits the draft.
func TestCtrlEnterSteersOnlyARunningTurn(t *testing.T) {
	m, k := testModel(t)
	ctrlEnter := tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}
	typeText(m, "hello")
	run(m, m.keyCmd(ctrlEnter))
	if calledWith(k, "POST /submit") || m.composer.Value() != "hello" {
		t.Fatalf("idle Ctrl+Enter sent or changed the draft (composer %q):\n%s", m.composer.Value(), strings.Join(k.seen(), "\n"))
	}
	m.composer.Reset()
	startTurn(m)
	typeText(m, "use make")
	run(m, m.keyCmd(ctrlEnter))
	if calls := strings.Join(k.seen(), "\n"); !strings.Contains(calls, `POST /inbox/items {"input":"use make","intent":"steer"}`) {
		t.Fatalf("Ctrl+Enter during a turn did not steer:\n%s", calls)
	}
	if m.composer.Value() != "" {
		t.Fatalf("the steered text stayed in the composer: %q", m.composer.Value())
	}
}

// Ctrl+Z suspends the TUI to the shell, as 1.x did; Bubble Tea releases the
// terminal first and ignores the request where there is no job control.
func TestCtrlZSuspendsToTheShell(t *testing.T) {
	m, k := testModel(t)
	typeText(m, "draft")
	cmd := m.keyCmd(tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("Ctrl+Z did nothing")
	}
	if _, ok := cmd().(tea.SuspendMsg); !ok {
		t.Fatal("Ctrl+Z did not ask to suspend")
	}
	if m.composer.Value() != "draft" || calledWith(k, "POST /") {
		t.Fatalf("Ctrl+Z touched the draft or the kernel: %q\n%s", m.composer.Value(), strings.Join(k.seen(), "\n"))
	}
}
