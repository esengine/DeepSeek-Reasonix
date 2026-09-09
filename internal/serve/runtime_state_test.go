package serve

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/eventwire"
	"reasonix/internal/sessioninbox"
)

type runtimeStateServeRunner struct{ started chan struct{} }

type runtimeStateServeSink struct {
	states chan event.RuntimeStateSnapshot
}

func (*runtimeStateServeSink) Emit(event.Event) {}
func (s *runtimeStateServeSink) RuntimeStateChanged(state event.RuntimeStateSnapshot) {
	s.states <- state
}

func (r runtimeStateServeRunner) Run(ctx context.Context, _ string) error {
	close(r.started)
	<-ctx.Done()
	return ctx.Err()
}

func runtimeStateServeController(t *testing.T, dir, name string, runner interface {
	Run(context.Context, string) error
}) *control.Controller {
	t.Helper()
	path := filepath.Join(dir, name+".jsonl")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	sink := &runtimeStateServeSink{states: make(chan event.RuntimeStateSnapshot, 64)}
	c := control.New(control.Options{SessionDir: dir, SessionPath: path, Label: name, Runner: runner, Sink: sink})
	t.Cleanup(func() {
		defer c.Close()
		before := c.RuntimeStateSnapshot()
		if !c.Running() && before.Phase != "executing" && before.Phase != "finishing" {
			return
		}
		// Stop dispatching the isolated follow-up queue, then let the real
		// finishing notification establish a barrier before TempDir removal.
		_ = c.SetInboxPaused(true)
		c.Cancel()
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		for {
			select {
			case state := <-sink.states:
				if state.Phase == "idle" && state.Revision > before.Revision {
					return
				}
			case <-deadline.C:
				t.Error("isolated runtime did not settle before cleanup")
				return
			}
		}
	})
	return c
}

func runtimeStateHTTPGet(t *testing.T, endpoint string, target any) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status=%d", endpoint, response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeStateHTTPIncludesDetachedAndKeepsSnapshotImmutable(t *testing.T) {
	dir := t.TempDir()
	foreground := runtimeStateServeController(t, dir, "foreground", nil)
	detached := runtimeStateServeController(t, dir, "detached", nil)
	server := New(foreground, nil, config.ServeConfig{})
	detachedPath := agent.CanonicalSessionPath(detached.SessionPath())
	server.detached[detachedPath] = &detachedSession{path: detachedPath, ctrl: detached}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	// This endpoint reads managed runtimes, so removing a transcript cannot
	// make it disappear or cause a catalog/history scan to fail.
	if err := os.Remove(detached.SessionPath()); err != nil {
		t.Fatal(err)
	}
	var first, second runtimeStatesView
	runtimeStateHTTPGet(t, httpServer.URL+"/runtime-states", &first)
	runtimeStateHTTPGet(t, httpServer.URL+"/runtime-states", &second)
	if first.SchemaVersion != 1 || first.Epoch == "" || first.Revision == 0 || len(first.Sessions) != 2 {
		t.Fatalf("invalid full snapshot: %+v", first)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("unchanged memory produced different versions/content: first=%+v second=%+v", first, second)
	}
	byPath := map[string]runtimeSessionView{}
	for _, session := range first.Sessions {
		byPath[session.SessionPath] = session
	}
	if got := byPath[agent.CanonicalSessionPath(foreground.SessionPath())]; !got.Current || got.State != foreground.RuntimeStateSnapshot() {
		t.Fatalf("foreground snapshot mismatch: %+v", got)
	}
	if got := byPath[detachedPath]; got.Current || got.State != detached.RuntimeStateSnapshot() {
		t.Fatalf("detached snapshot mismatch: %+v", got)
	}
	read := server.runtimeStatesSnapshot()
	read.Sessions[0].State.Phase = "corrupt-test-copy"
	if reflect.DeepEqual(read, server.runtimeStatesSnapshot()) {
		t.Fatal("caller mutated the stored projection through its returned slice")
	}
}

