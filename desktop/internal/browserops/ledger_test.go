package browserops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newOp(id string) Operation {
	return Operation{ID: id, SessionID: "s1", Generation: "g1", TabID: "tab1", Epoch: 3, DocumentToken: "doc", Action: "click", Digest: "abc"}
}

func TestReserveThenSettle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ops.json")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Reserve(newOp("op-1")); err != nil {
		t.Fatal(err)
	}
	got, ok := l.Lookup("op-1")
	if !ok || got.State != StateReserved || got.ReservedAt.IsZero() {
		t.Fatalf("after reserve: %+v ok=%v", got, ok)
	}
	if err := l.Settle("op-1", StateExecuted, ""); err != nil {
		t.Fatal(err)
	}
	if err := l.Settle("op-1", StateExecuted, ""); !errors.Is(err, ErrAlreadySettled) {
		t.Fatalf("second settle: %v", err)
	}
	if err := l.Settle("missing", StateExecuted, ""); !errors.Is(err, ErrUnknownOperation) {
		t.Fatalf("settle unknown id: %v", err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got, _ = reopened.Lookup("op-1")
	if got.State != StateExecuted || got.SettledAt.IsZero() {
		t.Fatalf("after reopen: %+v", got)
	}
}

func TestDuplicateAndInvalidIDs(t *testing.T) {
	l, err := Open(filepath.Join(t.TempDir(), "ops.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Reserve(newOp("op-1")); err != nil {
		t.Fatal(err)
	}
	if err := l.Reserve(newOp("op-1")); !errors.Is(err, ErrDuplicateOperation) {
		t.Fatalf("duplicate: %v", err)
	}
	if err := l.Settle("op-1", StateUnknown, "receipt lost"); err != nil {
		t.Fatal(err)
	}
	if err := l.Reserve(newOp("op-1")); !errors.Is(err, ErrDuplicateOperation) {
		t.Fatalf("duplicate after unknown must still be rejected: %v", err)
	}
	for _, bad := range []string{"", "has space", "x/y", string(make([]byte, 101))} {
		if err := l.Reserve(newOp(bad)); !errors.Is(err, ErrInvalidOperationID) {
			t.Errorf("id %q: %v", bad, err)
		}
	}
}

func TestReservedBecomesUnknownAfterCrash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ops.json")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Reserve(newOp("op-1")); err != nil {
		t.Fatal(err)
	}
	if err := l.Reserve(newOp("op-2")); err != nil {
		t.Fatal(err)
	}
	if err := l.Settle("op-2", StateNotExecuted, "element missing"); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	unknown := reopened.Unsettled()
	if len(unknown) != 1 || unknown[0].ID != "op-1" || unknown[0].Reason == "" {
		t.Fatalf("unsettled after crash: %+v", unknown)
	}
	if got, _ := reopened.Lookup("op-2"); got.State != StateNotExecuted {
		t.Fatalf("settled op must be untouched: %+v", got)
	}
}

func TestUnsupportedVersionAndCorruptFile(t *testing.T) {
	dir := t.TempDir()
	v2 := filepath.Join(dir, "v2.json")
	if err := os.WriteFile(v2, []byte(`{"version":2,"operations":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(v2); err == nil {
		t.Fatal("version 2 must be rejected")
	}
	corrupt := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(corrupt, []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(corrupt); err == nil {
		t.Fatal("corrupt file must be rejected, never silently reset")
	}
}

func TestPersistIsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ops.json")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	l.now = func() time.Time { return time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC) }
	if err := l.Reserve(newOp("op-1")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "ops.json" {
		t.Fatalf("temp files must not survive a successful write: %v", entries)
	}
}
