package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/provider"
)

// shrinkMetaReadBackoffs keeps corrupt-sidecar tests fast. Only the pacing of
// the torn-read retries changes; the retry-then-fail semantics stay intact.
func shrinkMetaReadBackoffs(t *testing.T) {
	t.Helper()
	old := branchMetaReadBackoffs
	branchMetaReadBackoffs = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { branchMetaReadBackoffs = old })
}

func assertNoRecoveryBranches(t *testing.T, sessionPath string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(sessionPath), "*-recovery-*.jsonl"))
	if err != nil {
		t.Fatalf("glob recovery branches: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("unexpected recovery branches: %v", matches)
	}
}

// A meta sidecar that exists but cannot be parsed must fail the save instead
// of being silently treated as revision 0 — the desync that used to fork
// bogus recovery branches. The unreadable ledger must also survive untouched
// so a healed read can pick up the real revision.
func TestSaveFailsClosedOnCorruptMetaLedger(t *testing.T) {
	shrinkMetaReadBackoffs(t)
	path := filepath.Join(t.TempDir(), "session.jsonl")
	metaPath := BranchMetaPath(path)
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot base: %v", err)
	}
	good, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read good meta: %v", err)
	}
	corrupt := []byte("{ torn json")
	if err := os.WriteFile(metaPath, corrupt, 0o644); err != nil {
		t.Fatalf("corrupt meta: %v", err)
	}

	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	saveErr := s.SaveSnapshot(path)
	if saveErr == nil {
		t.Fatal("SaveSnapshot with corrupt meta ledger succeeded, want error")
	}
	if errors.Is(saveErr, ErrSessionSnapshotConflict) {
		t.Fatalf("SaveSnapshot with corrupt meta misread as conflict: %v", saveErr)
	}
	assertNoRecoveryBranches(t, path)
	if b, err := os.ReadFile(metaPath); err != nil || string(b) != string(corrupt) {
		t.Fatalf("failed save rewrote the unreadable ledger: err=%v content=%q", err, b)
	}
	onDisk, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession after failed save: %v", err)
	}
	if got := len(onDisk.Messages); got != 2 {
		t.Fatalf("failed save changed transcript: %d messages, want 2", got)
	}

	// Once the sidecar reads cleanly again the same session saves normally.
	if err := os.WriteFile(metaPath, good, 0o644); err != nil {
		t.Fatalf("restore meta: %v", err)
	}
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot after meta repair: %v", err)
	}
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta after repair ok=%v err=%v", ok, err)
	}
	if meta.Revision != 2 {
		t.Fatalf("revision after repaired save = %d, want 2", meta.Revision)
	}
}

// An unreadable meta sidecar at load time must not stop the session from
// opening, and the revision-0 placeholder must not arm the CAS check: once
// the sidecar reads cleanly again, appends save without a bogus conflict.
func TestLoadSessionWithUnreadableMetaStillOpensAndAppends(t *testing.T) {
	shrinkMetaReadBackoffs(t)
	path := filepath.Join(t.TempDir(), "session.jsonl")
	metaPath := BranchMetaPath(path)
	seed := NewSession("sys")
	seed.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	seed.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	if err := seed.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot seed: %v", err)
	}
	good, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read good meta: %v", err)
	}
	if err := os.WriteFile(metaPath, []byte("{ torn json"), 0o644); err != nil {
		t.Fatalf("corrupt meta: %v", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession with unreadable meta: %v", err)
	}
	if got := len(loaded.Messages); got != 3 {
		t.Fatalf("loaded %d messages, want 3", got)
	}
	if !loaded.persisted.ok {
		t.Fatal("load must still establish a persistence baseline")
	}
	if loaded.persisted.revisionKnown {
		t.Fatal("baseline built without a readable ledger must be revision-unknown")
	}

	// The tear heals (another runtime's write completes / the corruption was
	// transient): the on-disk revision is an honest 1 against this runtime's
	// unknown baseline. The append must not be misread as a stale runtime.
	if err := os.WriteFile(metaPath, good, 0o644); err != nil {
		t.Fatalf("restore meta: %v", err)
	}
	loaded.Add(provider.Message{Role: provider.RoleUser, Content: "next"})
	if err := loaded.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot append after unreadable-meta load: %v", err)
	}
	assertNoRecoveryBranches(t, path)
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta after append ok=%v err=%v", ok, err)
	}
	if meta.Revision != 2 {
		t.Fatalf("revision after append = %d, want 2", meta.Revision)
	}
	if !loaded.persisted.revisionKnown {
		t.Fatal("successful save must re-learn the revision baseline")
	}

	// With the baseline re-learned, revision CAS is armed again.
	loaded.Add(provider.Message{Role: provider.RoleAssistant, Content: "reply"})
	if err := loaded.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot second append: %v", err)
	}
}

