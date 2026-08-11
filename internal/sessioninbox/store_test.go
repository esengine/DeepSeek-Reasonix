package sessioninbox

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/fileutil"
	"reasonix/internal/store"
)

func TestEnqueueSnapshotAndRead(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(session, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	rec, err := s.Enqueue(EnqueueRequest{
		Intent: IntentFollowup,
		Envelope: PromptEnvelope{
			DisplayText: "hello world",
			SubmitText:  "hello world",
		},
		Source: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.ItemID == "" || rec.Position != 1 {
		t.Fatalf("receipt = %+v", rec)
	}
	snap := s.Snapshot()
	if len(snap.Items) != 1 || snap.Items[0].Preview == "" {
		t.Fatalf("snapshot = %+v", snap)
	}
	// Body must not appear in snapshot metadata beyond preview.
	if strings.Contains(snap.Items[0].Preview, "\x00") {
		t.Fatal("unexpected binary in preview")
	}
	meta, env, err := s.ReadItem(rec.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	if meta.ID != rec.ItemID || env.SubmitText != "hello world" {
		t.Fatalf("read = meta=%+v env=%+v", meta, env)
	}
	// Unix: dir 0700. Windows reports 0777 and does not enforce owner-only bits.
	if runtime.GOOS != "windows" {
		info, err := os.Stat(store.SessionInboxDir(session))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("inbox dir perm = %o", info.Mode().Perm())
		}
	}
}

func TestIdempotentEnqueue(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	_ = os.WriteFile(session, []byte("{}\n"), 0o644)
	s, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, err := s.Enqueue(EnqueueRequest{
		Intent:      IntentFollowup,
		Envelope:    PromptEnvelope{SubmitText: "x"},
		Idempotency: "msg-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Enqueue(EnqueueRequest{
		Intent:      IntentFollowup,
		Envelope:    PromptEnvelope{SubmitText: "x"},
		Idempotency: "msg-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.ItemID != b.ItemID || !b.Idempotent {
		t.Fatalf("idempotency failed: a=%+v b=%+v", a, b)
	}
	if len(s.Snapshot().Items) != 1 {
		t.Fatalf("want 1 item, got %d", len(s.Snapshot().Items))
	}
}

func TestIdempotencyConflictRejectsDifferentInput(t *testing.T) {
	session := filepath.Join(t.TempDir(), "s.jsonl")
	s, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "first"}, Idempotency: "msg-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "different"}, Idempotency: "msg-1"}); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("different input error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestIdempotencyHashTreatsLegacyAndModernInvocationAsEquivalent(t *testing.T) {
	legacy, err := idempotencyRequestHash(completeEnqueueEnvelope(PromptEnvelope{
		DisplayText: "/init", Invocation: &StructuredInvocation{Name: "init"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	modern, err := idempotencyRequestHash(completeEnqueueEnvelope(PromptEnvelope{
		DisplayText: "/init", Invocations: []StructuredInvocation{{Name: "init", Kind: "skill", Offset: 7}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if legacy != modern {
		t.Fatalf("legacy hash %q != modern hash %q", legacy, modern)
	}
}

func TestIdempotencyReceiptSurvivesAckAndReopen(t *testing.T) {
	session := filepath.Join(t.TempDir(), "s.jsonl")
	s, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "write once"}, Idempotency: "msg-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimItem(first.ItemID); err != nil {
		t.Fatal(err)
	}
	if err := s.AckDequeue(first.ItemID); err != nil {
		t.Fatal(err)
	}
	s.Close()

	reopened, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	retry, err := reopened.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "write once"}, Idempotency: "msg-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !retry.Idempotent || retry.ItemID != first.ItemID || len(reopened.Snapshot().Items) != 0 {
		t.Fatalf("completed retry = %+v items=%+v", retry, reopened.Snapshot().Items)
	}
	if _, err := reopened.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "write twice"}, Idempotency: "msg-1"}); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("completed conflicting retry error = %v", err)
	}
}

func TestCollectAliasReceiptSurvivesAck(t *testing.T) {
	session := filepath.Join(t.TempDir(), "s.jsonl")
	s, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	first, err := s.Enqueue(EnqueueRequest{
		Envelope: PromptEnvelope{SubmitText: "first"}, Idempotency: "msg-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	secondRequest := PromptEnvelope{SubmitText: "second", Source: "bot", Extra: map[string]string{"route": "chat-1"}}
	if _, err := s.UpdateItemWithIdempotency(
		first.ItemID,
		PromptEnvelope{SubmitText: "first\nsecond"},
		"msg-2",
		secondRequest,
	); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimItem(first.ItemID); err != nil {
		t.Fatal(err)
	}
	if err := s.AckDequeue(first.ItemID); err != nil {
		t.Fatal(err)
	}
	retry, err := s.Enqueue(EnqueueRequest{Envelope: secondRequest, Idempotency: "msg-2"})
	if err != nil {
		t.Fatal(err)
	}
	if !retry.Idempotent || retry.ItemID != first.ItemID || len(s.Snapshot().Items) != 0 {
		t.Fatalf("completed collect replay = %+v snapshot=%+v", retry, s.Snapshot())
	}
}

func TestV1ManifestMigratesIdempotencyFingerprint(t *testing.T) {
	session := filepath.Join(t.TempDir(), "s.jsonl")
	s, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Enqueue(EnqueueRequest{
		Envelope: PromptEnvelope{SubmitText: "legacy durable input"}, Idempotency: "legacy-msg-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	manifestPath := filepath.Join(store.SessionInboxDir(session), manifestName)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var legacy map[string]any
	if err := json.Unmarshal(data, &legacy); err != nil {
		t.Fatal(err)
	}
	legacy["schemaVersion"] = float64(1)
	delete(legacy, "idempotencyHashes")
	delete(legacy, "receipts")
	data, err = json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	retry, err := reopened.Enqueue(EnqueueRequest{
		Envelope: PromptEnvelope{SubmitText: "legacy durable input"}, Idempotency: "legacy-msg-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !retry.Idempotent || retry.ItemID != first.ItemID {
		t.Fatalf("migrated replay = %+v, first = %+v", retry, first)
	}
	if reopened.man.SchemaVersion != SchemaVersion || !validSHA256(reopened.man.IdempotencyHashes["legacy-msg-1"]) {
		t.Fatalf("manifest was not upgraded with fingerprint: %+v", reopened.man)
	}
}

func TestManifestBlobPathEscapeIsQuarantinedWithoutTouchingTarget(t *testing.T) {
	session := filepath.Join(t.TempDir(), "s.jsonl")
	inboxDir := store.SessionInboxDir(session)
	if err := os.MkdirAll(filepath.Join(inboxDir, blobsDirName), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(inboxDir, "outside.json")
	if err := os.WriteFile(target, []byte("do-not-touch"), 0o600); err != nil {
		t.Fatal(err)
	}
	bad := manifest{
		SchemaVersion: SchemaVersion,
		RunID:         ProcessRunID(),
		Items: []InboxItemMeta{{
			ID:        newRandomID(),
			BlobName:  "../outside",
			Intent:    IntentFollowup,
			State:     StateQueued,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}},
	}
	data, err := json.Marshal(bad)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inboxDir, manifestName), data, 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if snap := s.Snapshot(); !snap.Paused || !snap.Recovered || len(snap.Items) != 0 {
		t.Fatalf("invalid manifest was not quarantined: %+v", snap)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "do-not-touch" {
		t.Fatalf("path escape target changed: %q err=%v", got, err)
	}
	if _, err := s.blobPath("../outside"); err == nil {
		t.Fatal("blobPath accepted a parent traversal")
	}
}

func TestValidateManifestRejectsSemanticCorruption(t *testing.T) {
	id := newRandomID()
	base := InboxItemMeta{
		ID: id, BlobName: id, Intent: IntentFollowup, State: StateQueued,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	for name, mutate := range map[string]func(*manifest){
		"negative size": func(m *manifest) { m.Items[0].ByteSize = -1 },
		"duplicate id":  func(m *manifest) { m.Items = append(m.Items, m.Items[0]) },
		"invalid state": func(m *manifest) { m.Items[0].State = InboxState("mystery") },
		"orphan idempotency": func(m *manifest) {
			m.Idempotency["msg-1"] = newRandomID()
			m.IdempotencyHashes["msg-1"] = strings.Repeat("a", 64)
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := emptyManifest(ProcessRunID())
			m.Items = []InboxItemMeta{base}
			mutate(m)
			if err := validateManifest(m, false); err == nil {
				t.Fatalf("semantic corruption %q was accepted", name)
			}
		})
	}
}

func TestStoreInstancesReloadManifestBeforeMutation(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	_ = os.WriteFile(session, []byte("{}\n"), 0o644)
	first, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	a, err := first.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "from first"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := second.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "from second"}})
	if err != nil {
		t.Fatal(err)
	}
	items := first.Snapshot().Items
	if len(items) != 2 || items[0].ID != a.ItemID || items[1].ID != b.ItemID {
		t.Fatalf("cross-store writes lost or reordered an item: %+v", items)
	}
}

func TestCapacityLimits(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	_ = os.WriteFile(session, []byte("{}\n"), 0o644)
	s, err := Open(session, Limits{MaxItems: 2, MaxItemBytes: 200, MaxTotalBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: strings.Repeat("a", 400)}}); !errors.Is(err, ErrItemTooLarge) {
		t.Fatalf("item too large: %v", err)
	}
	if _, err := s.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "one"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "two"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "three"}}); !errors.Is(err, ErrCapacityItems) {
		t.Fatalf("cap items: %v", err)
	}
}

func TestDeleteThenBlobGone(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	_ = os.WriteFile(session, []byte("{}\n"), 0o644)
	s, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rec, _ := s.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "bye"}})
	if err := s.DeleteItem(rec.ItemID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.SessionInboxDir(session), "blobs", rec.ItemID+".json")); !os.IsNotExist(err) {
		t.Fatalf("blob should be gone, err=%v", err)
	}
}

