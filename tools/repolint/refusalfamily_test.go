package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The tree's boundaries name every condition today, in both frontends. This is
// also the check's own regression: both sides are matched by shape, so moving
// the family or renaming a writer fails here rather than passing with two empty
// sets.
func TestInboxRefusalParityIsCleanInTree(t *testing.T) {
	if got := checkInboxRefusalParity("../.."); len(got) != 0 {
		t.Fatalf("checkInboxRefusalParity = %v, want none", got)
	}
	family, err := refusalSentinels("../..", inboxFamily)
	if err != nil {
		t.Fatal(err)
	}
	// The sentinel that motivated this: it had no case of its own, so an entry
	// the kernel no longer held was answered as an internal fault.
	if !slices.Contains(family, "ErrNotFound") {
		t.Fatalf("family = %v, want ErrNotFound among them", family)
	}
	// A spec that names a writer this check cannot find answers "lost its
	// subject" and files the finding against the writer, so a clean run above
	// is evidence both were read.
	for _, w := range inboxWriters {
		if _, _, _, err := refusalHandledSentinels("../..", inboxFamily, w); err != nil {
			t.Fatalf("%s: %v", w.file, err)
		}
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
`,
		`package acp

func inboxRefusal(err error) error {
	switch {
	case errors.Is(err, sessioninbox.ErrGone):
		return refuseInbox(ErrInboxRefused, "inbox.gone", err.Error())
	case errors.Is(err, sessioninbox.ErrHeld):
		return refuseInbox(ErrInboxRefused, "inbox.held", err.Error())
	default:
		return &RPCError{Code: ErrInternal, Message: err.Error()}
	}
}
`)
	got := checkInboxRefusalParity(root)
	if len(got) != 3 {
		t.Fatalf("findings = %v, want 3", got)
	}
	// The two failures are different instructions — one branch says the wrong
	// thing, the other says nothing — so a count that merged them would leave
	// the reader guessing which.
	if !strings.Contains(got[0].Msg, "ErrHeld") || !strings.Contains(got[0].Msg, "without a code") || got[0].File != inboxHTTPWriterFile {
		t.Errorf("first finding = %+v, want ErrHeld refused without a code by %s", got[0], inboxHTTPWriterFile)
	}
	if !strings.Contains(got[1].Msg, "ErrSealed") || !strings.Contains(got[1].Msg, "no case") || got[1].File != inboxHTTPWriterFile {
		t.Errorf("second finding = %+v, want ErrSealed having no HTTP case", got[1])
	}
	// The ACP writer is read too: a sentinel it does not name at all is the
	// defect this check was extended for.
	if !strings.Contains(got[2].Msg, "ErrSealed") || !strings.Contains(got[2].Msg, inboxACPWriterFunc) || got[2].File != inboxACPWriterFile {
		t.Errorf("third finding = %+v, want ErrSealed having no case in %s", got[2], inboxACPWriterFunc)
	}
}

// A case that answers without an identity is the same defect as a missing case:
// the client cannot tell this refusal from the fault that also carries prose.
func TestInboxRefusalParityNamesACaselessIdentity(t *testing.T) {
	root := fakeInboxTree(t,
		"package sessioninbox\n\nimport \"errors\"\n\nvar ErrGone = errors.New(\"gone\")\n",
		`package serve

func writeInboxError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sessioninbox.ErrGone):
		refuse(w, 409, "inbox.gone", err.Error(), nil)
	}
}
`,
		`package acp

func inboxRefusal(err error) error {
	switch {
	case errors.Is(err, sessioninbox.ErrGone):
		return &RPCError{Code: ErrInboxRefused, Message: err.Error()}
	}
	return nil
}
`)
	got := checkInboxRefusalParity(root)
	if len(got) != 1 || got[0].File != inboxACPWriterFile || !strings.Contains(got[0].Msg, "without a code") {
		t.Fatalf("findings = %+v, want one naming the ACP case that writes no identity", got)
	}
}

// A check that cannot find its subject reports that, rather than reporting
// parity: an empty family and a missing writer both compare clean.
func TestInboxRefusalParitySaysWhenItLostItsSubject(t *testing.T) {
	root := fakeInboxTree(t,
		"package sessioninbox\n",
		"package serve\n\nfunc somethingElse() {}\n",
		"package acp\n\nfunc alsoSomethingElse() {}\n")
	got := checkInboxRefusalParity(root)
	if len(got) != 1 || !strings.Contains(got[0].Msg, "lost its subject") || got[0].File != inboxFamilyFile {
		t.Fatalf("findings = %+v, want one saying the family is gone", got)
	}

	root = fakeInboxTree(t,
		"package sessioninbox\n\nimport \"errors\"\n\nvar ErrGone = errors.New(\"gone\")\n",
		"package serve\n\nfunc somethingElse() {}\n",
		`package acp

