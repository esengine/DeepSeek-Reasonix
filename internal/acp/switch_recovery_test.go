package acp

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// divergedACPSession writes a transcript to path whose on-disk content has
// diverged from the returned in-memory session, so the next Snapshot on a
// controller holding the stale session hits a conflict and retargets to a
// recovery branch.
func divergedACPSession(t *testing.T, path string) *agent.Session {
	t.Helper()
	disk := agent.NewSession("sys prompt")
	disk.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	disk.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	disk.Add(provider.Message{Role: provider.RoleUser, Content: "disk second"})
	if err := disk.Save(path); err != nil {
		t.Fatalf("save disk session: %v", err)
	}

	stale := agent.NewSession("sys prompt")
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	stale.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "local second"})
	return stale
}

// primaryRecoveryFiles filters a recovery-branch glob down to primary session
// transcripts, dropping the .events.jsonl / .guardian.jsonl sidecars that the
// *-recovery-*.jsonl pattern also matches.
func primaryRecoveryFiles(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*-recovery-*.jsonl"))
	if err != nil {
		t.Fatalf("glob recovery branches: %v", err)
	}
	primary := matches[:0]
	for _, path := range matches {
		base := filepath.Base(path)
		if strings.HasSuffix(base, ".events.jsonl") || strings.HasSuffix(base, ".guardian.jsonl") {
			continue
		}
		primary = append(primary, path)
	}
	return primary
}

func assertACPSessionOnRecoveryPath(t *testing.T, sess *acpSession, originalPath, recoveryPath string) {
	t.Helper()
	if recoveryPath == "" || recoveryPath == originalPath || !strings.Contains(filepath.Base(recoveryPath), "-recovery-") {
		t.Fatalf("session path = %q, want recovery path distinct from %q", recoveryPath, originalPath)
	}
	sess.mu.Lock()
	transcript := sess.transcript
	lease := sess.lease
	sess.mu.Unlock()
	if transcript != recoveryPath {
		t.Fatalf("session transcript = %q, want recovery path %q", transcript, recoveryPath)
	}
	if lease == nil || lease.Path() != agent.CanonicalSessionPath(recoveryPath) {
		got := ""
		if lease != nil {
			got = lease.Path()
		}
		t.Fatalf("session lease path = %q, want recovery path %q", got, recoveryPath)
	}
	// The original transcript's lease must have been released by the move so
	// another runtime can bind it.
	orig, err := agent.TryAcquireSessionLease(originalPath)
	if err != nil {
		t.Fatalf("original transcript lease should be free after recovery move: %v", err)
	}
	orig.Release()
}

// TestACPRebuildSessionContinuesRecoveryPathAfterSnapshotConflict is the ACP
// twin of the desktop rebuild fix: when the pre-rebuild Snapshot hits a
// conflict and retargets the old controller to a recovery branch, the session
// bookkeeping must follow at commit time (sessionRecoveredHandler moves
// sess.transcript and the lease), and AdoptHistory must bind the replacement
// controller to that recovery path. A pre-snapshot capture bound the
// just-recovered transcript back to the original file, so every later save
// re-conflicted and derived yet another recovery branch.
func TestACPRebuildSessionContinuesRecoveryPathAfterSnapshotConflict(t *testing.T) {
	dir := t.TempDir()
	originalPath := filepath.Join(dir, "acp-switch-conflict.jsonl")
	stale := divergedACPSession(t, originalPath)

	sink := newUpdateSink(&fakeNotifier{}, "sess-recovery")
	sess := &acpSession{
		id:         "sess-recovery",
		sink:       sink,
		cwd:        dir,
		model:      "fast",
		transcript: originalPath,
	}
	lease, err := agent.TryAcquireSessionLease(originalPath)
	if err != nil {
		t.Fatalf("acquire original session lease: %v", err)
	}
	sess.lease = lease
	t.Cleanup(sess.releaseSessionLease)

	svc := &service{
		factory:  &configurableFactory{dir: dir},
		sessions: map[string]*acpSession{sess.id: sess},
	}
	oldCtrl := control.New(control.Options{
		Executor:           agent.New(nil, nil, stale, agent.Options{}, event.Discard),
		SessionDir:         dir,
		SessionPath:        originalPath,
		Label:              "fast",
		OnSessionRecovered: svc.sessionRecoveredHandler(sess.id),
	})
	sess.ctrl = oldCtrl

	if err := svc.rebuildSession(context.Background(), sess, SessionConfigState{Model: "pro"}); err != nil {
		t.Fatalf("rebuildSession: %v", err)
	}
	if sess.ctrl == oldCtrl {
		t.Fatal("session controller was not replaced")
	}

	recoveryPath := sess.ctrl.SessionPath()
	assertACPSessionOnRecoveryPath(t, sess, originalPath, recoveryPath)

	// The rebuilt controller adopted the recovery file's baseline, so its next
	// snapshot must not derive a second recovery branch.
	if err := sess.ctrl.Snapshot(); err != nil {
		t.Fatalf("Snapshot after rebuild: %v", err)
	}
	if primary := primaryRecoveryFiles(t, dir); len(primary) != 1 || primary[0] != recoveryPath {
		t.Fatalf("recovery branches after follow-up snapshot = %v, want only %q", primary, recoveryPath)
	}
}

