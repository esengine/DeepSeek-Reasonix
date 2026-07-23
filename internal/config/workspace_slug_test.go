package config

import "testing"

// WorkspaceSlug folds case on Windows so equivalent spellings of one
// workspace (drive-letter case, Explorer renames) map to a single slug —
// the same key form agent.CanonicalSessionPath uses for session paths.
func TestWorkspaceSlugFoldsCaseOnWindows(t *testing.T) {
	setRuntimeGOOS(t, "windows")
	upper := WorkspaceSlug(`C:\Users\Dev\Proj`)
	lower := WorkspaceSlug(`c:\users\dev\proj`)
	if upper != lower {
		t.Fatalf("WorkspaceSlug case-split on windows: %q vs %q", upper, lower)
	}
	if want := "c--users-dev-proj"; upper != want {
		t.Fatalf("WorkspaceSlug = %q, want %q", upper, want)
	}
}

// Unix paths are case-sensitive: two spellings that differ in case are two
// different directories and must keep distinct slugs.
func TestWorkspaceSlugPreservesCaseOffWindows(t *testing.T) {
	setRuntimeGOOS(t, "linux")
	if got, want := WorkspaceSlug("/Users/Dev/Proj"), "-Users-Dev-Proj"; got != want {
		t.Fatalf("WorkspaceSlug = %q, want %q", got, want)
	}
	if WorkspaceSlug("/users/dev/proj") == WorkspaceSlug("/Users/Dev/Proj") {
		t.Fatal("WorkspaceSlug folded case off windows; unix paths are case-sensitive")
	}
}
