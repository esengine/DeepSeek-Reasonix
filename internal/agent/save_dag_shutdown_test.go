package agent

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"reasonix/internal/fileutil"
	"reasonix/internal/provider"
	"reasonix/internal/store"
)

// holdLockAndShortenWait makes the next save time out on the file lock the
// way a stalled peer process would cause it to.
func holdLockAndShortenWait(t *testing.T, path string) (release func()) {
	t.Helper()
	restore := SetSessionFileLockWaitForTest(40*time.Millisecond, 5*time.Millisecond)
	t.Cleanup(restore)
	held, err := HoldSessionFileLockForTest(path)
	if err != nil {
		t.Fatalf("hold session lock: %v", err)
	}
	released := false
	release = func() {
		if !released {
			released = true
			held()
		}
	}
	t.Cleanup(release)
	return release
}

func headsByKind(t *testing.T, path string) (main, other SessionHead) {
	t.Helper()
	heads, err := ListSessionHeads(path)
	if err != nil || len(heads) != 2 {
		t.Fatalf("heads = %+v err=%v, want main plus one more", heads, err)
	}
	return heads[0], heads[1]
}

func TestAppendForShutdownWithoutLockKeepsTailOnFreshHead(t *testing.T) {
	path := dagTestSession(t)
	s := dagSavedSession(t, path, "q1", "a1")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "q2"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "a2"})
	release := holdLockAndShortenWait(t, path)
	if err := s.SaveSnapshot(path); !errors.Is(err, ErrSessionFileLockHeld) {
		t.Fatalf("locked save err = %v, want ErrSessionFileLockHeld", err)
	}
	handled, err := s.AppendForShutdownWithoutLock(path, false)
	if err != nil || !handled {
		t.Fatalf("AppendForShutdownWithoutLock = handled %v err %v", handled, err)
	}
	assertNoTranscriptCopies(t, path)
	main, tail := headsByKind(t, path)
	if tail.Kind != HeadKindConcurrent || !tail.Selected || tail.MessageCount != 5 || tail.ParentHead != SessionMainHead {
		t.Fatalf("shutdown head = %+v", tail)
	}
	if !main.Covered || main.MessageCount != 3 {
		t.Fatalf("main head after unlocked append = %+v, want it covered by the shutdown head", main)
	}
	if ref, ok := s.Head(); !ok || ref.HeadID != tail.ID {
		t.Fatalf("session head = %+v ok=%v, want the shutdown head", ref, ok)
	}
	events := s.DrainHeadEvents()
	if len(events) != 1 || events[0].Kind != HeadEventForkedConcurrent || events[0].HeadID != tail.ID {
		t.Fatalf("head events = %+v", events)
	}
	release()
	// The next locked save continues on the shutdown head and finally refreshes
	// the derived files it skipped.
	if err := s.Save(path); err != nil {
		t.Fatalf("locked save after shutdown append: %v", err)
	}
	if meta, _, _ := LoadBranchMeta(path); meta.HeadID != tail.ID || meta.HeadCount != 2 {
		t.Fatalf("meta mirror after the next locked save = head %q count %d", meta.HeadID, meta.HeadCount)
	}
	s.Add(provider.Message{Role: provider.RoleUser, Content: "q3"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	st := dagReplay(t, path)
	if got := strings.Join(dagChain(st, tail.ID), ","); got != "sys,q1,a1,q2,a2,q3" {
		t.Fatalf("shutdown head chain = %s", got)
	}
	if len(st.heads) != 2 {
		t.Fatalf("a later locked save must not fork again: %d heads", len(st.heads))
	}
	reloaded, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if ref, _ := reloaded.Head(); ref.HeadID != tail.ID || len(reloaded.Messages) != 6 {
		t.Fatalf("reload = head %q with %d messages", ref.HeadID, len(reloaded.Messages))
	}
}

func TestAppendForShutdownWithoutLockAfterTornTail(t *testing.T) {
	path := dagTestSession(t)
	s := dagSavedSession(t, path, "q1", "a1")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "q2"})
	logPath := store.SessionEventLog(path)
	f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"schema_version":2,"type":"message","id":"torn-by-a-crash","head":"main","msgs":[{"role":"user","con`); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	release := holdLockAndShortenWait(t, path)
	if err := s.SaveSnapshot(path); !errors.Is(err, ErrSessionFileLockHeld) {
		t.Fatalf("locked save err = %v", err)
	}
	if handled, err := s.AppendForShutdownWithoutLock(path, false); err != nil || !handled {
		t.Fatalf("AppendForShutdownWithoutLock = handled %v err %v", handled, err)
	}
	st := dagReplay(t, path)
	if st.damaged || st.holes != 1 {
		t.Fatalf("replay after append behind a torn tail: damaged=%v holes=%d", st.damaged, st.holes)
	}
	_, tail := headsByKind(t, path)
	if got := strings.Join(dagChain(st, tail.ID), ","); got != "sys,q1,a1,q2" {
		t.Fatalf("shutdown head chain = %s", got)
	}
	raw, _ := os.ReadFile(logPath)
	if !strings.Contains(string(raw), "torn-by-a-crash") {
		t.Fatal("the torn line must stay in the log until a rotation reclaims it")
	}
	release()
	reloaded, err := LoadSession(path)
	if err != nil || len(reloaded.Messages) != 4 || reloaded.Messages[3].Content != "q2" {
		t.Fatalf("reload = %d messages err=%v", len(reloaded.Messages), err)
	}
	// With the lease held (the single-writer proof), the next locked save
	// rotates the hole away even though it has nothing new to append.
	lease, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	defer lease.Release()
	if err := reloaded.Save(path); err != nil {
		t.Fatalf("locked save after a hole: %v", err)
	}
	if again := dagReplay(t, path); again.holes != 0 || again.generation != 2 {
		t.Fatalf("the next locked save must rotate the hole away: holes=%d generation=%d", again.holes, again.generation)
	}
}

func TestAppendForShutdownWithoutLockWritesMarkersOnTheSameHead(t *testing.T) {
	path := dagTestSession(t)
	s := dagSavedSession(t, path, "q1")
	if !s.QueueTurnBegin("turn-1", false) {
		t.Fatal("QueueTurnBegin refused")
	}
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "a1"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	if !s.QueueTurnEnd("turn-1") {
		t.Fatal("QueueTurnEnd refused")
	}
	release := holdLockAndShortenWait(t, path)
	if err := s.SaveSnapshot(path); !errors.Is(err, ErrSessionFileLockHeld) {
		t.Fatalf("locked save err = %v", err)
	}
	if handled, err := s.AppendForShutdownWithoutLock(path, false); err != nil || !handled {
		t.Fatalf("AppendForShutdownWithoutLock = handled %v err %v", handled, err)
	}
	release()
	st := dagReplay(t, path)
	if len(st.heads) != 1 || st.heads[SessionMainHead].openTurn != nil {
		t.Fatalf("a marker-only batch must close the turn on the same head: heads=%d openTurn=%+v", len(st.heads), st.heads[SessionMainHead].openTurn)
	}
	if got := dagEntryTypes(t, path); got[len(got)-1] != sessionDAGTypeTurnEnd {
		t.Fatalf("entries = %v, want the turn_end appended last", got)
	}
}

func TestAppendForShutdownWithoutLockLeavesSchemaOneToRecovery(t *testing.T) {
	useSchemaOneLog(t)
	path := dagTestSession(t)
	s := dagSavedSession(t, path, "q1")
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "a1"})
	holdLockAndShortenWait(t, path)
	if handled, err := s.AppendForShutdownWithoutLock(path, false); handled || err != nil {
		t.Fatalf("schema-1 session = handled %v err %v, want the recovery-copy path", handled, err)
	}
}