func TestCrashAfterBlobBeforeManifestLeavesNoValidItem(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	_ = os.WriteFile(session, []byte("{}\n"), 0o644)
	s, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	// Inject crash after blob rename, before manifest commit.
	fileutil.CrashPoint = func(op, path string) {
		if op == "inbox-manifest-write" {
			panic("inject crash before manifest")
		}
	}
	t.Cleanup(func() { fileutil.CrashPoint = nil })
	func() {
		defer func() { _ = recover() }()
		_, _ = s.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "orphan"}})
	}()
	fileutil.CrashPoint = nil
	// Re-open: no valid items; orphan blob may exist and is GC'd/quarantined.
	s2, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if n := len(s2.Snapshot().Items); n != 0 {
		t.Fatalf("want 0 valid items after crash, got %d", n)
	}
}

func TestUpdateCrashPointsPreserveCompleteRevision(t *testing.T) {
	tests := []struct {
		crashOp string
		want    string
	}{
		{crashOp: "inbox-blob-write", want: "old body"},
		{crashOp: "inbox-blob-rename", want: "old body"},
		{crashOp: "inbox-manifest-write", want: "old body"},
		{crashOp: "inbox-manifest-commit", want: "new body"},
	}
	for _, tt := range tests {
		t.Run(tt.crashOp, func(t *testing.T) {
			dir := t.TempDir()
			session := filepath.Join(dir, "s.jsonl")
			s, err := Open(session, Limits{})
			if err != nil {
				t.Fatal(err)
			}
			rec, err := s.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "old body"}})
			if err != nil {
				t.Fatal(err)
			}
			fileutil.CrashPoint = func(op, _ string) {
				if op == tt.crashOp {
					panic("injected update crash")
				}
			}
			func() {
				defer func() { _ = recover() }()
				_, _ = s.UpdateItem(rec.ItemID, PromptEnvelope{SubmitText: "new body"})
			}()
			fileutil.CrashPoint = nil
			s.Close()

			reopened, err := Open(session, Limits{})
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			_, env, err := reopened.ReadItem(rec.ItemID)
			if err != nil {
				t.Fatal(err)
			}
			if env.SubmitText != tt.want {
				t.Fatalf("recovered body = %q, want %q", env.SubmitText, tt.want)
			}
		})
	}
	t.Cleanup(func() { fileutil.CrashPoint = nil })
}

