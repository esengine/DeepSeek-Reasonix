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

	"reasonix/internal/contract/eventwire"
)

// recordingKernel answers every route with success and records what the TUI
// asked of it.
type recordingKernel struct {
	mu    sync.Mutex
	calls []string
}

func (k *recordingKernel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	k.mu.Lock()
	k.calls = append(k.calls, r.Method+" "+r.URL.Path+" "+strings.TrimSpace(string(body)))
	k.mu.Unlock()
	switch r.URL.Path {
	case "/complete":
		// "看 @no": the token starts after one CJK rune and a space, two UTF-16 units.
		_ = json.NewEncoder(w).Encode(map[string]any{"kind": "ref", "from": 2, "to": 5,
			"items": []map[string]any{{"label": "notes.md", "insert": "@notes.md "}, {"label": "notes/", "insert": "@notes/"}}})
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
	m := newModel(context.Background(), Options{Client: &Client{HTTP: srv.Client(), Base: srv.URL}})
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
		"enter":      {Code: tea.KeyEnter},
		"esc":        {Code: tea.KeyEscape},
		"ctrl+s":     {Code: 's', Mod: tea.ModCtrl},
		"ctrl+c":     {Code: 'c', Mod: tea.ModCtrl},
		"y":          {Code: 'y', Text: "y"},
		"a":          {Code: 'a', Text: "a"},
		"n":          {Code: 'n', Text: "n"},
		"down":       {Code: tea.KeyDown},
		"ctrl+home":  {Code: tea.KeyHome, Mod: tea.ModCtrl},
		"ctrl+end":   {Code: tea.KeyEnd, Mod: tea.ModCtrl},
		"shift+pgup": {Code: tea.KeyPgUp, Mod: tea.ModShift},
		"shift+pgdn": {Code: tea.KeyPgDown, Mod: tea.ModShift},
		"shift+up":   {Code: tea.KeyUp, Mod: tea.ModShift},
		"shift+down": {Code: tea.KeyDown, Mod: tea.ModShift},
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
	if got := renderItem(&m.tr.Items[1], 80, m.sayShown[say.ID]); strings.Contains(got, "first block") {
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

func TestLargePasteFoldsAndExpandsOnSend(t *testing.T) {
	m, k := testModel(t)
	big := strings.Repeat("line\n", 10)
	m.Update(tea.PasteMsg{Content: big})
	if v := m.composer.Value(); v != "[Pasted text #1 +11 lines]" {
		t.Fatalf("composer = %q", v)
	}
	run(m, press(m, "enter"))
	raw, _ := json.Marshal(map[string]string{"input": big})
	if calls := strings.Join(k.seen(), "\n"); !strings.Contains(calls, string(raw)) {
		t.Fatalf("the paste did not go whole:\n%s", calls)
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

func TestAskEscDeclinesWithNothingSelected(t *testing.T) {
	m, k := testModel(t)
	apply(m, askEvent())
	run(m, press(m, "esc"))
	want := `POST /answer {"answers":[{"QuestionID":"q1","Selected":null},{"QuestionID":"q2","Selected":null}],"id":"ask1"}`
	if calls := strings.Join(k.seen(), "\n"); !strings.Contains(calls, want) {
		t.Fatalf("decline call missing:\n%s", calls)
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

// The task list is read from the kernel when it says the list moved, and drawn
// while it still has work in it.
func TestTodosFollowTheKernel(t *testing.T) {
	m, _ := testModel(t)
	_, cmd := m.Update(updateMsg{u: Update{Event: eventwire.Event{Kind: "todo_progress"}}, ok: true})
	run(m, cmd)
	v := m.View().Content
	for _, want := range []string{"✔ read the code", "▶ fix the bug", "○ run tests"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view missing %q:\n%s", want, v)
		}
	}
	m.todos = []TodoItem{{Content: "done", Status: "completed"}}
	if strings.Contains(m.View().Content, "To-dos") {
		t.Fatal("a finished list stayed on screen")
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
