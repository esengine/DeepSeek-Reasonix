package main

import (
	"os"
	"testing"
)

func TestUpdateHandoffOwnerPIDWaitsForShellParentInHostMode(t *testing.T) {
	app := &App{}
	if got := app.updateHandoffOwnerPID(); got != os.Getpid() {
		t.Fatalf("Wails owner pid = %d, want %d", got, os.Getpid())
	}
	app.hostShell = &hostShellBridge{app: app}
	if got := app.updateHandoffOwnerPID(); got != os.Getppid() {
		t.Fatalf("host-mode owner pid = %d, want parent %d", got, os.Getppid())
	}
}

func TestHostUpdateHandoffQuitsWithoutSchedulingASecondRestart(t *testing.T) {
	app, _, shell := newHostShellBridgeForTest(t, nil)
	app.relaunchDesktop(false)
	calls := shell.methods()
	if len(calls) != 1 || calls[0] != "host/app.quit" {
		t.Fatalf("helper-owned restart asked shell to %v", calls)
	}
	app.relaunchDesktop(true)
	calls = shell.methods()
	if len(calls) != 2 || calls[1] != "host/app.relaunch" {
		t.Fatalf("shell-owned restart asked shell to %v", calls)
	}
}

func TestWindowsUpdateWaitsForTheShellInsteadOfItsService(t *testing.T) {
	original := os.Args
	t.Cleanup(func() { os.Args = original })
	os.Args = []string{"reasonix-desktop", "--host-rpc"}
	if got := windowsUpdateOwnerPID(); got != os.Getppid() {
		t.Fatalf("host PID=%d want %d", got, os.Getppid())
	}
	os.Args = []string{"reasonix-desktop"}
	if got := windowsUpdateOwnerPID(); got != os.Getpid() {
		t.Fatalf("legacy PID=%d want %d", got, os.Getpid())
	}
}
