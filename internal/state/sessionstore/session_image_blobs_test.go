package sessionstore

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/store"
)

// testImageDataURL is a distinct data URL of about size bytes, so no two turns
// share a payload the store could deduplicate.
func testImageDataURL(seed uint64, size int) string {
	raw := make([]byte, size*3/4)
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	for i := range raw {
		raw[i] = byte(r.Uint32())
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)
}

func imageTurn(i int, url string) []provider.Message {
	return []provider.Message{
		{Role: provider.RoleUser, Content: fmt.Sprintf("look at picture %d", i), Images: []string{url}},
		{Role: provider.RoleAssistant, Content: fmt.Sprintf("picture %d seen", i)},
	}
}

func requireSameTranscript(t *testing.T, got, want []provider.Message) {
	t.Helper()
	g, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	w, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(g, w) {
		t.Fatalf("reloaded transcript differs: got %d messages (%d bytes), want %d (%d bytes)", len(got), len(g), len(want), len(w))
	}
}

func eventLogBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(store.SessionEventLog(path))
	if err != nil {
		t.Fatalf("read event log: %v", err)
	}
	return b
}

// The event log a writer replays before every append grows with text, not with
// attachments: an image lands beside it and comes back byte-identical.
func TestSessionImagesLeaveTheEventLog(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "session.jsonl")
	s := NewSession("system")
	userImage := testImageDataURL(1, 1<<20)
	for _, m := range imageTurn(1, userImage) {
		s.Add(m)
	}
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("first save: %v", err)
	}
	toolImage := testImageDataURL(2, 1<<20)
	s.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "shot", Name: "screen"}}})
	s.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "shot", Name: "screen", Content: "captured",
		Images: []string{toolImage, "https://example.test/remote.png", userImage}})
	if err := s.Save(path); err != nil {
		t.Fatalf("append save: %v", err)
	}

	log := eventLogBytes(t, path)
	if len(log) > 64<<10 {
		t.Fatalf("event log is %d bytes; image payloads were written into it", len(log))
	}
	for _, url := range []string{userImage, toolImage} {
		if bytes.Contains(log, []byte(url[len(url)-4096:])) {
			t.Fatal("event log carries an image payload")
		}
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	requireSameTranscript(t, loaded.Snapshot(), s.Snapshot())

	// The checkpoint is what export hands out, so it keeps the images inline.
	checkpoint, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(checkpoint, []byte(toolImage)) {
		t.Fatal("the exported checkpoint lost the image")
	}

	if err := s.SaveRewriteCompact(path); err != nil {
		t.Fatalf("compacting save: %v", err)
	}
	if n := len(eventLogBytes(t, path)); n > 64<<10 {
		t.Fatalf("compacted event log is %d bytes; image payloads were folded into it", n)
	}
	loaded, err = LoadSession(path)
	if err != nil {
		t.Fatalf("load after compaction: %v", err)
	}
	requireSameTranscript(t, loaded.Snapshot(), s.Snapshot())
}

// A blob is durable before the record that names it: a crash at the log append
// finds the image already on disk.
func TestSessionImageBlobLandsBeforeItsLogRecord(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "session.jsonl")
	s := NewSession("system")
	url := testImageDataURL(3, 4096)
	for _, m := range imageTurn(1, url) {
		s.Add(m)
	}
	blobs := -1
	fileutil.CrashPoint = func(op, _ string) {
		if op != "wal-append" {
			return
		}
		blobs = 0
		entries, _ := os.ReadDir(store.SessionBlobsDir(path))
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), ".") {
				blobs++
			}
		}
		panic(crashSentinel{})
	}
	defer func() { fileutil.CrashPoint = nil }()
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(crashSentinel); !ok {
					panic(r)
				}
			}
		}()
		_ = s.SaveSnapshot(path)
	}()
	if blobs != 1 {
		t.Fatalf("blobs on disk when the log append began = %d, want 1", blobs)
	}
}

