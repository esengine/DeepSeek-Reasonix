package tui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/eventwire"
)

// recordingKernel answers every route with success and records what the TUI
// asked of it.
type recordingKernel struct {
	mu    sync.Mutex
	calls []string
	git   bool
	// keyless makes the kernel refuse a turn the way it does with no key set.
	keyless bool
}

func (k *recordingKernel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	k.mu.Lock()
	k.calls = append(k.calls, r.Method+" "+r.URL.Path+" "+strings.TrimSpace(string(body)))
	k.mu.Unlock()
	switch r.URL.Path {
	case "/complete":
		if r.URL.Query().Get("line") == "/" {
			_ = json.NewEncoder(w).Encode(map[string]any{"kind": "slash", "from": 0, "to": 1, "items": []map[string]any{
				{"label": "/compact", "insert": "/compact ", "hint": "fold the conversation", "kind": "builtin"},
				{"label": "/deploy", "insert": "/deploy ", "hint": "ship it", "kind": "command"},
				{"label": "/review", "insert": "/review ", "hint": "review the diff", "kind": "subagent"},
				{"label": "/mcp__docs__search", "insert": "/mcp__docs__search ", "kind": "prompt"},
			}})
			return
		}
		// "看 @no": the token starts after one CJK rune and a space, two UTF-16 units.
		_ = json.NewEncoder(w).Encode(map[string]any{"kind": "ref", "from": 2, "to": 5,
			"items": []map[string]any{{"label": "notes.md", "insert": "@notes.md "}, {"label": "notes/", "insert": "@notes/"}}})
	case "/workspace/git":
		if k.git {
			_ = json.NewEncoder(w).Encode(k.gitReply())
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"repo": false})
	case "/todos":
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"content": "read the code", "status": "completed"},
			{"content": "fix the bug", "status": "in_progress", "activeForm": "fixing the bug"},
			{"content": "run tests", "status": "pending"},
		})
	case "/sessions":
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"name": "a", "path": "/s/a.jsonl", "title": "fix the parser", "turns": 3, "current": true},
			{"name": "b", "path": "/s/b.jsonl", "title": "write the docs", "turns": 2},
			{"name": "c", "path": "/s/c.jsonl", "title": "fix the lexer", "turns": 1},
		})
	case "/history":
		_ = json.NewEncoder(w).Encode([]map[string]any{{"role": "user", "content": "write the docs"}})
	case "/inbox/items":
		_ = json.NewEncoder(w).Encode(map[string]string{"itemId": "q-7"})
	case "/provider-setup":
		_ = json.NewEncoder(w).Encode(map[string]any{"required": true, "provider": "beta", "model": "b1"})
	case "/submit":
		if k.keyless {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "provider.key_missing", "message": "no key"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case "/provider-setup/connections":
		_ = json.NewEncoder(w).Encode(map[string]any{"revision": "r1", "connections": []map[string]any{
			{"name": "alpha", "kind": "openai", "models": 2, "keyRequired": true},
			{"name": "beta", "kind": "anthropic", "models": 1, "active": true},
		}})
	case "/skills":
		_ = json.NewEncoder(w).Encode(map[string]any{"skills": []map[string]any{
			{"name": "test", "scope": "builtin", "enabled": true},
			{"name": "deploy", "scope": "project", "enabled": false},
			{"name": "audit", "scope": "global", "enabled": true, "subagent": true},
		}})
	case "/models":
		_ = json.NewEncoder(w).Encode(map[string]any{"current": "alpha/fast", "models": []map[string]any{
			{"ref": "alpha/fast", "provider": "alpha", "model": "fast", "active": true},
			{"ref": "alpha/deep", "provider": "alpha", "model": "deep"},
			{"ref": "beta/solo", "provider": "beta", "model": "solo"},
		}})
	case "/mcp":
		_ = json.NewEncoder(w).Encode(map[string]any{"servers": []map[string]any{
			{"name": "docs", "state": "ready", "enabled": true, "transport": "http", "source": "user", "tools": 2,
				"toolList": []map[string]any{{"name": "search", "readOnly": true}, {"name": "purge", "destructive": true}}},
			{"name": "db", "state": "disabled", "enabled": false, "transport": "stdio", "source": "project_mcp_json", "launch": "node db.js --token ***"},
			{"name": "mail", "state": "disabled", "enabled": false, "transport": "stdio", "source": "user_config"},
		}})
	case "/checkpoints":
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"turn": 0, "prompt": "read the code"}, {"turn": 1, "prompt": "fix the bug", "files": 2},
		})
	case "/rewind/prepare":
		_ = json.NewEncoder(w).Encode(map[string]any{"planId": "p-1", "canFiles": true, "canConversation": true})
	case "/rewind/commit":
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "conversationOk": true})
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (k *recordingKernel) seen() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]string(nil), k.calls...)
}

