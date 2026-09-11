package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/config"
)

func TestRemotePrefsFailedSaveDoesNotPublishCache(t *testing.T) {
	root := t.TempDir()
	blocked := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(blocked, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REASONIX_STATE_HOME", blocked)

	if err := setRemoteSessionTitleOverride("box", "~/app", "session", "unsaved"); err == nil {
		t.Fatal("setRemoteSessionTitleOverride unexpectedly succeeded")
	}
	if got := remoteSessionTitleOverride("box", "~/app", "session"); got != "" {
		t.Fatalf("failed save published cached title %q", got)
	}
}

func TestRemotePrefsMutationPreservesExternalWrites(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REASONIX_STATE_HOME", root)
	if err := setRemoteSessionTitleOverride("box", "~/app", "session-a", "Local title"); err != nil {
		t.Fatal(err)
	}
	// Exercise the ordinary read path before another desktop instance replaces
	// the file with a newer snapshot.
	if got := remoteSessionTitleOverride("box", "~/app", "session-a"); got != "Local title" {
		t.Fatalf("warm title = %q", got)
	}

	path := remotePrefsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var external remotePrefs
	if err := json.Unmarshal(data, &external); err != nil {
		t.Fatal(err)
	}
	external.LastHostID = "external-host"
	external.SessionTitles[remoteSessionPrefKey("box", "~/app", "session-b")] = "External title"
	data, err = json.Marshal(external)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := config.LockConfigFileEdits(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		unlock()
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		result <- setRemoteSessionPinned("box", "~/app", "session-a", true)
	}()
	select {
	case err := <-result:
		unlock()
		t.Fatalf("remote prefs mutation bypassed the cross-process lock: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	unlock()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	got := loadRemotePrefs()
	if got.LastHostID != "external-host" || got.SessionTitles[remoteSessionPrefKey("box", "~/app", "session-b")] != "External title" {
		t.Fatalf("mutation overwrote external fields: %+v", got)
	}
}
