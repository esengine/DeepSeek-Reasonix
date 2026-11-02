package sessionstore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestV4RemovalSurvivesCleanup(t *testing.T) {
	for _, operation := range []string{"remove", "delete", "clear", ForkPendingOperation} {
		t.Run(operation, func(t *testing.T) {
			_, _, dir := v4Project(t)
			path := v4ImportPath(dir, v4TestID)
			if err := MarkCleanupPending(path, operation); err != nil {
				t.Fatal(err)
			}
			rows, err := ListSessions(dir)
			if err != nil || len(rows) != 0 {
				t.Fatalf("pending rows = %+v, %v", rows, err)
			}
			if _, err := LoadSession(path); !os.IsNotExist(err) {
				t.Fatalf("pending import = %v", err)
			}
			if err := ClearCleanupPending(path); err != nil {
				t.Fatal(err)
			}
			rows, err = ListSessions(dir)
			if err != nil {
				t.Fatal(err)
			}
			if operation == ForkPendingOperation {
				if len(rows) != 1 || rows[0].Path != path {
					t.Fatalf("published session disappeared: %+v", rows)
				}
				if _, err := LoadSession(path); err != nil {
					t.Fatal(err)
				}
			} else {
				if len(rows) != 0 {
					t.Fatalf("removed session rediscovered: %+v", rows)
				}
				if _, err := LoadSession(path); !os.IsNotExist(err) {
					t.Fatalf("removed import = %v", err)
				}
			}
		})
	}
}

func TestV4RemovalPublicationFailureKeepsCleanupPending(t *testing.T) {
	_, _, dir := v4Project(t)
	path := v4ImportPath(dir, v4TestID)
	if err := MarkCleanupPending(path, "remove"); err != nil {
		t.Fatal(err)
	}
	blocked := v4RemovalPath(path)
	if err := os.MkdirAll(filepath.Join(blocked, "held"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := ClearCleanupPending(path); err == nil {
		t.Fatal("cleared cleanup despite failing to persist deletion")
	}
	if !IsCleanupPending(path) {
		t.Fatal("lost cleanup retry after publication failure")
	}
	if err := os.RemoveAll(blocked); err != nil {
		t.Fatal(err)
	}
	if err := ClearCleanupPending(path); err != nil {
		t.Fatal(err)
	}
	if IsVisibleSession(path) {
		t.Fatal("completed retry exposed the legacy source")
	}
}

func TestV4RemovalDoesNotFollowMarkersOutsideSessionDirectory(t *testing.T) {
	for _, pending := range []bool{false, true} {
		name := "removed"
		if pending {
			name = "pending"
		}
		t.Run(name, func(t *testing.T) {
			_, _, dir := v4Project(t)
			path := v4ImportPath(dir, v4TestID)
			outside := filepath.Join(t.TempDir(), "outside.json")
			if err := os.WriteFile(outside, []byte(`{"operation":"remove","createdAt":1}`), 0600); err != nil {
				t.Fatal(err)
			}
			marker := v4RemovalPath(path)
			if pending {
				marker = CleanupPendingPath(path)
			}
			if err := os.Symlink(outside, marker); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if pending {
				if err := ClearCleanupPending(path); err == nil {
					t.Fatal("read a cleanup marker outside the session directory")
				}
				if !IsCleanupPending(path) {
					t.Fatal("lost pending cleanup after rejecting an external marker")
				}
				if _, err := os.Lstat(v4RemovalPath(path)); !os.IsNotExist(err) {
					t.Fatalf("published removal from external data: %v", err)
				}
			} else if !IsVisibleSession(path) {
				t.Fatal("external marker controlled session visibility")
			}
		})
	}
}