func testModel(t *testing.T) (*model, *recordingKernel) {
	t.Helper()
	t.Setenv("REASONIX_DISABLE_MOUSE", "0")
	k := &recordingKernel{}
	srv := httptest.NewServer(k)
	t.Cleanup(srv.Close)
	m := newModel(context.Background(), Options{Client: &Client{HTTP: srv.Client(), Base: srv.URL}, QuitCommands: []string{"/quit", "/exit"}})
	// No stream in these tests: a closed channel answers the wait at once.
	closed := make(chan Update)
	close(closed)
	m.updates = closed
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m, k
}

// run executes a command tree the way the program would, feeding every
// message it produces back into the model, except prints and ticks.
func run(m *model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	switch msg := msg.(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			run(m, c)
		}
	case nil, statusTickMsg:
	default:
		if _, isSeq := msg.(tea.Cmd); isSeq {
			return
		}
		// A sequence arrives as bubbletea's own slice of commands.
		if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeFor[tea.Cmd]() {
			for i := range v.Len() {
				run(m, v.Index(i).Interface().(tea.Cmd))
			}
			return
		}
		_, next := m.Update(msg)
		run(m, next)
	}
}

func typeText(m *model, s string) {
	for _, r := range s {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func press(m *model, k string) tea.Cmd {
	codes := map[string]tea.KeyPressMsg{
		"enter":        {Code: tea.KeyEnter},
		"esc":          {Code: tea.KeyEscape},
		"ctrl+s":       {Code: 's', Mod: tea.ModCtrl},
		"ctrl+c":       {Code: 'c', Mod: tea.ModCtrl},
		"ctrl+o":       {Code: 'o', Mod: tea.ModCtrl},
		"y":            {Code: 'y', Text: "y"},
		"a":            {Code: 'a', Text: "a"},
		"n":            {Code: 'n', Text: "n"},
		"down":         {Code: tea.KeyDown},
		"ctrl+home":    {Code: tea.KeyHome, Mod: tea.ModCtrl},
		"ctrl+end":     {Code: tea.KeyEnd, Mod: tea.ModCtrl},
		"shift+pgup":   {Code: tea.KeyPgUp, Mod: tea.ModShift},
		"shift+pgdown": {Code: tea.KeyPgDown, Mod: tea.ModShift},
		"shift+insert": {Code: tea.KeyInsert, Mod: tea.ModShift},
	}
	_, cmd := m.Update(codes[k])
	return cmd
}

func apply(m *model, evs ...eventwire.Event) {
	for _, ev := range evs {
		m.tr.Apply(ev)
	}
	m.commit()
}

// The scrollback takes rows in the order they happened: a finished block of a
// streaming answer goes early, a call still running holds back what follows it.
func TestCommitKeepsTheOrderTheConversationHappenedIn(t *testing.T) {
	m, _ := testModel(t)
	m.tr.AddUser("go")
	apply(m, eventwire.Event{Kind: "turn_started"}, eventwire.Event{Kind: "text", Text: "first block\n\nstill writ"})
	say := m.tr.Items[1]
	if !m.committed[m.tr.Items[0].ID] || m.committed[say.ID] || m.sayShown[say.ID] != len("first block\n\n") {
		t.Fatalf("committed=%v shown=%v", m.committed, m.sayShown)
	}
	apply(m,
		eventwire.Event{Kind: "tool_dispatch", Tool: &eventwire.Tool{ID: "a", Name: "bash"}},
		eventwire.Event{Kind: "tool_dispatch", Tool: &eventwire.Tool{ID: "b", Name: "bash"}},
		eventwire.Event{Kind: "tool_result", Tool: &eventwire.Tool{ID: "b", Output: "ok"}},
	)
	a, b := m.tr.Items[2], m.tr.Items[3]
	if !m.committed[say.ID] || m.committed[a.ID] || m.committed[b.ID] {
		t.Fatalf("a finished call jumped a running one: %v", m.committed)
	}
	apply(m, eventwire.Event{Kind: "tool_result", Tool: &eventwire.Tool{ID: "a", Output: "ok"}})
	if !m.committed[a.ID] || !m.committed[b.ID] {
		t.Fatalf("settled calls were not committed: %v", m.committed)
	}
	if got := renderItem(&m.tr.Items[1], 80, m.sayShown[say.ID], false); strings.Contains(got, "first block") {
		t.Fatalf("the answer's printed block was printed again: %q", got)
	}
}

// Input waiting in the queue stays on screen but does not hold back the rows
// that come after it.
func TestQueuedInputDoesNotHoldTheScrollback(t *testing.T) {
	m, _ := testModel(t)
	apply(m, eventwire.Event{Kind: "turn_started"})
	m.tr.AddQueued("later", false)
	apply(m, eventwire.Event{Kind: "message", Text: "done"})
	if !m.committed[m.tr.Items[1].ID] || m.committed[m.tr.Items[0].ID] {
		t.Fatalf("committed = %v, items %+v", m.committed, m.tr.Items)
	}
	if v := m.View(); !strings.Contains(v.Content, "later") {
		t.Fatalf("queued input left the screen: %q", v.Content)
	}
}

func TestEnterSubmitsWhenIdleAndQueuesWhileRunning(t *testing.T) {
	m, k := testModel(t)
	typeText(m, "hello")
	run(m, press(m, "enter"))
	apply(m, eventwire.Event{Kind: "turn_started"})
	typeText(m, "and tests")
	run(m, press(m, "enter"))
	typeText(m, "stop, use make")
	run(m, press(m, "ctrl+s"))
	calls := strings.Join(k.seen(), "\n")
	for _, want := range []string{
		`POST /submit {"input":"hello"}`,
		`POST /inbox/items {"input":"and tests","intent":"followup"}`,
		`POST /inbox/items {"input":"stop, use make","intent":"steer"}`,
	} {
		if !strings.Contains(calls, want) {
			t.Fatalf("missing %q in\n%s", want, calls)
		}
	}
	queued := m.tr.Items[len(m.tr.Items)-1]
	if !queued.Pending || queued.QueueID != "q-7" {
		t.Fatalf("queued row = %+v", queued)
	}
	run(m, press(m, "esc"))
	if !strings.Contains(strings.Join(k.seen(), "\n"), "POST /cancel") {
		t.Fatal("esc did not cancel the running turn")
	}
}

// An approval takes the answers the host said it honours, and no others.
func TestApprovalKeysAnswerOnlyWhatTheHostAllows(t *testing.T) {
	m, k := testModel(t)
	apply(m, eventwire.Event{Kind: "approval_request", Approval: &eventwire.Approval{ID: "ap1", Tool: "bash", Subject: "rm x"}})
	run(m, press(m, "a"))
	if m.tr.OpenPrompt() == nil {
		t.Fatal("a session grant the host does not offer was accepted")
	}
	run(m, press(m, "y"))
	if m.tr.OpenPrompt() != nil {
		t.Fatal("y did not settle the approval")
	}
	if calls := strings.Join(k.seen(), "\n"); !strings.Contains(calls, `POST /approve {"allow":true,"id":"ap1","persist":false,"session":false}`) {
		t.Fatalf("approve call missing:\n%s", calls)
	}
}

// The panel lists only the grants the host offers; the cursor walks them and
// enter answers with the row it is on.
func TestApprovalPanelAnswersTheRowUnderTheCursor(t *testing.T) {
	m, k := testModel(t)
	apply(m, eventwire.Event{Kind: "approval_request", Approval: &eventwire.Approval{ID: "ap1", Tool: "bash", Subject: "rm x", AllowsSession: true}})
	v := m.View().Content
	if strings.Count(v, "\n") == 0 || !strings.Contains(v, "rm x") || strings.Contains(v, "4. ") {
		t.Fatalf("panel should show three rows for the subject:\n%s", v)
	}
	run(m, press(m, "down"))
	run(m, press(m, "enter"))
	if calls := strings.Join(k.seen(), "\n"); !strings.Contains(calls, `POST /approve {"allow":true,"id":"ap1","persist":false,"session":true}`) {
		t.Fatalf("session grant missing:\n%s", calls)
	}
}

// 1.x moved the approval cursor with j/k and Ctrl+N/Ctrl+P as well as the
// arrows, and docs/CLI.md still says approval rows take them.
func TestApprovalCursorMovesWithJKAndCtrlNP(t *testing.T) {
	m, k := testModel(t)
	apply(m, eventwire.Event{Kind: "approval_request", Approval: &eventwire.Approval{ID: "ap1", Tool: "bash", Subject: "rm x", AllowsSession: true, AllowsPersist: true}})
	for _, step := range []struct {
		key  tea.KeyPressMsg
		want int
	}{
		{tea.KeyPressMsg{Code: 'j', Text: "j"}, 1},
		{tea.KeyPressMsg{Code: 'j', Text: "j"}, 2},
		{tea.KeyPressMsg{Code: 'k', Text: "k"}, 1},
		{ctrlN, 2},
		{ctrlP, 1},
	} {
		m.Update(step.key)
		if m.tr.OpenPrompt() == nil {
			t.Fatalf("%s answered the approval instead of moving the cursor", step.key)
		}
		if m.apSel.row != step.want {
			t.Fatalf("cursor after %s = %d, want %d", step.key, m.apSel.row, step.want)
		}
	}
	run(m, press(m, "enter"))
	calls := strings.Join(k.seen(), "\n")
	if !strings.Contains(calls, `POST /approve {"allow":true,"id":"ap1","persist":false,"session":true}`) || strings.Count(calls, "POST /approve") != 1 {
		t.Fatalf("enter on the second row should grant the session once:\n%s", calls)
	}
}

func TestLargePasteFoldsAndExpandsOnSend(t *testing.T) {
	m, k := testModel(t)
	big := strings.Repeat("line\n", 12) + "line"
	m.Update(tea.PasteMsg{Content: big})
	if v := m.composer.Value(); v != "[Pasted text #1 · 13 lines] " {
		t.Fatalf("composer = %q", v)
	}
	run(m, press(m, "enter"))
	label := "[Pasted text #1 · 13 lines]"
	want := label + "\n\n--- Begin " + label + " ---\n" + big + "\n--- End " + label + " ---"
	raw, _ := json.Marshal(map[string]string{"input": want})
	if calls := strings.Join(k.seen(), "\n"); !strings.Contains(calls, strings.TrimSuffix(string(raw), "}")) {
		t.Fatalf("the paste did not go as the block 1.x sends:\n%s", calls)
	}
	var shown string
	for _, it := range m.tr.Items {
		if it.Kind == ItemUser {
			shown = it.Text
		}
	}
	if shown != "[Pasted text #1 · 13 lines]" {
		t.Fatalf("the transcript keeps the label the person saw, got %q", shown)
	}
}

// A terminal that ends rows with a bare carriage return still pastes lines.
func TestPasteCountsLinesWhateverTheTerminalEndsThemWith(t *testing.T) {
	for name, sep := range map[string]string{"cr": "\r", "crlf": "\r\n", "lf": "\n"} {
		m, _ := testModel(t)
		m.Update(tea.PasteMsg{Content: strings.Join([]string{"a", "b", "c", "d", "e", "f"}, sep)})
		if v := m.composer.Value(); v != "[Pasted text #1 · 6 lines] " {
			t.Errorf("%s: composer = %q", name, v)
		}
	}
}

// Four short lines stay as typed; five fold, as does one very long line.
func TestPasteFoldsAtFiveLinesOrAThousandCharacters(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"a\nb\nc\nd", "a\nb\nc\nd"},
		{"a\nb\nc\nd\ne", "[Pasted text #1 · 5 lines] "},
		{strings.Repeat("x", 999), strings.Repeat("x", 999)},
		{strings.Repeat("x", 1000), "[Pasted text #1 · 1 lines] "},
	}
	for _, c := range cases {
		m, _ := testModel(t)
		m.Update(tea.PasteMsg{Content: c.text})
		if v := m.composer.Value(); v != c.want {
			t.Errorf("paste of %d bytes: composer = %q, want %q", len(c.text), v, c.want)
		}
	}
}

