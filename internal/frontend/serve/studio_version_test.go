package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/platform/update"
)

func studioServer(t *testing.T, in *update.Install) *httptest.Server {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	h := NewHub(HubOptions{Install: in})
	srv := httptest.NewServer(operatorHandler(h))
	t.Cleanup(srv.Close)
	return srv
}

// A kernel nobody declared an install for is not a Studio: it has no build of
// its own to report or change. It says so with a code, because "not a Studio"
// and "a catalog that came back empty" are different answers and a panel that
// folds them together offers to update a server with no application around it.
func TestVersionRoutesRefuseByNameWithoutAnInstall(t *testing.T) {
	srv := studioServer(t, nil)

	resp, err := http.Get(srv.URL + "/studio/versions")
	if err != nil {
		t.Fatal(err)
	}
	if code := refusalCode(t, resp); code != codeNoInstall {
		t.Fatalf("GET /studio/versions refused with %q, want %q", code, codeNoInstall)
	}

	resp, err = http.Post(srv.URL+"/studio/pin", "application/json", bytes.NewReader([]byte(`{"version":"2.9.0"}`)))
	if err != nil {
		t.Fatal(err)
	}
	if code := refusalCode(t, resp); code != codeNoInstall {
		t.Fatalf("POST /studio/pin refused with %q, want %q", code, codeNoInstall)
	}
}

func refusalCode(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body.Code
}

// Pinning is the half of a rollback that outlives the install, so what the
// route must actually do is write it where the next launch reads it.
func TestPinRouteWritesTheHoldAndReleasesIt(t *testing.T) {
	srv := studioServer(t, &update.Install{Version: "2.10.0"})

	pin := func(body string) int {
		t.Helper()
		resp, err := http.Post(srv.URL+"/studio/pin", "application/json", bytes.NewReader([]byte(body)))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	if code := pin(`{"version":"2.9.0"}`); code != http.StatusNoContent {
		t.Fatalf("pin = %d, want 204", code)
	}
	if got := config.LoadForEdit(config.UserConfigPath()).DesktopPinnedVersion(); got != "2.9.0" {
		t.Fatalf("pinned version on disk = %q, want 2.9.0", got)
	}
	// An empty version is a value, not a missing field: it is the only way back
	// to following the catalog.
	if code := pin(`{"version":""}`); code != http.StatusNoContent {
		t.Fatalf("release = %d, want 204", code)
	}
	if got := config.LoadForEdit(config.UserConfigPath()).DesktopPinnedVersion(); got != "" {
		t.Fatalf("pinned version on disk = %q, want the hold released", got)
	}
}

func TestPinRouteRefusesABodyItCannotRead(t *testing.T) {
	srv := studioServer(t, &update.Install{Version: "2.10.0"})
	resp, err := http.Post(srv.URL+"/studio/pin", "application/json", bytes.NewReader([]byte(`not json`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	// The identity, not the sentence: a frontend tells refusals apart by code.
	if body.Code != codePinRejected {
		t.Fatalf("code = %q, want %q", body.Code, codePinRejected)
	}
}

func notesGet(t *testing.T, srv *httptest.Server, path string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body
}

func TestNotesRouteRefusesByNameWithoutAnInstall(t *testing.T) {
	srv := studioServer(t, nil)
	status, body := notesGet(t, srv, "/studio/versions/2.31.0/notes")
	if status != http.StatusNotFound || body["code"] != codeNoInstall {
		t.Fatalf("got %d %v, want 404 %s", status, body, codeNoInstall)
	}
}

func TestNotesRouteSaysWhichKindOfFailureItWas(t *testing.T) {
	srv := studioServer(t, &update.Install{Version: "2.10.0"})
	var retry []bool
	prev := notesReader
	t.Cleanup(func() { notesReader = prev })
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{update.ErrNotesBadVersion, http.StatusBadRequest, codeNotesBadVersion},
		{update.ErrNotesAbsent, http.StatusNotFound, codeNotesAbsent},
		{fmt.Errorf("%w: GET: 403", update.ErrNotesUnreachable), http.StatusBadGateway, codeNotesUnreachable},
		{update.ErrNotesTooLarge, http.StatusBadGateway, codeNotesTooLarge},
	} {
		notesReader = func(_ context.Context, _ update.Install, _ string, r bool) (update.VersionNotes, error) {
			retry = append(retry, r)
			return update.VersionNotes{}, tc.err
		}
		status, body := notesGet(t, srv, "/studio/versions/2.31.0/notes?retry=1")
		if status != tc.status || body["code"] != tc.code {
			t.Errorf("%v: got %d %v, want %d %s", tc.err, status, body, tc.status, tc.code)
		}
	}
	if len(retry) == 0 || !retry[0] {
		t.Fatalf("retry=1 was not passed through: %v", retry)
	}
}

func TestNotesRouteAnswersTheDocument(t *testing.T) {
	srv := studioServer(t, &update.Install{Version: "2.10.0"})
	prev := notesReader
	t.Cleanup(func() { notesReader = prev })
	var asked string
	notesReader = func(_ context.Context, in update.Install, v string, _ bool) (update.VersionNotes, error) {
		asked = v
		return update.VersionNotes{Version: "2.31.0", Markdown: "# hi", Cached: true}, nil
	}
	status, body := notesGet(t, srv, "/studio/versions/v2.31.0/notes")
	if status != http.StatusOK || body["markdown"] != "# hi" || body["cached"] != true || asked != "v2.31.0" {
		t.Fatalf("got %d %v (asked %q)", status, body, asked)
	}
}

type stubTransport func(*http.Request) (*http.Response, error)

func (f stubTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNotesRouteKeepsADocumentOnceAndNeverAnErrorPage(t *testing.T) {
	dir := t.TempDir()
	var hits int
	contentType := "text/html"
	body := "<html>oops</html>"
	transport := stubTransport(func(r *http.Request) (*http.Response, error) {
		hits++
		if r.URL.String() != "https://dl.reasonix.io/studio/notes/2.31.0.md" {
			t.Errorf("fetched %s", r.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	srv := httptest.NewServer(operatorHandler(NewHub(HubOptions{Install: &update.Install{Version: "2.10.0"}, NotesTransport: transport, NotesDir: dir})))
	t.Cleanup(srv.Close)

	status, got := notesGet(t, srv, "/studio/versions/2.31.0/notes")
	if status != http.StatusBadGateway || got["code"] != codeNotesUnreachable {
		t.Fatalf("html page: got %d %v", status, got)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("the refused page was written: %v", entries)
	}

	contentType, body = "text/markdown; charset=utf-8", "# notes"
	status, got = notesGet(t, srv, "/studio/versions/2.31.0/notes?retry=1")
	if status != http.StatusOK || got["markdown"] != "# notes" || got["cached"] != false {
		t.Fatalf("markdown: got %d %v", status, got)
	}
	before := hits
	status, got = notesGet(t, srv, "/studio/versions/2.31.0/notes")
	if status != http.StatusOK || got["cached"] != true || hits != before {
		t.Fatalf("second read: got %d %v, hits %d -> %d", status, got, before, hits)
	}
}

func TestNotesRouteRefusesAnOverlongVersion(t *testing.T) {
	srv := studioServer(t, &update.Install{Version: "2.10.0"})
	status, got := notesGet(t, srv, "/studio/versions/2.0.0-"+strings.Repeat("a", 80)+"/notes")
	if status != http.StatusBadRequest || got["code"] != codeNotesBadVersion {
		t.Fatalf("got %d %v", status, got)
	}
}
