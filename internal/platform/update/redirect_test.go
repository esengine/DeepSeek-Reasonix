package update

import (
	"errors"
	"net/http"
	"testing"

	"reasonix/internal/safety/redirectguard"
)

// The mechanism is held by internal/safety/redirectguard. What this holds is the
// decision made here: Studio's own CDN is trusted, and a name that merely ends
// in ours is not.
func TestUpdateTrustsItsOwnReleaseHosts(t *testing.T) {
	follow := redirectguard.Follow(releaseHosts...)
	for _, tc := range []struct {
		url  string
		want bool
	}{
		{"https://dl.reasonix.io/studio/versions.json", true},
		{"https://objects.githubusercontent.com/blob/x", true},
		{"https://dl.reasonix.io.evil.test/x", false},
		{"http://dl.reasonix.io/x", false},
	} {
		req, err := http.NewRequest(http.MethodGet, tc.url, nil)
		if err != nil {
			t.Fatal(err)
		}
		if followed := follow(req, nil) == nil; followed != tc.want {
			t.Fatalf("follow(%q) followed=%v, want %v", tc.url, followed, tc.want)
		}
	}
}

// The guard sat unreferenced while the downloads it was written for followed
// redirects wherever they pointed: the shells assemble their client from
// netclient, which sets no policy at all. New is where that is closed, and it
// copies because that client is shared with the rest of the app.
func TestNewGuardsTheClientsItWasHanded(t *testing.T) {
	caller := &http.Client{}
	fallback := &http.Client{}
	u := New(Options{HTTP: caller, Fallback: fallback})

	if u.opts.HTTP.CheckRedirect == nil || u.opts.Fallback.CheckRedirect == nil {
		t.Fatal("an updater fetched release bytes with no redirect policy")
	}
	if caller.CheckRedirect != nil || fallback.CheckRedirect != nil {
		t.Fatal("the caller's own client was mutated; it is shared with the rest of the app")
	}
	if u.opts.HTTP == caller || u.opts.Fallback == fallback {
		t.Fatal("the guarded client is the caller's, not a copy of it")
	}
}

func TestNewKeepsAPolicyTheCallerAlreadyChose(t *testing.T) {
	chosen := func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	u := New(Options{HTTP: &http.Client{CheckRedirect: chosen}})
	if err := u.opts.HTTP.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatal("New replaced a redirect policy its caller had already decided")
	}
}

// A nil client is how the version panel is built before a proxy is resolved;
// guarding must not turn that into a client that exists.
func TestNewLeavesAnAbsentClientAbsent(t *testing.T) {
	if u := New(Options{}); u.opts.HTTP != nil || u.opts.Fallback != nil {
		t.Fatal("guarding invented a client out of nil")
	}
}