// Labels count up through the session, and one the person edited is no longer
// the paste, so it goes as typed.
func TestPasteLabelsCountUpAndAnEditedLabelStaysLiteral(t *testing.T) {
	m, k := testModel(t)
	five := "1\n2\n3\n4\n5"
	m.Update(tea.PasteMsg{Content: five})
	m.Update(tea.PasteMsg{Content: "6\n7\n8\n9\n10"})
	if v := m.composer.Value(); v != "[Pasted text #1 · 5 lines] [Pasted text #2 · 5 lines] " {
		t.Fatalf("composer = %q", v)
	}
	m.composer.SetValue("[Pasted text #2 · 9 lines]")
	run(m, press(m, "enter"))
	if calls := strings.Join(k.seen(), "\n"); strings.Contains(calls, "--- Begin") {
		t.Fatalf("a label nothing stands behind must go literally:\n%s", calls)
	}
}

// Over an open panel the paste is the answer being typed, so it lands as text.
func TestPasteOverAnOpenPanelIsNotFolded(t *testing.T) {
	m, _ := testModel(t)
	apply(m, eventwire.Event{Kind: "approval_request", Approval: &eventwire.Approval{ID: "ap1", Tool: "bash", Subject: "rm x"}})
	if m.tr.OpenPrompt() == nil {
		t.Fatal("the approval did not open")
	}
	m.Update(tea.PasteMsg{Content: "a\nb\nc\nd\ne\nf"})
	if strings.Contains(m.composer.Value(), "Pasted text") {
		t.Fatalf("composer = %q", m.composer.Value())
	}
}