// A blob missing with no other copy costs that image, not the session: the
// message keeps its text and names the image it lost, the request says so, and
// the stored copy coming back restores it.
func TestSessionMissingImageBlobIsATypedFact(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "session.jsonl")
	url := testImageDataURL(4, 4096)
	s := NewSession("system")
	for _, m := range imageTurn(1, url) {
		s.Add(m)
	}
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	blob := onlyBlob(t, path)
	stashed, err := os.ReadFile(blob)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(blob); err != nil {
		t.Fatal(err)
	}
	stripCheckpointImages(t, path)
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("load with a missing blob: %v", err)
	}
	user := loaded.Snapshot()[1]
	stored, _ := json.Marshal(user)
	if len(user.Images) != 0 || user.Content != "look at picture 1" || !bytes.Contains(stored, []byte(`"unavailable_images"`)) {
		t.Fatalf("user message = %s, want its text untouched and the lost image named", stored)
	}
	if got := provider.ModelMessages(loaded.Snapshot())[1].Content; !strings.Contains(got, "1 image(s) unavailable") {
		t.Fatalf("the request does not say the image is gone: %q", got)
	}
	loaded.Add(provider.Message{Role: provider.RoleUser, Content: "and now?"})
	if err := loaded.Save(path); err != nil {
		t.Fatalf("save after degraded load: %v", err)
	}
	for _, f := range []string{path, store.SessionEventLog(path)} {
		if b, _ := os.ReadFile(f); bytes.Contains(b, []byte("unavailable: the stored copy")) {
			t.Fatalf("%s carries the rendered note", filepath.Base(f))
		}
	}
	if err := os.WriteFile(blob, stashed, 0o600); err != nil {
		t.Fatal(err)
	}
	healed, err := LoadSession(path)
	if err != nil {
		t.Fatalf("reload with the blob back: %v", err)
	}
	if got := healed.Snapshot()[1]; len(got.Images) != 1 || got.Images[0] != url {
		t.Fatalf("the returned blob did not restore the image: %d images", len(got.Images))
	}
}

// A reference cannot name a file outside the session's blob directory, and a
// blob whose bytes do not hash to its name is treated as missing.
func TestSessionImageBlobReferenceIsVerified(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "session.jsonl")
	s := NewSession("system")
	for _, m := range imageTurn(1, testImageDataURL(5, 4096)) {
		s.Add(m)
	}
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := os.WriteFile(onlyBlob(t, path), []byte("data:image/png;base64,AAAA"), 0o600); err != nil {
		t.Fatal(err)
	}
	stripCheckpointImages(t, path)
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("load with a corrupt blob: %v", err)
	}
	if user := loaded.Snapshot()[1]; len(user.Images) != 0 {
		t.Fatalf("a blob that fails its hash was resolved: %q", user.Images)
	}
	for _, key := range []string{"sha256:../../session.jsonl", "sha256:" + strings.Repeat("g", 64), "md5:00"} {
		if _, ok := readSessionImageBlob(store.SessionBlobsDir(path), key); ok {
			t.Fatalf("reference %q resolved", key)
		}
	}
}

// Every copy a session makes of itself — a rewind's version, a recovery
// branch, a rename — carries its images, since each has its own blob store.
func TestSessionImageCopiesCarryTheirImages(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "session.jsonl")
	s := NewSession("system")
	for _, m := range imageTurn(1, testImageDataURL(6, 4096)) {
		s.Add(m)
	}
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	want := s.Snapshot()

	version, err := SaveSupersededVersion(path, want)
	if err != nil {
		t.Fatalf("save version: %v", err)
	}
	live := s.CloneWithMessages(want)
	for _, m := range imageTurn(2, testImageDataURL(7, 4096)) {
		live.Add(m)
	}
	s.Add(provider.Message{Role: provider.RoleUser, Content: "written elsewhere"})
	if err := s.Save(path); err != nil {
		t.Fatalf("diverging save: %v", err)
	}
	if err := live.SaveSnapshot(path); err == nil {
		t.Fatal("a diverged snapshot saved over the other writer")
	}
	branch, err := live.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: path})
	if err != nil {
		t.Fatalf("save recovery branch: %v", err)
	}
	want = append(want, provider.Message{Role: provider.RoleUser, Content: "written elsewhere"})
	renamed := filepath.Join(dir, "renamed.jsonl")
	if err := os.Rename(path, renamed); err != nil {
		t.Fatal(err)
	}
	if err := migrateSessionSidecars(path, renamed, BranchID(renamed)); err != nil {
		t.Fatalf("move sidecars: %v", err)
	}
	if _, err := os.Stat(store.SessionBlobsDir(path)); !os.IsNotExist(err) {
		t.Fatalf("the renamed session left its blobs under the old name: %v", err)
	}
	for copyPath, transcript := range map[string][]provider.Message{
		version: want[:len(want)-1], branch.Path: live.Snapshot(), renamed: want,
	} {
		loaded, err := LoadSession(copyPath)
		if err != nil {
			t.Fatalf("load %s: %v", filepath.Base(copyPath), err)
		}
		requireSameTranscript(t, loaded.Snapshot(), transcript)
	}
}

