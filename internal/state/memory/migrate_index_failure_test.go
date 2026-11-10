package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

// A migration rewrites a legacy fact as V2 before it publishes the index. If the
// publication fails, the rewrite is already on disk, and the next run treats the
// fact as migrated and skips it — so the index must not be left stale by a run
// that reported failure.
func TestMigrateV2RetryAfterIndexFailureRepairsTheIndex(t *testing.T) {
	dir := testenv.TempDir(t)
	legacy := "---\nname: alpha\ndescription: legacy\n---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "alpha.md"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	store := Store{Dir: dir}

	withFault(t, func(op, path string) error {
		if op == "publish-index" && strings.HasPrefix(path, dir) {
			return &os.PathError{Op: "rename", Path: path, Err: os.ErrPermission}
		}
		return nil
	})
	if _, err := store.MigrateV2(); err == nil {
		t.Fatal("the migration reported success although the index could not be published")
	}
	memoryFault = nil

	raw, err := os.ReadFile(filepath.Join(dir, "alpha.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "id:") {
		t.Fatalf("the failed migration did not rewrite the fact, so this case is not the one under test:\n%s", raw)
	}

	if _, err := store.MigrateV2(); err != nil {
		t.Fatalf("the second migration could not converge: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, indexFile))
	if os.IsNotExist(err) {
		t.Fatalf("the second migration left no index although the fact is migrated")
	}
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "alpha.md") {
		t.Fatalf("the second migration skipped the fact and left the index stale:\n%s", body)
	}
}
