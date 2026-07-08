package memory

import (
	"strings"
	"testing"
)

// TestSaveBoundsLongNames: a memory name distilled from a long
// title/description used to fail the write with ENAMETOOLONG because
// slug(name)+".md" exceeded the 255-byte filename component limit.
func TestSaveBoundsLongNames(t *testing.T) {
	s := StoreFor(t.TempDir(), t.TempDir())
	long := strings.Repeat("always-verify-the-desktop-session-lease-before-rebuild-", 8) + "end"
	path, err := s.Save(Memory{Name: long, Description: "d", Type: TypeProject, Body: "b"})
	if err != nil {
		t.Fatalf("Save long name: %v", err)
	}
	if path == "" {
		t.Fatal("Save returned empty path")
	}
	// Distinct long names must persist as distinct files.
	other := strings.Repeat("always-verify-the-desktop-session-lease-before-rebuild-", 8) + "other"
	path2, err := s.Save(Memory{Name: other, Description: "d", Type: TypeProject, Body: "b2"})
	if err != nil {
		t.Fatalf("Save second long name: %v", err)
	}
	if path2 == path {
		t.Fatalf("distinct long names collapsed to one file: %q", path)
	}
	if got := s.List(); len(got) != 2 {
		t.Fatalf("List() = %d memories, want 2", len(got))
	}
}

// TestSlugShortNamesUnchanged: short names keep their historical slug so
// existing memory files keep resolving after upgrade.
func TestSlugShortNamesUnchanged(t *testing.T) {
	if got := slug("My Setting: prefer 中文"); got != "my-setting-prefer-中文" {
		t.Fatalf("short slug changed: %q", got)
	}
}
