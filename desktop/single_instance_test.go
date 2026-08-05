package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/options"
)

func TestSingleInstanceLockRestoresExistingInstance(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	app := NewApp()
	lock := singleInstanceLock(app)

	if lock == nil {
		t.Fatal("singleInstanceLock returned nil")
	}
	id := singleInstanceID()
	if lock.UniqueId != id {
		t.Fatalf("UniqueId = %q, want %q", lock.UniqueId, id)
	}
	if !strings.HasPrefix(lock.UniqueId, singleInstanceIDPrefix+".") {
		t.Fatalf("UniqueId = %q, want prefix %s.", lock.UniqueId, singleInstanceIDPrefix)
	}
	if lock.OnSecondInstanceLaunch == nil {
		t.Fatal("OnSecondInstanceLaunch should restore the existing window")
	}

	lock.OnSecondInstanceLaunch(options.SecondInstanceData{})
}

func TestSingleInstanceIDScopesToReasonixHome(t *testing.T) {
	first := filepath.Join(t.TempDir(), "first")
	second := filepath.Join(t.TempDir(), "second")
	t.Setenv("REASONIX_HOME", first)
	firstID := singleInstanceID()
	t.Setenv("REASONIX_HOME", filepath.Join(first, "."))
	if got := singleInstanceID(); got != firstID {
		t.Fatalf("same data home produced different ids: %q != %q", got, firstID)
	}
	t.Setenv("REASONIX_HOME", second)
	if got := singleInstanceID(); got == firstID {
		t.Fatalf("different data homes produced the same id %q", got)
	}
}

func TestSingleInstanceIDDoesNotSplitReleaseChannels(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	oldChannel := channel
	t.Cleanup(func() { channel = oldChannel })
	channel = "stable"
	stableID := singleInstanceID()
	channel = "canary"
	if got := singleInstanceID(); got != stableID {
		t.Fatalf("same data home split by channel: stable=%q canary=%q", stableID, got)
	}
}

func TestSingleInstanceIDResolvesMissingHomeThroughSymlink(t *testing.T) {
	root := t.TempDir()
	realParent := filepath.Join(root, "real")
	if err := os.MkdirAll(realParent, 0o755); err != nil {
		t.Fatal(err)
	}
	aliasParent := filepath.Join(root, "alias")
	if err := os.Symlink(realParent, aliasParent); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	t.Setenv("REASONIX_HOME", filepath.Join(realParent, "not-created", "home"))
	realID := singleInstanceID()
	t.Setenv("REASONIX_HOME", filepath.Join(aliasParent, "not-created", "home"))
	if got := singleInstanceID(); got != realID {
		t.Fatalf("aliased missing data home produced different ids: %q != %q", got, realID)
	}
}

func TestSingleInstanceLockSkipsInDevMode(t *testing.T) {
	t.Setenv("REASONIX_DEV", "1")
	if lock := singleInstanceLock(NewApp()); lock != nil {
		t.Fatalf("singleInstanceLock returned %#v, want nil in dev mode", lock)
	}
}
