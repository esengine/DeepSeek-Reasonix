package serve

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

// settlesWithinSubmit lets the turn it admits run to completion before the
// submit returns, which is the interleaving a turn that ends at once produces.
type settlesWithinSubmit struct {
	control.SessionAPI
	t   *testing.T
	got <-chan string
}

func (s settlesWithinSubmit) SubmitHTTPFormat(input, format string) control.Admission {
	admission := s.SessionAPI.SubmitHTTPFormat(input, format)
	select {
	case <-s.got:
	case <-time.After(testenv.Budget(s.t) / 2):
		s.t.Error("the turn never reached the runner")
		return admission
	}
	for deadline := time.Now().Add(testenv.Budget(s.t) / 2); s.Running(); {
		if time.Now().After(deadline) {
			s.t.Error("the turn never finished")
			return admission
		}
		time.Sleep(time.Millisecond)
	}
	return admission
}

// A turn the controller admitted is accepted, however soon it finished: the
// answer comes from admission, not from whether the turn is still running when
// the handler looks.
func TestSubmitOfATurnThatEndsAtOnceIsAccepted(t *testing.T) {
	got := make(chan string, 1)
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: fakeRunner{got: got}, Sink: bc, WorkspaceRoot: t.TempDir()})
	t.Cleanup(ctrl.Close)
	srv := httptest.NewServer(operatorHandler(New(settlesWithinSubmit{SessionAPI: ctrl, t: t, got: got}, bc, config.ServeConfig{})))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/submit", strings.NewReader(`{"input":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /submit = %d, want 202: the turn was admitted and ran", resp.StatusCode)
	}
}

// A submission the controller dropped is refused with 409 whatever else is
// going on around it.
func TestSubmitToAClosedControllerIsRefused(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: fakeRunner{got: make(chan string, 1)}, Sink: bc, WorkspaceRoot: t.TempDir()})
	srv := httptest.NewServer(operatorHandler(New(ctrl, bc, config.ServeConfig{})))
	defer srv.Close()
	ctrl.Close()

	resp, err := http.Post(srv.URL+"/submit", "application/json", strings.NewReader(`{"input":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("POST /submit = %d, want 409", resp.StatusCode)
	}
}