func TestRuntimeStateHTTPStatusUsesRequestedDetachedController(t *testing.T) {
	dir := t.TempDir()
	foreground := runtimeStateServeController(t, dir, "foreground", nil)
	runner := runtimeStateServeRunner{started: make(chan struct{})}
	detached := runtimeStateServeController(t, dir, "detached", runner)
	server := New(foreground, nil, config.ServeConfig{})
	detachedPath := agent.CanonicalSessionPath(detached.SessionPath())
	server.detached[detachedPath] = &detachedSession{path: detachedPath, ctrl: detached}
	detached.Send("isolated running turn")
	select {
	case <-runner.started:
	case <-time.After(5 * time.Second):
		t.Fatal("detached runner did not start")
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	var status struct {
		SessionPath  string                     `json:"sessionPath"`
		Running      bool                       `json:"running"`
		RuntimeState event.RuntimeStateSnapshot `json:"runtimeState"`
	}
	runtimeStateHTTPGet(t, httpServer.URL+"/status?runtime=1&session="+url.QueryEscape(detachedPath), &status)
	if status.SessionPath != detachedPath {
		t.Fatalf("detached status path = %q, want canonical identity %q", status.SessionPath, detachedPath)
	}
	if !status.Running || status.RuntimeState.Phase != "executing" || status.RuntimeState.RuntimeEpoch != detached.RuntimeStateSnapshot().RuntimeEpoch {
		t.Fatalf("detached status was borrowed from foreground: %+v", status)
	}
	if foreground.RuntimeStateSnapshot().Running {
		t.Fatal("fixture foreground unexpectedly running")
	}
}

func TestOwnedRuntimeStatusCanonicalizesResponsePath(t *testing.T) {
	dir := t.TempDir()
	foreground := runtimeStateServeController(t, dir, "foreground", nil)
	detached := runtimeStateServeController(t, dir, "detached", nil)
	server := New(foreground, nil, config.ServeConfig{})
	path := agent.CanonicalSessionPath(detached.SessionPath())
	server.detached[path] = &detachedSession{path: path, ctrl: detached}
	// A noncanonical spelling must not escape through the status response,
	// even when lookup correctly resolves it to the detached owner.
	raw := filepath.Dir(path) + string(filepath.Separator) + "." + string(filepath.Separator) + filepath.Base(path)
	status, ok := server.ownedRuntimeStatusView(raw)
	if !ok || status["sessionPath"] != path {
		t.Fatalf("owned status did not preserve canonical identity: ok=%v path=%v want=%q", ok, status["sessionPath"], path)
	}
	state := status["runtimeState"].(event.RuntimeStateSnapshot)
	if state.RuntimeEpoch != detached.RuntimeStateSnapshot().RuntimeEpoch {
		t.Fatal("canonical response borrowed the foreground runtime")
	}
}

func readRuntimeSSEFrame(t *testing.T, reader *bufio.Reader) eventwire.Event {
	t.Helper()
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var frame eventwire.Event
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &frame); err != nil {
			t.Fatal(err)
		}
		return frame
	}
}

// Runtime snapshots are an additive host-only channel. Existing transcript
// tests still assert their exact protocol sequence after filtering that kind.
func nextServeProtocolFrame(t *testing.T, frames <-chan []byte, beforeRuntime func(eventwire.Event)) eventwire.Event {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case raw := <-frames:
			var frame eventwire.Event
			if err := json.Unmarshal(raw, &frame); err != nil {
				t.Fatal(err)
			}
			if frame.Kind == "runtime_state" {
				if beforeRuntime != nil {
					beforeRuntime(frame)
				}
				continue
			}
			return frame
		case <-timer.C:
			t.Fatal("expected protocol frame was not delivered")
		}
	}
}

func assertNoServeProtocolFrames(t *testing.T, frames <-chan []byte) {
	t.Helper()
	for {
		select {
		case raw := <-frames:
			var frame eventwire.Event
			if err := json.Unmarshal(raw, &frame); err != nil {
				t.Fatal(err)
			}
			if frame.Kind != "runtime_state" {
				t.Fatalf("unexpected duplicate protocol frame: %s", raw)
			}
		default:
			return
		}
	}
}