func TestCrossProcessRecoveryPauses(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	_ = os.WriteFile(session, []byte("{}\n"), 0o644)
	s, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := s.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "work"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetState(rec.ItemID, StateRunning, ""); err != nil {
		t.Fatal(err)
	}
	s.Close()

	// Simulate another process by rewriting runID in a fresh Open with different ProcessRunID.
	// Open always uses ProcessRunID(); force recovery by editing manifest runId.
	manPath := filepath.Join(store.SessionInboxDir(session), "manifest.json")
	data, _ := os.ReadFile(manPath)
	data = []byte(strings.Replace(string(data), ProcessRunID(), "other-run-id-0000", 1))
	_ = os.WriteFile(manPath, data, 0o600)

	s2, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	snap := s2.Snapshot()
	if !snap.Paused || !snap.Recovered {
		t.Fatalf("want paused+recovered, got %+v", snap)
	}
	if len(snap.Items) != 1 || snap.Items[0].State != StateUncertain {
		t.Fatalf("want uncertain item, got %+v", snap.Items)
	}
}

func TestPreviewDoesNotMaterializeHugeBody(t *testing.T) {
	huge := strings.Repeat("x", 1<<20)
	p := PreviewText(huge, 40)
	if len(p) > 80 {
		t.Fatalf("preview too long: %d", len(p))
	}
	if !strings.HasSuffix(p, "…") {
		t.Fatalf("want ellipsis, got %q", p)
	}
}

