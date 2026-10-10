package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
)

func indexServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The index is the shape scripts/update-versions-index.sh publishes.
const sampleIndex = `{
  "schemaVersion": 1,
  "updatedAt": "2026-09-01T00:00:00Z",
  "versions": [
    {"version":"2.0.0","tag":"studio-v2.0.0","channel":"stable","publishedAt":"2026-09-01T00:00:00Z",
     "manifest":"https://dl.reasonix.io/studio-v2.0.0/latest.json"},
    {"version":"1.25.1","tag":"desktop-v1.25.1","channel":"stable","publishedAt":"2026-08-12T10:00:00Z",
     "manifest":"https://dl.reasonix.io/desktop-v1.25.1/latest.json"}
  ]
}`

func TestFetchIndexReadsPublishedShape(t *testing.T) {
	srv := indexServer(t, 200, sampleIndex)
	idx, err := FetchIndex(context.Background(), srv.Client(), srv.URL, "")
	if err != nil {
		t.Fatalf("FetchIndex: %v", err)
	}
	if len(idx.Versions) != 2 {
		t.Fatalf("got %d versions, want 2", len(idx.Versions))
	}
	if idx.Versions[0].Tag != "studio-v2.0.0" {
		t.Errorf("first entry = %q, want the newest release", idx.Versions[0].Tag)
	}
	if idx.Versions[0].Manifest == "" {
		t.Error("an entry without its manifest cannot be rolled back to")
	}
}

// A newer publisher may add fields. Refusing the whole list would strand a user
// on a broken build with no way back — exactly when they need this most.
func TestFetchIndexToleratesUnknownFields(t *testing.T) {
	srv := indexServer(t, 200, `{"schemaVersion":9,"future":{"x":1},"versions":[
	  {"version":"2.0.0","tag":"studio-v2.0.0","manifest":"https://x/latest.json","extra":true}]}`)
	idx, err := FetchIndex(context.Background(), srv.Client(), srv.URL, "")
	if err != nil {
		t.Fatalf("FetchIndex: %v", err)
	}
	if len(idx.Versions) != 1 {
		t.Fatalf("got %d versions, want the entry a newer index still carries", len(idx.Versions))
	}
}

// One malformed row must not cost the user every other version.
func TestFetchIndexDropsUnusableEntries(t *testing.T) {
	srv := indexServer(t, 200, `{"schemaVersion":1,"versions":[
	  {"version":"","manifest":"https://x/latest.json"},
	  {"version":"1.9.0","manifest":""},
	  {"version":"2.0.0","tag":"studio-v2.0.0","manifest":"https://x/2/latest.json"}]}`)
	idx, err := FetchIndex(context.Background(), srv.Client(), srv.URL, "")
	if err != nil {
		t.Fatalf("FetchIndex: %v", err)
	}
	if len(idx.Versions) != 1 || idx.Versions[0].Version != "2.0.0" {
		t.Fatalf("kept %+v, want only the usable entry", idx.Versions)
	}
}

func TestFetchIndexReportsAnUnavailableCatalog(t *testing.T) {
	srv := indexServer(t, 503, "nope")
	if _, err := FetchIndex(context.Background(), srv.Client(), srv.URL, ""); err == nil {
		t.Error("a 503 must surface, not read as an empty catalog")
	}
}

