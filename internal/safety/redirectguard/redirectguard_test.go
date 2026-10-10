package redirectguard

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

// The hosts a Studio release is served from, used as the subject throughout.
var releaseHosts = []string{"reasonix.io", ".reasonix.io", "github.com", ".githubusercontent.com"}

func hop(t *testing.T, raw string, hops int) error {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		t.Fatalf("NewRequest(%q): %v", raw, err)
	}
	return Follow(releaseHosts...)(req, make([]*http.Request, hops))
}

// One case per field, because a guard that stopped reading the scheme would
// still pass a test that only ever handed it another hostname.
func TestFollowRefusesEachUnsafeShape(t *testing.T) {
	for _, tc := range []struct{ name, url string }{
		{"a plain-HTTP downgrade", "http://dl.reasonix.io/studio/x.tar.gz"},
		{"credentials in the authority", "https://user:pw@dl.reasonix.io/x"},
		{"a port, which no release host answers on", "https://dl.reasonix.io:8443/x"},
		{"a host that merely ends in ours", "https://dl.reasonix.io.evil.test/x"},
		{"an unrelated host", "https://evil.test/x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := hop(t, tc.url, 1)
			if err == nil {
				t.Fatalf("Follow(%q) = nil, want a refusal", tc.url)
			}
			// The identity, not the sentence: a caller telling a refusal from a
			// transport failure has to reach it through errors.Is.
			if !errors.Is(err, ErrRefused) {
				t.Fatalf("Follow(%q) = %v, which does not carry ErrRefused", tc.url, err)
			}
		})
	}
}

func TestFollowStopsAChain(t *testing.T) {
	if err := hop(t, "https://dl.reasonix.io/x", maxHops); err == nil {
		t.Fatalf("a chain of %d hops was followed; want it stopped", maxHops)
	}
}

func TestFollowRefusesARequestWithNoTarget(t *testing.T) {
	if err := Follow(releaseHosts...)(nil, nil); !errors.Is(err, ErrRefused) {
		t.Fatal("a redirect with no request at all was allowed through")
	}
}

// The hosts releases actually come from. A guard that refused these would break
// every download rather than protect one.
func TestFollowPassesTheHostsReleasesComeFrom(t *testing.T) {
	for _, raw := range []string{
		"https://dl.reasonix.io/studio/versions.json",
		"https://reasonix.io/studio/x",
		"https://github.com/esengine/DeepSeek-Reasonix/releases/download/v1/x",
		"https://objects.githubusercontent.com/blob/x",
	} {
		if err := hop(t, raw, 1); err != nil {
			t.Fatalf("Follow(%q) = %v, want it followed", raw, err)
		}
	}
}

// A resolver accepts both of these for the same name; a string comparison
// accepts neither, and the refusal would look like an attack rather than a URL
// that spelled its host unusually.
func TestFollowComparesHostsTheWayDNSDoes(t *testing.T) {
	for _, raw := range []string{"https://DL.Reasonix.IO/x", "https://dl.reasonix.io./x"} {
		if err := hop(t, raw, 1); err != nil {
			t.Fatalf("Follow(%q) = %v, want the same name matched", raw, err)
		}
	}
}

// An empty list is the shape a caller lands on by passing a slice it never
// filled; it must refuse everything rather than wave everything through.
func TestFollowWithNoHostsTrustsNothing(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://github.com/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := Follow()(req, nil); !errors.Is(err, ErrRefused) {
		t.Fatal("a guard with no trusted hosts followed a redirect anyway")
	}
}

func TestStayOnOriginFollowsOnlyTheHostItStartedOn(t *testing.T) {
	for _, tc := range []struct {
		name, from, to string
		hops           int
		want           bool
	}{
		{"same host and port", "http://localhost:8080/v1", "http://localhost:8080/v2", 1, true},
		{"path-only move on https", "https://api.example.com/v1", "https://api.example.com/v2", 1, true},
		{"http to https upgrade on default ports", "http://relay.example.com/v1", "https://relay.example.com/v1", 1, true},
		{"host case and root label", "https://API.example.com/v1", "https://api.example.com./v1", 1, true},
		{"another port on the same host", "http://127.0.0.1:1/v1", "http://127.0.0.1:2/v1", 1, false},
		{"another host", "https://api.example.com/v1", "https://evil.test/v1", 1, false},
		{"a sibling subdomain", "https://api.example.com/v1", "https://cdn.example.com/v1", 1, false},
		{"https downgraded to http", "https://api.example.com/v1", "http://api.example.com/v1", 1, false},
		{"credentials in the target", "https://api.example.com/v1", "https://u:p@api.example.com/v1", 1, false},
		{"too many hops", "https://api.example.com/v1", "https://api.example.com/v2", 10, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first, err := http.NewRequest(http.MethodPost, tc.from, nil)
			if err != nil {
				t.Fatal(err)
			}
			next, err := http.NewRequest(http.MethodPost, tc.to, nil)
			if err != nil {
				t.Fatal(err)
			}
			via := make([]*http.Request, tc.hops)
			for i := range via {
				via[i] = first
			}
			err = StayOnOrigin()(next, via)
			if (err == nil) != tc.want {
				t.Fatalf("StayOnOrigin %s -> %s = %v, want follow=%v", tc.from, tc.to, err, tc.want)
			}
			if err != nil && !errors.Is(err, ErrRefused) {
				t.Fatalf("refusal %v does not carry ErrRefused", err)
			}
		})
	}
}

func TestStayOnOriginNamesTheHostItWasSentTo(t *testing.T) {
	first, _ := http.NewRequest(http.MethodPost, "https://example.com/v1", nil)
	next, _ := http.NewRequest(http.MethodPost, "https://www.example.com/v1?token=x", nil)
	err := StayOnOrigin()(next, []*http.Request{first})
	var left *OriginLeft
	if !errors.As(err, &left) || left.From != "example.com" || left.To != "www.example.com" {
		t.Fatalf("err = %#v, want OriginLeft example.com -> www.example.com", err)
	}
	if !errors.Is(err, ErrRefused) || strings.Contains(err.Error(), "token") {
		t.Fatalf("err = %v: must be ErrRefused and carry no query", err)
	}
}

func TestRefusalsAreRecognisedAndPermanent(t *testing.T) {
	var p interface{ Permanent() bool }
	if !errors.As(ErrRefused, &p) || !p.Permanent() {
		t.Fatal("ErrRefused must report itself permanent")
	}
	if err := Follow(releaseHosts...)(&http.Request{}, nil); !errors.Is(err, ErrRefused) {
		t.Fatalf("a request with no URL = %v, want ErrRefused", err)
	}
	empty, _ := http.NewRequest(http.MethodGet, "https:///x", nil)
	if err := Follow(releaseHosts...)(empty, nil); !errors.Is(err, ErrRefused) {
		t.Fatalf("a URL with no hostname = %v, want ErrRefused", err)
	}
	if err := StayOnOrigin()(empty, nil); !errors.Is(err, ErrRefused) {
		t.Fatalf("no earlier request to stay on = %v, want ErrRefused", err)
	}
	if permitted("", releaseHosts) || permitted(" . ", releaseHosts) {
		t.Fatal("an empty host was permitted")
	}
}
