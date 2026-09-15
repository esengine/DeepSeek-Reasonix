package session

import (
	"path/filepath"
	"testing"
)

func TestQueryStatReturnsHeaderBackedIdentityWithoutReadingHistory(t *testing.T) {
	service, err := NewService("local", NewFilesystemPersistence(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Shutdown(t.Context()) })
	runtime, err := service.Create(t.Context(), CreateOptions{
		SessionID: "header-stat", CWD: "/workspace", Origin: SessionOriginNew,
	})
	if err != nil {
		t.Fatal(err)
	}

	info, err := service.Query().Stat(t.Context(), runtime.Ref())
	if err != nil {
		t.Fatal(err)
	}
	// Headers record CWD in OS-native form, so the expectation is cleaned the
	// same way the writer cleans it instead of assuming POSIX separators.
	if info.Ref != runtime.Ref() || info.CWD != filepath.Clean("/workspace") || info.Origin != SessionOriginNew {
		t.Fatalf("stat = %#v", info)
	}
}
