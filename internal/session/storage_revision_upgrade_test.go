package session

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Seed only revision-1 events, then label the fixture with its original
// revision. Do not use this helper to downgrade real session data.
func revisionOneFixture(t *testing.T) (string, string, string, []byte, []byte) {
	t.Helper()
	root, id := filepath.Join(t.TempDir(), "sessions"), "revision-one"
	persistence := NewFilesystemPersistence(root)
	writer, err := persistence.Create(CreateOptions{SessionID: id})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Append(t.Context(), Batch{OperationID: "old-message", Events: []Event{{Kind: "message/complete", Payload: []byte(`{"message":{"id":"old","role":"user","content":"preserve original bytes"}}`)}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, id)
	manifestPath := filepath.Join(dir, "manifest.json")
	manifest, err := readStoredManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest.StorageRevision = 1
	if err := writeManifestFile(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	logBytes, err := os.ReadFile(filepath.Join(dir, currentLogName))
	if err != nil {
		t.Fatal(err)
	}
	return root, id, dir, manifestBytes, logBytes
}

func assertRevisionFixtureBytes(t *testing.T, dir string, manifestBytes, logBytes []byte) {
	t.Helper()
	for name, want := range map[string][]byte{"manifest.json": manifestBytes, currentLogName: logBytes} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s bytes changed", name)
		}
	}
}

func TestStorageRevisionReadOnlyDoesNotUpgrade(t *testing.T) {
	root, id, dir, manifestBytes, logBytes := revisionOneFixture(t)
	persistence := NewFilesystemPersistence(root)
	reader, err := persistence.Open(id, ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Read(t.Context(), 0, 10); err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertRevisionFixtureBytes(t, dir, manifestBytes, logBytes)
}

func TestStorageRevisionWriterUpgradesWithoutRewritingLog(t *testing.T) {
	root, id, dir, _, logBytes := revisionOneFixture(t)
	writer, err := NewFilesystemPersistence(root).Open(id, ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	manifest, err := readStoredManifest(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.StorageRevision != StorageRevision {
		t.Fatalf("revision=%d, want=%d", manifest.StorageRevision, StorageRevision)
	}
	got, err := os.ReadFile(filepath.Join(dir, currentLogName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, logBytes) {
		t.Fatal("upgrade rewrote original event bytes")
	}
}

func TestStorageRevisionDamagedLogDoesNotUpgrade(t *testing.T) {
	root, id, dir, manifestBytes, logBytes := revisionOneFixture(t)
	logBytes[0] ^= 0xff
	if err := os.WriteFile(filepath.Join(dir, currentLogName), logBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	writer, err := NewFilesystemPersistence(root).Open(id, ReadWrite)
	if err == nil {
		_ = writer.Close(context.Background())
		t.Fatal("damaged log was opened for writing")
	}
	assertRevisionFixtureBytes(t, dir, manifestBytes, logBytes)
}

func TestStorageRevisionExternalLeasePreventsUpgrade(t *testing.T) {
	root, id, dir, manifestBytes, logBytes := revisionOneFixture(t)
	release, err := acquireSessionWriter(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	writer, err := NewFilesystemPersistence(root).Open(id, ReadWrite)
	if writer != nil {
		_ = writer.Close(context.Background())
	}
	if !errors.Is(err, ErrWriterOwned) {
		t.Fatalf("open while owned: %v", err)
	}
	assertRevisionFixtureBytes(t, dir, manifestBytes, logBytes)
}

func TestPreviousReaderRejectsStorageRevisionThree(t *testing.T) {
	const previousMaxStorageRevision = 2
	if StorageRevision <= previousMaxStorageRevision {
		t.Fatalf("StorageRevision=%d is not newer than previous reader max %d", StorageRevision, previousMaxStorageRevision)
	}
	root, id, dir, _, _ := revisionOneFixture(t)
	writer, err := NewFilesystemPersistence(root).Open(id, ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	manifest, err := readStoredManifest(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.StorageRevision != StorageRevision {
		t.Fatalf("revision=%d, want=%d", manifest.StorageRevision, StorageRevision)
	}
	previousAccepts := manifest.SchemaVersion == SchemaVersion && manifest.Codec == Codec &&
		manifest.StorageRevision >= 1 && manifest.StorageRevision <= previousMaxStorageRevision
	if previousAccepts {
		t.Fatal("previous reader predicate accepted StorageRevision 3")
	}
}

func TestStorageRevisionUnknownRejectedByReadersAndWriters(t *testing.T) {
	for _, mode := range []AccessMode{ReadOnly, ReadWrite} {
		root, id, dir, _, logBytes := revisionOneFixture(t)
		manifestPath := filepath.Join(dir, "manifest.json")
		manifest, err := readStoredManifest(manifestPath)
		if err != nil {
			t.Fatal(err)
		}
		manifest.StorageRevision = 999
		if err := writeManifestFile(manifestPath, manifest); err != nil {
			t.Fatal(err)
		}
		manifestBytes, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatal(err)
		}
		handle, err := NewFilesystemPersistence(root).Open(id, mode)
		if handle != nil {
			_ = handle.Close(context.Background())
		}
		if !errors.Is(err, ErrUnsupportedVersion) {
			t.Fatalf("mode=%v unknown revision: %v", mode, err)
		}
		assertRevisionFixtureBytes(t, dir, manifestBytes, logBytes)
	}
}

func TestStorageRevisionManifestPublishFailureReleasesWriter(t *testing.T) {
	_, id, dir, manifestBytes, logBytes := revisionOneFixture(t)
	manifestPath := filepath.Join(dir, "manifest.json")
	backupPath := filepath.Join(dir, "manifest.before-failure.json")
	// ObserveRecovery is called after validation and before manifest publication.
	// A directory at the rename destination deterministically rejects the atomic
	// publish on every platform, without timing or permission assumptions.
	writer, err := OpenWithOptions(dir, id, OpenOptions{ObserveRecovery: func(RecoveryOpenStats) {
		if err := os.Rename(manifestPath, backupPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(manifestPath, 0o700); err != nil {
			t.Fatal(err)
		}
	}})
	if writer != nil {
		_ = writer.Close(context.Background())
	}
	if err == nil {
		t.Fatal("manifest publish unexpectedly succeeded")
	}
	got, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, manifestBytes) {
		t.Fatal("failed upgrade changed old manifest bytes")
	}
	got, err = os.ReadFile(filepath.Join(dir, currentLogName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, logBytes) {
		t.Fatal("failed upgrade changed log bytes")
	}
	if err := os.Remove(manifestPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backupPath, manifestPath); err != nil {
		t.Fatal(err)
	}
	// The failed open must release its lease, so a retry can finish the upgrade.
	retry, err := Open(dir, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := retry.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	manifest, err := readStoredManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.StorageRevision != StorageRevision {
		t.Fatalf("retry revision=%d, want=%d", manifest.StorageRevision, StorageRevision)
	}
}