// Compaction-style rewrites from a revision-unknown baseline must fall back to
// digest+version ownership instead of failing the revision equality check.
func TestSaveRewriteWithUnreadableMetaBaselineOwnsByDigest(t *testing.T) {
	shrinkMetaReadBackoffs(t)
	path := filepath.Join(t.TempDir(), "session.jsonl")
	metaPath := BranchMetaPath(path)
	seed := NewSession("sys")
	seed.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	seed.Add(provider.Message{Role: provider.RoleAssistant, Content: "a long detailed answer"})
	if err := seed.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot seed: %v", err)
	}
	good, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read good meta: %v", err)
	}
	if err := os.WriteFile(metaPath, []byte("{ torn json"), 0o644); err != nil {
		t.Fatalf("corrupt meta: %v", err)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession with unreadable meta: %v", err)
	}
	if err := os.WriteFile(metaPath, good, 0o644); err != nil {
		t.Fatalf("restore meta: %v", err)
	}

	rewritten := loaded.Snapshot()
	rewritten[len(rewritten)-1].Content = "compacted"
	loaded.Replace(rewritten)
	if err := loaded.SaveRewrite(path); err != nil {
		t.Fatalf("SaveRewrite from unknown-revision baseline: %v", err)
	}
	assertNoRecoveryBranches(t, path)
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta after rewrite ok=%v err=%v", ok, err)
	}
	if meta.Revision != 2 {
		t.Fatalf("revision after rewrite = %d, want 2", meta.Revision)
	}
	reloaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession after rewrite: %v", err)
	}
	if got := reloaded.Messages[len(reloaded.Messages)-1].Content; got != "compacted" {
		t.Fatalf("rewrite tail = %q, want compacted", got)
	}
}

// A missing sidecar is not damage: it is the legitimate revision-0 state of a
// session that never recorded one, and must keep arming the CAS baseline.
func TestMissingMetaRemainsKnownZeroRevisionBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot base: %v", err)
	}
	if err := os.Remove(BranchMetaPath(path)); err != nil {
		t.Fatalf("remove meta: %v", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession without meta: %v", err)
	}
	if !loaded.persisted.ok || !loaded.persisted.revisionKnown || loaded.persisted.revision != 0 {
		t.Fatalf("missing meta baseline = %+v, want known revision 0", loaded.persisted)
	}
	loaded.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	if err := loaded.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot append without meta: %v", err)
	}
	assertNoRecoveryBranches(t, path)
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta recreated ok=%v err=%v", ok, err)
	}
	if meta.Revision != 1 {
		t.Fatalf("recreated revision = %d, want 1", meta.Revision)
	}
}

// Losing the event index (a listing accelerator, never read by LoadSession)
// must not fail a save whose transcript and revision already landed, and must
// not leave the baseline behind disk where the next save reads as a conflict.
func TestSaveSnapshotToleratesEventIndexWriteFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	// Squat a directory on the index path so every index write must fail.
	if err := os.MkdirAll(SessionEventIndexPath(path), 0o755); err != nil {
		t.Fatalf("pre-create index dir: %v", err)
	}
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot with failing event index = %v, want nil", err)
	}
	first, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta first ok=%v err=%v", ok, err)
	}
	if first.Revision != 1 {
		t.Fatalf("first revision = %d, want 1", first.Revision)
	}

	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("second SaveSnapshot = %v, want nil (baseline must advance despite index failure)", err)
	}
	second, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta second ok=%v err=%v", ok, err)
	}
	if second.Revision != 2 {
		t.Fatalf("second revision = %d, want 2", second.Revision)
	}
	assertNoRecoveryBranches(t, path)
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if got := len(loaded.Messages); got != 3 {
		t.Fatalf("loaded %d messages, want 3", got)
	}
	if info, err := os.Stat(SessionEventIndexPath(path)); err != nil || !info.IsDir() {
		t.Fatalf("fixture broke: index path no longer a directory (err=%v)", err)
	}
}

