package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func checkAgainst(t *testing.T, body string, opts Options) (Status, error) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	opts.IndexURL, opts.HTTP = srv.URL, srv.Client()
	return New(opts).Check(context.Background())
}

const catalog = `{"schemaVersion":1,"versions":[
  {"version":"2.1.0","tag":"studio-v2.1.0","manifest":"https://x/2.1.0/latest.json"},
  {"version":"2.0.0","tag":"studio-v2.0.0","manifest":"https://x/2.0.0/latest.json"},
  {"version":"1.25.1","tag":"desktop-v1.25.1","manifest":"https://x/1.25.1/latest.json"}]}`

func TestCheckReportsAnUpdate(t *testing.T) {
	st, err := checkAgainst(t, catalog, Options{Current: "2.0.0"})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if st.Latest != "2.1.0" || !st.Available || !st.Newer || !st.AutoOK {
		t.Fatalf("status = %+v, want an installable 2.1.0", st)
	}
	// The catalog comes back whole: a rollback needs what is behind the running
	// build as much as an update needs what is ahead.
	if len(st.Entries) != 3 {
		t.Errorf("entries = %d, want the whole catalog", len(st.Entries))
	}
}

func TestCheckWillNotAutoInstallOverAPin(t *testing.T) {
	st, err := checkAgainst(t, catalog, Options{Current: "1.25.1", Pinned: "1.25.1"})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if st.AutoOK {
		t.Error("a pinned build must not be updated out from under the user")
	}
	if !st.Available || !st.Newer {
		t.Error("the pin suppresses installing, not reporting: 2.1.0 must still surface")
	}
}

// Which version is running is a local fact. A catalog that cannot be reached
// must not be able to erase it — that is the moment a user most needs to know
// what they are on.
func TestCheckKeepsTheRunningVersionWhenTheCatalogFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	st, err := New(Options{Current: "2.0.0", IndexURL: srv.URL, HTTP: srv.Client()}).Check(context.Background())
	if err == nil {
		t.Fatal("an unreachable catalog must be reported")
	}
	if st.Current != "2.0.0" {
		t.Errorf("current = %q, want it preserved through the failure", st.Current)
	}
	if st.Available || st.Newer || st.AutoOK {
		t.Errorf("status = %+v, want nothing offered from a catalog we could not read", st)
	}
}

func TestCheckOnAnUnpublishedBuild(t *testing.T) {
	st, err := checkAgainst(t, catalog, Options{Current: "2.2.0-dev"})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if st.Newer || st.AutoOK {
		t.Errorf("status = %+v, want no update offered to a build ahead of the catalog", st)
	}
	if !st.Available {
		t.Error("other versions exist and must remain reachable for a rollback")
	}
}
