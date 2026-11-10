package memory

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"reasonix/internal/base/testenv"
)

func denyMemoryDirectory(t *testing.T, path string) {
	t.Helper()
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o755) })
	if _, err := os.ReadDir(path); !errors.Is(err, os.ErrPermission) {
		t.Skip("directory permissions do not deny reads on this platform or for this user")
	}
}

func directoryErrorStore(t *testing.T) (Store, Memory, string) {
	t.Helper()
	s := Store{Dir: testenv.TempDir(t), GlobalDir: testenv.TempDir(t)}
	if _, err := s.Save(Memory{Name: "global-fact", Scope: FactScopeGlobal, Body: "global"}); err != nil {
		t.Fatal(err)
	}
	first, err := s.SaveWithOptions(Memory{Name: "fact", Body: "v1", SubjectKey: "project.tool"}, SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(Memory{Name: "fact", Body: "v2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(Memory{Name: "archived", Body: "archive"}); err != nil {
		t.Fatal(err)
	}
	archive, err := s.Archive("archived")
	if err != nil {
		t.Fatal(err)
	}
	return s, first.Memory, archive
}

func requireDirectoryError(t *testing.T, err error, path string, cause error) {
	t.Helper()
	var directory *DirectoryReadError
	if !errors.As(err, &directory) || directory.Path != path || !errors.Is(err, cause) {
		t.Fatalf("error = %v, want DirectoryReadError for %q wrapping %v", err, path, cause)
	}
}

func TestAllMemoryDirectoryReadsReportFailures(t *testing.T) {
	tests := []struct {
		name string
		path func(Store, Memory) string
		run  func(*testing.T, Store, Memory, string) error
	}{
		{"migration", func(s Store, _ Memory) string { return s.Dir }, func(_ *testing.T, s Store, _ Memory, _ string) error { _, err := s.MigrateV2(); return err }},
		{"list", func(s Store, _ Memory) string { return s.Dir }, func(t *testing.T, s Store, _ Memory, _ string) error {
			list, err := s.List()
			if list != nil {
				t.Fatal("partial list")
			}
			return err
		}},
		{"list-all", func(s Store, _ Memory) string { return s.Dir }, func(t *testing.T, s Store, _ Memory, _ string) error {
			list, err := s.ListAll()
			if list != nil {
				t.Fatal("partial list")
			}
			return err
		}},
		{"pinned-guidance", func(s Store, _ Memory) string { return s.Dir }, func(t *testing.T, s Store, _ Memory, _ string) error {
			list, err := s.pinnedGuidance()
			if list != nil {
				t.Fatal("partial guidance")
			}
			return err
		}},
		{"active", func(s Store, _ Memory) string { return s.Dir }, func(_ *testing.T, s Store, m Memory, _ string) error {
			_, _, _, err := s.findActiveInDir(s.Dir, m.ID)
			return err
		}},
		{"revisions", func(s Store, m Memory) string { return filepath.Join(s.Dir, ".revisions", m.ID) }, func(t *testing.T, s Store, m Memory, _ string) error {
			list, err := s.Revisions(m.ID)
			if list != nil {
				t.Fatal("partial revisions")
			}
			return err
		}},
		{"subject", func(s Store, _ Memory) string { return s.Dir }, func(_ *testing.T, s Store, _ Memory, _ string) error {
			_, _, err := s.findActiveSubject(FactScopeProject, "project.tool")
			return err
		}},
		{"archived-lookup", func(s Store, _ Memory) string { return filepath.Join(s.Dir, ".archive") }, func(_ *testing.T, s Store, _ Memory, archive string) error {
			_, _, _, err := s.findArchivedByPath(archive)
			return err
		}},
		{"max-revision", func(s Store, m Memory) string { return filepath.Join(s.Dir, ".revisions", m.ID) }, func(t *testing.T, s Store, m Memory, _ string) error {
			max, err := s.maxKnownRevision(m.ID)
			if max != 0 {
				t.Fatal("partial maximum")
			}
			return err
		}},
		{"archive-repair", func(s Store, _ Memory) string { return filepath.Join(s.Dir, ".archive") }, func(_ *testing.T, s Store, _ Memory, _ string) error {
			_, _, err := s.repairArchivedRef("missing-id")
			return err
		}},
		{"archived-list", func(s Store, _ Memory) string { return filepath.Join(s.Dir, ".archive") }, func(t *testing.T, s Store, _ Memory, _ string) error {
			list, err := s.ListArchived()
			if list != nil {
				t.Fatal("partial archives")
			}
			return err
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, mode := range []string{"permissions", "permission-fault", "io-fault", "not-directory"} {
				t.Run(mode, func(t *testing.T) {
					if mode == "not-directory" && runtime.GOOS == "windows" {
						t.Skip("syscall.ENOTDIR is ERROR_PATH_NOT_FOUND on Windows; a real file in a directory's place is covered by readMemoryDirectory's stat")
					}
					s, m, archive := directoryErrorStore(t)
					path := tc.path(s, m)
					cause := error(os.ErrPermission)
					if mode == "permissions" {
						denyMemoryDirectory(t, path)
					} else {
						if mode != "permission-fault" {
							cause = syscall.EIO
						}
						if mode == "not-directory" {
							cause = syscall.ENOTDIR
						}
						withFault(t, func(op, candidate string) error {
							if op == "read-dir" && candidate == path {
								return cause
							}
							return nil
						})
					}
					requireDirectoryError(t, tc.run(t, s, m, archive), path, cause)
				})
			}
		})
	}
}

func TestMemoryMissingDirectoriesRemainEmpty(t *testing.T) {
	s := Store{Dir: filepath.Join(testenv.TempDir(t), "missing"), GlobalDir: filepath.Join(testenv.TempDir(t), "global")}
	if list, err := s.ListAll(); list != nil || err != nil {
		t.Fatalf("ListAll = %v, %v", list, err)
	}
	if list, err := s.ListArchived(); list != nil || err != nil {
		t.Fatalf("ListArchived = %v, %v", list, err)
	}
	if _, found, err := s.Read("missing"); found || err != nil {
		t.Fatalf("Read = %v, %v", found, err)
	}
	if max, err := s.maxKnownRevision("missing"); max != 0 || err != nil {
		t.Fatalf("maximum = %d, %v", max, err)
	}
	if _, err := s.MigrateV2(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.findActiveSubject(FactScopeProject, "project.tool"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(Memory{Name: "new-fact", Body: "created"}); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreArchivedStopsBeforeWritingOnUnreadableHistory(t *testing.T) {
	s, m, _ := directoryErrorStore(t)
	archive, err := s.Archive(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	indexBefore := mustReadString(t, filepath.Join(s.Dir, indexFile))
	archiveBefore := mustReadString(t, archive)
	history := filepath.Join(s.Dir, ".revisions", m.ID)
	denyMemoryDirectory(t, history)
	_, err = s.RestoreArchived(archive)
	requireDirectoryError(t, err, history, os.ErrPermission)
	if _, err := os.Stat(filepath.Join(s.Dir, m.Name+".md")); !os.IsNotExist(err) {
		t.Fatalf("active fact created: %v", err)
	}
	if got := mustReadString(t, archive); got != archiveBefore {
		t.Fatal("archive changed")
	}
	if got := mustReadString(t, filepath.Join(s.Dir, indexFile)); got != indexBefore {
		t.Fatal("index changed")
	}
	if err := os.Chmod(history, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(history, "000000002.md")); !os.IsNotExist(err) {
		t.Fatalf("snapshot created before read completed: %v", err)
	}
	restored, err := s.RestoreArchived(archive)
	if err != nil || restored.Memory.Revision != 3 {
		t.Fatalf("retry = %+v, %v", restored, err)
	}
}

func TestUnreadableStoreReachesToolsAndRecall(t *testing.T) {
	opts := Options{CWD: testenv.TempDir(t), UserDir: testenv.TempDir(t)}
	s := StoreFor(opts.UserDir, opts.CWD)
	if _, err := s.Save(Memory{Name: "compiler", Body: "compiler configuration", Activation: ActivationPinned}); err != nil {
		t.Fatal(err)
	}
	denyMemoryDirectory(t, s.Dir)
	for _, args := range []string{`{"operation":"read","name":"compiler"}`, `{"operation":"list"}`, `{"operation":"search","query":"compiler"}`} {
		_, err := NewRecallTool(s).Execute(context.Background(), json.RawMessage(args))
		requireDirectoryError(t, err, s.Dir, os.ErrPermission)
	}
	set := Load(opts)
	requireDirectoryError(t, set.StoreError, s.Dir, os.ErrPermission)
	if len(set.PinnedGuidance) != 0 {
		t.Fatal("loaded incomplete pinned guidance")
	}
	for _, result := range []RecallResult{set.AutoRecall("compiler configuration", RecallOptions{}), AutoRecall(s, "compiler configuration", RecallOptions{})} {
		requireDirectoryError(t, result.StoreError, s.Dir, os.ErrPermission)
		if len(result.Hits) != 0 || result.SuppressionCode != SuppressUnreadable || !strings.Contains(result.Block(), "could not be read") {
			t.Fatalf("recall = %+v", result)
		}
	}
	assessment := AssessRememberWrite(s, json.RawMessage(`{"name":"new","type":"project","description":"new fact","body":"new body"}`))
	if assessment.AutoAllow {
		t.Fatal("automatic write allowed after incomplete read")
	}
}

func TestSaveStopsBeforeWritingWhenOtherScopeCannotBeRead(t *testing.T) {
	s, m, _ := directoryErrorStore(t)
	path := filepath.Join(s.Dir, m.Name+".md")
	before := mustReadString(t, path)
	index := mustReadString(t, filepath.Join(s.Dir, indexFile))
	withFault(t, func(op, path string) error {
		if op == "read-dir" && path == s.GlobalDir {
			return os.ErrPermission
		}
		return nil
	})
	_, err := s.Save(Memory{Name: m.Name, Body: "v3"})
	requireDirectoryError(t, err, s.GlobalDir, os.ErrPermission)
	if got := mustReadString(t, path); got != before {
		t.Fatal("active fact changed before duplicate discovery completed")
	}
	if got := mustReadString(t, filepath.Join(s.Dir, indexFile)); got != index {
		t.Fatal("index changed before duplicate discovery completed")
	}
	if _, err := os.Stat(filepath.Join(s.Dir, ".revisions", m.ID, "000000002.md")); !os.IsNotExist(err) {
		t.Fatalf("snapshot created before duplicate discovery completed: %v", err)
	}
}

func TestRestoreReportsUnreadableRevisionDirectory(t *testing.T) {
	s := Store{Dir: testenv.TempDir(t)}
	first, err := s.SaveWithOptions(Memory{Name: "fact", Body: "v1"}, SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(Memory{Name: "fact", Body: "v2"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Dir, ".revisions", first.Memory.ID)
	denyMemoryDirectory(t, path)
	if _, err := s.Restore(first.Memory.ID, 1); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("Restore error = %v, want permission denied", err)
	}
}