func TestUpdateUsesImmutableBlobRevision(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	_ = os.WriteFile(session, []byte("{}\n"), 0o644)
	s, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rec, err := s.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "original"}})
	if err != nil {
		t.Fatal(err)
	}
	meta, _, err := s.ReadItem(rec.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	oldBlob := blobNameFor(meta)
	updated, err := s.UpdateItem(rec.ItemID, PromptEnvelope{SubmitText: "revised"})
	if err != nil {
		t.Fatal(err)
	}
	if blobNameFor(updated) == oldBlob {
		t.Fatal("update must write a new blob name, not overwrite in place")
	}
	oldPath, err := s.blobPath(oldBlob)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old blob should be removed after successful update, err=%v", err)
	}
	_, env, err := s.ReadItem(rec.ItemID)
	if err != nil || env.SubmitText != "revised" {
		t.Fatalf("read after update = %+v err=%v", env, err)
	}
}

func TestCorruptManifestSalvagesBlobs(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	_ = os.WriteFile(session, []byte("{}\n"), 0o644)
	s, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "keep-me"}}); err != nil {
		t.Fatal(err)
	}
	s.Close()

	// Corrupt the manifest.
	manPath := filepath.Join(store.SessionInboxDir(session), "manifest.json")
	if err := os.WriteFile(manPath, []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	snap := s2.Snapshot()
	if !snap.Paused || !snap.Recovered {
		t.Fatalf("want paused+recovered after corrupt manifest, got %+v", snap)
	}
	if len(snap.Items) == 0 || snap.RecoveredN == 0 {
		t.Fatalf("salvage must surface blobs, got items=%d recoveredN=%d", len(snap.Items), snap.RecoveredN)
	}
}