func TestRuntimeStateSSEFiltersSessionsAndPreservesHostOnlyPayload(t *testing.T) {
	dir := t.TempDir()
	foreground := runtimeStateServeController(t, dir, "foreground", nil)
	broadcaster := NewBroadcaster()
	server := New(foreground, broadcaster, config.ServeConfig{})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	client := &http.Client{Timeout: 5 * time.Second}
	currentResponse, err := client.Get(httpServer.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer currentResponse.Body.Close()
	allResponse, err := client.Get(httpServer.URL + "/events?all=1")
	if err != nil {
		t.Fatal(err)
	}
	defer allResponse.Body.Close()
	backgroundPath := agent.CanonicalSessionPath(filepath.Join(dir, "background.jsonl"))
	background := event.RuntimeStateSnapshot{SchemaVersion: 1, RuntimeEpoch: "background", Revision: 2, Phase: "idle"}
	current := event.RuntimeStateSnapshot{SchemaVersion: 1, RuntimeEpoch: "foreground", Revision: 3, Phase: "finishing", Running: true}
	backgroundSink := newSessionTagSink(broadcaster)
	backgroundSink.SetPath(backgroundPath)
	backgroundSink.RuntimeStateChanged(background)
	foregroundSink := newSessionTagSink(broadcaster)
	foregroundSink.SetPath(foreground.SessionPath())
	foregroundSink.RuntimeStateChanged(current)
	currentFrame := readRuntimeSSEFrame(t, bufio.NewReader(currentResponse.Body))
	if currentFrame.Kind != "runtime_state" || !currentFrame.SessionCurrent || currentFrame.RuntimeState == nil || *currentFrame.RuntimeState != current {
		t.Fatalf("current stream received background or changed payload: %+v", currentFrame)
	}
	allReader := bufio.NewReader(allResponse.Body)
	backgroundFrame := readRuntimeSSEFrame(t, allReader)
	if backgroundFrame.Kind != "runtime_state" || backgroundFrame.SessionCurrent || backgroundFrame.SessionPath != backgroundPath || backgroundFrame.RuntimeState == nil || *backgroundFrame.RuntimeState != background {
		t.Fatalf("all-session background frame mismatch: %+v", backgroundFrame)
	}
	if next := readRuntimeSSEFrame(t, allReader); next.SessionPath != agent.CanonicalSessionPath(foreground.SessionPath()) || !next.SessionCurrent {
		t.Fatalf("all-session foreground frame mismatch: %+v", next)
	}
}

func TestRuntimeStateInboxReceiptRetainsRichFollowupAndSessionFence(t *testing.T) {
	dir := t.TempDir()
	runner := runtimeStateServeRunner{started: make(chan struct{})}
	ctrl := runtimeStateServeController(t, dir, "foreground", runner)
	ctrl.Send("hold isolated foreground turn")
	select {
	case <-runner.started:
	case <-time.After(5 * time.Second):
		t.Fatal("foreground runner did not start")
	}
	server := New(ctrl, nil, config.ServeConfig{})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	request := map[string]any{
		"input": "run fixture-skill", "display": "rich display", "intent": "followup", "idempotencyKey": "stable-key",
		"invocations": []control.InvocationRequest{{Name: "fixture-skill", Kind: "skill", Offset: 4}},
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, httpServer.URL+"/inbox/items", strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sessionPathHeader, ctrl.SessionPath())
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("enqueue status=%d", response.StatusCode)
	}
	var enqueued sessioninbox.InboxReceipt
	if err := json.NewDecoder(response.Body).Decode(&enqueued); err != nil {
		t.Fatal(err)
	}
	_, envelope, err := ctrl.ReadInboxItem(enqueued.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.DisplayText != "rich display" || len(envelope.Invocations) != 1 || envelope.Invocations[0].Name != "fixture-skill" || envelope.Invocations[0].Kind != "skill" || envelope.Invocations[0].Offset != 4 {
		t.Fatalf("HTTP enqueue lost rich prompt: %+v", envelope)
	}
	var recovered sessioninbox.InboxReceipt
	runtimeStateHTTPGet(t, httpServer.URL+"/inbox/receipt?key=stable-key&session="+url.QueryEscape(ctrl.SessionPath()), &recovered)
	if recovered.ItemID != enqueued.ItemID {
		t.Fatalf("idempotency receipt changed item: enqueued=%+v recovered=%+v", enqueued, recovered)
	}
	var snapshot sessioninbox.InboxSnapshot
	runtimeStateHTTPGet(t, httpServer.URL+"/inbox?session="+url.QueryEscape(ctrl.SessionPath()), &snapshot)
	if snapshot.SessionPath != ctrl.SessionPath() || len(snapshot.Items) != 1 || snapshot.Items[0].ID != enqueued.ItemID {
		t.Fatalf("remote inbox snapshot does not match receipt: %+v", snapshot)
	}
	for _, endpoint := range []string{"/inbox", "/inbox/receipt?key=stable-key"} {
		for _, fence := range []string{"query", "header", "conflicting"} {
			t.Run(endpoint+"/"+fence, func(t *testing.T) {
				u, err := url.Parse(httpServer.URL + endpoint)
				if err != nil {
					t.Fatal(err)
				}
				query := u.Query()
				wrongPath := filepath.Join(dir, "wrong.jsonl")
				if fence == "query" || fence == "conflicting" {
					query.Set("session", wrongPath)
				} else {
					query.Set("session", ctrl.SessionPath())
				}
				u.RawQuery = query.Encode()
				req, _ := http.NewRequest(http.MethodGet, u.String(), nil)
				switch fence {
				case "header":
					req.Header.Set(expectedSessionPathHeader, wrongPath)
				case "conflicting":
					req.Header.Set(expectedSessionPathHeader, ctrl.SessionPath())
				}
				response, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				if response.StatusCode != http.StatusConflict {
					t.Fatalf("inbox read crossed %s fence: status=%d", fence, response.StatusCode)
				}
			})
		}
	}
	wrong, err := http.Get(httpServer.URL + "/inbox/receipt?key=stable-key&session=" + url.QueryEscape(filepath.Join(dir, "wrong.jsonl")))
	if err != nil {
		t.Fatal(err)
	}
	defer wrong.Body.Close()
	if wrong.StatusCode != http.StatusConflict {
		t.Fatalf("receipt lookup crossed session fence: status=%d", wrong.StatusCode)
	}
}