// TestACPPersistAfterTurnMovesBookkeepingToRecoveryPath covers the autosave
// path: a turn-end Snapshot in persistAfterTurn that recovers onto a recovery
// branch must move sess.transcript and the session lease with the controller,
// so session/prompt reports the live file, session/delete destroys it, and the
// recovery transcript stays lease-guarded against other runtimes.
func TestACPPersistAfterTurnMovesBookkeepingToRecoveryPath(t *testing.T) {
	dir := t.TempDir()
	originalPath := filepath.Join(dir, "acp-autosave-conflict.jsonl")
	stale := divergedACPSession(t, originalPath)

	sink := newUpdateSink(&fakeNotifier{}, "sess-autosave")
	sess := &acpSession{
		id:         "sess-autosave",
		sink:       sink,
		cwd:        dir,
		model:      "fast",
		transcript: originalPath,
	}
	lease, err := agent.TryAcquireSessionLease(originalPath)
	if err != nil {
		t.Fatalf("acquire original session lease: %v", err)
	}
	sess.lease = lease
	t.Cleanup(sess.releaseSessionLease)

	svc := &service{
		factory:  &configurableFactory{dir: dir},
		sessions: map[string]*acpSession{sess.id: sess},
	}
	ctrl := control.New(control.Options{
		Executor:           agent.New(nil, nil, stale, agent.Options{}, event.Discard),
		SessionDir:         dir,
		SessionPath:        originalPath,
		Label:              "fast",
		OnSessionRecovered: svc.sessionRecoveredHandler(sess.id),
	})
	sess.ctrl = ctrl
	t.Cleanup(ctrl.Close)

	sess.persistAfterTurn("hello")

	recoveryPath := ctrl.SessionPath()
	assertACPSessionOnRecoveryPath(t, sess, originalPath, recoveryPath)
	if primary := primaryRecoveryFiles(t, dir); len(primary) != 1 || primary[0] != recoveryPath {
		t.Fatalf("recovery branches after autosave = %v, want only %q", primary, recoveryPath)
	}
	// The next turn-end autosave writes the recovery file the session now
	// owns; it must not derive a second recovery branch.
	sess.persistAfterTurn("again")
	if got := ctrl.SessionPath(); got != recoveryPath {
		t.Fatalf("controller session path after second autosave = %q, want %q", got, recoveryPath)
	}
	if primary := primaryRecoveryFiles(t, dir); len(primary) != 1 || primary[0] != recoveryPath {
		t.Fatalf("recovery branches after second autosave = %v, want only %q", primary, recoveryPath)
	}
}

// recoverACPSessionAndRestart drives an autosave recovery for session id in
// dir, then simulates a process restart: the live session's lease is released,
// its controller closed, and a fresh service (empty session registry, same
// session dir) is returned alongside the original and recovery paths.
func recoverACPSessionAndRestart(t *testing.T, dir, id string) (originalPath, recoveryPath string, restarted *service) {
	t.Helper()
	originalPath = transcriptPath(dir, id)
	stale := divergedACPSession(t, originalPath)

	svc := &service{
		factory:  &configurableFactory{dir: dir},
		sessions: map[string]*acpSession{},
	}
	sess := &acpSession{
		id:         id,
		sink:       newUpdateSink(&fakeNotifier{}, id),
		cwd:        dir,
		model:      "fast",
		title:      "recovered title",
		transcript: originalPath,
	}
	lease, err := agent.TryAcquireSessionLease(originalPath)
	if err != nil {
		t.Fatalf("acquire original session lease: %v", err)
	}
	sess.lease = lease
	svc.sessions[id] = sess
	ctrl := control.New(control.Options{
		Executor:           agent.New(nil, nil, stale, agent.Options{}, event.Discard),
		SessionDir:         dir,
		SessionPath:        originalPath,
		Label:              "fast",
		OnSessionRecovered: svc.sessionRecoveredHandler(id),
	})
	sess.ctrl = ctrl

	sess.persistAfterTurn("hello")
	recoveryPath = ctrl.SessionPath()
	assertACPSessionOnRecoveryPath(t, sess, originalPath, recoveryPath)

	sess.releaseSessionLease()
	ctrl.Close()
	restarted = &service{
		conn:     NewConn(strings.NewReader(""), io.Discard),
		factory:  &configurableFactory{dir: dir},
		sessions: map[string]*acpSession{},
	}
	return originalPath, recoveryPath, restarted
}

