package fileutil

import (
	"path/filepath"
	"testing"
)

func TestUnderExcludesTheRootItself(t *testing.T) {
	root := t.TempDir()
	if Under(root, root) {
		t.Error("Under(root, root) = true; the root is not below itself")
	}
	if !Under(filepath.Join(root, "child"), root) {
		t.Error("a child is below the root")
	}
}

func TestAtOrUnderIncludesTheRootItself(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{root, filepath.Join(root, "child"), filepath.Join(root, "a", "b")} {
		if !AtOrUnder(path, root) {
			t.Errorf("AtOrUnder(%q, root) = false", path)
		}
	}
}

func TestContainmentRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "sibling")
	for _, path := range []string{outside, filepath.Join(root, "..", "sibling"), filepath.Dir(root)} {
		if AtOrUnder(path, root) {
			t.Errorf("AtOrUnder(%q, root) = true", path)
		}
		if Under(path, root) {
			t.Errorf("Under(%q, root) = true", path)
		}
	}
}

// An interior ".." must be resolved before the comparison, or a path spells
// its way inside a root it never reaches.
func TestContainmentResolvesInteriorDotDot(t *testing.T) {
	root := t.TempDir()
	if !AtOrUnder(filepath.Join(root, "a", "..", "b"), root) {
		t.Error("a path that cleans to root/b is inside root")
	}
	if AtOrUnder(filepath.Join(root, "a", "..", "..", "escaped"), root) {
		t.Error("a path that cleans outside root is not inside it")
	}
}

func TestContainmentRejectsEmptySides(t *testing.T) {
	root := t.TempDir()
	if AtOrUnder("", root) || AtOrUnder(root, "") || Under("", root) || Under(root, "") {
		t.Error("an empty side answers no question and is not containment")
	}
}
