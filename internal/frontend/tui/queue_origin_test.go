package tui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/base/i18n"
)

func spaceKey(m *model) tea.Cmd {
	_, c := m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	return c
}

// inboxWire is a kernel whose /inbox answers the JSON this test wrote, so the
// origin the runtime stamps on its own entry is on the wire even before the
// TUI has a field to decode it into. The fixture speaks the kernel's names, not
// the mirror's: a test that built InboxItem values could not tell a dropped
// field from a field the fixture never sent.
type inboxWire struct {
	mu       sync.Mutex
	snapshot string
	bodies   map[string]string
	calls    []string
}

func (k *inboxWire) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	k.mu.Lock()
	k.calls = append(k.calls, strings.TrimSpace(r.Method+" "+r.URL.Path+" "+string(raw)))
	k.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/inbox":
		_, _ = io.WriteString(w, k.snapshot)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/inbox/items/"):
		id := strings.TrimPrefix(r.URL.Path, "/inbox/items/")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"envelope": map[string]string{"submitText": k.bodies[id]},
		})
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (k *inboxWire) seen() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]string(nil), k.calls...)
}

func (k *inboxWire) did(call string) bool {
	for _, c := range k.seen() {
		if strings.HasPrefix(c, call) {
			return true
		}
	}
	return false
}

// inboxWireModel is a running turn with the queue this test put on the wire.
func inboxWireModel(t *testing.T, snapshot string) (*model, *inboxWire) {
	t.Helper()
	k := &inboxWire{snapshot: snapshot, bodies: map[string]string{
		"typed-1":    "the line I typed",
		"job-1-host": "job-1 — done",
	}}
	srv := httptest.NewServer(k)
	t.Cleanup(srv.Close)
	m := newModel(context.Background(), Options{Client: &Client{HTTP: srv.Client(), Base: srv.URL}})
	closed := make(chan Update)
	close(closed)
	m.updates = closed
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	startTurn(m)
	run(m, m.fetchQueue())
	return m, k
}

// The kernel appends a finished background job's continuation while the turn
// it belongs to is still running: a line the person never typed, queued the way
// a typed one is. It is the newest row, so the arrow key selects it and d d
// reaches DELETE — which the kernel honours silently, dropping the continuation.
func TestDeleteKeyCannotTakeBackAHostContinuation(t *testing.T) {
	const snapshot = `{"schemaVersion":2,"revision":7,"paused":false,"items":[
		{"id":"typed-1","intent":"followup","state":"queued","preview":"the line I typed"},
		{"id":"job-1-host","intent":"followup","state":"queued","preview":"job-1 — done","origin":"host"}
	],"capacity":{"items":2,"maxItems":64,"bytes":80,"maxBytes":67108864,"maxItemBytes":4194304}}`
	m, k := inboxWireModel(t, snapshot)
	run(m, keyUp(m))
	run(m, letter(m, 'd'))
	run(m, letter(m, 'd'))
	for _, call := range k.seen() {
		if strings.HasPrefix(call, "DELETE /inbox/items/job-1-host") {
			t.Fatalf("the TUI issued %q: a line the person never wrote was taken back, "+
				"and the runtime's continuation was dropped", call)
		}
	}
}

// The count on the working line is what the person reads as their outbox.
func TestAHostContinuationIsNotCountedAsThePersonsPendingMessage(t *testing.T) {
	const snapshot = `{"schemaVersion":2,"revision":7,"paused":false,"items":[
		{"id":"typed-1","intent":"followup","state":"queued","preview":"the line I typed"},
		{"id":"job-1-host","intent":"followup","state":"queued","preview":"job-1 — done","origin":"host"}
	]}`
	m, _ := inboxWireModel(t, snapshot)
	if got := m.workingLine(); !strings.Contains(got, "1 in inbox") {
		t.Fatalf("the count is not the person's pending line alone: %q", got)
	}
	if rows := rowsText(m); strings.Contains(rows, "job-1 — done") {
		t.Fatalf("the runtime's continuation is drawn as the person's pending row:\n%s", rows)
	}
}

// The control: a queued line the person did type is still counted and still
// takeable, with the host continuation present in the same snapshot.
func TestAPersonAuthoredQueuedLineIsStillCountedAndDeletable(t *testing.T) {
	const snapshot = `{"schemaVersion":2,"revision":7,"paused":false,"items":[
		{"id":"typed-1","intent":"followup","state":"queued","preview":"the line I typed"},
		{"id":"job-1-host","intent":"followup","state":"queued","preview":"job-1 — done","origin":"host"}
	]}`
	m, k := inboxWireModel(t, snapshot)
	run(m, keyUp(m))
	if m.queue.selID != "typed-1" {
		t.Fatalf("up selected %q, not the person's line", m.queue.selID)
	}
	run(m, letter(m, 'd'))
	run(m, letter(m, 'd'))
	if !k.did("DELETE /inbox/items/typed-1") {
		t.Fatalf("the person's own line could not be taken back: %v", k.seen())
	}
}