// Meta-only writers (rename, model stamp) racing content saves must never
// roll the revision ledger backwards or manufacture conflicts.
func TestSessionMetaConcurrentWritersKeepRevisionMonotonic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "turn 0"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot seed: %v", err)
	}
	if err := RenameSession(path, "seed-title"); err != nil {
		t.Fatalf("RenameSession seed: %v", err)
	}
	if err := SetBranchModelPreserveUpdated(path, "prov/model-seed"); err != nil {
		t.Fatalf("SetBranchModelPreserveUpdated seed: %v", err)
	}

	const saves = 25
	stop := make(chan struct{})
	var stopOnce sync.Once
	stopAll := func() { stopOnce.Do(func() { close(stop) }) }
	var wg sync.WaitGroup
	defer func() {
		stopAll()
		wg.Wait()
	}()
	metaErrCh := make(chan error, 2)
	var regressed atomic.Bool

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if err := RenameSession(path, fmt.Sprintf("title-%d", i)); err != nil {
				select {
				case metaErrCh <- err:
				default:
				}
				return
			}
			if err := SetBranchModelPreserveUpdated(path, fmt.Sprintf("prov/model-%d", i)); err != nil {
				select {
				case metaErrCh <- err:
				default:
				}
				return
			}
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		last := int64(0)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if meta, ok, err := LoadBranchMeta(path); err == nil && ok {
				if meta.Revision < last {
					regressed.Store(true)
					return
				}
				last = meta.Revision
			}
		}
	}()

	for i := 1; i <= saves; i++ {
		s.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("turn %d", i)})
		if err := s.SaveSnapshot(path); err != nil {
			t.Fatalf("SaveSnapshot %d under meta-writer hammer: %v", i, err)
		}
	}
	stopAll()
	wg.Wait()
	close(metaErrCh)
	for err := range metaErrCh {
		t.Fatalf("meta writer error: %v", err)
	}
	if regressed.Load() {
		t.Fatal("meta revision regressed while meta-only writers raced saves")
	}

	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta final ok=%v err=%v", ok, err)
	}
	if want := int64(saves + 1); meta.Revision != want {
		t.Fatalf("final revision = %d, want %d", meta.Revision, want)
	}
	digest, err := digestSessionMessages(s.Snapshot())
	if err != nil {
		t.Fatalf("digest final content: %v", err)
	}
	if meta.ContentDigest != digestString(digest) {
		t.Fatalf("final content digest = %q, want %q", meta.ContentDigest, digestString(digest))
	}
	if meta.CustomTitle == "" || meta.Model == "" {
		t.Fatalf("meta-only fields lost under hammer: %+v", meta)
	}
	assertNoRecoveryBranches(t, path)
}

// A saver that blocks on the save lock must persist whatever the session
// holds when it finally enters the critical section, not a stale capture from
// when it was scheduled — the out-of-order landing that used to surface as
// "session changed on disk" adoptions between the turn-end snapshot, periodic
// autosave, and shutdown snapshot.
func TestSaveSnapshotCapturesContentUnderSaveLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "turn 0"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot seed: %v", err)
	}

	unlock := lockSessionSavePath(path)
	done := make(chan error, 1)
	go func() { done <- s.SaveSnapshot(path) }()
	// Let the saver reach the save lock, then grow the session while it waits.
	time.Sleep(50 * time.Millisecond)
	s.Add(provider.Message{Role: provider.RoleUser, Content: "added while saver waited"})
	unlock()
	if err := <-done; err != nil {
		t.Fatalf("SaveSnapshot after lock release: %v", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if got := len(loaded.Messages); got != 3 {
		t.Fatalf("saved %d messages, want 3 (snapshot must be captured under the save lock)", got)
	}
	assertNoRecoveryBranches(t, path)
}

// Multiple in-process savers of one session (turn-end snapshot, periodic
// autosave, shutdown snapshot) must never conflict with each other: the
// snapshot is captured under the save lock, so a stale pre-lock capture can
// no longer land after a newer one.
func TestConcurrentSnapshotSaversNeverConflict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "turn 0"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot seed: %v", err)
	}

	const adds = 30
	stop := make(chan struct{})
	errCh := make(chan error, 64)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if err := s.SaveSnapshot(path); err != nil {
					select {
					case errCh <- err:
					default:
					}
				}
				select {
				case <-stop:
					return
				default:
				}
			}
		}()
	}
	for i := 1; i <= adds; i++ {
		s.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("turn %d", i)})
		time.Sleep(time.Millisecond)
	}
	close(stop)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if errors.Is(err, ErrSessionSnapshotConflict) {
			t.Fatalf("concurrent snapshot savers conflicted: %v", err)
		}
		t.Errorf("concurrent snapshot saver error: %v", err)
	}

	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("final SaveSnapshot: %v", err)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if got, want := len(loaded.Messages), adds+2; got != want {
		t.Fatalf("final message count = %d, want %d", got, want)
	}
	assertNoRecoveryBranches(t, path)
}

