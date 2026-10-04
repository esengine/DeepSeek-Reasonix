package tui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// queueKernel answers the routes the local commands read and records every
// request; anything else is a 204.
type queueKernel struct {
	mu      sync.Mutex
	calls   []string
	answers map[string]any
}

func (k *queueKernel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	k.mu.Lock()
	k.calls = append(k.calls, r.Method+" "+r.URL.Path+" "+strings.TrimSpace(string(body)))
	k.mu.Unlock()
	if v, ok := k.answers[r.Method+" "+r.URL.Path]; ok {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(v)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (k *queueKernel) seen() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return strings.Join(k.calls, "\n")
}

func queueModel(t *testing.T, answers map[string]any) (*model, *queueKernel) {
	t.Helper()
	t.Setenv("REASONIX_DISABLE_MOUSE", "0")
	k := &queueKernel{answers: answers}
	srv := httptest.NewServer(k)
	t.Cleanup(srv.Close)
	m := newModel(context.Background(), Options{Client: &Client{HTTP: srv.Client(), Base: srv.URL}})
	closed := make(chan Update)
	close(closed)
	m.updates = closed
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m, k
}

func queueLastNotice(m *model) string {
	for _, it := range slices.Backward(m.tr.Items) {
		if it.Kind == ItemNotice {
			return it.Text
		}
	}
	return ""
}

func queueSend(m *model, line string) {
	m.composer.SetValue(line)
	run(m, press(m, "enter"))
}

// While a turn runs, plain text becomes a queued follow-up; the three queue
// commands are answered by the kernel instead and leave no pending row.
func TestQueueCommandsAreNotQueuedAsProse(t *testing.T) {
	m, k := queueModel(t, map[string]any{
		"POST /inbox/items": map[string]string{"itemId": "abcdef1234567890", "disposition": "steer_accepted"},
		"GET /inbox": map[string]any{"revision": 4, "items": []map[string]any{
			{"id": "11112222333", "intent": "followup", "state": "queued", "preview": "write tests"},
		}},
	})
	m.tr.Running = true
	queueSend(m, "/steer use the small fix")
	if got := queueLastNotice(m); got != "steer accepted #abcdef12" {
		t.Fatalf("steer notice = %q", got)
	}
	if !strings.Contains(k.seen(), `POST /inbox/items {"input":"use the small fix","intent":"steer"}`) {
		t.Fatalf("steer did not reach the inbox as a steer:\n%s", k.seen())
	}
	queueSend(m, "/queue")
	if got := queueLastNotice(m); got != "inbox rev=4 items=1\n  1. [followup/queued] write tests #11112222" {
		t.Fatalf("queue notice = %q", got)
	}
	for _, it := range m.tr.Items {
		if it.Pending {
			t.Fatalf("a local command left a pending queued row: %+v", it)
		}
	}
	if strings.Count(k.seen(), "POST /inbox/items") != 1 {
		t.Fatalf("/queue was sent as a follow-up:\n%s", k.seen())
	}
}

func TestSteerWithoutTextShowsUsage(t *testing.T) {
	m, k := queueModel(t, nil)
	queueSend(m, "/steer")
	if got := queueLastNotice(m); got != "usage: /steer <guidance>" || strings.Contains(k.seen(), "/inbox") {
		t.Fatalf("notice = %q, calls:\n%s", got, k.seen())
	}
}

func TestTakeoverNeedsATargetAndNeverStealsAHeldSession(t *testing.T) {
	m, k := queueModel(t, map[string]any{
		"GET /sessions": []map[string]any{
			{"path": "/s/a.jsonl", "current": true}, {"path": "/s/b.jsonl"},
		},
	})
	queueSend(m, "/takeover")
	if got := queueLastNotice(m); !strings.HasPrefix(got, "takeover: no refused session") {
		t.Fatalf("no-arg notice = %q", got)
	}
	queueSend(m, "/takeover 9")
	if got := queueLastNotice(m); !strings.Contains(got, "1–2") {
		t.Fatalf("out-of-range notice = %q", got)
	}
	queueSend(m, "/takeover 2")
	if !strings.Contains(k.seen(), `POST /resume {"path":"/s/b.jsonl"}`) {
		t.Fatalf("takeover did not resume the indexed session:\n%s", k.seen())
	}
	if !strings.Contains(queueLastNotice(m), "not taken over") {
		t.Fatalf("notice = %q", queueLastNotice(m))
	}
}

func TestStatusReportsTheFieldsTheFooterCompresses(t *testing.T) {
	m, _ := queueModel(t, map[string]any{
		"GET /status": map[string]any{
			"label": "deepseek-chat", "toolApprovalMode": "auto", "effort": "high",
			"used": 50000, "window": 100000, "cacheHit": 90, "cacheMiss": 10,
			"jobs": []map[string]any{{"id": "j1"}, {"id": "j2"}},
		},
	})
	queueSend(m, "/status")
	got := queueLastNotice(m)
	for _, want := range []string{"Session status", "mode       Auto", "model      deepseek-chat",
		"context    50.0K / 100.0K ctx (50%)", "effort     effort high", "cache      ", "jobs       ⚙ 2", "config     "} {
		if !strings.Contains(got, want) {
			t.Fatalf("status lacks %q:\n%s", want, got)
		}
	}
}

// The kernel owns where the file goes; this screen only reports it.
func TestExportAsksTheKernelAndReportsItsPath(t *testing.T) {
	m, k := queueModel(t, map[string]any{
		"POST /sessions/export": map[string]any{"path": "/ws/session-1.md", "messages": 3},
	})
	queueSend(m, "/export")
	if got := queueLastNotice(m); got != "session exported to /ws/session-1.md" {
		t.Fatalf("notice = %q", got)
	}
	if strings.Contains(k.seen(), "GET /history") {
		t.Fatalf("export read the history itself:\n%s", k.seen())
	}
}

func TestExportOfAnEmptyConversationSaysSo(t *testing.T) {
	m, _ := queueModel(t, map[string]any{"POST /sessions/export": map[string]any{"path": "", "messages": 0}})
	queueSend(m, "/export")
	if got := queueLastNotice(m); got != "no messages to export" {
		t.Fatalf("notice = %q", got)
	}
}

// Counting never crosses the last thing the person said, and 1 is the newest.
func TestCopyTakesTheNewestAssistantTextOfTheLatestTurn(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "1")
	history := []HistoryMessage{
		{Role: "user", Content: "old"}, {Role: "assistant", Content: "old answer"},
		{Role: "user", Content: "now"}, {Role: "assistant", Content: "first"},
		{Role: "assistant", Content: "..."}, {Role: "tool", Content: "out"},
		{Role: "user", Content: "host", HostAuthored: true}, {Role: "assistant", Content: "second"},
	}
	if got := copyParts(history); len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("parts = %q", got)
	}
	m, _ := queueModel(t, map[string]any{"GET /history": history})
	queueSend(m, "/copy 2")
	if got := queueLastNotice(m); got != "copied response to clipboard" {
		t.Fatalf("notice = %q", got)
	}
	queueSend(m, "/copy 3")
	if got := queueLastNotice(m); got != "no assistant response to copy" {
		t.Fatalf("out-of-range notice = %q", got)
	}
}

