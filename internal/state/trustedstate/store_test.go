package trustedstate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func appendN(t *testing.T, s *Store, stream string, n int) []Digest {
	t.Helper()
	var out []Digest
	for i := range n {
		_, d, err := s.Append(context.Background(), stream, "bundle", []byte{byte('a' + i)})
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
		out = append(out, d)
	}
	return out
}

func TestAppendChainsAndVerifies(t *testing.T) {
	root := t.TempDir()
	s := Open(root, func() IntegrityLevel { return HostProtected })
	digests := appendN(t, s, "ws", 3)

	h, err := Open(root, nil).Verify("ws")
	if err != nil {
		t.Fatalf("a fresh process must verify the chain: %v", err)
	}
	if h.Generation != 3 || h.Record != digests[2] || h.Integrity != HostProtected {
		t.Fatalf("head = %+v, want generation 3 at %s", h, digests[2])
	}
	rec, err := s.Record(digests[1])
	if err != nil || rec.Parent != digests[0] || rec.Generation != 2 {
		t.Fatalf("record 2 = %+v, %v", rec, err)
	}
}

func TestNilIntegrityClaimsNothing(t *testing.T) {
	s := Open(t.TempDir(), nil)
	rec, _, err := s.Append(context.Background(), "ws", "bundle", []byte("x"))
	if err != nil || rec.Integrity != TamperEvidentOnly {
		t.Fatalf("record = %+v, %v; want tamper_evident_only", rec, err)
	}
}

func TestChangedObjectIsTampered(t *testing.T) {
	root := t.TempDir()
	s := Open(root, nil)
	appendN(t, s, "ws", 2)
	rec, err := s.Record(mustHead(t, s, "ws").Record)
	if err != nil {
		t.Fatal(err)
	}
	path, _ := s.objectPath(rec.Payload)
	if err := os.WriteFile(path, []byte("rewritten"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Open(root, nil).Verify("ws")
	if !errors.Is(err, ErrTampered) || FailureCode(err) != "trusted_state.tampered" {
		t.Fatalf("Verify = %v (%s), want ErrTampered", err, FailureCode(err))
	}
}

func TestObservedHeadRefusesRollback(t *testing.T) {
	root := t.TempDir()
	s := Open(root, nil)
	appendN(t, s, "ws", 2)
	old, _ := os.ReadFile(headPath(root, "ws"))
	appendN(t, s, "ws", 1)
	if err := os.WriteFile(headPath(root, "ws"), old, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Head("ws"); !errors.Is(err, ErrTampered) {
		t.Fatalf("Head after rollback = %v, want ErrTampered", err)
	}
	if _, _, err := s.Append(context.Background(), "ws", "bundle", []byte("z")); !errors.Is(err, ErrTampered) {
		t.Fatalf("Append on a rolled-back head = %v, want ErrTampered", err)
	}
}

// A writer that owns the whole store can build a self-consistent history. The
// chain alone accepts it; only a head this process already observed refuses it.
func TestRewrittenHistoryIsDetectedOnlyAgainstObservedHead(t *testing.T) {
	root := t.TempDir()
	s := Open(root, nil)
	appendN(t, s, "ws", 2)

	forger := Open(t.TempDir(), nil)
	for _, p := range []string{"forged-1", "forged-2", "forged-3"} {
		if _, _, err := forger.Append(context.Background(), "ws", "bundle", []byte(p)); err != nil {
			t.Fatal(err)
		}
	}
	copyTree(t, forger.Root(), root)

	if _, err := Open(root, nil).Verify("ws"); err != nil {
		t.Fatalf("a process with no observed head cannot tell a forged chain: %v", err)
	}
	if _, err := s.Verify("ws"); !errors.Is(err, ErrTampered) {
		t.Fatalf("Verify against the observed head = %v, want ErrTampered", err)
	}
}

func TestDeletedHeadAfterObservationIsTampered(t *testing.T) {
	root := t.TempDir()
	s := Open(root, nil)
	appendN(t, s, "ws", 1)
	if err := os.Remove(headPath(root, "ws")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Head("ws"); !errors.Is(err, ErrTampered) {
		t.Fatalf("Head = %v, want ErrTampered", err)
	}
	if _, err := Open(root, nil).Head("ws"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a fresh process sees an empty stream: %v", err)
	}
}

func TestStreamNameCannotEscape(t *testing.T) {
	s := Open(t.TempDir(), nil)
	for _, name := range []string{"", "../x", "a/b", "A", `a\b`} {
		if _, _, err := s.Append(context.Background(), name, "bundle", nil); !errors.Is(err, ErrInvalidStream) {
			t.Errorf("Append(%q) = %v, want ErrInvalidStream", name, err)
		}
	}
}

func TestUnwritableRootIsTyped(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := Open(file, nil).Append(context.Background(), "ws", "bundle", []byte("x"))
	if !errors.Is(err, ErrUnwritable) || FailureCode(err) != "trusted_state.unwritable" {
		t.Fatalf("Append under a file = %v (%s), want ErrUnwritable", err, FailureCode(err))
	}
}

func mustHead(t *testing.T, s *Store, stream string) Head {
	t.Helper()
	h, err := s.Head(stream)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func headPath(root, stream string) string {
	return filepath.Join(root, "streams", stream, "HEAD")
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() == "LOCK" {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		dst := filepath.Join(to, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	var h Head
	data, _ := os.ReadFile(headPath(to, "ws"))
	if json.Unmarshal(data, &h) != nil || h.Generation != 3 {
		t.Fatalf("forged head not installed: %s", data)
	}
}
