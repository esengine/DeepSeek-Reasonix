package main

import (
	"testing"

	"github.com/wailsapp/wails/v2/pkg/options"
)

func TestSingleInstanceLockRestoresExistingInstance(t *testing.T) {
	app := NewApp()
	lock := singleInstanceLock(app)

	if lock == nil {
		t.Fatal("singleInstanceLock returned nil")
	}
	if lock.UniqueId != singleInstanceID {
		t.Fatalf("UniqueId = %q, want %q", lock.UniqueId, singleInstanceID)
	}
	if lock.OnSecondInstanceLaunch == nil {
		t.Fatal("OnSecondInstanceLaunch should restore the existing window")
	}

	lock.OnSecondInstanceLaunch(options.SecondInstanceData{})
}
