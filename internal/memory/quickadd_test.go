package memory

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestAppendDocCreatesAndAppends verifies the "#" quick-add path: a fresh file
// gets a Notes section, and a second note joins the same section rather than
// scattering.
func TestAppendDocCreatesAndAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "REASONIX.md")

	if err := AppendDoc(path, "first note"); err != nil {
		t.Fatal(err)
	}
	if err := AppendDoc(path, "second note"); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	if strings.Count(body, quickAddHeading) != 1 {
		t.Fatalf("want exactly one Notes section, got:\n%s", body)
	}
	if !strings.Contains(body, "- first note") || !strings.Contains(body, "- second note") {
		t.Fatalf("notes missing:\n%s", body)
	}
	// Order preserved: first before second.
	if strings.Index(body, "first note") > strings.Index(body, "second note") {
		t.Fatalf("notes out of order:\n%s", body)
	}
}

// TestAppendDocPreservesExistingContent verifies a hand-written file keeps its
// content and the note lands under a Notes section appended to the end.
func TestAppendDocPreservesExistingContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "REASONIX.md")
	original := "# My project\n\nSome existing guidance the user wrote.\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := AppendDoc(path, "added via hash"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	body := string(b)
	if !strings.Contains(body, "Some existing guidance the user wrote.") {
		t.Fatalf("existing content lost:\n%s", body)
	}
	if !strings.Contains(body, "- added via hash") {
		t.Fatalf("note not added:\n%s", body)
	}
}

// TestAppendDocNormalizesNote ensures a multi-line note can't corrupt the
// single-line bullet format.
func TestAppendDocNormalizesNote(t *testing.T) {
	path := filepath.Join(t.TempDir(), "REASONIX.md")
	if err := AppendDoc(path, "line one\nline two\t with   spaces"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	body := string(b)
	if !strings.Contains(body, "- line one line two with spaces") {
		t.Fatalf("note not normalised to one line:\n%s", body)
	}
}

func TestAppendDocRejectsSymlinkDestination(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on common Windows setups")
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "AGENTS.md")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	if err := AppendDoc(link, "must stay local"); err == nil {
		t.Fatal("AppendDoc followed a symlink destination")
	}
	body, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "keep me" {
		t.Fatalf("outside target changed to %q", body)
	}
}
