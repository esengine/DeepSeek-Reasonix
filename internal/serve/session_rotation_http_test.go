package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/eventwire"
)

func TestServePlanDecisionValidatesRequest(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc})
	srv := httptest.NewServer(New(ctrl, bc, config.ServeConfig{}).Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/plan-decision", "application/json", strings.NewReader(`{"action":"revise_plan"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("plan decision missing id = %d, want 400", resp.StatusCode)
	}
}

func TestServeSilentRotationsPublishSessionChanged(t *testing.T) {
	for _, endpoint := range []string{"/clear", "/new"} {
		t.Run(endpoint, func(t *testing.T) {
			bc := NewBroadcaster()
			exec := agent.New(nil, nil, agent.NewSession("system"), agent.Options{}, bc)
			ctrl := control.New(control.Options{Executor: exec, Sink: bc, SessionDir: t.TempDir()})
			ctrl.EnsureSessionPath()
			oldPath := ctrl.SessionPath()
			server := New(ctrl, bc, config.ServeConfig{})
			all, stop := bc.SubscribeAll()
			defer stop()
			httpServer := httptest.NewServer(server.Handler())
			defer httpServer.Close()

			resp, err := http.Post(httpServer.URL+endpoint, "application/json", nil)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusNoContent {
				t.Fatalf("%s status = %d, want 204", endpoint, resp.StatusCode)
			}
			var frame eventwire.Event
			select {
			case data := <-all:
				if err := json.Unmarshal(data, &frame); err != nil {
					t.Fatal(err)
				}
			default:
				t.Fatalf("%s emitted no routing barrier", endpoint)
			}
			if frame.Kind != "session_changed" || !frame.SessionCurrent || !frame.SessionReset || frame.SessionPath == "" || frame.SessionPath == oldPath {
				t.Fatalf("%s routing frame = %+v, old path %q", endpoint, frame, oldPath)
			}
		})
	}
}

func TestServeResumeBuffersSynchronousEventsUntilRoutePublication(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "active.jsonl")
	target := filepath.Join(dir, "target.jsonl")
	saveServeTestSession(t, active)
	saveServeTestSession(t, target)

	bc := NewBroadcaster()
	tag := NewSessionTagSink(bc)
	tag.SetPath(active)
	ctrl := control.New(control.Options{Sink: tag, SessionDir: dir, SessionPath: active})
	defer ctrl.Close()
	server := New(ctrl, bc, config.ServeConfig{})
	server.RegisterSessionTag(ctrl, tag)
	all, stop := bc.SubscribeAll()
	defer stop()
	resumeBindHookForTest = func() {
		tag.Emit(event.Event{Kind: event.Notice, Text: "synchronous resume warning"})
	}
	defer func() { resumeBindHookForTest = nil }()

	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	payload := `{"path":` + strconv.Quote(target) + `}`
	resp, err := http.Post(httpServer.URL+"/resume", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("resume status = %d, want 204", resp.StatusCode)
	}

	canonicalTarget := agent.CanonicalSessionPath(target)
	for _, wantKind := range []string{"notice", "session_changed"} {
		select {
		case data := <-all:
			var frame eventwire.Event
			if err := json.Unmarshal(data, &frame); err != nil {
				t.Fatal(err)
			}
			if frame.Kind != wantKind || frame.SessionPath != canonicalTarget || !frame.SessionCurrent {
				t.Fatalf("resumed %s frame = %+v, want target-tagged foreground frame", wantKind, frame)
			}
		case <-time.After(time.Second):
			t.Fatalf("resume emitted no %s frame", wantKind)
		}
	}
}

func TestServeClearSessionEndpoint(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc, SessionDir: t.TempDir()})
	srv := httptest.NewServer(New(ctrl, bc, config.ServeConfig{}).Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/clear", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("clear session = %d, want 204", resp.StatusCode)
	}
	if got := resp.Header.Get(sessionPathHeader); got == "" || got != ctrl.SessionPath() {
		t.Errorf("clear session path header = %q, controller path %q", got, ctrl.SessionPath())
	}
}

func TestServeSubmitClearCompletesRotationBeforeReturning(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc, SessionDir: t.TempDir()})
	ctrl.EnsureSessionPath()
	srv := httptest.NewServer(New(ctrl, bc, config.ServeConfig{}).Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/submit", "application/json", strings.NewReader(`{"input":"/clear"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("submit clear status = %d, want 204", resp.StatusCode)
	}
	if got := resp.Header.Get(sessionPathHeader); got == "" || got != ctrl.SessionPath() {
		t.Fatalf("submit clear returned path %q, controller path %q", got, ctrl.SessionPath())
	}
}

func TestServeNewSessionEndpoint(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc, SessionDir: t.TempDir()})
	srv := httptest.NewServer(New(ctrl, bc, config.ServeConfig{}).Handler())
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/new", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("new session = %d, want 204", resp.StatusCode)
	}
	if got := resp.Header.Get(sessionPathHeader); got == "" || got != ctrl.SessionPath() {
		t.Errorf("new session path header = %q, controller path %q", got, ctrl.SessionPath())
	}
}

func TestServeManagementSubmitReturnsNoContent(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc})
	srv := httptest.NewServer(New(ctrl, bc, config.ServeConfig{}).Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/submit", "application/json", strings.NewReader(`{"input":"/context"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("management submit = %d, want 204", resp.StatusCode)
	}
}