// A host continuation that stopped needs a decision only the person can make,
// so it stays on screen — marked as the runtime's, not theirs — and the retry
// rule #14180 gave a held entry still reads its block code.
func TestAHostContinuationThatNeedsADecisionIsMarkedAndStillRetried(t *testing.T) {
	const snapshot = `{"schemaVersion":2,"revision":8,"paused":false,"items":[
		{"id":"job-1-host","intent":"followup","state":"blocked","preview":"job-1 — done","origin":"host","blockCode":"steer_unapplied"}
	]}`
	m, k := inboxWireModel(t, snapshot)
	rows := rowsText(m)
	if !strings.Contains(rows, "job-1 — done") || !strings.Contains(rows, i18n.M.QueueHostTag) {
		t.Fatalf("a host continuation that needs a decision is not drawn marked:\n%s", rows)
	}
	run(m, keyUp(m))
	run(m, letter(m, 'r'))
	if !k.did("POST /inbox/items/job-1-host/retry") {
		t.Fatalf("the retry rule stopped reaching the kernel for a held entry: %v", k.seen())
	}
}

// A newer schema is read-only: the kernel refuses every mutation with
// 409 inbox.schema_readonly, so the resume key must say why instead of asking.
func TestAReadonlySnapshotStopsTheResumeKeyAndSaysWhy(t *testing.T) {
	const snapshot = `{"schemaVersion":99,"revision":9,"paused":true,"readonly":true,"items":[
		{"id":"typed-1","intent":"followup","state":"queued","preview":"the line I typed"}
	]}`
	m, k := inboxWireModel(t, snapshot)
	run(m, spaceKey(m))
	if k.did("POST /inbox/resume") {
		t.Fatalf("the TUI offered resume on a queue the kernel refuses with 409 inbox.schema_readonly: %v", k.seen())
	}
	if got := queueLastNotice(m); !strings.Contains(got, i18n.M.QueueReadonlyNote) {
		t.Fatalf("the refused key did not say why: %q", got)
	}
	rows := rowsText(m)
	if strings.Contains(rows, i18n.M.QueueRowsPaused) {
		t.Fatalf("the row still offers space to resume a read-only queue:\n%s", rows)
	}
	if !strings.Contains(rows, i18n.M.QueueRowsReadonly) {
		t.Fatalf("the row does not say the queue is read-only:\n%s", rows)
	}
	// The typed verbs are the same request by another route: they must not earn
	// the 409 the surface already knows about.
	queueSend(m, "/queue resume")
	queueSend(m, "/queue delete 1")
	if k.did("POST /inbox/resume") || k.did("DELETE /inbox/items/typed-1") {
		t.Fatalf("a /queue verb reached the kernel on a read-only queue: %v", k.seen())
	}
}

// The three fields the audit found missing on this mirror have to survive the
// wire decode: origin decides whose the line is, readonly whether it may be
// changed, and capacity is carried only to keep this copy honest with the
// kernel's snapshot.
func TestInboxDecodeCarriesOriginReadonlyAndCapacity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"schemaVersion":99,"revision":5,"paused":true,"readonly":true,`+
			`"items":[{"id":"h1","intent":"followup","state":"blocked","preview":"p",`+
			`"origin":"host","blockCode":"steer_unapplied"}],`+
			`"capacity":{"items":1,"maxItems":64,"bytes":9,"maxBytes":67108864,"maxItemBytes":4194304}}`)
	}))
	t.Cleanup(srv.Close)
	snap, err := (&Client{HTTP: srv.Client(), Base: srv.URL}).Inbox(context.Background())
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if !snap.Readonly {
		t.Error("the newer schema's read-only flag was dropped")
	}
	if len(snap.Items) != 1 || snap.Items[0].Origin != "host" {
		t.Fatalf("items = %+v, want the host origin decoded", snap.Items)
	}
	want := InboxCapacity{Items: 1, MaxItems: 64, Bytes: 9, MaxBytes: 67108864, MaxItemBytes: 4194304}
	if snap.Capacity != want {
		t.Fatalf("capacity = %+v, want %+v", snap.Capacity, want)
	}
}

// The kernel lists the turn it is running beside what waits, and the desktop
// leaves a taken entry out of the outbox because it is already in the
// transcript. The same rule here is what turns the reported "2 in inbox" back
// into the one line the person still owes.
func TestATakenEntryLeavesThePendingRows(t *testing.T) {
	const snapshot = `{"schemaVersion":2,"revision":6,"paused":false,"items":[
		{"id":"running-1","intent":"followup","state":"running","preview":"the turn in flight"},
		{"id":"typed-1","intent":"followup","state":"queued","preview":"the line I typed"}
	]}`
	m, _ := inboxWireModel(t, snapshot)
	if got := m.workingLine(); !strings.Contains(got, "1 in inbox") {
		t.Fatalf("a taken entry was counted as the person's pending message: %q", got)
	}
	if rows := rowsText(m); strings.Contains(rows, "the turn in flight") {
		t.Fatalf("a taken entry was drawn as a pending row:\n%s", rows)
	}
	run(m, keyUp(m))
	if m.queue.selID != "typed-1" {
		t.Fatalf("up selected %q, not the line the person still owes", m.queue.selID)
	}
}
