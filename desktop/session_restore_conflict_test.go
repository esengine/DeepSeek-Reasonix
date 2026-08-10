package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/sessioninbox"
	"reasonix/internal/store"
)

func TestRestoreTrashedSessionFileRejectsActiveInboxConflict(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(sessionPath, []byte("trash data"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldReceipt := enqueueSessionInboxForRestoreTest(t, sessionPath, "old queued work")
	if err := deleteSessionFile(dir, sessionPath); err != nil {
		t.Fatalf("trash: %v", err)
	}

	trashPath := filepath.Join(dir, sessionTrashDir, filepath.Base(sessionPath), filepath.Base(sessionPath))
	if err := os.WriteFile(sessionPath, nil, 0o644); err != nil {
		t.Fatalf("write live stub: %v", err)
	}
	activeReceipt := enqueueSessionInboxForRestoreTest(t, sessionPath, "active queued work")

	err := restoreTrashedSessionFile(dir, trashPath)
	if err == nil || !strings.Contains(err.Error(), "session artifact already exists: session.inbox") {
		t.Fatalf("restore conflict error = %v, want active inbox conflict", err)
	}
	if info, err := os.Stat(sessionPath); err != nil || info.Size() != 0 {
		t.Fatalf("active transcript stub should remain empty, info=%v err=%v", info, err)
	}
	assertSessionInboxItemForRestoreTest(t, sessionPath, activeReceipt.ItemID, "active queued work")
	if _, err := os.Stat(trashPath); err != nil {
		t.Fatalf("trash transcript should remain after rejected restore: %v", err)
	}
	assertSessionInboxItemForRestoreTest(t, trashPath, oldReceipt.ItemID, "old queued work")
}

func TestRestoreTrashedSessionFileRejectsOrphanSidecarConflict(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "orphan-sidecar.jsonl")
	if err := os.WriteFile(sessionPath, []byte("trash data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := deleteSessionFile(dir, sessionPath); err != nil {
		t.Fatalf("trash: %v", err)
	}
	trashPath := filepath.Join(dir, sessionTrashDir, filepath.Base(sessionPath), filepath.Base(sessionPath))
	if err := os.WriteFile(sessionPath, nil, 0o644); err != nil {
		t.Fatalf("write live stub: %v", err)
	}
	metaPath := store.SessionMeta(sessionPath)
	if err := os.WriteFile(metaPath, []byte("active metadata"), 0o600); err != nil {
		t.Fatalf("write active sidecar: %v", err)
	}

	err := restoreTrashedSessionFile(dir, trashPath)
	if err == nil || !strings.Contains(err.Error(), filepath.Base(metaPath)) {
		t.Fatalf("restore conflict error = %v, want orphan sidecar conflict", err)
	}
	if got, err := os.ReadFile(metaPath); err != nil || string(got) != "active metadata" {
		t.Fatalf("active sidecar = %q, %v; want preserved metadata", got, err)
	}
	if _, err := os.Stat(sessionPath); err != nil {
		t.Fatalf("active transcript stub should remain: %v", err)
	}
	if _, err := os.Stat(trashPath); err != nil {
		t.Fatalf("trash transcript should remain after rejected restore: %v", err)
	}
}

func TestRestoreTrashedSessionFilePreservesStubOnSubagentConflict(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "subagent-conflict.jsonl")
	if err := os.WriteFile(sessionPath, []byte("trash data"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := "sa_20260102_030405_000000000_aabbccddeeff"
	writeSubagentArtifact(t, dir, ref, agent.BranchID(sessionPath))
	if err := deleteSessionFile(dir, sessionPath); err != nil {
		t.Fatalf("trash: %v", err)
	}
	trashPath := filepath.Join(dir, sessionTrashDir, filepath.Base(sessionPath), filepath.Base(sessionPath))
	if err := os.WriteFile(sessionPath, nil, 0o644); err != nil {
		t.Fatalf("write live stub: %v", err)
	}
	activeSubagent := filepath.Join(dir, "subagents", ref+".jsonl")
	if err := os.MkdirAll(filepath.Dir(activeSubagent), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(activeSubagent, []byte("active subagent"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := restoreTrashedSessionFile(dir, trashPath); err == nil {
		t.Fatal("restore should reject an active subagent conflict")
	}
	if _, err := os.Stat(sessionPath); err != nil {
		t.Fatalf("active transcript stub should survive preflight failure: %v", err)
	}
	if got, err := os.ReadFile(activeSubagent); err != nil || string(got) != "active subagent" {
		t.Fatalf("active subagent = %q, %v; want preserved data", got, err)
	}
	if _, err := os.Stat(trashPath); err != nil {
		t.Fatalf("trash transcript should remain after rejected restore: %v", err)
	}
}

func enqueueSessionInboxForRestoreTest(t *testing.T, sessionPath, text string) sessioninbox.InboxReceipt {
	t.Helper()
	inbox, err := sessioninbox.Open(sessionPath, sessioninbox.Limits{})
	if err != nil {
		t.Fatalf("open inbox: %v", err)
	}
	receipt, err := inbox.Enqueue(sessioninbox.EnqueueRequest{
		Envelope: sessioninbox.PromptEnvelope{DisplayText: text, SubmitText: text},
	})
	inbox.Close()
	if err != nil {
		t.Fatalf("enqueue inbox item: %v", err)
	}
	return receipt
}

func assertSessionInboxItemForRestoreTest(t *testing.T, sessionPath, itemID, want string) {
	t.Helper()
	inbox, err := sessioninbox.Open(sessionPath, sessioninbox.Limits{})
	if err != nil {
		t.Fatalf("reopen inbox: %v", err)
	}
	defer inbox.Close()
	_, envelope, err := inbox.ReadItem(itemID)
	if err != nil {
		t.Fatalf("read inbox item %s: %v", itemID, err)
	}
	if envelope.SubmitText != want {
		t.Fatalf("inbox item %s = %q, want %q", itemID, envelope.SubmitText, want)
	}
}
