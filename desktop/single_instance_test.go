package main

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/options"
)

func TestSingleInstanceIDUsesProfileHome(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "reasonix-desktop")
	legacy := sha256.Sum256([]byte(executable))
	wantLegacy := singleInstanceIDPrefix + "." + hex.EncodeToString(legacy[:8])
	if got := singleInstanceIDFor(executable, ""); got != wantLegacy {
		t.Fatalf("default ID = %q, want legacy ID %q", got, wantLegacy)
	}

	base := t.TempDir()
	homeA := filepath.Join(base, "a")
	homeB := filepath.Join(base, "b")
	idA := singleInstanceIDFor(executable, homeA)
	if idA != singleInstanceIDFor(executable, filepath.Join(base, ".", "a")) {
		t.Fatal("equivalent home paths produced different IDs")
	}
	if idA == singleInstanceIDFor(executable, homeB) {
		t.Fatal("different homes produced the same ID")
	}
}

func TestSingleInstanceLockRestoresExistingInstance(t *testing.T) {
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

func TestSingleInstanceLockSkipsInDevMode(t *testing.T) {
	t.Setenv("REASONIX_DEV", "1")
	if lock := singleInstanceLock(NewApp()); lock != nil {
		t.Fatalf("singleInstanceLock returned %#v, want nil in dev mode", lock)
	}
}
