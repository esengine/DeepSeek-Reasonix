package store

import (
	"strings"
	"testing"
)

func TestRemoteWorkspaceSlug(t *testing.T) {
	// The slug carries a readable stem plus a hash of the exact path. Assert the
	// readable prefix and that a trailing slash is normalized to the same slug.
	if got := RemoteWorkspaceSlug("/home/dev/projects/app"); !strings.HasPrefix(got, "home-dev-projects-app-") {
		t.Errorf("slug = %q, want home-dev-projects-app-<hash> prefix", got)
	}
	if RemoteWorkspaceSlug("/home/dev/projects/app") != RemoteWorkspaceSlug("/home/dev/projects/app/") {
		t.Error("trailing slash produced a different slug")
	}
	if got := RemoteWorkspaceSlug("/"); !strings.HasPrefix(got, "root-") {
		t.Errorf("root slug = %q, want root-<hash>", got)
	}
}

// TestRemoteWorkspaceSlugNoCollision is the reviewer's reproduction: distinct
// paths that reduce to the same separator-replaced stem must not share serve
// state files.
func TestRemoteWorkspaceSlugNoCollision(t *testing.T) {
	a := RemoteWorkspaceSlug("/srv/a-b")
	b := RemoteWorkspaceSlug("/srv/a/b")
	if a == b {
		t.Fatalf("/srv/a-b and /srv/a/b collided to the same slug %q", a)
	}
	// Same path (idempotent).
	if RemoteWorkspaceSlug("/srv/a/b") != b {
		t.Error("slug is not deterministic")
	}
}

func TestRemoteWorkspaceSlugBoundsLongPaths(t *testing.T) {
	long := "/data/" + strings.Repeat("verydeepdir/", 40) + "app"
	slug := RemoteWorkspaceSlug(long)
	if len(slug) > 200 {
		t.Fatalf("slug length %d exceeds bound", len(slug))
	}
	other := RemoteWorkspaceSlug(long + "2")
	if slug == other {
		t.Fatal("distinct long paths collapsed to one slug")
	}
}

func TestRemoteServeFileNames(t *testing.T) {
	slug := "home-dev-app"
	if got := RemoteServeStateName(slug); got != "serve-home-dev-app.json" {
		t.Fatalf("state name = %q", got)
	}
	if got := RemoteServeTokenName(slug); got != "serve-home-dev-app.token" {
		t.Fatalf("token name = %q", got)
	}
	if got := RemoteServeLogName(slug); got != "serve-home-dev-app.log" {
		t.Fatalf("log name = %q", got)
	}
	if got := RemoteServePortName(slug); got != "serve-home-dev-app.port" {
		t.Fatalf("port name = %q", got)
	}
	if got := RemoteServePidName(slug); got != "serve-home-dev-app.pid" {
		t.Fatalf("pid name = %q", got)
	}
}
