package sessionstore

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/store"
)

func onlyBlob(t *testing.T, path string) string {
	t.Helper()
	blobs := blobNames(t, path)
	if len(blobs) != 1 {
		t.Fatalf("blob dir holds %v, want one blob", blobs)
	}
	return filepath.Join(store.SessionBlobsDir(path), blobs[0])
}

func blobNames(t *testing.T, path string) []string {
	t.Helper()
	entries, err := os.ReadDir(store.SessionBlobsDir(path))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	return names
}

// stripCheckpointImages leaves the checkpoint without any copy of an image, so
// a test can reach the state where a blob is the only one.
func stripCheckpointImages(t *testing.T, path string) {
	t.Helper()
	msgs, err := loadSessionMessagesFromJSONL(path)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, m := range msgs {
		m.Images = nil
		if err := enc.Encode(m); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func imageSession(t *testing.T, path string, urls ...string) *Session {
	t.Helper()
	s := NewSession("system")
	for i, url := range urls {
		for _, m := range imageTurn(i+1, url) {
			s.Add(m)
		}
	}
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	return s
}

// A lost blob whose image the checkpoint still inlines comes back from there,
// and a save that changes nothing leaves that copy where it was.
func TestSessionMissingBlobIsRestoredFromTheCheckpoint(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "session.jsonl")
	url := testImageDataURL(11, 4096)
	s := imageSession(t, path, url)
	s.Add(provider.Message{Role: provider.RoleUser, Content: "next"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(store.SessionBlobsDir(path)); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	requireSameTranscript(t, loaded.Snapshot(), s.Snapshot())
	if len(blobNames(t, path)) != 1 {
		t.Fatal("the blob was not written back from the checkpoint")
	}
	if err := loaded.SaveSnapshot(path); err != nil {
		t.Fatalf("snapshot after restore: %v", err)
	}
	if cp, _ := os.ReadFile(path); !bytes.Contains(cp, []byte(url)) {
		t.Fatal("the checkpoint lost its copy of the image")
	}
}

// A transcript missing an image never replaces a checkpoint that still holds
// it: the save is refused under its own identity and the file is untouched.
func TestSessionDegradedTranscriptNeverOverwritesTheCheckpointCopy(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "session.jsonl")
	url := testImageDataURL(12, 4096)
	imageSession(t, path, url)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(store.SessionBlobsDir(path)); err != nil {
		t.Fatal(err)
	}
	stripCheckpointImages(t, path)
	degraded, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	err = degraded.SaveRewriteCompact(path)
	if !errors.Is(err, ErrSessionImagesUnavailable) {
		t.Fatalf("rewrite over the only copy = %v, want ErrSessionImagesUnavailable", err)
	}
	if cp, _ := os.ReadFile(path); !bytes.Contains(cp, []byte(url)) {
		t.Fatal("the checkpoint's copy of the image was destroyed")
	}
	// Every checkpoint rewrite, not only this save path, holds the line.
	if err := writeSessionMessages(path, degraded.Snapshot()); !errors.Is(err, ErrSessionImagesUnavailable) {
		t.Fatalf("checkpoint rewrite = %v, want ErrSessionImagesUnavailable", err)
	}
	if cp, _ := os.ReadFile(path); !bytes.Equal(cp, original) {
		t.Fatal("a refused checkpoint rewrite changed the file")
	}
}

// A runtime still holding an image whose blob vanished writes it back instead
// of being told the transcript diverged.
func TestSessionLiveRuntimeRestoresAVanishedBlob(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "session.jsonl")
	url := testImageDataURL(13, 4096)
	s := imageSession(t, path, url)
	if err := os.Remove(onlyBlob(t, path)); err != nil {
		t.Fatal(err)
	}
	stripCheckpointImages(t, path)
	s.Add(provider.Message{Role: provider.RoleUser, Content: "next"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("save after the blob vanished: %v", err)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	requireSameTranscript(t, loaded.Snapshot(), s.Snapshot())
}

// When the blob cannot be written back, the save says why under its own
// identity rather than reporting a divergence.
func TestSessionUnrestorableBlobHasItsOwnError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("needs a directory the test user cannot write")
	}
	path := filepath.Join(testenv.TempDir(t), "session.jsonl")
	s := imageSession(t, path, testImageDataURL(14, 4096))
	if err := os.Remove(onlyBlob(t, path)); err != nil {
		t.Fatal(err)
	}
	stripCheckpointImages(t, path)
	dir := store.SessionBlobsDir(path)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	s.Add(provider.Message{Role: provider.RoleUser, Content: "next"})
	err := s.SaveSnapshot(path)
	if !errors.Is(err, ErrSessionImagesUnavailable) || errors.Is(err, ErrSessionSnapshotConflict) {
		t.Fatalf("save = %v, want ErrSessionImagesUnavailable and not a conflict", err)
	}
}

// A record whose images live in blobs is stamped so that a reader predating
// them refuses the log; text-only records keep the schema every reader knows.
func TestSessionImageRecordsAreStampedForNewerReaders(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "session.jsonl")
	s := imageSession(t, path, testImageDataURL(15, 4096))
	s.Add(provider.Message{Role: provider.RoleUser, Content: "text only"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(store.SessionEventLog(path))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var got []int
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var rec struct {
			SchemaVersion int               `json:"schema_version"`
			ImageBlobs    []json.RawMessage `json:"image_blobs"`
		}
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatal(err)
		}
		if len(rec.ImageBlobs) > 0 && rec.SchemaVersion == sessionEventSchemaVersion {
			t.Fatal("a record with image blobs carries the schema older readers accept")
		}
		got = append(got, rec.SchemaVersion)
	}
	if !slices.Equal(got, []int{3, 1}) {
		t.Fatalf("record schemas = %v, want [3 1]", got)
	}
	probe, err := probeSessionEventLog(path)
	if err != nil || !probe.native || probe.futureSchema {
		t.Fatalf("this build does not own its own log: %+v, %v", probe, err)
	}
	if loaded, err := LoadSession(path); err != nil || loaded.Len() != s.Len() {
		t.Fatalf("reload: %v", err)
	}
}