func TestCorruptManifestSalvageRefusesSymlinkBlob(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges")
	}
	session := filepath.Join(t.TempDir(), "s.jsonl")
	inboxDir := store.SessionInboxDir(session)
	blobsDir := filepath.Join(inboxDir, blobsDirName)
	if err := os.MkdirAll(blobsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "external.json")
	if err := os.WriteFile(target, []byte(`{"submitText":"external secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(blobsDir, newRandomID()+blobSuffix)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inboxDir, manifestName), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := s.Snapshot(); !got.Paused || !got.Recovered || len(got.Items) != 0 {
		t.Fatalf("symlink blob was salvaged: %+v", got)
	}
	data, err := os.ReadFile(target)
	if err != nil || !strings.Contains(string(data), "external secret") {
		t.Fatalf("external symlink target changed: %q err=%v", data, err)
	}
}

func TestEnqueueRefusesSymlinkBlobsDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges")
	}
	session := filepath.Join(t.TempDir(), "s.jsonl")
	s, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(store.SessionInboxDir(session), blobsDirName)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "must stay scoped"}}); err == nil {
		t.Fatal("enqueue accepted a symlink blobs directory")
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("enqueue wrote through symlink: %+v", entries)
	}
}

func TestFreezeRefsRejectsWorkspaceEscape(t *testing.T) {
	ws := t.TempDir()
	refs, err := FreezeRefs(context.Background(), ws, []string{"/etc/passwd"})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 {
		t.Fatalf("want 1 ref, got %d", len(refs))
	}
	if !strings.Contains(string(refs[0].Content), "outside workspace") && !strings.Contains(string(refs[0].Content), "freeze failed") {
		t.Fatalf("want workspace escape rejection, got %q", refs[0].Content)
	}
}

func TestFreezeRefsRejectsSymlinkEscape(t *testing.T) {
	ws := t.TempDir()
	external := t.TempDir()
	secret := filepath.Join(external, "secret.txt")
	if err := os.WriteFile(secret, []byte("outside-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(ws, "linked-secret.txt")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	refs, err := FreezeRefs(context.Background(), ws, []string{"linked-secret.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 {
		t.Fatalf("want one blocked marker, got %d", len(refs))
	}
	if strings.Contains(string(refs[0].Content), "outside-secret") {
		t.Fatal("workspace-local symlink leaked content from outside the workspace")
	}
	if !strings.Contains(string(refs[0].Content), "path escapes workspace") {
		t.Fatalf("want symlink escape rejection, got %q", refs[0].Content)
	}
}

func TestApplyFrozenRefsIsDeterministic(t *testing.T) {
	bodies := map[string]string{
		"z/file.txt": "z-body",
		"a/file.txt": "a-body",
	}
	first := ApplyFrozenRefs("inspect refs", bodies)
	for range 20 {
		if got := ApplyFrozenRefs("inspect refs", bodies); got != first {
			t.Fatalf("frozen reference serialization changed between calls:\n%s\n---\n%s", first, got)
		}
	}
	if strings.Index(first, "@a/file.txt") > strings.Index(first, "@z/file.txt") {
		t.Fatalf("frozen references are not sorted: %q", first)
	}
}

func TestMoveAndPause(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	_ = os.WriteFile(session, []byte("{}\n"), 0o644)
	s, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, _ := s.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "a"}})
	b, _ := s.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "b"}})
	if err := s.MoveItem(b.ItemID, 0); err != nil {
		t.Fatal(err)
	}
	items := s.Snapshot().Items
	if items[0].ID != b.ItemID || items[1].ID != a.ItemID {
		t.Fatalf("order = %v", items)
	}
	if err := s.SetPaused(true); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.NextQueued(); ok {
		t.Fatal("paused inbox must not dispatch")
	}
}

func TestDiscardPendingItemsIsScopedAndAtomic(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	_ = os.WriteFile(session, []byte("{}\n"), 0o644)
	s, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, _ := s.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "a"}})
	b, _ := s.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "b"}})
	c, _ := s.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "c"}})

	if err := s.SetState(b.ItemID, StateRunning, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.DiscardPendingItems([]string{a.ItemID, b.ItemID}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("discard admitted item error = %v, want ErrInvalidState", err)
	}
	if got := len(s.Snapshot().Items); got != 3 {
		t.Fatalf("failed batch discard changed manifest: got %d items", got)
	}

	if err := s.DiscardPendingItems([]string{a.ItemID, "already-consumed"}); err != nil {
		t.Fatal(err)
	}
	items := s.Snapshot().Items
	if len(items) != 2 || items[0].ID != b.ItemID || items[1].ID != c.ItemID {
		t.Fatalf("scoped discard left items = %+v", items)
	}
}

func TestDiscardPendingItemsEnforcesSourceOwnership(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	_ = os.WriteFile(session, []byte("{}\n"), 0o644)
	s, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	desktop, _ := s.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "desktop"}, Source: "desktop"})
	bot, _ := s.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "bot"}, Source: "bot"})
	if err := s.SetState(bot.ItemID, StateRunning, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.DiscardPendingItemsOwned([]string{desktop.ItemID, bot.ItemID}, "desktop"); err != nil {
		t.Fatal(err)
	}
	items := s.Snapshot().Items
	if len(items) != 1 || items[0].ID != bot.ItemID || items[0].State != StateRunning {
		t.Fatalf("source-scoped discard changed foreign work: %+v", items)
	}
}