func TestCtrlCTwiceQuitsWhenIdle(t *testing.T) {
	m, _ := testModel(t)
	if cmd := press(m, "ctrl+c"); cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("one ctrl+c quit")
		}
	}
	cmd := press(m, "ctrl+c")
	if cmd == nil {
		t.Fatal("second ctrl+c did nothing")
	}
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Fatal("second ctrl+c did not quit")
	}
}

// ^C clears the prompt and leaves shell mode, matching send(): the next input is
// not silently still a `!` shell command.
func TestCtrlCLeavesShellModeWhenNotVi(t *testing.T) {
	m, _ := testModel(t)
	m.shell = true
	m.composer.SetValue("ls")
	press(m, "ctrl+c")
	if got := m.composer.Value(); got != "" {
		t.Fatalf("prompt after ^C = %q, want empty", got)
	}
	if m.shell {
		t.Fatal("^C cleared the prompt but left shell mode on")
	}
}

// ^C cancels a running turn even while an ask answer is being typed, where Esc
// is ignored in vi mode: the composer must not swallow the universal interrupt.
func TestCtrlCCancelsWhileTypingAnAskAnswer(t *testing.T) {
	m, k := testModel(t)
	apply(m, eventwire.Event{Kind: "turn_started"})
	apply(m, askEvent())
	m.openAsk(m.tr.OpenPrompt())
	m.ask.entry = entryAnswer
	run(m, press(m, "ctrl+c"))
	if calls := strings.Join(k.seen(), "\n"); !strings.Contains(calls, "POST /cancel") {
		t.Fatalf("^C while typing an ask answer did not cancel:\n%s", calls)
	}
}

