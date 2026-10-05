package memory

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

// TestAppendDocCreatesAndAppends verifies the "#" quick-add path: a fresh file
// gets a Notes section, and a second note joins the same section rather than
// scattering.
func TestAppendDocCreatesAndAppends(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "REASONIX.md")

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
	path := filepath.Join(testenv.TempDir(t), "REASONIX.md")
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
	path := filepath.Join(testenv.TempDir(t), "REASONIX.md")
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
	outside := filepath.Join(testenv.TempDir(t), "outside.md")
	if err := os.WriteFile(outside, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(testenv.TempDir(t), "AGENTS.md")
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

// TestAppendDocPropagatesReadErrors pins the fail-safe contract: a doc-memory
// file that exists but cannot be read (permission, encoding, IO) must surface
// the read error instead of treating the file as empty and overwriting it with
// a fresh Notes-only document, which would silently destroy existing content.
func TestAppendDocPropagatesReadErrors(t *testing.T) {
	// A directory is a real path that os.ReadFile refuses with a non-NotExist
	// error (EISDIR), exercising the "exists but unreadable" branch without
	// relying on permission bits that vary across platforms and CI users.
	dir := t.TempDir()
	if err := AppendDoc(dir, "note"); err == nil {
		t.Fatal("AppendDoc on an unreadable path must fail")
	} else if !strings.Contains(err.Error(), "read") {
		t.Fatalf("error must surface the read failure, got %q", err)
	}
}

// TestAppendDocDoesNotOverwriteUnreadableDoc pins the consequence the EISDIR
// case cannot reach: when the document is present and readable-check-fails but
// its directory stays writable, the old code rebuilt the file from an empty
// body and reported success, so the user's content was replaced rather than a
// write failing.
func TestAppendDocDoesNotOverwriteUnreadableDoc(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits do not gate reads the same way on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission bits")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "REASONIX.md")
	original := "# Project memory\n\n- keep this line\n"
	if err := os.WriteFile(path, []byte(original), 0o200); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	if err := AppendDoc(path, "new note"); err == nil {
		t.Fatal("AppendDoc reported success on an unreadable document")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != original {
		t.Fatalf("unreadable document was rewritten; content lost:\n%s", body)
	}
}