func TestCopyPickerChoosesWithTheArrowKeys(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "1")
	m, _ := queueModel(t, map[string]any{"GET /history": []HistoryMessage{
		{Role: "user", Content: "q"}, {Role: "assistant", Content: "alpha"}, {Role: "assistant", Content: "beta"},
	}})
	queueSend(m, "/copy")
	if m.copying == nil || len(m.copying.parts) != 2 || m.copying.parts[0] != "beta" {
		t.Fatalf("picker = %+v", m.copying)
	}
	if v := m.View().Content; !strings.Contains(v, "pick a response to copy") {
		t.Fatalf("picker not drawn:\n%s", v)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	run(m, press(m, "enter"))
	if m.copying != nil || queueLastNotice(m) != "copied response to clipboard" {
		t.Fatalf("picker = %+v, notice = %q", m.copying, queueLastNotice(m))
	}
}

func TestLocalCommandsAreOfferedInTheMenu(t *testing.T) {
	m, _ := queueModel(t, nil)
	var labels []string
	for _, c := range m.localCommands("/") {
		labels = append(labels, c.Label)
	}
	got := strings.Join(labels, " ")
	for _, want := range []string{"/queue", "/steer", "/takeover", "/status", "/export", "/copy"} {
		if !strings.Contains(got, want) {
			t.Fatalf("menu lacks %s: %s", want, got)
		}
	}
}

func TestQueueRefsRefuseWhatIsOutOfRangeOrAmbiguous(t *testing.T) {
	snap := InboxSnapshot{Items: []InboxItem{{ID: "5aaa"}, {ID: "5bbb"}, {ID: "7ccc"}}}
	for _, ref := range []string{"4", "0", "5", "9zz"} {
		if id, err := resolveQueueRef(snap, []string{ref}); err == nil {
			t.Fatalf("ref %q resolved to %q", ref, id)
		}
	}
	if id, err := resolveQueueRef(snap, []string{"3"}); err != nil || id != "7ccc" {
		t.Fatalf("a position resolves by position: %q, %v", id, err)
	}
	if id, err := resolveQueueRef(snap, []string{"5a"}); err != nil || id != "5aaa" {
		t.Fatalf("unique prefix = %q, %v", id, err)
	}
}

func TestUsageLevelComesFromTheCommandNotItsWording(t *testing.T) {
	if got := queueCommand(context.Background(), nil, []string{"bogus"}); got.level != "warn" {
		t.Fatalf("usage level = %q", got.level)
	}
	if got := steerCommand(context.Background(), nil, ""); got.level != "warn" {
		t.Fatalf("steer usage level = %q", got.level)
	}
}