func TestReplaySkipsTornLineBetweenEntries(t *testing.T) {
	path := dagTestSession(t)
	ids, base := dagLinearLog(t, path)
	logPath := store.SessionEventLog(path)
	f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"schema_version":2,"type":"message","id":"torn","head":"ma`); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	torn := dagReplay(t, path)
	if !torn.damaged || torn.holes != 0 || len(torn.nodes) != len(ids) {
		t.Fatalf("torn tail must stay damaged: damaged=%v holes=%d nodes=%d", torn.damaged, torn.holes, len(torn.nodes))
	}
	f, err = os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	dagAppend(t, path, dagMessageEntry(t, SessionMainHead, ids[len(ids)-1], "", dagMsg(provider.RoleAssistant, "after the hole", "after"), base.Add(time.Minute)))
	st := dagReplay(t, path)
	if st.damaged || st.holes != 1 || len(st.nodes) != len(ids)+1 {
		t.Fatalf("replay past a hole: damaged=%v holes=%d nodes=%d", st.damaged, st.holes, len(st.nodes))
	}
	if got := dagChain(st, SessionMainHead); got[len(got)-1] != "after the hole" {
		t.Fatalf("chain = %v", got)
	}
	if repaired, err := repairSessionDAGTail(path, st, time.Now().Add(time.Hour)); repaired || err != nil {
		t.Fatalf("a hole is not a torn tail: repaired=%v err=%v", repaired, err)
	}
}