func TestRollbackableExcludesTheRunningVersion(t *testing.T) {
	srv := indexServer(t, 200, sampleIndex)
	idx, err := FetchIndex(context.Background(), srv.Client(), srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	got := idx.Rollbackable("v2.0.0") // the leading v the tag disagrees about
	if len(got) != 1 || got[0].Version != "1.25.1" {
		t.Fatalf("rollbackable = %+v, want only the older release", got)
	}
	if !got[0].IsOlderThan("2.0.0") {
		t.Error("1.25.1 must read as older than 2.0.0")
	}
}

func TestCompareVersionsOrdersReleases(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"2.0.0", "1.25.1", 1},
		{"1.25.1", "2.0.0", -1},
		{"1.25.1", "v1.25.1", 0},
		{"1.9.0", "1.25.0", -1}, // numeric, not lexical: 9 < 25
		{"2.0.0", "2.0.0-rc1", 1},
	}
	for _, tc := range cases {
		if got := CompareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestFetchManifestAtRequiresAVersion(t *testing.T) {
	ok := indexServer(t, 200, `{"version":"1.25.1","platforms":{}}`)
	m, err := FetchManifestAt(context.Background(), ok.Client(), ok.URL, "")
	if err != nil {
		t.Fatalf("FetchManifestAt: %v", err)
	}
	if m.Version != "1.25.1" {
		t.Errorf("version = %q", m.Version)
	}
	empty := indexServer(t, 200, `{"platforms":{}}`)
	if _, err := FetchManifestAt(context.Background(), empty.Client(), empty.URL, ""); err == nil {
		t.Error("a manifest without a version must not be installable")
	}
}

// A prerelease must read as older than its own release, so moving forward off
// an rc is never presented as a rollback.
func TestPrereleaseRanksBelowItsRelease(t *testing.T) {
	idx := &Index{Versions: []IndexEntry{
		{Version: "2.0.0", Manifest: "https://x/2/latest.json"},
		{Version: "1.25.1", Manifest: "https://x/1/latest.json"},
	}}
	got := idx.Rollbackable("2.0.0-rc1")
	if len(got) != 2 {
		t.Fatalf("rollbackable = %+v, want both entries offered from an rc", got)
	}
	if got[0].IsOlderThan("2.0.0-rc1") {
		t.Error("2.0.0 must read as newer than 2.0.0-rc1 — that move is an update")
	}
	if !got[1].IsOlderThan("2.0.0-rc1") {
		t.Error("1.25.1 must read as older than 2.0.0-rc1")
	}
}

// The catalog is per line. An empty URL used to fall back to a constant naming
// the desktop line's, which nothing publishes, so the mistake surfaced as a 404.
func TestFetchIndexRequiresACatalogURL(t *testing.T) {
	if _, err := FetchIndex(context.Background(), http.DefaultClient, "  ", ""); err == nil {
		t.Fatal("an empty catalog URL was accepted")
	}
}

func TestCatalogReadsCarryTheClientIdentity(t *testing.T) {
	var agents []string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agents = append(agents, r.Header.Get("User-Agent"))
		_, _ = w.Write([]byte(strings.ReplaceAll(sampleIndex, "https://dl.reasonix.io", srv.URL)))
	}))
	defer srv.Close()
	ctx := context.Background()
	if _, err := fetchJSON(ctx, srv.Client(), srv.URL, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := FetchIndex(ctx, srv.Client(), srv.URL, ""); err != nil {
		t.Fatal(err)
	}
	_, _ = FetchManifestAt(ctx, srv.Client(), srv.URL, "")
	u := New(Options{HTTP: srv.Client(), IndexURL: srv.URL})
	if _, err := u.Check(ctx); err != nil {
		t.Fatal(err)
	}
	_, _ = u.ManifestFor(ctx, "2.0.0")
	for i, got := range agents {
		want := provider.ClientUserAgent()
		if i >= 3 {
			want = UserAgent(provider.ClientVersion())
		}
		if got != want {
			t.Errorf("request %d User-Agent = %q, want %q", i, got, want)
		}
	}
	if len(agents) < 6 {
		t.Fatalf("saw %d requests, want at least 6", len(agents))
	}
}

func TestUpdaterUserAgentOptionWinsOverTheDefault(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(sampleIndex))
	}))
	defer srv.Close()
	u := New(Options{HTTP: srv.Client(), IndexURL: srv.URL, UserAgent: "custom/1"})
	if _, err := u.Check(context.Background()); err != nil || got != "custom/1" {
		t.Fatalf("err=%v User-Agent=%q", err, got)
	}
}
