package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

// failIndexPublishOnce makes the next MEMORY.md publication in dir fail, then
// stops failing, so the same intent can be retried.
func failIndexPublishOnce(t *testing.T, dir string) {
	t.Helper()
	withFault(t, func(op, path string) error {
		if op == "publish-index" && strings.HasPrefix(path, dir) {
			return &os.PathError{Op: "rename", Path: path, Err: os.ErrPermission}
		}
		return nil
	})
}

func activeFactsNamed(t *testing.T, store Store, name string) []Memory {
	t.Helper()
	var out []Memory
	for _, m := range mustStore(t, store).ListAll() {
		if m.Name == name {
			out = append(out, m)
		}
	}
	return out
}

// A failed save must not advance what the store reports, so retrying the same
// intent lands where it would have landed had the index been writable.
func TestStoreV2CreateRetryAfterIndexFailureLandsOnce(t *testing.T) {
	dir := testenv.TempDir(t)
	store := Store{Dir: dir}
	failIndexPublishOnce(t, dir)

	if _, err := store.SaveWithOptions(Memory{Name: "alpha", Description: "d", Body: "v1"}, SaveOptions{}); err == nil {
		t.Fatal("the save reported success although the index could not be published")
	}
	memoryFault = nil

	second, err := store.SaveWithOptions(Memory{Name: "alpha", Description: "d", Body: "v1"}, SaveOptions{})
	if err != nil {
		t.Fatalf("the retry of the same intent did not converge: %v", err)
	}
	facts := activeFactsNamed(t, store, "alpha")
	if len(facts) != 1 {
		t.Fatalf("after the retry the name is active %d times: %+v", len(facts), facts)
	}
	if facts[0].Revision != 1 {
		t.Fatalf("the failed save advanced the revision: got %d, want 1 (body %q)", facts[0].Revision, facts[0].Body)
	}
	body, err := os.ReadFile(filepath.Join(dir, indexFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "alpha.md") {
		t.Fatalf("the retry left the index without the fact:\n%s", body)
	}
	if second.Memory.Revision != 1 {
		t.Fatalf("the retry reported revision %d, want 1", second.Memory.Revision)
	}
}

// The reachable production case: the failed attempt already advanced the file,
// so a retry that carries the caller's expected revision must still be able to
// land rather than report a conflict the caller cannot resolve.
func TestStoreV2GuardedUpdateRetryAfterIndexFailureConverges(t *testing.T) {
	dir := testenv.TempDir(t)
	store := Store{Dir: dir}
	first, err := store.SaveWithOptions(Memory{Name: "alpha", Description: "d", Body: "v1"}, SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	failIndexPublishOnce(t, dir)
	intent := Memory{ID: first.Memory.ID, Name: "alpha", Description: "d", Body: "v2"}
	opts := SaveOptions{ExpectedRevision: first.Memory.Revision, RequireExpectedRevision: true}

	if _, err := store.SaveWithOptions(intent, opts); err == nil {
		t.Fatal("the update reported success although the index could not be published")
	}
	memoryFault = nil

	if _, err := store.SaveWithOptions(intent, opts); err != nil {
		t.Fatalf("the guarded retry could not converge after a failed index publication: %v", err)
	}
	facts := activeFactsNamed(t, store, "alpha")
	if len(facts) != 1 {
		t.Fatalf("after the retry the name is active %d times: %+v", len(facts), facts)
	}
	body, err := os.ReadFile(filepath.Join(dir, indexFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "alpha.md") {
		t.Fatalf("the retry left the index without the fact:\n%s", body)
	}
}

// A failed save must not normalise what the user wrote. The file an update
// replaces can carry frontmatter fields this code does not know, hand-written
// comments and its own formatting, and a rollback that re-renders the parsed
// Memory silently drops all of it.
func TestStoreV2FailedUpdateRestoresTheFileByteForByte(t *testing.T) {
	dir := testenv.TempDir(t)
	store := Store{Dir: dir}
	first, err := store.SaveWithOptions(Memory{Name: "alpha", Description: "d", Body: "v1"}, SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	handWritten := "---\n" +
		"id: " + first.Memory.ID + "\n" +
		"name: alpha\n" +
		"description: \"d\"\n" +
		"revision: 1\n" +
		"custom_field: keep me\n" +
		"another: 'quoted value'\n" +
		"---\n\n" +
		"<!-- a hand-written comment -->\n" +
		"v1 kept exactly\n"
	path := filepath.Join(dir, "alpha.md")
	if err := os.WriteFile(path, []byte(handWritten), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	failIndexPublishOnce(t, dir)
	if _, err := store.SaveWithOptions(Memory{ID: first.Memory.ID, Name: "alpha", Description: "d", Body: "v2"},
		SaveOptions{ExpectedRevision: 1, RequireExpectedRevision: true}); err == nil {
		t.Fatal("the update reported success although the index could not be published")
	}
	memoryFault = nil

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("the failed update rewrote the file instead of restoring it:\n--- before ---\n%s\n--- after ---\n%s",
			before, after)
	}
}