// A save that lands its transcript bytes and then fails to record the
// revision (fail-closed record, or a crash between the two writes) leaves the
// ledger describing older content. The next save of the same snapshot takes
// the up-to-date path and must heal the ledger — record the revision and
// digest the interrupted save deferred — instead of skipping it forever.
func TestSameContentSaveHealsStaleLedgerDigest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	metaPath := BranchMetaPath(path)
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot base: %v", err)
	}
	staleMeta, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read base meta: %v", err)
	}

	// Land new content, then rewind the sidecar to the pre-append ledger:
	// the exact on-disk aftermath of an append whose revision record failed.
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot append: %v", err)
	}
	if err := os.WriteFile(metaPath, staleMeta, 0o644); err != nil {
		t.Fatalf("rewind meta: %v", err)
	}

	// Any runtime resuming this file now pairs the landed transcript with the
	// stale revision — the post-crash shape.
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession on stale ledger: %v", err)
	}
	if err := loaded.SaveSnapshot(path); err != nil {
		t.Fatalf("same-content SaveSnapshot: %v", err)
	}
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta after heal ok=%v err=%v", ok, err)
	}
	if meta.Revision != 2 {
		t.Fatalf("healed revision = %d, want 2", meta.Revision)
	}
	digest, err := digestSessionMessages(loaded.Snapshot())
	if err != nil {
		t.Fatalf("digest current messages: %v", err)
	}
	if meta.ContentDigest != digestString(digest) {
		t.Fatalf("healed digest = %s, want %s", meta.ContentDigest, digestString(digest))
	}
	assertNoRecoveryBranches(t, path)

	// The healed baseline keeps working: the next append saves cleanly.
	loaded.Add(provider.Message{Role: provider.RoleUser, Content: "two"})
	if err := loaded.SaveSnapshot(path); err != nil {
		t.Fatalf("append after heal: %v", err)
	}
	after, _, err := LoadBranchMeta(path)
	if err != nil {
		t.Fatalf("LoadBranchMeta after append: %v", err)
	}
	if after.Revision != 3 {
		t.Fatalf("revision after post-heal append = %d, want 3", after.Revision)
	}
	assertNoRecoveryBranches(t, path)
}

// The surviving in-process saver — whose baseline never advanced because the
// failed save returned before markPersisted — heals through the same
// up-to-date path on its autosave retry of the identical snapshot.
func TestSameContentRetryHealsLedgerForSurvivingSaver(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	metaPath := BranchMetaPath(path)
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot base: %v", err)
	}
	staleMeta, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read base meta: %v", err)
	}
	baseline := s.persistState(path)

	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot append: %v", err)
	}
	// Rewind the sidecar and the in-memory baseline to the mid-save failure
	// state: bytes landed, record failed, markPersisted never ran.
	if err := os.WriteFile(metaPath, staleMeta, 0o644); err != nil {
		t.Fatalf("rewind meta: %v", err)
	}
	s.setPersistedBaseline(path, baseline.digest, baseline.version, baseline.revision, true, 0)

	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("autosave retry: %v", err)
	}
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta after heal ok=%v err=%v", ok, err)
	}
	if meta.Revision != 2 {
		t.Fatalf("healed revision = %d, want 2", meta.Revision)
	}
	assertNoRecoveryBranches(t, path)
}

// A legacy sidecar that predates content digests is not a stale ledger: the
// up-to-date path must leave it untouched rather than bump a revision other
// runtimes still hold as their baseline.
func TestUpToDateSaveLeavesDigestlessLegacyMetaAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	metaPath := BranchMetaPath(path)
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot base: %v", err)
	}
	// Strip the digest by editing the raw sidecar: the save helpers back-fill
	// an empty digest from the existing meta, exactly like real legacy files
	// acquired one only through a content-bearing save.
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("decode meta: %v", err)
	}
	delete(fields, "content_digest")
	stripped, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("encode meta: %v", err)
	}
	if err := os.WriteFile(metaPath, stripped, 0o644); err != nil {
		t.Fatalf("write legacy meta: %v", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession legacy meta: %v", err)
	}
	if err := loaded.SaveSnapshot(path); err != nil {
		t.Fatalf("same-content SaveSnapshot: %v", err)
	}
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta ok=%v err=%v", ok, err)
	}
	if meta.Revision != 1 {
		t.Fatalf("legacy revision = %d, want 1 (no gratuitous bump)", meta.Revision)
	}
	if meta.ContentDigest != "" {
		t.Fatalf("legacy digest = %q, want empty (untouched)", meta.ContentDigest)
	}
	assertNoRecoveryBranches(t, path)
}