// TestACPLoadAfterRestartFollowsRecoveryTranscript covers the restart half of
// the recovery move: session/load and session/resume resolve the session id to
// the transcript the session actually lives in. Without the id-keyed redirect,
// a restart reopened the pre-recovery file and the user's recovered work
// silently vanished from ACP's view.
func TestACPLoadAfterRestartFollowsRecoveryTranscript(t *testing.T) {
	dir := t.TempDir()
	id := "sess-restart"
	originalPath, recoveryPath, svc := recoverACPSessionAndRestart(t, dir, id)

	if _, err := svc.openExistingSession(context.Background(), "session/load", id, dir, nil, false); err != nil {
		t.Fatalf("openExistingSession after restart: %v", err)
	}
	loaded := svc.session(id)
	if loaded == nil {
		t.Fatal("session not registered after load")
	}
	t.Cleanup(func() {
		loaded.releaseSessionLease()
		loaded.ctrl.Close()
	})
	assertACPSessionOnRecoveryPath(t, loaded, originalPath, recoveryPath)
	if got := loaded.ctrl.SessionPath(); got != recoveryPath {
		t.Fatalf("loaded controller session path = %q, want recovery path %q", got, recoveryPath)
	}
	// The test factory's controller has no executor, so prove the content via
	// the transcript ACP now points at: it must hold the recovered local line,
	// not the pre-recovery disk line.
	resumed, err := agent.LoadSession(loaded.transcript)
	if err != nil {
		t.Fatalf("load resolved transcript: %v", err)
	}
	msgs := resumed.Snapshot()
	if len(msgs) == 0 {
		t.Fatal("resolved transcript is empty")
	}
	if got := msgs[len(msgs)-1].Content; got != "local second" {
		t.Fatalf("resolved transcript last message = %q, want recovered local transcript (%q)", got, "local second")
	}
}

// TestACPDeleteAfterRestartRemovesRecoveryAndIDKeyedFiles: session/delete on a
// non-live recovered session must remove both the recovery transcript (the
// session's live file) and the id-keyed original, or the survivor resurfaces
// in session/list as a ghost that can never be deleted by id.
func TestACPDeleteAfterRestartRemovesRecoveryAndIDKeyedFiles(t *testing.T) {
	dir := t.TempDir()
	id := "sess-del"
	originalPath, recoveryPath, svc := recoverACPSessionAndRestart(t, dir, id)

	raw, err := json.Marshal(SessionDeleteParams{SessionID: id})
	if err != nil {
		t.Fatalf("marshal delete params: %v", err)
	}
	if _, err := svc.sessionDelete(context.Background(), raw); err != nil {
		t.Fatalf("sessionDelete after restart: %v", err)
	}
	for _, path := range []string{originalPath, recoveryPath, acpMetaPath(originalPath), acpMetaPath(recoveryPath)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s should be removed by session/delete, stat err = %v", path, err)
		}
	}
	res, err := svc.sessionList(context.Background(), nil)
	if err != nil {
		t.Fatalf("sessionList after delete: %v", err)
	}
	if sessions := res.(SessionListResult).Sessions; len(sessions) != 0 {
		t.Fatalf("session list after delete = %#v, want empty", sessions)
	}
}

// TestACPSessionListAfterRecoveryShowsSingleActiveEntry: after a recovery the
// id-keyed sidecar becomes a redirect, and session/list must present exactly
// one entry for the id, backed by the active recovery transcript's metadata
// (the live title), never the stale pre-recovery sidecar.
func TestACPSessionListAfterRecoveryShowsSingleActiveEntry(t *testing.T) {
	dir := t.TempDir()
	id := "sess-list"
	_, _, svc := recoverACPSessionAndRestart(t, dir, id)

	res, err := svc.sessionList(context.Background(), nil)
	if err != nil {
		t.Fatalf("sessionList after recovery: %v", err)
	}
	sessions := res.(SessionListResult).Sessions
	if len(sessions) != 1 {
		t.Fatalf("session list after recovery = %#v, want exactly one entry", sessions)
	}
	if sessions[0].SessionID != id {
		t.Fatalf("session list entry id = %q, want %q", sessions[0].SessionID, id)
	}
	if sessions[0].Title != "recovered title" {
		t.Fatalf("session list entry title = %q, want the active transcript's title %q", sessions[0].Title, "recovered title")
	}
}
