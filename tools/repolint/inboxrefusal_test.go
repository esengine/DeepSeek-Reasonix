package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The tree's boundary names every condition today. This is also the check's own
// regression: both sides are matched by shape, so moving the family or renaming
// the writer fails here rather than passing with two empty sets.
func TestInboxRefusalParityIsCleanInTree(t *testing.T) {
	if got := checkInboxRefusalParity("../.."); len(got) != 0 {
		t.Fatalf("checkInboxRefusalParity = %v, want none", got)
	}
	family, err := inboxSentinels("../..")
	if err != nil {
		t.Fatal(err)
	}
	// The sentinel that motivated this: it had no case of its own, so an entry
	// the kernel no longer held was answered as an internal fault.
	if !slices.Contains(family, "ErrNotFound") {
		t.Fatalf("family = %v, want ErrNotFound among them", family)
	}
}

func TestInboxRefusalParityNamesWhatWasLeftUnnamed(t *testing.T) {
	root := fakeInboxTree(t,
		`package sessioninbox

import "errors"

var (
	ErrGone   = errors.New("gone")
	ErrHeld   = errors.New("held")
	ErrSealed = errors.New("sealed")
)
`,
		`package serve

func writeInboxError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sessioninbox.ErrGone):
		refuse(w, 409, "inbox.gone", err.Error(), nil)
	case errors.Is(err, sessioninbox.ErrHeld):
		writeErr(w, 409, err)
	default:
		writeErr(w, 500, err)
	}
}
`)
	got := checkInboxRefusalParity(root)
	if len(got) != 2 {
		t.Fatalf("findings = %v, want 2", got)
	}
	// The two failures are different instructions — one branch says the wrong
	// thing, the other says nothing — so a count that merged them would leave
	// the reader guessing which.
	if !strings.Contains(got[0].Msg, "ErrHeld") || !strings.Contains(got[0].Msg, "without a code") {
		t.Errorf("first finding = %q, want ErrHeld refused without a code", got[0].Msg)
	}
	if !strings.Contains(got[1].Msg, "ErrSealed") || !strings.Contains(got[1].Msg, "no case") {
		t.Errorf("second finding = %q, want ErrSealed having no case", got[1].Msg)
	}
}

// A check that cannot find its subject reports that, rather than reporting
// parity: an empty family and a missing writer both compare clean.
func TestInboxRefusalParitySaysWhenItLostItsSubject(t *testing.T) {
	root := fakeInboxTree(t,
		"package sessioninbox\n",
		"package serve\n\nfunc somethingElse() {}\n")
	got := checkInboxRefusalParity(root)
	if len(got) != 1 || !strings.Contains(got[0].Msg, "lost its subject") {
		t.Fatalf("findings = %v, want one saying the subject is gone", got)
	}
}

func fakeInboxTree(t *testing.T, family, writer string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range map[string]string{inboxFamilyFile: family, inboxWriterFile: writer} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
