package update

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func rowVersions(rows []VersionEntry) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Version
	}
	return out
}

// The running build is the panel's statement of what runs and its only handle
// for pinning, so it cannot depend on a catalog fetch that may have failed.
func TestVersionRowsAlwaysCarryTheRunningBuild(t *testing.T) {
	rows := versionRows(nil, "2.0.0-dev")
	if len(rows) != 1 || rows[0].Version != "2.0.0-dev" || !rows[0].Current {
		t.Fatalf("rows = %+v, want the running build alone and marked current", rows)
	}
	if rows[0].Older {
		t.Error("the running build is not older than itself")
	}
	if got := versionRows(nil, ""); len(got) != 0 {
		t.Errorf("rows = %+v, want nothing to invent when there is no version", got)
	}
}

// A published catalog that does not list the running build (a release still
// publishing, or a local one) must not drop it.
func TestVersionRowsPrependAnUnpublishedRunningBuild(t *testing.T) {
	rows := versionRows([]IndexEntry{
		{Version: "1.25.1", Tag: "desktop-v1.25.1"},
	}, "2.0.0-dev")
	if got := rowVersions(rows); len(got) != 2 || got[0] != "2.0.0-dev" || got[1] != "1.25.1" {
		t.Fatalf("rows = %v, want the running build first", got)
	}
	if !rows[0].Current || rows[1].Current {
		t.Errorf("rows = %+v, want exactly the running build marked current", rows)
	}
	if !rows[1].Older {
		t.Error("1.25.1 is older than the running build and must say so")
	}
}

// When the catalog does carry it, the row is marked rather than duplicated.
func TestVersionRowsDoNotDuplicateTheRunningBuild(t *testing.T) {
	rows := versionRows([]IndexEntry{
		{Version: "2.0.0", Tag: "studio-v2.0.0"},
		{Version: "1.25.1", Tag: "desktop-v1.25.1"},
	}, "v2.0.0") // the leading v the tag and the manifest disagree about
	if got := rowVersions(rows); len(got) != 2 {
		t.Fatalf("rows = %v, want no duplicate row for the running build", got)
	}
	if !rows[0].Current || rows[0].Tag != "studio-v2.0.0" {
		t.Errorf("rows[0] = %+v, want the catalog entry marked current, keeping its tag", rows[0])
	}
}

// Newest first, and a newer published release is not "older".
func TestVersionRowsOrderNewestFirst(t *testing.T) {
	rows := versionRows([]IndexEntry{
		{Version: "1.9.0"},
		{Version: "2.1.0"},
		{Version: "1.25.0"},
	}, "1.25.0")
	if got := rowVersions(rows); len(got) != 3 || got[0] != "2.1.0" || got[1] != "1.25.0" || got[2] != "1.9.0" {
		t.Fatalf("rows = %v, want 2.1.0, 1.25.0, 1.9.0", got)
	}
	if rows[0].Older {
		t.Error("2.1.0 is ahead of the running build, not behind it")
	}
	if !rows[2].Older {
		t.Error("1.9.0 is behind the running build")
	}
}

type agentCapture struct{ got []string }

func (a *agentCapture) RoundTrip(r *http.Request) (*http.Response, error) {
	a.got = append(a.got, r.Header.Get("User-Agent"))
	return &http.Response{StatusCode: http.StatusForbidden, Status: "403 Forbidden", Header: http.Header{}, Body: http.NoBody, Request: r}, nil
}

func TestHubReadsTheCatalogAsStudio(t *testing.T) {
	cap := &agentCapture{}
	hubOver(context.Background(), Install{Version: "2.26.0"}, &http.Client{Transport: cap})
	want := UserAgent("2.26.0")
	if len(cap.got) != 1 || cap.got[0] != want || !strings.HasPrefix(want, "Reasonix-Studio/2.26.0 (") {
		t.Fatalf("catalog User-Agent = %q, want %q", cap.got, want)
	}
}
