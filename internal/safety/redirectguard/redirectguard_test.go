package redirectguard

import (
	"errors"
	"net/http"
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
