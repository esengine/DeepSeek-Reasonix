package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"
)

func TestFilesystemPersistenceCreatesImmutableSessionHeader(t *testing.T) {
	root := t.TempDir()
	persistence := NewFilesystemPersistence(root)
	session, err := persistence.Create(CreateOptions{
		SessionID:       "child-session",
		CWD:             filepath.Join(root, "workspace"),
		ParentSessionID: "parent-session",
		Origin:          SessionOriginFork,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := session.Close(t.Context()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	headerPath := filepath.Join(root, "child-session", sessionHeaderName)
	body, err := os.ReadFile(headerPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", headerPath, err)
	}
	var header SessionHeader
	if err := json.Unmarshal(body, &header); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if header.SchemaVersion != SessionHeaderSchemaVersion || header.SessionID != "child-session" ||
		header.CWD != filepath.Join(root, "workspace") || header.ParentSessionID != "parent-session" ||
		header.Origin != SessionOriginFork || header.CreatedAt.IsZero() {
		t.Fatalf("header = %#v", header)
	}
	// Windows has no POSIX permission bits and always reports 0666 for a
	// regular file, so assert the owner-only mode only where the OS can carry it.
	if info, err := os.Stat(headerPath); err != nil {
		t.Fatalf("Stat: %v", err)
	} else if goruntime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("header mode = %o, want 600", info.Mode().Perm())
	}

	info, err := persistence.Stat(t.Context(), "child-session")
	if err != nil {
		t.Fatalf("Stat session: %v", err)
	}
	if info.CWD != header.CWD || info.ParentSessionID != header.ParentSessionID || info.Origin != header.Origin {
		t.Fatalf("SessionInfo = %#v", info)
	}
}

func TestFilesystemPersistenceRejectsUnknownSessionHeaderVersion(t *testing.T) {
	root := t.TempDir()
	persistence := NewFilesystemPersistence(root)
	session, err := persistence.Create(CreateOptions{SessionID: "future", CWD: root, Origin: SessionOriginNew})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := session.Close(t.Context()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	headerPath := filepath.Join(root, "future", sessionHeaderName)
	if err := os.WriteFile(headerPath, []byte(`{"schemaVersion":99,"sessionId":"future"}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := persistence.Stat(t.Context(), "future"); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("Stat err = %v, want ErrUnsupportedVersion", err)
	}
	if _, err := persistence.Open("future", ReadOnly); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("Open err = %v, want ErrUnsupportedVersion", err)
	}
}

func TestLegacySessionWithoutHeaderRemainsReadable(t *testing.T) {
	root := t.TempDir()
	persistence := NewFilesystemPersistence(root)
	session, err := persistence.Create(CreateOptions{SessionID: "legacy-compatible"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := session.Close(t.Context()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "legacy-compatible", sessionHeaderName)); !os.IsNotExist(err) {
		t.Fatalf("legacy header stat err = %v, want not exist", err)
	}
	if _, err := persistence.Open("legacy-compatible", ReadOnly); err != nil {
		t.Fatalf("Open legacy-compatible: %v", err)
	}
}
