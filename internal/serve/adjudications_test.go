package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/testenv"
)

func adjudicationServer(t *testing.T) (*httptest.Server, *control.Controller) {
	t.Helper()
	ctrl := control.New(control.Options{})
	t.Cleanup(ctrl.Close)
	ctrl.SetSessionPath(filepath.Join(testenv.TempDir(t), "s.jsonl"))
	s := New(ctrl, NewBroadcaster(), config.ServeConfig{})
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return srv, ctrl
}

func readAdjudications(t *testing.T, srv *httptest.Server) adjudicationView {
	t.Helper()
	resp, err := http.Get(srv.URL + "/adjudications")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /adjudications = %d", resp.StatusCode)
	}
	var view adjudicationView
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	return view
}

// The route answers with the whole truth, so a client replaces its copy rather
// than folding frames. An empty session still answers with both lists present:
// a missing key and an empty one read differently on the far side.
func TestAdjudicationsAnswersAWholeSnapshot(t *testing.T) {
	srv, _ := adjudicationServer(t)
	view := readAdjudications(t, srv)
	if view.SchemaVersion != adjudicationSchemaVersion {
		t.Fatalf("schema_version = %d", view.SchemaVersion)
	}
	if view.Active == nil || view.History == nil {
		t.Fatalf("lists must be present even when empty: %+v", view)
	}
}

// State is the kernel's word. "interrupted" appears in no journal record — it
// is what an open barrier means once nothing is waiting on it — so a client
// that tried to derive it would be missing the half only the host knows.
func TestAdjudicationsReportsInterruptedAndItsHistory(t *testing.T) {
	srv, ctrl := adjudicationServer(t)
	ctrl.OpenBarrierForTest("7", "ask", "Delete X?")

	view := readAdjudications(t, srv)
	if len(view.Active) != 1 || view.Active[0].State != stateInterrupted {
		t.Fatalf("active = %+v, want one interrupted barrier", view.Active)
	}
	if view.Active[0].Question != "Delete X?" || view.Active[0].BarrierID != "7" {
		t.Fatalf("active entry lost its provenance: %+v", view.Active[0])
	}
	if view.Active[0].OpenedAt == "" || !strings.Contains(view.Active[0].OpenedAt, "T") {
		t.Fatalf("opened_at is not a moment: %q", view.Active[0].OpenedAt)
	}

	ctrl.CloseBarrierForTest("7", "superseded", "turn-42")
	view = readAdjudications(t, srv)
	if len(view.Active) != 0 {
		t.Fatalf("a settled barrier is still active: %+v", view.Active)
	}
	if len(view.History) != 1 || view.History[0].State != "superseded" {
		t.Fatalf("history = %+v, want the superseded entry", view.History)
	}
	if view.History[0].SupersededBy != "turn-42" {
		t.Fatalf("history lost the successor: %+v", view.History[0])
	}
}

// Nothing accepts a barrier id back. The interrupted surface is provenance, and
// a route that took one would rebuild the answerable ghost the kernel refuses
// to offer.
func TestNoRouteAcceptsABarrierID(t *testing.T) {
	srv, ctrl := adjudicationServer(t)
	ctrl.OpenBarrierForTest("7", "ask", "Delete X?")
	for _, path := range []string{"/answer", "/approve"} {
		resp, err := http.Post(srv.URL+path, "application/json", strings.NewReader(`{"id":"7"}`))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if view := readAdjudications(t, srv); len(view.Active) != 1 {
			t.Fatalf("POST %s settled an interrupted barrier: %+v", path, view)
		}
	}
}
