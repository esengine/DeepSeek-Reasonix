package isolation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/gitcmd"
)

// These are deliberately edited disk records, not states produced by Execute.
// Restart and the boot sweeper ingest this same host-local persistence boundary.
func TestDurableRecordCannotRedirectSettlement(t *testing.T) {
	for _, operation := range []string{"paths", "apply", "discard", "sweep"} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			repo := gitRepo(t)
			managed, outside := testenv.TempDir(t), testenv.TempDir(t)
			owner := NewStore(outside, repo, opened(t, repo))
			report, err := owner.Execute(ctx, writes(map[string]string{"a.txt": "retained work\n"}), repo, "work", "")
			if err != nil {
				t.Fatal(err)
			}
			id := idOf(t, report)
			records := owner.durable()
			if len(records) != 1 {
				t.Fatalf("records = %v", records)
			}
			d := records[0]
			original, err := os.ReadFile(entryPath(d.Cand))
			if err != nil {
				t.Fatal(err)
			}
			// Copy a real valid record under another store's root. Its embedded
			// candidate still names the original, outside that store's authority.
			write(t, managed, "copied/"+entryFile, string(original))
			store := NewStore(managed, repo, opened(t, repo))
			switch operation {
			case "paths":
				_, err = store.Paths(id)
			case "apply":
				_, err = store.Apply(ctx, id)
			case "discard":
				_, err = store.Discard(ctx, id)
			case "sweep":
				store.SweepExpired(ctx, -time.Second)
			}
			if operation != "sweep" && err == nil {
				t.Error("accepted a candidate outside the marker's managed slot")
			}
			if got := read(t, repo, "a.txt"); got != "one\n" {
				t.Errorf("redirected operation changed source: %q", got)
			}
			if got, err := os.ReadFile(entryPath(d.Cand)); err != nil || string(got) != string(original) {
				t.Errorf("outside marker changed: %q, %v", got, err)
			}
			if got := read(t, d.Cand.WorktreeRoot, "a.txt"); got != "retained work\n" {
				t.Errorf("outside candidate changed: %q", got)
			}
			if got := read(t, managed, "copied/"+entryFile); got != string(original) {
				t.Error("invalid marker was changed instead of retained")
			}
		})
	}
}

func TestDurableRecordCompatibility(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "pinned", true: "legacy"}[legacy], func(t *testing.T) {
			repo := gitRepo(t)
			write(t, repo, "sub/seed.txt", "seed\n")
			for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "subdirectory"}} {
				cmd := gitcmd.Command(context.Background(), repo, args...)
				cmd.Env = append(cmd.Env, "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, out)
				}
			}
			workspace := filepath.Join(repo, "sub")
			managed := testenv.TempDir(t)
			store := NewStore(managed, workspace, opened(t, workspace))
			report, err := store.Execute(context.Background(), writes(map[string]string{"new.txt": "retained\n"}), workspace, "work", "")
			if err != nil {
				t.Fatal(err)
			}
			id := idOf(t, report)
			d := store.durable()[0]
			if legacy {
				d.Snap.Repo, d.Cand.Repo = gitcmd.Repo{}, gitcmd.Repo{}
			}
			encoded, err := json.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			// A future field is retained byte for byte: discovery is not migration.
			encoded = append(encoded[:len(encoded)-1], []byte(`,"future":{"keep":true}}`)...)
			if err := os.WriteFile(entryPath(d.Cand), encoded, 0o600); err != nil {
				t.Fatal(err)
			}
			resumed := NewStore(managed, workspace, opened(t, workspace))
			// Neither identity format starts Git merely to remain discoverable.
			originalPath := os.Getenv("PATH")
			t.Setenv("PATH", testenv.TempDir(t))
			if got := resumed.Pending(); !slices.Equal(got, []string{id}) {
				t.Fatalf("pending = %v, want [%s]", got, id)
			}
			if got := read(t, filepath.Dir(entryPath(d.Cand)), entryFile); got != string(encoded) {
				t.Fatal("discovery rewrote metadata")
			}
			if !legacy {
				return
			}
			t.Setenv("PATH", originalPath)
			if _, err := resumed.Apply(context.Background(), id); err != nil {
				t.Fatal(err)
			}
			if got := read(t, workspace, "new.txt"); got != "retained\n" {
				t.Fatalf("legacy subdirectory result = %q", got)
			}
		})
	}
}