func largeSessionTest(t *testing.T) {
	t.Helper()
	if os.Getenv("REASONIX_LARGE_SESSION_TEST") == "" {
		t.Skip("set REASONIX_LARGE_SESSION_TEST=1 to write sessions past the former 128 MiB replay budget")
	}
}

// The writer must never produce a log its own reader refuses: image turns past
// the former 128 MiB replay budget keep saving and loading, and the log stays
// under the budget the writer folds at.
func TestSessionImageTurnsPastTheReplayBudgetKeepSaving(t *testing.T) {
	largeSessionTest(t)
	path := filepath.Join(testenv.TempDir(t), "session.jsonl")
	s := NewSession("system")
	const turns = 72
	for i := 1; i <= turns; i++ {
		for _, m := range imageTurn(i, testImageDataURL(uint64(100+i), 2<<20)) {
			s.Add(m)
		}
		if err := s.Save(path); err != nil {
			t.Fatalf("save #%d: %v", i, err)
		}
		if i%12 == 0 || i >= 60 {
			loaded, err := LoadSession(path)
			if err != nil {
				t.Fatalf("load after save #%d: %v", i, err)
			}
			if got := loaded.Len(); got != s.Len() {
				t.Fatalf("load after save #%d: %d messages, want %d", i, got, s.Len())
			}
		}
		if size := int64(len(eventLogBytes(t, path))); size > 128<<20 {
			t.Fatalf("save #%d left a %d-byte event log", i, size)
		}
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("final load: %v", err)
	}
	requireSameTranscript(t, loaded.Snapshot(), s.Snapshot())
}

// A log an earlier build already wrote past 128 MiB, images inline, opens again
// and is folded below the budget by its next save.
func TestSessionLegacyInlineLogPastTheReplayBudgetOpens(t *testing.T) {
	largeSessionTest(t)
	path := filepath.Join(testenv.TempDir(t), "session.jsonl")
	want := []provider.Message{{Role: provider.RoleSystem, Content: "system"}}
	var logBuf bytes.Buffer
	appendRecord := func(rec sessionEventRecord) {
		rec.SchemaVersion = sessionEventSchemaVersion
		b, err := json.Marshal(rec)
		if err != nil {
			t.Fatal(err)
		}
		logBuf.Write(b)
		logBuf.WriteByte('\n')
	}
	appendRecord(sessionEventRecord{Type: sessionEventTypeReplace, Revision: 1, Messages: want})
	for i := 1; i <= 66; i++ {
		turn := imageTurn(i, testImageDataURL(uint64(500+i), 2<<20))
		appendRecord(sessionEventRecord{Type: sessionEventTypeAppend, Revision: int64(i + 1), BaseRevision: int64(i),
			MessageIndex: len(want), Messages: turn})
		want = append(want, turn...)
	}
	if logBuf.Len() <= 128<<20 {
		t.Fatalf("fixture log is %d bytes, want past 128 MiB", logBuf.Len())
	}
	if err := os.WriteFile(store.SessionEventLog(path), logBuf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	logBuf = bytes.Buffer{}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("load legacy log: %v", err)
	}
	requireSameTranscript(t, loaded.Snapshot(), want)
	loaded.Add(provider.Message{Role: provider.RoleUser, Content: "still here?"})
	if err := loaded.Save(path); err != nil {
		t.Fatalf("save after legacy load: %v", err)
	}
	if size := len(eventLogBytes(t, path)); size > 128<<20 {
		t.Fatalf("event log is still %d bytes after the save", size)
	}
	again, err := LoadSession(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	requireSameTranscript(t, again.Snapshot(), loaded.Snapshot())
}
