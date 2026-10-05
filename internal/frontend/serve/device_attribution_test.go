package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

// attributionRig is a server over a scripted model, recording the turns it
// announces.
type attributionRig struct {
	s       *Server
	mu      sync.Mutex
	started []event.Event
	done    chan struct{}
}

func newAttributionRig(t *testing.T, turns ...testutil.Turn) *attributionRig {
	t.Helper()
	dir := testenv.TempDir(t)
	exec := agent.New(testutil.NewMock("exec", turns...), tool.NewRegistry(), sessionstore.NewSession("sys"), agent.Options{}, event.Discard)
	rig := &attributionRig{done: make(chan struct{}, 8)}
	sink := event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.TurnStarted:
			rig.mu.Lock()
			rig.started = append(rig.started, e)
			rig.mu.Unlock()
		case event.TurnDone:
			rig.done <- struct{}{}
		}
	})
	c := control.New(control.Options{
		Runner: exec, Executor: exec, Sink: sink,
		SessionDir: dir, SessionPath: filepath.Join(dir, "s.jsonl"),
	})
	t.Cleanup(c.Close)
	rig.s = &Server{ctrl: c}
	return rig
}

// submit posts a line the way the page does, from a paired device when id is
// set and from the window otherwise, and waits for the turn to finish.
func (rig *attributionRig) submit(t *testing.T, input, deviceID string, ordinal int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader(`{"input":"`+input+`"}`))
	if deviceID != "" {
		req = req.WithContext(withDeviceReach(context.Background(), deviceID, ordinal))
	}
	rec := httptest.NewRecorder()
	rig.s.submit(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST /submit %q = %d %s", input, rec.Code, rec.Body.String())
	}
	select {
	case <-rig.done:
	case <-time.After(20 * time.Second):
		t.Fatalf("the turn for %q never finished", input)
	}
	// TurnDone is emitted before the controller lets go of the session, and
	// the next submit is refused until it has.
	deadline := time.Now().Add(5 * time.Second)
	for rig.s.ctrl.Running() {
		if time.Now().After(deadline) {
			t.Fatalf("the turn for %q finished but still owns the session", input)
		}
		time.Sleep(time.Millisecond)
	}
}

// A line sent from a phone is the phone's in the record and on the wire: the
// window, which did not send it, is told what was said and by which device.
func TestADevicesMessageIsLandedAndAnnouncedAsItsOwn(t *testing.T) {
	rig := newAttributionRig(t, testutil.Turn{Text: "one"}, testutil.Turn{Text: "two"})
	rig.submit(t, "from the phone", "dev-a", 2)
	rig.submit(t, "from the window", "", 0)

	rig.mu.Lock()
	started := append([]event.Event(nil), rig.started...)
	rig.mu.Unlock()
	if len(started) != 2 {
		t.Fatalf("announced %d turns, want 2", len(started))
	}
	if got := started[0]; got.Text != "from the phone" || got.Via == nil || *got.Via != (provider.Via{Device: "dev-a", Ordinal: 2}) {
		t.Fatalf("the phone's turn announced text %q via %+v", got.Text, got.Via)
	}
	if got := started[1]; got.Text != "from the window" || got.Via != nil {
		t.Fatalf("the window's turn announced text %q via %+v, want no device", got.Text, got.Via)
	}

	rec := httptest.NewRecorder()
	rig.s.history(rec, httptest.NewRequest(http.MethodGet, "/history", nil))
	var record []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
		Via     *struct {
			Device  string `json:"device"`
			Ordinal int    `json:"ordinal"`
		} `json:"via"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &record); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	seen := map[string]string{}
	for _, m := range record {
		if m.Role != "user" {
			continue
		}
		seen[m.Content] = "window"
		if m.Via != nil {
			seen[m.Content] = m.Via.Device
		}
	}
	if seen["from the phone"] != "dev-a" || seen["from the window"] != "window" {
		t.Fatalf("history attributes the lines as %v", seen)
	}
}

// The device is local UI metadata, like the model that answered: a provider
// request never carries it.
func TestADevicesMarkNeverReachesAProvider(t *testing.T) {
	msgs := []provider.Message{{Role: provider.RoleUser, Content: "hi", Via: &provider.Via{Device: "dev-a", Ordinal: 1}}}
	if got := provider.ProjectionMessages(msgs); got[0].Via != nil {
		t.Fatalf("projected message still carries %+v", got[0].Via)
	}
}

// A decision made on a phone is the phone's on its receipt, and only there: it
// names who decided and allows exactly what the same answer from the window
// would.
func TestADevicesApprovalIsRecordedOnItsReceipt(t *testing.T) {
	dir := testenv.TempDir(t)
	exec := agent.New(testutil.NewMock("exec",
		testutil.Turn{Text: "here is the plan"},
		testutil.Turn{Text: "executed the approved plan"},
	), tool.NewRegistry(), sessionstore.NewSession("sys"), agent.Options{}, event.Discard)
	var s *Server
	receipts := make(chan *provider.DecisionReceipt, 4)
	done := make(chan struct{}, 4)
	sink := event.FuncSink(func(e event.Event) {
		switch {
		case e.Kind == event.ApprovalRequest:
			id := e.Approval.ID
			go func() {
				req := httptest.NewRequest(http.MethodPost, "/approve", strings.NewReader(`{"id":"`+id+`","allow":true}`))
				s.approve(httptest.NewRecorder(), req.WithContext(withDeviceReach(context.Background(), "dev-b", 3)))
			}()
		case e.Kind == event.Notice && e.DecisionReceipt != nil:
			receipts <- e.DecisionReceipt
		case e.Kind == event.TurnDone:
			done <- struct{}{}
		}
	})
	c := control.New(control.Options{
		Runner: exec, Executor: exec, Sink: sink,
		SessionDir: dir, SessionPath: filepath.Join(dir, "s.jsonl"),
	})
	t.Cleanup(c.Close)
	s = &Server{ctrl: c}
	c.EnableInteractiveApproval()
	c.SetPlanMode(true)
	c.Submit("规划一下这件事")
	select {
	case r := <-receipts:
		if r.Via == nil || *r.Via != (provider.Via{Device: "dev-b", Ordinal: 3}) {
			t.Fatalf("receipt %+v, want it attributed to device 3", r)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("no decision was recorded")
	}
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("the approved turn never finished")
	}
}

// A line queued on a phone runs long after the request that queued it, so the
// device rides the queued item and the turn it becomes is still the phone's.
func TestAQueuedLineRunsAsTheDevicesOwn(t *testing.T) {
	rig := newAttributionRig(t, testutil.Turn{Text: "done"})
	req := httptest.NewRequest(http.MethodPost, "/inbox/items", strings.NewReader(`{"input":"queued on the phone"}`))
	req = req.WithContext(withDeviceReach(context.Background(), "dev-c", 4))
	rec := httptest.NewRecorder()
	rig.s.inboxEnqueue(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST /inbox/items = %d %s", rec.Code, rec.Body.String())
	}
	select {
	case <-rig.done:
	case <-time.After(20 * time.Second):
		t.Fatal("the queued line never ran")
	}
	rig.mu.Lock()
	defer rig.mu.Unlock()
	if len(rig.started) != 1 {
		t.Fatalf("announced %d turns, want 1", len(rig.started))
	}
	if got := rig.started[0]; got.Text != "queued on the phone" || got.Via == nil || got.Via.Device != "dev-c" || got.Via.Ordinal != 4 {
		t.Fatalf("the queued turn announced text %q via %+v", got.Text, got.Via)
	}
}
