package sessionstore

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"reasonix/internal/base/testenv"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionv4"
	"reasonix/internal/state/sessionv4/v4fixture"
)

const v4TestID = "0123456789abcdef0123456789abcdef"

// v4Project is a project directory holding one 1.x v4 conversation and the
// 2.x sessions directory beside it.
func v4Project(t *testing.T) (*v4fixture.Store, string, string) {
	t.Helper()
	s := v4fixture.New(t)
	dir := s.Session(v4TestID, 3)
	s.Batch(dir, v4fixture.Ended,
		v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m1", "system", "sys")},
		v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m2", "user", "<ctx>", map[string]any{"origin": "host"})},
		v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m3", "user", "hello")},
		v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m4", "assistant", "hi")},
		v4fixture.Event{Kind: "session/title", Payload: map[string]any{"title": "Greeting"}},
	)
	sessions := filepath.Join(filepath.Dir(s.Root), "sessions")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatal(err)
	}
	return s, dir, sessions
}

func treeSums(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	sums := map[string][32]byte{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		sums[p] = sha256.Sum256(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return sums
}

// A 1.x v4 conversation is listed at the path it will be imported to, and
// opening it imports it there without touching a byte of 1.x's store.
func TestV4SessionIsListedAndImportedOnOpen(t *testing.T) {
	s, _, sessions := v4Project(t)
	before := treeSums(t, s.Root)

	list, err := ListSessions(sessions)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(sessions, "v4-"+v4TestID+".jsonl")
	if len(list) != 1 || list[0].Path != want || list[0].Preview != "hello" || list[0].Turns != 1 || list[0].CustomTitle != "Greeting" {
		t.Fatalf("list = %+v", list)
	}

	loaded, err := LoadSession(want)
	if err != nil {
		t.Fatal(err)
	}
	msgs := loaded.Snapshot()
	if len(msgs) != 4 || msgs[3].Content != "hi" || !msgs[1].HostAuthored {
		t.Fatalf("imported %+v", msgs)
	}
	meta, ok, err := LoadBranchMeta(want)
	if err != nil || !ok || meta.ImportedFrom == nil || meta.CustomTitle != "Greeting" || meta.Turns != 1 {
		t.Fatalf("meta = %+v, %v, %v", meta, ok, err)
	}
	after := treeSums(t, s.Root)
	for p, sum := range before {
		if after[p] != sum {
			t.Errorf("%s changed", p)
		}
	}
	if len(after) != len(before) {
		t.Errorf("1.x store has %d files, had %d", len(after), len(before))
	}

	list, _ = ListSessions(sessions)
	if len(list) != 1 || list[0].Path != want {
		t.Fatalf("after import the list = %+v", list)
	}
}

// When 1.x continues a conversation this copy has not moved on from, the next
// open brings the copy up to date; once this copy has its own turns, it stays.
func TestV4ImportFollowsOnlyAnUntouchedCopy(t *testing.T) {
	s, dir, sessions := v4Project(t)
	path := filepath.Join(sessions, "v4-"+v4TestID+".jsonl")
	if _, err := LoadSession(path); err != nil {
		t.Fatal(err)
	}

	s.Batch(dir, v4fixture.Ended,
		v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m5", "user", "again")},
		v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m6", "assistant", "sure")},
	)
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if msgs := loaded.Snapshot(); len(msgs) != 6 || msgs[5].Content != "sure" {
		t.Fatalf("untouched copy not refreshed: %+v", msgs)
	}

	loaded.Add(provider.Message{Role: provider.RoleUser, Content: "from 2.x"})
	if err := loaded.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	s.Batch(dir, v4fixture.Ended, v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m7", "user", "from 1.x")})
	loaded, err = LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	last := loaded.Snapshot()[len(loaded.Snapshot())-1]
	if !strings.Contains(last.Content, "from 2.x") {
		t.Fatalf("a continued copy was overwritten: last = %+v", last)
	}
}

// The lazy entry points read the 1.x store by path; none of them may keep a
// handle on it, which on Windows would stop 1.x renaming or removing it.
func TestV4LazyReadsLeaveTheStoreFreeToMove(t *testing.T) {
	s, _, sessions := v4Project(t)
	if list, err := ListSessions(sessions); err != nil || len(list) != 1 {
		t.Fatalf("list = %v, %v", list, err)
	}
	if err := PrepareSessionPath(filepath.Join(sessions, "v4-"+v4TestID+".jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(s.Root, s.Root+"-moved"); err != nil {
		t.Fatalf("store cannot be renamed after lazy reads: %v", err)
	}
}

func TestImportV4FromCopiesAndIsIdempotent(t *testing.T) {
	s, _, sessions := v4Project(t)
	root, err := os.OpenRoot(s.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	dest := filepath.Join(testenv.TempDir(t), "ws")
	if n, skipped, err := ImportV4From(root.FS(), dest, nil, nil); n != 1 || len(skipped) != 0 || err != nil {
		t.Fatalf("first import = %d, %v", n, err)
	}
	if n, skipped, err := ImportV4From(root.FS(), dest, nil, nil); n != 0 || len(skipped) != 0 || err != nil {
		t.Fatalf("second import = %d, %v", n, err)
	}
	_ = sessions
}

// writeListingCache stores the title 1.x cached for a conversation whose log
// carries none, the way 1.x writes it, so it describes the log as it stands.
func writeListingCache(t *testing.T, store *v4fixture.Store, dir, title string) {
	t.Helper()
	opened, err := sessionv4.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	meta := map[string]any{
		"version": 5, "codec": "reasonix.session.linear/v4", "sessionId": opened.Manifest.SessionID,
		"createdAt": opened.Manifest.CreatedAt.UTC().Format(time.RFC3339Nano), "title": title,
		"turns": 1, "preview": "hello", "logSize": opened.LogSize, "logModTimeNs": opened.LogModTime.UnixNano(),
	}
	raw, _ := json.Marshal(meta)
	cache := filepath.Join(store.Root, ".query-cache", opened.Manifest.SessionID)
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "catalog-metadata.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func untitledV4(t *testing.T) (*v4fixture.Store, string, string) {
	t.Helper()
	s := v4fixture.New(t)
	dir := s.Session(v4TestID, 3)
	s.Batch(dir, v4fixture.Ended,
		v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m3", "user", "hello")},
		v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m4", "assistant", "hi")},
	)
	sessions := filepath.Join(filepath.Dir(s.Root), "sessions")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatal(err)
	}
	return s, dir, sessions
}

// 1.x keeps a title in its listing cache when the log never recorded one; the
// copy carries it, as the unimported row did.
func TestImportKeepsTheTitleOnlyTheListingCacheHolds(t *testing.T) {
	s, dir, sessions := untitledV4(t)
	writeListingCache(t, s, dir, "Floppy")
	if n, _, err := ImportV4From(os.DirFS(s.Root), sessions, nil, nil); err != nil || n != 1 {
		t.Fatalf("imported %d, err %v", n, err)
	}
	meta, ok, err := LoadBranchMeta(v4ImportPath(sessions, v4TestID))
	if err != nil || !ok || meta.CustomTitle != "Floppy" {
		t.Fatalf("meta %+v ok=%v err=%v, want the cached title", meta, ok, err)
	}
}

// A copy an earlier import left without a title is given it by the next import;
// a title the person set is left alone.
func TestReimportFillsAnEmptyTitleAndKeepsAChosenOne(t *testing.T) {
	s, dir, sessions := untitledV4(t)
	if n, _, err := ImportV4From(os.DirFS(s.Root), sessions, nil, nil); err != nil || n != 1 {
		t.Fatalf("imported %d, err %v", n, err)
	}
	path := v4ImportPath(sessions, v4TestID)
	if meta, _, _ := LoadBranchMeta(path); meta.CustomTitle != "" {
		t.Fatalf("setup: title %q", meta.CustomTitle)
	}
	writeListingCache(t, s, dir, "Floppy")
	if n, _, err := ImportV4From(os.DirFS(s.Root), sessions, nil, nil); err != nil || n != 0 {
		t.Fatalf("second import: %d, err %v", n, err)
	}
	if meta, _, _ := LoadBranchMeta(path); meta.CustomTitle != "Floppy" {
		t.Fatalf("title %q, want it filled from the cache", meta.CustomTitle)
	}
	meta, _, _ := LoadBranchMeta(path)
	meta.CustomTitle = "Mine"
	if err := SaveBranchMeta(path, meta); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ImportV4From(os.DirFS(s.Root), sessions, nil, nil); err != nil {
		t.Fatal(err)
	}
	if meta, _, _ := LoadBranchMeta(path); meta.CustomTitle != "Mine" {
		t.Fatalf("title %q, a chosen title must stay", meta.CustomTitle)
	}
}

// A conversation 1.x had filed away keeps that mark once moved. Without this the
// person's archived list turns into active conversations on the first launch
// after the upgrade, and every one of them has to be put away again by hand.
func TestImportV4FromCarriesTheArchivedMark(t *testing.T) {
	s, _, _ := v4Project(t)
	root, err := os.OpenRoot(s.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	dest := filepath.Join(testenv.TempDir(t), "ws")

	n, skipped, err := ImportV4From(root.FS(), dest, nil, func(string) BranchMeta {
		return BranchMeta{Archived: true}
	})
	if n != 1 || len(skipped) != 0 || err != nil {
		t.Fatalf("import = %d, %v", n, err)
	}

	meta, ok, err := LoadBranchMeta(v4ImportPath(dest, v4TestID))
	if err != nil || !ok {
		t.Fatalf("meta = %v, %v", ok, err)
	}
	if !meta.Archived {
		t.Fatal("an archived conversation came back as an active one")
	}
}

// A migration that names nothing keeps the default, so an install with no archive
// list behaves exactly as before.
func TestImportV4FromLeavesTheMarkOffWhenUnasked(t *testing.T) {
	s, _, _ := v4Project(t)
	root, err := os.OpenRoot(s.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	dest := filepath.Join(testenv.TempDir(t), "ws")

	if n, skipped, err := ImportV4From(root.FS(), dest, nil, nil); n != 1 || len(skipped) != 0 || err != nil {
		t.Fatalf("import = %d, %v", n, err)
	}
	meta, ok, err := LoadBranchMeta(v4ImportPath(dest, v4TestID))
	if err != nil || !ok {
		t.Fatalf("meta = %v, %v", ok, err)
	}
	if meta.Archived {
		t.Fatal("a conversation nobody archived was filed as archived")
	}
}

// PrepareSessionPath refreshes a copy 1.x has continued since the import. That
// refresh rebuilds the meta, so an archived conversation has to still read as
// archived afterwards — otherwise the first continued turn brings it back to the
// everyday list.
func TestPrepareSessionPathKeepsTheArchivedMark(t *testing.T) {
	s, dir, sessions := v4Project(t)
	path := filepath.Join(sessions, "v4-"+v4TestID+".jsonl")

	// Import it the way the migration does, for a conversation 1.x had archived.
	opened, err := sessionv4.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := importV4As(path, opened, BranchMeta{Archived: true}); err != nil {
		t.Fatal(err)
	}
	before, ok, err := LoadBranchMeta(path)
	if err != nil || !ok || !before.Archived {
		t.Fatalf("import did not archive: %+v, %v", before, err)
	}

	// 1.x continues the conversation, which is what makes the copy stale.
	s.Batch(dir, v4fixture.Ended, v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m9", "user", "continued in 1.x")})
	if err := PrepareSessionPath(path); err != nil {
		t.Fatal(err)
	}

	after, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("meta = %v, %v", ok, err)
	}
	if !after.Archived {
		t.Fatal("refreshing the copy brought an archived conversation back")
	}
}

// An interrupted v4 import leaves the transcript and no complete sidecar. The
// next import must complete the sidecar — keeping 1.x's archive mark — and bring
// the copy up to the source when 1.x went on without it. The sidecar may be
// absent outright or be one an earlier import left without its source.
func TestInterruptedV4ImportRetryCompletesTheSidecar(t *testing.T) {
	for _, tc := range []struct {
		name          string
		removeSidecar bool
	}{
		{"sidecar missing", true},
		{"sidecar without its import source", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, dir, sessions := v4Project(t)
			opened, err := sessionv4.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			tr, err := opened.Transcript()
			if err != nil {
				t.Fatal(err)
			}
			path := v4ImportPath(sessions, v4TestID)
			partial := NewSession("")
			partial.Messages = tr.Messages
			if err := partial.SaveIfAbsent(path); err != nil {
				t.Fatal(err)
			}
			if tc.removeSidecar {
				if err := os.Remove(BranchMetaPath(path)); err != nil {
					t.Fatal(err)
				}
			} else if meta, ok, err := LoadBranchMeta(path); err != nil || !ok || meta.ImportedFrom != nil {
				t.Fatalf("setup: sidecar ok=%v importedFrom=%v err=%v, want one without its source", ok, meta.ImportedFrom != nil, err)
			}
			// 1.x continues the conversation while the copy has no complete
			// sidecar.
			s.Batch(dir, v4fixture.Ended,
				v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m9", "user", "continued in 1.x")},
				v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m10", "assistant", "answer in 1.x")},
			)

			n, skipped, err := ImportV4From(os.DirFS(s.Root), sessions, nil, func(string) BranchMeta {
				return BranchMeta{Archived: true}
			})
			if err != nil || len(skipped) != 0 {
				t.Fatalf("rerun: n=%d skipped=%v err=%v", n, skipped, err)
			}
			if n != 1 {
				t.Fatalf("rerun completed %d copies, want 1", n)
			}
			meta, ok, err := LoadBranchMeta(path)
			if err != nil || !ok {
				t.Fatalf("rerun left no sidecar: ok=%v err=%v", ok, err)
			}
			if !meta.Archived || meta.ImportedFrom == nil {
				t.Fatalf("completed sidecar = %+v, want archived with its import source", meta)
			}
			loaded, err := LoadSession(path)
			if err != nil {
				t.Fatal(err)
			}
			if msgs := loaded.Snapshot(); len(msgs) != 6 || msgs[5].Content != "answer in 1.x" {
				t.Fatalf("copy brought up to %d messages, want 6 with 1.x's tail", len(msgs))
			}
			if meta, _, _ := LoadBranchMeta(path); !meta.Archived {
				t.Fatal("following 1.x dropped the archived mark")
			}
		})
	}
}

// A copy with both its transcript and its sidecar is complete: a rerun must not
// re-copy it or touch the sidecar.
func TestCompletedV4ImportIsNotReCopied(t *testing.T) {
	s, _, sessions := v4Project(t)
	if n, skipped, err := ImportV4From(os.DirFS(s.Root), sessions, nil, nil); n != 1 || len(skipped) != 0 || err != nil {
		t.Fatalf("first import: n=%d skipped=%v err=%v", n, skipped, err)
	}
	path := v4ImportPath(sessions, v4TestID)
	pinned := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	for _, p := range []string{path, BranchMetaPath(path)} {
		if err := os.Chtimes(p, pinned, pinned); err != nil {
			t.Fatal(err)
		}
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeMeta, err := os.ReadFile(BranchMetaPath(path))
	if err != nil {
		t.Fatal(err)
	}

	if n, skipped, err := ImportV4From(os.DirFS(s.Root), sessions, nil, nil); n != 0 || len(skipped) != 0 || err != nil {
		t.Fatalf("rerun: n=%d skipped=%v err=%v, want 0 nil", n, skipped, err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	afterMeta, err := os.ReadFile(BranchMetaPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || string(beforeMeta) != string(afterMeta) {
		t.Fatal("a completed v4 import was re-copied")
	}
	for _, p := range []string{path, BranchMetaPath(path)} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if !info.ModTime().Equal(pinned) {
			t.Fatalf("%s was rewritten on a completed retry: mtime %s want %s", p, info.ModTime(), pinned)
		}
	}
}

// Completing an interrupted import never replays over turns the person added in
// 2.x, and the sidecar it writes describes the copy that exists.
func TestInterruptedV4ImportKeepsA2xTurn(t *testing.T) {
	s, dir, sessions := v4Project(t)
	opened, err := sessionv4.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := opened.Transcript()
	if err != nil {
		t.Fatal(err)
	}
	path := v4ImportPath(sessions, v4TestID)
	partial := NewSession("")
	partial.Messages = tr.Messages
	if err := partial.SaveIfAbsent(path); err != nil {
		t.Fatal(err)
	}
	// The person continues in 2.x before the import gets its second run.
	current, err := loadSessionUnlocked(path)
	if err != nil {
		t.Fatal(err)
	}
	current.Add(provider.Message{Role: provider.RoleUser, Content: "from 2.x"})
	if err := current.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}

	if _, skipped, err := ImportV4From(os.DirFS(s.Root), sessions, nil, func(string) BranchMeta {
		return BranchMeta{Archived: true}
	}); err != nil || len(skipped) != 0 {
		t.Fatalf("rerun: skipped=%v err=%v", skipped, err)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	msgs := loaded.Snapshot()
	if len(msgs) != 5 || !strings.Contains(msgs[4].Content, "from 2.x") {
		t.Fatalf("the 2.x turn was lost: %+v", msgs)
	}
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok || !meta.Archived {
		t.Fatalf("sidecar = %+v ok=%v err=%v, want it completed and archived", meta, ok, err)
	}
	if meta.Turns != 2 {
		t.Fatalf("sidecar lists %d turns, want the copy's 2", meta.Turns)
	}
}
