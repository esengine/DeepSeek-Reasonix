package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"reasonix/internal/control"
)

// fakeRunner stands in for an agent.Runner: it records the composed input and
// returns without emitting model events, so the controller's TurnDone is the
// observable signal.
type fakeRunner struct{ got chan string }

func (f fakeRunner) Run(_ context.Context, input string) error { f.got <- input; return nil }

func TestServeSubmitRunsAndBroadcastsTurnDone(t *testing.T) {
	bc := NewBroadcaster()
	got := make(chan string, 1)
	ctrl := control.New(control.Options{Runner: fakeRunner{got: got}, Sink: bc})
	srv := httptest.NewServer(New(ctrl, bc).Handler())
	defer srv.Close()

	sub, cancel := bc.Subscribe() // observe the broadcast deterministically
	defer cancel()

	resp, err := http.Post(srv.URL+"/submit", "application/json", strings.NewReader(`{"input":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("submit status = %d, want 202", resp.StatusCode)
	}

	select {
	case in := <-got:
		if in != "hi" {
			t.Errorf("runner ran %q, want hi", in)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runner never ran")
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case data := <-sub:
			var w wireEvent
			if err := json.Unmarshal(data, &w); err == nil && w.Kind == "turn_done" {
				return
			}
		case <-deadline:
			t.Fatal("never saw turn_done on the stream")
		}
	}
}

func TestServeEndpoints(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc}) // no runner needed for these
	srv := httptest.NewServer(New(ctrl, bc).Handler())
	defer srv.Close()

	if resp, err := http.Get(srv.URL + "/history"); err != nil || resp.StatusCode != 200 {
		t.Fatalf("history = %v / %v", resp, err)
	}

	if resp, _ := http.Get(srv.URL + "/context"); resp.StatusCode != 200 {
		t.Errorf("context status = %d", resp.StatusCode)
	}

	resp, err := http.Post(srv.URL+"/plan", "application/json", strings.NewReader(`{"on":true}`))
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("plan = %v / status %d", err, resp.StatusCode)
	}
	if c := ctrl.Compose("x"); !strings.Contains(c, "Plan mode") {
		t.Error("/plan {on:true} should have enabled plan mode (Compose would prepend the marker)")
	}

	if resp, _ := http.Post(srv.URL+"/submit", "application/json", strings.NewReader(`{}`)); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("empty submit should be 400, got %d", resp.StatusCode)
	}
}

func TestServeCancelEndpoint(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc})
	srv := httptest.NewServer(New(ctrl, bc).Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/cancel", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("cancel status = %d, want 204", resp.StatusCode)
	}
}

func TestServeApproveMissingID(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc})
	srv := httptest.NewServer(New(ctrl, bc).Handler())
	defer srv.Close()

	// Missing id should return 400.
	resp, err := http.Post(srv.URL+"/approve", "application/json", strings.NewReader(`{"allow":true}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("approve missing id = %d, want 400", resp.StatusCode)
	}

	// Malformed JSON should return 400.
	resp2, _ := http.Post(srv.URL+"/approve", "application/json", strings.NewReader(`{bad`))
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusBadRequest {
		t.Errorf("approve bad json = %d, want 400", resp2.StatusCode)
	}
}

func TestServeNewSessionEndpoint(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc})
	srv := httptest.NewServer(New(ctrl, bc).Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/new", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("new session = %d, want 204", resp.StatusCode)
	}
}

func TestServeCompactEndpoint(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc})
	srv := httptest.NewServer(New(ctrl, bc).Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/compact", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("compact = %d, want 204", resp.StatusCode)
	}
}

func TestServeIndexPage(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc})
	srv := httptest.NewServer(New(ctrl, bc).Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("index status = %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("index content-type = %q, want text/html", ct)
	}
}

func TestServeSubmitMalformedJSON(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc})
	srv := httptest.NewServer(New(ctrl, bc).Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/submit", "application/json", strings.NewReader(`{not json`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed submit = %d, want 400", resp.StatusCode)
	}
}

func TestServePlanMalformedJSON(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc})
	srv := httptest.NewServer(New(ctrl, bc).Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/plan", "application/json", strings.NewReader(`{bad`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed plan = %d, want 400", resp.StatusCode)
	}
}

func TestServeContextEndpoint(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc})
	srv := httptest.NewServer(New(ctrl, bc).Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/context")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("context status = %d", resp.StatusCode)
	}
	var body map[string]int
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode context: %v", err)
	}
	// Before any turn, used should be 0.
	if body["used"] != 0 {
		t.Errorf("used = %d, want 0", body["used"])
	}
}
