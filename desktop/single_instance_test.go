package main

import (
	"strings"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/options"
)

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
