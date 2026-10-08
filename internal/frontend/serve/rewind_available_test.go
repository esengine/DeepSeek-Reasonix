package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

func TestRewindAvailableUndoRouteReturnsNullWhenUnavailable(t *testing.T) {
	ctrl := control.New(control.Options{})
	defer ctrl.Close()
	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/rewind/undo")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /rewind/undo = %d, want 200", resp.StatusCode)
	}
	var got any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("GET /rewind/undo = %#v, want null", got)
	}
}

func TestRewindAvailableUndoRouteReturnsNullDuringRotation(t *testing.T) {
	dir := t.TempDir()
	rotating, resume := make(chan struct{}), make(chan struct{})
	var resumeOnce sync.Once
	release := func() { resumeOnce.Do(func() { close(resume) }) }
	session := sessionstore.NewSession("sys")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	exec := agent.New(nil, tool.NewRegistry(), session, agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{
		Executor: exec, SessionDir: dir, SessionPath: filepath.Join(dir, "session.jsonl"),
		WorkspaceRoot: dir, Label: "test", SystemPrompt: "sys",
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice && strings.HasPrefix(e.Text, "created branch ") {
				close(rotating)
				<-resume
			}
		}),
	})
	defer ctrl.Close()
	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	defer srv.Close()
	defer release()
	done := make(chan error, 1)
	go func() { _, err := ctrl.Branch("test"); done <- err }()
	select {
	case <-rotating:
	case err := <-done:
		t.Fatalf("branch ended before query: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("branch did not reach rotation window")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(srv.URL + "/rewind/undo")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /rewind/undo while rotating = %d, want 200", resp.StatusCode)
	}
	var offer any
	if err := json.NewDecoder(resp.Body).Decode(&offer); err != nil {
		t.Fatal(err)
	}
	if offer != nil {
		t.Fatalf("undo offer while rotating = %#v, want null", offer)
	}
	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("branch did not finish")
	}
}
