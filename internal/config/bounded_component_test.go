package config

import (
	"strings"
	"testing"
)

// TestWorkspaceSlugShortPathUnchanged: existing short paths must keep their
// exact historical slug so on-disk project directories keep resolving.
func TestWorkspaceSlugShortPathUnchanged(t *testing.T) {
	setRuntimeGOOS(t, "linux")
	got := WorkspaceSlug("/Users/me/proj")
	if got != "-Users-me-proj" {
		t.Fatalf("WorkspaceSlug short path = %q, want -Users-me-proj", got)
	}
}

// TestWorkspaceSlugBoundsDeepPaths: a workspace path whose flattened slug
// exceeds the 255-byte filename component limit used to make every derived
// directory (project sessions, MemoryCompiler, auto-memory) uncreatable with
// ENAMETOOLONG.
func TestWorkspaceSlugBoundsDeepPaths(t *testing.T) {
	deep := "/data/" + strings.Repeat("deeply-nested-workspace-segment/", 12) + "proj"
	slug := WorkspaceSlug(deep)
	if len(slug) > 255 {
		t.Fatalf("slug length = %d, exceeds 255-byte component limit", len(slug))
	}
	// Distinct deep paths must not collapse to one slug.
	other := "/data/" + strings.Repeat("deeply-nested-workspace-segment/", 12) + "proj2"
	if WorkspaceSlug(other) == slug {
		t.Fatal("distinct deep paths share one slug")
	}
	// Deterministic across calls.
	if WorkspaceSlug(deep) != slug {
		t.Fatal("slug is not deterministic")
	}
}

func TestBoundFilenameComponentRuneBoundary(t *testing.T) {
	// A multi-byte-rune input truncated mid-rune must back off to a valid
	// boundary, not emit invalid UTF-8 into a filename.
	long := strings.Repeat("工作区", 100)
	got := BoundFilenameComponent(long, 255)
	if len(got) > 255 {
		t.Fatalf("bounded component length = %d, want <= 255", len(got))
	}
	if strings.Contains(got, "�") {
		t.Fatalf("bounded component contains replacement char: %q", got)
	}
}