// A fold must shrink the log: text a fold cannot drop is not rewritten on every
// save, while a log whose images a fold would move out is.
func TestSessionEventLogFoldsOnlyWhenItShrinks(t *testing.T) {
	const mib = int64(1 << 20)
	if sessionEventLogOversized(200*mib, 200*mib, nil) {
		t.Fatal("a 200 MiB text-only log that folds to itself was folded")
	}
	if !sessionEventLogOversized(300*mib, 200*mib, nil) {
		t.Fatal("a log carrying 100 MiB a fold drops was not folded")
	}
	legacy := []provider.Message{{Role: provider.RoleUser, Images: []string{"data:image/png;base64," + strings.Repeat("A", int(80*mib))}}}
	if !sessionEventLogOversized(140*mib, 140*mib, legacy) {
		t.Fatal("a legacy log whose images a fold moves out was not folded")
	}
}

// The reader's ceiling covers the largest log a writer can leave, a legacy one
// folding just past the threshold, with room for its text, and no more.
func TestSessionReplayCeiling(t *testing.T) {
	if sessionEventReplayMaxBytes != 512<<20 {
		t.Fatalf("replay ceiling = %d, want 512 MiB", sessionEventReplayMaxBytes)
	}
	if sessionEventReplayMaxBytes < 2*sessionEventLogFoldBytes {
		t.Fatal("the reader refuses logs the writer may leave")
	}
}