func TestDurableRecordRejectsSymlinkedSlotAndMarker(t *testing.T) {
	for _, kind := range []string{"slot", "marker"} {
		t.Run(kind, func(t *testing.T) {
			repo := gitRepo(t)
			managed := testenv.TempDir(t)
			store := NewStore(managed, repo, opened(t, repo))
			report, err := store.Execute(context.Background(), writes(map[string]string{"a.txt": "retained\n"}), repo, "work", "")
			if err != nil {
				t.Fatal(err)
			}
			id := idOf(t, report)
			d := store.durable()[0]
			mark := entryPath(d.Cand)
			var link, target string
			if kind == "slot" {
				target, link = filepath.Dir(mark), filepath.Join(testenv.TempDir(t), "slot")
			} else {
				target = filepath.Join(testenv.TempDir(t), "saved.json")
				if err := os.Rename(mark, target); err != nil {
					t.Fatal(err)
				}
				link = mark
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlink creation unavailable: %v", err)
			}
			if kind == "slot" {
				managed = filepath.Dir(link)
			}
			resumed := NewStore(managed, repo, opened(t, repo))
			resumed.SweepExpired(context.Background(), -time.Second)
			if slices.Contains(resumed.Pending(), id) {
				t.Error("offered a symlinked record slot or marker")
			}
			if got := read(t, d.Cand.WorktreeRoot, "a.txt"); got != "retained\n" {
				t.Fatalf("candidate changed: %q", got)
			}
			if _, err := os.Stat(target); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDurableRecordAcceptsLinkedSourceWorkspace(t *testing.T) {
	ctx := context.Background()
	repo := gitRepo(t)
	parent := NewStore(testenv.TempDir(t), repo, opened(t, repo))
	e, err := parent.Begin(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { parent.Abort(ctx, e) })
	root := e.Cand.WorkspaceRoot
	managed := testenv.TempDir(t)
	child := NewStore(managed, root, opened(t, root))
	report, err := child.Execute(ctx, writes(map[string]string{"a.txt": "child\n"}), root, "work", "")
	if err != nil {
		t.Fatal(err)
	}
	id := idOf(t, report)
	resumed := NewStore(managed, root, opened(t, root))
	if got := resumed.Pending(); !slices.Equal(got, []string{id}) {
		t.Fatalf("linked-source pending = %v", got)
	}
	if _, err := resumed.Apply(ctx, id); err != nil {
		t.Fatal(err)
	}
	if got := read(t, root, "a.txt"); got != "child\n" {
		t.Fatalf("linked-source apply = %q", got)
	}
	if got := read(t, repo, "a.txt"); got != "one\n" {
		t.Fatal("linked-source apply reached the original workspace")
	}
}

func TestDurableRecordChecksCandidateGitMarker(t *testing.T) {
	repo := gitRepo(t)
	managed := testenv.TempDir(t)
	store := NewStore(managed, repo, opened(t, repo))
	report, err := store.Execute(context.Background(), writes(map[string]string{"a.txt": "retained\n"}), repo, "work", "")
	if err != nil {
		t.Fatal(err)
	}
	id := idOf(t, report)
	d := store.durable()[0]
	write(t, d.Cand.WorktreeRoot, ".git", "gitdir: "+opened(t, repo).GitDir+"\n")
	resumed := NewStore(managed, repo, opened(t, repo))
	if got := resumed.Pending(); slices.Contains(got, id) {
		t.Fatal("trusted a stale pinned candidate identity after its marker changed")
	}
	if _, err := resumed.Paths(id); err == nil {
		t.Fatal("accepted candidate with changed Git marker")
	}
	if got := read(t, d.Cand.WorktreeRoot, "a.txt"); got != "retained\n" {
		t.Fatal("changed candidate was removed")
	}
}

func TestDurableRecordRejectsHardlinkedCandidateMarker(t *testing.T) {
	repo := gitRepo(t)
	managed := testenv.TempDir(t)
	store := NewStore(managed, repo, opened(t, repo))
	for range 2 {
		if _, err := store.Execute(context.Background(), writes(map[string]string{"a.txt": "retained\n"}), repo, "work", ""); err != nil {
			t.Fatal(err)
		}
	}
	records := store.durable()
	if len(records) != 2 {
		t.Fatalf("records = %v", records)
	}
	left, right := records[0], records[1]
	marker := filepath.Join(left.Cand.WorktreeRoot, ".git")
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(right.Cand.WorktreeRoot, ".git"), marker); err != nil {
		t.Fatal(err)
	}
	left.Cand.Repo = right.Cand.Repo
	left.Cand.Repo.Dir, left.Cand.Repo.WorkTree = left.Cand.WorktreeRoot, left.Cand.WorktreeRoot
	// Repo.Verify historically compares .git files by inode. The hard link
	// makes a different candidate's administrative backlink pass that check.
	if err := left.Cand.Repo.Verify(); err != nil {
		t.Fatalf("fixture did not establish the shared-inode alias: %v", err)
	}
	encoded, err := json.Marshal(left)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entryPath(left.Cand), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	resumed := NewStore(managed, repo, opened(t, repo))
	if got := resumed.Pending(); slices.Contains(got, left.ID) || !slices.Contains(got, right.ID) {
		t.Fatalf("hardlinked marker admission = %v; want only %s", got, right.ID)
	}
	if got := read(t, filepath.Dir(entryPath(left.Cand)), entryFile); got != string(encoded) {
		t.Fatal("rejected marker changed")
	}
}

func TestRecordDirectoryConfinesIdentityQueries(t *testing.T) {
	root, outside := testenv.TempDir(t), testenv.TempDir(t)
	write(t, root, "inside/kept.txt", "inside\n")
	write(t, outside, "secret.txt", "outside\n")
	dir, err := openRecordDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.root.Close()
	if !dir.same(filepath.Join(root, "inside"), "inside") {
		t.Fatal("bounded descendant identity was refused")
	}
	if _, ok := dir.local(outside); ok || dir.same(outside, ".") {
		t.Fatal("accepted an outside identity claim")
	}
	if _, err := dir.root.ReadFile(filepath.Join("..", filepath.Base(outside), "secret.txt")); err == nil {
		t.Fatal("opened namespace allowed a parent escape")
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if _, ok := dir.local(filepath.Join(root, "escape")); ok {
		t.Fatal("accepted an outside symlink identity")
	}
	if _, err := dir.root.ReadFile("escape/secret.txt"); err == nil {
		t.Fatal("opened namespace followed an outside symlink")
	}
}

func TestDurableRecordUnderLiteralManagedDirectory(t *testing.T) {
	repo := gitRepo(t)
	managed := filepath.Join(testenv.TempDir(t), "managed[1]")
	store := NewStore(managed, repo, opened(t, repo))
	report, err := store.Execute(context.Background(), writes(map[string]string{"a.txt": "retained\n"}), repo, "work", "")
	if err != nil {
		t.Fatal(err)
	}
	id := idOf(t, report)
	resumed := NewStore(managed, repo, opened(t, repo))
	if got := resumed.Pending(); !slices.Equal(got, []string{id}) {
		t.Fatalf("literal managed root pending = %v", got)
	}
	if _, err := resumed.Apply(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if got := read(t, repo, "a.txt"); got != "retained\n" {
		t.Fatal(got)
	}
}

func TestDurableRecordBindsSourceAndCandidateIdentity(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*durableEntry, string, string)
	}{
		{"source-root", func(d *durableEntry, _, other string) { d.Snap.RepoRoot = other }},
		{"source-repo", func(d *durableEntry, _, other string) { d.Snap.Repo = opened(t, other) }},
		{"candidate-repo", func(d *durableEntry, repo, _ string) { d.Cand.Repo = opened(t, repo) }},
		{"candidate-workspace", func(d *durableEntry, repo, _ string) { d.Cand.WorkspaceRoot = repo }},
		{"prefix-escape", func(d *durableEntry, _, _ string) { d.Snap.Prefix = "../other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, other := gitRepo(t), gitRepo(t)
			managed := testenv.TempDir(t)
			store := NewStore(managed, repo, opened(t, repo))
			report, err := store.Execute(context.Background(), writes(map[string]string{"a.txt": "work\n"}), repo, "work", "")
			if err != nil {
				t.Fatal(err)
			}
			id := idOf(t, report)
			d := store.durable()[0]
			mark := entryPath(d.Cand)
			raw, err := os.ReadFile(mark)
			if err != nil {
				t.Fatal(err)
			}
			changed := d
			tc.edit(&changed, repo, other)
			encoded, err := json.Marshal(changed)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(mark, encoded, 0o600); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.WriteFile(mark, raw, 0o600); err != nil {
					t.Error(err)
				}
			})
			restarted := NewStore(managed, repo, opened(t, repo))
			if slices.Contains(restarted.Pending(), id) {
				t.Error("offered mismatched record")
			}
			if _, err := restarted.Paths(id); err == nil {
				t.Error("resolved paths from mismatched record")
			}
			if got := read(t, filepath.Dir(mark), entryFile); got != string(encoded) {
				t.Error("rewrote mismatched record")
			}
		})
	}
}
