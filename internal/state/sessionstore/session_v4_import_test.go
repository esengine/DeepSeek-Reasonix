package sessionstore

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
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
	dest := filepath.Join(t.TempDir(), "ws")
	if n, err := ImportV4From(root.FS(), dest); n != 1 || err != nil {
		t.Fatalf("first import = %d, %v", n, err)
	}
	if n, err := ImportV4From(root.FS(), dest); n != 0 || err != nil {
		t.Fatalf("second import = %d, %v", n, err)
	}
	_ = sessions
}