func askEvent() eventwire.Event {
	return eventwire.Event{Kind: "ask_request", Ask: &eventwire.Ask{ID: "ask1", Questions: []eventwire.AskQuestion{
		{ID: "q1", Prompt: "Which database?", Options: []eventwire.AskOption{{Label: "Postgres"}, {Label: "SQLite"}}},
		{ID: "q2", Prompt: "Which extras?", Multi: true, Options: []eventwire.AskOption{{Label: "cache"}, {Label: "search"}, {Label: "queue"}}},
	}}}
}

// A question panel is answered one question at a time: a number answers a
// single choice, numbers toggle a multi choice, the typed row adds an answer
// no option offered, and the submit tab sends the batch.
func TestAskIsAnsweredQuestionByQuestion(t *testing.T) {
	m, k := testModel(t)
	apply(m, askEvent())
	m.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	if v := m.View().Content; !strings.Contains(v, "Which extras?") {
		t.Fatalf("the panel did not move to the second question:\n%s", v)
	}
	m.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	m.Update(tea.KeyPressMsg{Code: '3', Text: "3"})
	m.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	m.Update(tea.KeyPressMsg{Code: '4', Text: "4"})
	typeText(m, "metrics")
	run(m, press(m, "enter"))
	if m.tr.OpenPrompt() == nil {
		t.Fatal("the panel sent before the answers were reviewed")
	}
	run(m, press(m, "enter"))
	if m.tr.OpenPrompt() != nil {
		t.Fatal("the answered card stayed open")
	}
	want := `POST /answer {"answers":[{"QuestionID":"q1","Selected":["SQLite"]},{"QuestionID":"q2","Selected":["queue","metrics"]}],"id":"ask1"}`
	if calls := strings.Join(k.seen(), "\n"); !strings.Contains(calls, want) {
		t.Fatalf("answer call missing:\n%s", calls)
	}
}

// An option can ask for more than itself ("paste the output"): Tab picks it
// and opens a note line, and the note goes to the kernel after the pick.
func TestAskPickCarriesATypedNote(t *testing.T) {
	m, k := testModel(t)
	apply(m, eventwire.Event{Kind: "ask_request", Ask: &eventwire.Ask{ID: "ask1", Questions: []eventwire.AskQuestion{
		{ID: "q1", Prompt: "How to go on?", Options: []eventwire.AskOption{{Label: "Skip"}, {Label: "Paste the output"}}},
	}}})
	press(m, "down")
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.tr.OpenPrompt() == nil {
		t.Fatal("Tab answered the question instead of opening a note")
	}
	typeText(m, "exit 1")
	if v := m.View().Content; !strings.Contains(v, i18n.M.AskNoteHint) {
		t.Fatalf("the note line is not shown under the pick:\n%s", v)
	}
	run(m, press(m, "enter"))
	want := `POST /answer {"answers":[{"QuestionID":"q1","Selected":["Paste the output","exit 1"]}],"id":"ask1"}`
	if calls := strings.Join(k.seen(), "\n"); !strings.Contains(calls, want) {
		t.Fatalf("answer call missing:\n%s", calls)
	}
}

// Picking an option replaces an answer typed earlier for a single-choice
// question; it is not sent alongside the pick.
func TestAskPickReplacesATypedAnswer(t *testing.T) {
	m, k := testModel(t)
	apply(m, askEvent())
	m.Update(tea.KeyPressMsg{Code: '3', Text: "3"})
	typeText(m, "MySQL")
	run(m, press(m, "enter"))
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	m.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	run(m, press(m, "enter"))
	want := `POST /answer {"answers":[{"QuestionID":"q1","Selected":["SQLite"]},{"QuestionID":"q2","Selected":["cache"]}],"id":"ask1"}`
	if calls := strings.Join(k.seen(), "\n"); !strings.Contains(calls, want) {
		t.Fatalf("answer call missing:\n%s", calls)
	}
}

func TestAskEscDeclinesWithNothingSelected(t *testing.T) {
	m, k := testModel(t)
	apply(m, askEvent())
	run(m, press(m, "esc"))
	want := `POST /answer {"answers":[{"QuestionID":"q1","Selected":null},{"QuestionID":"q2","Selected":null}],"id":"ask1"}`
	if calls := strings.Join(k.seen(), "\n"); !strings.Contains(calls, want) {
		t.Fatalf("decline call missing:\n%s", calls)
	}
}

func singleChoiceAsk() eventwire.Event {
	return eventwire.Event{Kind: "ask_request", Ask: &eventwire.Ask{ID: "ask-autosubmit", Questions: []eventwire.AskQuestion{
		{ID: "q1", Prompt: "One", Options: []eventwire.AskOption{{Label: "A"}}},
		{ID: "q2", Prompt: "Two", Options: []eventwire.AskOption{{Label: "B"}}},
	}}}
}