// A compaction publishes a log naming only what it keeps, then drops the rest;
// a crash before that publish keeps every blob the old log still names.
func TestSessionCompactionSweepsBlobsOnlyAfterPublishing(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "session.jsonl")
	kept, dropped := testImageDataURL(16, 4096), testImageDataURL(17, 4096)
	s := imageSession(t, path, kept, dropped)
	blobs := store.SessionBlobsDir(path)
	stale := filepath.Join(blobs, ".atomic-stale.tmp")
	fresh := filepath.Join(blobs, ".atomic-fresh.tmp")
	for _, tmp := range []string{stale, fresh} {
		if err := os.WriteFile(tmp, []byte("partial"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	s.Rewrite(s.Snapshot()[:3], "rewind")

	fileutil.CrashPoint = func(op, p string) {
		if op == "atomic-write" && p == store.SessionEventLog(path) {
			panic(crashSentinel{})
		}
	}
	func() {
		defer func() {
			fileutil.CrashPoint = nil
			if r := recover(); r != nil {
				if _, ok := r.(crashSentinel); !ok {
					panic(r)
				}
			}
		}()
		_ = s.SaveRewriteCompact(path)
	}()
	if n := len(blobNames(t, path)); n != 2 {
		t.Fatalf("a compaction that never published left %d blobs, want 2", n)
	}

	if err := s.SaveRewriteCompact(path); err != nil {
		t.Fatalf("compact: %v", err)
	}
	if got := blobNames(t, path); len(got) != 1 || sessionImageBlobScheme+got[0] != sessionImageKey(kept) {
		t.Fatalf("blobs after compaction = %v, want only the kept image", got)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("a stale temp file survived the sweep")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("a temp file a writer may still own was swept")
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	requireSameTranscript(t, loaded.Snapshot(), s.Snapshot())
}

// A file already under a blob's name is trusted only when it hashes to it: one
// of the right size and the wrong bytes is replaced on the next write.
func TestSessionCorruptBlobOfTheRightSizeIsRewritten(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "session.jsonl")
	url := testImageDataURL(18, 4096)
	s := imageSession(t, path, url)
	blob := onlyBlob(t, path)
	if err := os.WriteFile(blob, bytes.Repeat([]byte("x"), len(url)), 0o600); err != nil {
		t.Fatal(err)
	}
	stripCheckpointImages(t, path)
	if err := s.SaveRewriteCompact(path); err != nil {
		t.Fatalf("compact: %v", err)
	}
	if b, _ := os.ReadFile(blob); string(b) != url {
		t.Fatal("the corrupt blob was kept")
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	requireSameTranscript(t, loaded.Snapshot(), s.Snapshot())
}

// Blobs move before the event log that names them.
func TestSessionRenameMovesBlobsBeforeTheLog(t *testing.T) {
	moves := sessionSidecarMoves("a.jsonl", "b.jsonl")
	at := func(target string) int {
		return slices.IndexFunc(moves, func(p [2]string) bool { return p[0] == target })
	}
	blobs, log := at(store.SessionBlobsDir("a.jsonl")), at(store.SessionEventLog("a.jsonl"))
	if blobs < 0 || log < 0 || blobs > log {
		t.Fatalf("blobs move at %d, the event log at %d; blobs must go first", blobs, log)
	}
}

// Trashing a recovery branch takes its sub-agents' directories along too.
func TestRecoveryTrashMovesSubagentSidecarDirs(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "parent.jsonl")
	subDir := filepath.Join(dir, "subagents")
	sub := filepath.Join(subDir, "sa_trash.jsonl")
	meta := SubagentMeta{Ref: "sa_trash", ParentSession: BranchID(path), Status: SubagentCompleted}
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	for p, body := range map[string][]byte{
		sub: []byte("{}\n"), filepath.Join(subDir, "sa_trash.meta.json"): metaBytes,
		filepath.Join(store.SessionBlobsDir(sub), strings.Repeat("a", 64)): []byte("blob"),
		filepath.Join(store.SessionOutputsDir(sub), "call-1.txt"):          []byte("spilled"),
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	itemDir := filepath.Join(dir, "trash-item")
	if err := moveRecoverySubagentArtifacts(dir, path, itemDir); err != nil {
		t.Fatal(err)
	}
	for _, d := range store.SessionSidecarDirs(sub) {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Errorf("%s stayed behind", filepath.Base(d))
		}
		if _, err := os.Stat(filepath.Join(itemDir, "subagents", filepath.Base(d))); err != nil {
			t.Errorf("%s did not reach the trash: %v", filepath.Base(d), err)
		}
	}
}

// A torn log's salvaged tail still names its images by blob; a later
// compaction must leave those blobs, since the salvage is the only way back to
// the turns it holds and the checkpoint may never have seen them.
func TestSessionCompactionKeepsBlobsTheSalvageNames(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "session.jsonl")
	s := imageSession(t, path, testImageDataURL(31, 4096))
	salvaged := testImageDataURL(32, 4096)
	for _, m := range imageTurn(2, salvaged) {
		s.Add(m)
	}
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	logPath := store.SessionEventLog(path)
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.SplitAfter(raw, []byte("\n"))
	torn := slices.Concat(bytes.Join(lines[:len(lines)-2], nil), []byte("{torn\n"), bytes.Join(lines[len(lines)-2:], nil))
	if err := os.WriteFile(logPath, torn, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.SessionEventIndex(path)); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.Join(bytes.SplitAfter(checkpoint, []byte("\n"))[:3], nil), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded.Add(provider.Message{Role: provider.RoleUser, Content: "after the tear"})
	if err := loaded.SaveSnapshot(path); err != nil {
		t.Fatalf("repairing save: %v", err)
	}
	key := sessionImageKey(salvaged)
	if damaged, _ := os.ReadFile(store.SessionEventLogDamaged(path)); !bytes.Contains(damaged, []byte(key)) {
		t.Fatal("the salvage does not name the image, so this proves nothing")
	}
	if err := loaded.SaveRewriteCompact(path); err != nil {
		t.Fatalf("compact: %v", err)
	}
	if _, ok := readSessionImageBlob(store.SessionBlobsDir(path), key); !ok {
		t.Fatal("the compaction swept a blob only the salvage still names")
	}
}