func inboxRefusal(err error) error {
	switch {
	case errors.Is(err, sessioninbox.ErrGone):
		return refuseInbox(ErrInboxRefused, "inbox.gone", err.Error())
	}
	return nil
}
`)
	got = checkInboxRefusalParity(root)
	if len(got) != 1 || !strings.Contains(got[0].Msg, "lost its subject") || got[0].File != inboxHTTPWriterFile {
		t.Fatalf("findings = %+v, want one saying the HTTP writer is gone", got)
	}
}

// The lease family is read through the same mechanism as the inbox one: the
// store's sentinels and the ACP session writer are both matched by shape, so a
// clean tree is evidence both sides were read.
func TestSessionLeaseRefusalParityIsCleanInTree(t *testing.T) {
	if got := checkSessionLeaseRefusalParity("../.."); len(got) != 0 {
		t.Fatalf("checkSessionLeaseRefusalParity = %v, want none", got)
	}
	family, err := refusalSentinels("../..", leaseFamily)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ErrSessionLeaseHeld", "ErrSessionLeaseUnprovable"} {
		if !slices.Contains(family, name) {
			t.Fatalf("family = %v, want %s among them", family, name)
		}
	}
	for _, w := range sessionLeaseWriters {
		if _, _, _, err := refusalHandledSentinels("../..", leaseFamily, w); err != nil {
			t.Fatalf("%s: %v", w.file, err)
		}
	}
}

// TestSessionLeaseRefusalParityNamesALeaseSentinelWithNoCase is the guard this
// change exists for: a sentinel added beside the two the lease already declares
// must fail the check by name, against the writer that dropped it, not reach a
// client as an internal fault.
func TestSessionLeaseRefusalParityNamesALeaseSentinelWithNoCase(t *testing.T) {
	root := fakeLeaseTree(t,
		`package sessionstore

import "errors"

var (
	ErrSessionLeaseHeld       = errors.New("held")
	ErrSessionLeaseUnprovable = errors.New("unprovable")
	ErrSessionLeaseExpired    = errors.New("expired")
)
`,
		`package acp

func sessionLeaseRefusal(method string, err error) *RPCError {
	switch {
	case errors.Is(err, sessionstore.ErrSessionLeaseHeld):
		return refuseSessionLease(ErrSessionLeaseHeld, "busy.session_in_use", method)
	case errors.Is(err, sessionstore.ErrSessionLeaseUnprovable):
		return refuseSessionLease(ErrSessionLeaseUnprovable, "session.lease_unprovable", method)
	default:
		return &RPCError{Code: ErrInternal, Message: err.Error()}
	}
}
`)
	got := checkSessionLeaseRefusalParity(root)
	if len(got) != 1 {
		t.Fatalf("findings = %v, want one for the undeclared lease sentinel", got)
	}
	if !strings.Contains(got[0].Msg, "ErrSessionLeaseExpired") ||
		!strings.Contains(got[0].Msg, sessionLeaseACPWriterFunc) ||
		got[0].File != sessionLeaseACPWriterFile {
		t.Fatalf("finding = %+v, want ErrSessionLeaseExpired with no case in %s", got[0], sessionLeaseACPWriterFunc)
	}
}

// A lease case that writes prose instead of data keeps the holder and the
// unprovable state indistinguishable on the wire.
func TestSessionLeaseRefusalParityNamesACaselessIdentity(t *testing.T) {
	root := fakeLeaseTree(t,
		"package sessionstore\n\nimport \"errors\"\n\nvar ErrSessionLeaseHeld = errors.New(\"held\")\n",
		`package acp

func sessionLeaseRefusal(method string, err error) *RPCError {
	switch {
	case errors.Is(err, sessionstore.ErrSessionLeaseHeld):
		return &RPCError{Code: ErrSessionLeaseHeld, Message: method + ": " + err.Error()}
	}
	return nil
}
`)
	got := checkSessionLeaseRefusalParity(root)
	if len(got) != 1 || got[0].File != sessionLeaseACPWriterFile || !strings.Contains(got[0].Msg, "without a code") {
		t.Fatalf("findings = %+v, want one naming the lease case that writes no identity", got)
	}
}

// The lease check says when either side went missing, instead of comparing two
// empty sets and reporting clean.
func TestSessionLeaseRefusalParitySaysWhenItLostItsSubject(t *testing.T) {
	root := fakeLeaseTree(t, "package sessionstore\n", "package acp\n\nfunc somethingElse() {}\n")
	got := checkSessionLeaseRefusalParity(root)
	if len(got) != 1 || !strings.Contains(got[0].Msg, "lost its subject") || got[0].File != sessionLeaseFamilyFile {
		t.Fatalf("findings = %+v, want one saying the lease family is gone", got)
	}

	root = fakeLeaseTree(t,
		"package sessionstore\n\nimport \"errors\"\n\nvar ErrSessionLeaseHeld = errors.New(\"held\")\n",
		"package acp\n\nfunc somethingElse() {}\n")
	got = checkSessionLeaseRefusalParity(root)
	if len(got) != 1 || !strings.Contains(got[0].Msg, "lost its subject") || got[0].File != sessionLeaseACPWriterFile {
		t.Fatalf("findings = %+v, want one saying the lease writer is gone", got)
	}
}

func fakeInboxTree(t *testing.T, family, httpWriter, acpWriter string) string {
	t.Helper()
	return fakeRefusalTree(t, map[string]string{
		inboxFamilyFile:     family,
		inboxHTTPWriterFile: httpWriter,
		inboxACPWriterFile:  acpWriter,
	})
}

func fakeLeaseTree(t *testing.T, family, acpWriter string) string {
	t.Helper()
	return fakeRefusalTree(t, map[string]string{
		sessionLeaseFamilyFile:    family,
		sessionLeaseACPWriterFile: acpWriter,
	})
}

func fakeRefusalTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
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