// With auto-submit on, answering the last question of a multi-question ask
// commits the whole batch at once, with no Submit tab and no extra Enter.
func TestAskAutoSubmitCommitsFullyAnsweredBatch(t *testing.T) {
	m, k := testModel(t)
	m.opts.AutoSubmit = true
	apply(m, singleChoiceAsk())
	_, cmd := m.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	run(m, cmd)
	if m.tr.OpenPrompt() == nil {
		t.Fatal("answering q1 must advance to q2, not submit")
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	run(m, cmd)
	if m.tr.OpenPrompt() != nil {
		t.Fatal("auto-submit must commit a fully answered batch after the last question")
	}
	if calls := strings.Join(k.seen(), "\n"); !strings.Contains(calls, `POST /answer {"answers":[{"QuestionID":"q1","Selected":["A"]},{"QuestionID":"q2","Selected":["B"]}],"id":"ask-autosubmit"}`) {
		t.Fatalf("answer call missing:\n%s", calls)
	}
}

// With auto-submit on there is no Submit tab to page onto: right stops at the
// last question and never commits, so editing an answer after a skip-back
// cannot submit the batch by an arrow.
func TestAskAutoSubmitArrowStopsAtLastQuestion(t *testing.T) {
	m, k := testModel(t)
	m.opts.AutoSubmit = true
	apply(m, singleChoiceAsk())
	// Answer q2 first, then q1, so the batch completes away from the end.
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	run(m, cmd)
	_, cmd = m.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	run(m, cmd)
	_, cmd = m.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	run(m, cmd)
	if m.tr.OpenPrompt() == nil {
		t.Fatal("the batch must still be open before the arrow")
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	run(m, cmd)
	if m.tr.OpenPrompt() == nil {
		t.Fatal("an arrow must not commit the batch")
	}
	if m.ask == nil || m.ask.tab != 1 {
		t.Fatalf("right must stop at the last question, got tab %d", m.ask.tab)
	}
	if calls := strings.Join(k.seen(), "\n"); strings.Contains(calls, "POST /answer") {
		t.Fatalf("an arrow must make no /answer call:\n%s", calls)
	}
}

// auto-submit fires only when nothing is unanswered: reaching the end of a batch
// with a skipped question jumps back to that question instead of committing.
func TestAskAutoSubmitJumpsToSkippedQuestion(t *testing.T) {
	m, _ := testModel(t)
	m.opts.AutoSubmit = true
	apply(m, singleChoiceAsk())
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	run(m, cmd)
	if m.tr.OpenPrompt() == nil {
		t.Fatal("moving to q2 must not submit with q1 unanswered")
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	run(m, cmd)
	if m.tr.OpenPrompt() == nil {
		t.Fatal("auto-submit must not commit an incomplete batch")
	}
	if m.ask == nil || m.ask.tab != 0 {
		t.Fatalf("auto-submit must jump to the skipped q1, got tab %d", m.ask.tab)
	}
}

func threeChoiceAsk() eventwire.Event {
	return eventwire.Event{Kind: "ask_request", Ask: &eventwire.Ask{ID: "ask-autosubmit3", Questions: []eventwire.AskQuestion{
		{ID: "q1", Prompt: "One", Options: []eventwire.AskOption{{Label: "A"}}},
		{ID: "q2", Prompt: "Two", Options: []eventwire.AskOption{{Label: "B"}}},
		{ID: "q3", Prompt: "Three", Options: []eventwire.AskOption{{Label: "C"}}},
	}}}
}

// Answering a question the panel was thrown back to skips on to the next
// unanswered one, not to the already-answered question that merely follows it;
// only the last question, with nothing left, commits.
func TestAskAutoSubmitSkipsToTheNextUnansweredAfterSkipBack(t *testing.T) {
	m, k := testModel(t)
	m.opts.AutoSubmit = true
	apply(m, threeChoiceAsk())
	// Answer q2, leaving q1 (behind) and q3 (ahead) unanswered: q1 is the gap
	// the panel is thrown back to.
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	run(m, cmd)
	run(m, press(m, "enter"))
	if m.ask == nil || m.ask.tab != 0 {
		t.Fatalf("answering q2 must be thrown back to q1, got tab %d", m.ask.tab)
	}
	run(m, press(m, "enter"))
	if m.tr.OpenPrompt() == nil {
		t.Fatal("answering q1 must not commit while q3 is unanswered")
	}
	if m.ask == nil || m.ask.tab != 2 {
		t.Fatalf("answering q1 must skip to the unanswered q3, got tab %d", m.ask.tab)
	}
	run(m, press(m, "enter"))
	if m.tr.OpenPrompt() != nil {
		t.Fatal("answering the last question must commit the batch")
	}
	if calls := strings.Join(k.seen(), "\n"); !strings.Contains(calls, `POST /answer {"answers":[{"QuestionID":"q1","Selected":["A"]},{"QuestionID":"q2","Selected":["B"]},{"QuestionID":"q3","Selected":["C"]}],"id":"ask-autosubmit3"}`) {
		t.Fatalf("answer call missing:\n%s", calls)
	}
}

// With auto-submit on the Submit tab is not drawn.
func TestAskAutoSubmitHidesSubmitTab(t *testing.T) {
	m, _ := testModel(t)
	m.opts.AutoSubmit = true
	apply(m, singleChoiceAsk())
	it := m.tr.OpenPrompt()
	if it == nil {
		t.Fatal("ask not open")
	}
	m.openAsk(it)
	if tabs := m.askTabs(it); strings.Contains(tabs, "Submit") {
		t.Fatalf("auto-submit must hide the Submit tab, got %q", tabs)
	}
}

// Without auto-submit the Submit tab is shown and still needs its explicit
// Enter even when the batch is fully answered.
func TestAskWithoutAutoSubmitWaitsOnSubmit(t *testing.T) {
	m, _ := testModel(t)
	apply(m, singleChoiceAsk())
	_, cmd := m.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	run(m, cmd)
	_, cmd = m.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	run(m, cmd)
	if m.tr.OpenPrompt() == nil {
		t.Fatal("without auto-submit the ask must stop at the Submit tab")
	}
	run(m, press(m, "enter"))
	if m.tr.OpenPrompt() != nil {
		t.Fatal("Enter on the Submit tab must submit the batch")
	}
}

// 1.x's question card took j/k as Down/Up and h/l as Left/Right, and
// docs/GUIDE.md lists them for it; a typed answer still takes them as text.
func TestAskCardMovesWithJKAndHL(t *testing.T) {
	m, k := testModel(t)
	apply(m, askEvent())
	key := func(r rune) { m.Update(tea.KeyPressMsg{Code: r, Text: string(r)}) }
	for _, step := range []struct {
		key         rune
		tab, cursor int
	}{
		{'j', 0, 1},
		{'k', 0, 0},
		{'l', 1, 0},
		{'j', 1, 1},
	} {
		key(step.key)
		if m.ask == nil || m.ask.tab != step.tab || m.ask.cursor != step.cursor {
			t.Fatalf("after %q the card is at %+v, want tab %d cursor %d", step.key, m.ask, step.tab, step.cursor)
		}
	}
	pressSpace(m)
	key('h')
	if m.ask.tab != 0 {
		t.Fatalf("h left the card on tab %d, want 0", m.ask.tab)
	}
	key('3')
	typeText(m, "hjkl")
	run(m, press(m, "enter"))
	key('l')
	run(m, press(m, "enter"))
	want := `POST /answer {"answers":[{"QuestionID":"q1","Selected":["hjkl"]},{"QuestionID":"q2","Selected":["search"]}],"id":"ask1"}`
	if calls := strings.Join(k.seen(), "\n"); !strings.Contains(calls, want) {
		t.Fatalf("answer call missing:\n%s", calls)
	}
}

// An @-token opens the menu as it is typed, and the chosen item replaces the
// token the kernel named — counted in UTF-16, so a CJK line splices where the
// kernel meant.
func TestCompletionReplacesTheTokenTheKernelNamed(t *testing.T) {
	m, k := testModel(t)
	for _, r := range "看 @no" {
		_, cmd := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		run(m, cmd)
	}
	if m.menu == nil || len(m.menu.c.Items) != 2 {
		t.Fatalf("menu = %+v", m.menu)
	}
	if !strings.Contains(strings.Join(k.seen(), "\n"), "GET /complete") {
		t.Fatal("the menu was not asked for")
	}
	run(m, press(m, "enter"))
	if got := m.composer.Value(); got != "看 @notes.md " {
		t.Fatalf("composer = %q", got)
	}
	if m.menu != nil {
		t.Fatal("the menu stayed open after a file was chosen")
	}
}

func TestUTF16Offsets(t *testing.T) {
	line := "看 @no😀x"
	for b, u := range map[int]int{0: 0, len("看"): 1, len("看 "): 2, len("看 @no"): 5, len("看 @no😀"): 7} {
		if got := utf16At(line, b); got != u {
			t.Errorf("utf16At(%d) = %d, want %d", b, got, u)
		}
		if got := byteAt(line, u); got != b {
			t.Errorf("byteAt(%d) = %d, want %d", u, got, b)
		}
	}
}

// The task list is read from the kernel when it says the list moved.
func TestTodosFollowTheKernel(t *testing.T) {
	m, _ := testModel(t)
	_, cmd := m.Update(updateMsg{us: []Update{{Event: eventwire.Event{Kind: "todo_progress"}}}, ok: true})
	run(m, cmd)
	v := m.View().Content
	for _, want := range []string{"✔ read the code", "▶ fix the bug", "○ run tests"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view missing %q:\n%s", want, v)
		}
	}
}

// A pasted image stands in the composer as a token and goes to the kernel as
// the reference it was stored under.
func TestPastedImageSendsItsReference(t *testing.T) {
	m, k := testModel(t)
	typeText(m, "what is this ")
	m.Update(clipImageMsg{ref: "@.reasonix/attachments/shot.png"})
	if got := m.composer.Value(); got != "what is this [image #1] " {
		t.Fatalf("composer = %q", got)
	}
	run(m, press(m, "enter"))
	if calls := strings.Join(k.seen(), "\n"); !strings.Contains(calls, `{"input":"what is this @.reasonix/attachments/shot.png"}`) {
		t.Fatalf("submit missing the reference:\n%s", calls)
	}
}

// ui.show_turn_usage = false keeps each request's receipt off the screen,
// both while the answer above it still streams and once it has settled.
func TestHiddenTurnUsageNeverReachesTheScreen(t *testing.T) {
	usage := eventwire.Event{Kind: "usage", Usage: &eventwire.Usage{TotalTokens: 1200, PromptTokens: 1000, CompletionTokens: 200}}
	for _, hide := range []bool{false, true} {
		m, _ := testModel(t)
		m.opts.HideTurnUsage = hide
		m.tr.AddUser("go")
		apply(m, eventwire.Event{Kind: "turn_started"}, eventwire.Event{Kind: "text", Text: "working"}, usage)
		live := m.View().Content
		apply(m, eventwire.Event{Kind: "message", Text: "working"}, eventwire.Event{Kind: "turn_done"})
		settled := strings.Join(m.content(nil), "\n")
		for when, screen := range map[string]string{"streaming": live, "settled": settled} {
			if got := strings.Contains(screen, "1.2K tok"); got == hide {
				t.Errorf("hide=%v %s: receipt shown = %v\n%s", hide, when, got, screen)
			}
		}
	}
}

// A `!` command is the user's own: it goes to the kernel marked to stay local,
// and no turn is waiting to be named after it.
func TestShellModeRunsTheCommandLocally(t *testing.T) {
	m, k := testModel(t)
	typeText(m, "!")
	typeText(m, "ls -la")
	run(m, press(m, "enter"))
	if !strings.Contains(strings.Join(k.seen(), "\n"), `POST /submit {"input":"!ls -la","localShell":true}`) {
		t.Fatalf("calls:\n%s", strings.Join(k.seen(), "\n"))
	}
	if len(m.tr.awaiting) != 0 {
		t.Fatalf("the command waits to be named by a turn: %v", m.tr.awaiting)
	}
}

// TestWaitUpdateCoalescesQueuedFrames proves a burst of queued stream frames is
// handed to the model as one message, so a burst costs one render rather than
// one render per frame — the view re-parses the whole growing answer each draw.
func TestWaitUpdateCoalescesQueuedFrames(t *testing.T) {
	m, _ := testModel(t)
	ch := make(chan Update, 8)
	m.updates = ch
	for range 5 {
		ch <- Update{Event: eventwire.Event{Kind: "text", Text: "x"}}
	}
	msg, ok := m.waitUpdate()().(updateMsg)
	if !ok {
		t.Fatalf("waitUpdate returned %T, want updateMsg", msg)
	}
	if len(msg.us) != 5 || !msg.ok {
		t.Fatalf("coalesced %d frames (ok=%v), want 5 frames and ok", len(msg.us), msg.ok)
	}
}

// TestWaitUpdateReportsAClosedStream proves the wait ends rather than spinning
// once the stream is gone.
func TestWaitUpdateReportsAClosedStream(t *testing.T) {
	m, _ := testModel(t) // testModel wires a closed updates channel
	msg, ok := m.waitUpdate()().(updateMsg)
	if !ok {
		t.Fatalf("waitUpdate returned %T, want updateMsg", msg)
	}
	if msg.ok {
		t.Fatal("a closed stream should report ok=false")
	}
}

// TestViewportHoldsOnShrink proves the transcript does not scroll back up when
// the content shrinks under a following viewport and the freed rows still fit
// inside it — a settled diff collapsing its raw preamble to one formatted line,
// say. A shrink larger than the viewport cannot hold (it would blank the
// transcript) and re-anchors instead: TestCollapsedDiffReanchorsTheTranscript.
func TestViewportHoldsOnShrink(t *testing.T) {
	m, _ := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	rows := func(n int) func(int, bool) string {
		return func(int, bool) string { return strings.Repeat("row\n", n) }
	}
	m.scr.blocks = append(m.scr.blocks, block{render: rows(40)})
	m.View()
	held := m.scr.yoff
	if held == 0 {
		t.Fatalf("viewport did not follow the tail: yoff=%d", held)
	}

	m.scr.blocks[0].render, m.scr.blocks[0].lines = rows(37), nil
	m.View()
	if m.scr.yoff != held {
		t.Fatalf("viewport moved on shrink: yoff %d -> %d", held, m.scr.yoff)
	}

	m.scr.blocks[0].render, m.scr.blocks[0].lines = rows(60), nil
	m.View()
	if m.scr.yoff <= held {
		t.Fatalf("viewport did not scroll on growth: yoff %d -> %d", held, m.scr.yoff)
	}
}

// A resumed conversation already holds labels; the next paste must not reuse one.
func TestPasteNumberingContinuesPastWhatTheResumedSessionHolds(t *testing.T) {
	m, _ := testModel(t)
	run(m, func() tea.Msg {
		return historyMsg{msgs: []HistoryMessage{{Role: "user", Content: "[Pasted text #4 · 9 lines]\n\n--- Begin [Pasted text #4 · 9 lines] ---\nx\n--- End [Pasted text #4 · 9 lines] ---"}}, reprint: true}
	})
	m.Update(tea.PasteMsg{Content: "1\n2\n3\n4\n5"})
	if v := m.composer.Value(); v != "[Pasted text #5 · 5 lines] " {
		t.Fatalf("composer = %q", v)
	}
}