func TestRotationCarriesAppendsThatLandedDuringReplace(t *testing.T) {
	path := dagTestSession(t)
	s := dagSavedSession(t, path, "q1", "a1")
	st := dagReplay(t, path)
	leaf := st.heads[SessionMainHead].leaf
	sessionDAGRotateBeforeReplace = func(sessionPath string) {
		dagAppend(t, sessionPath, dagMessageEntry(t, SessionMainHead, leaf, "", dagMsg(provider.RoleUser, "landed mid-rotation", "late"), time.Now().UTC()))
	}
	t.Cleanup(func() { sessionDAGRotateBeforeReplace = nil })
	if err := rotateSessionDAG(path, st, time.Now().UTC()); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	sessionDAGRotateBeforeReplace = nil
	fresh := dagReplay(t, path)
	if fresh.generation != 2 {
		t.Fatalf("generation after rotation = %d", fresh.generation)
	}
	if got := strings.Join(dagChain(fresh, SessionMainHead), ","); got != "sys,q1,a1,landed mid-rotation" {
		t.Fatalf("chain after rotation = %s, want the late append carried over", got)
	}
	_ = s
}

func TestUnlockedAppendWaitsForRotationMarkerThenReappends(t *testing.T) {
	path := dagTestSession(t)
	dagSavedSession(t, path, "q1", "a1")
	st := dagReplay(t, path)
	leaf := st.heads[SessionMainHead].leaf
	rotated, err := buildRotatedSessionDAG(st, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	data, err := encodeSessionDAGEntries(rotated, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	marker := store.SessionEventLogRotating(path)
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// A rotation that already read the log publishes its replacement while the
	// appender is waiting on the marker, then clears the marker.
	published := make(chan struct{})
	go func() {
		defer close(published)
		time.Sleep(150 * time.Millisecond)
		if err := fileutil.AtomicWriteFileStrict(store.SessionEventLog(path), data, 0o600); err != nil {
			t.Error(err)
		}
		_ = os.Remove(marker)
	}()
	entry := dagMessageEntry(t, SessionMainHead, leaf, "", dagMsg(provider.RoleUser, "appended around the rotation", "around"), time.Now().UTC())
	started := time.Now()
	if _, err := appendSessionDAGEntriesUnlocked(path, []sessionDAGEntry{entry}); err != nil {
		t.Fatalf("unlocked append: %v", err)
	}
	<-published
	if time.Since(started) < 150*time.Millisecond {
		t.Fatal("the append returned before the rotation marker cleared")
	}
	fresh := dagReplay(t, path)
	if fresh.generation != 2 {
		t.Fatalf("generation = %d, want the rotated log", fresh.generation)
	}
	if got := strings.Join(dagChain(fresh, SessionMainHead), ","); got != "sys,q1,a1,appended around the rotation" {
		t.Fatalf("chain after the rotation = %s, want the append re-landed in the new log", got)
	}
}
