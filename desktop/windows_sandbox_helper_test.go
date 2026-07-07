package main

import (
	"testing"

	"reasonix/internal/sandbox"
)

// The Windows bash sandbox relaunches os.Executable() with
// sandbox.WindowsHelperCommand as argv[1]. If the desktop binary loses this
// route, every sandboxed command on Windows starts a second GUI instance and
// returns empty output (#6051, #6067, #6072). These tests pin the route.

func TestWindowsSandboxHelperRouteRecognized(t *testing.T) {
	// argv[2:] is empty, so the helper rejects it with a usage error — the
	// point here is only that the route matched (ok == true) and the process
	// would exit instead of booting the GUI.
	code, ok := runWindowsSandboxHelperIfRequested([]string{"reasonix-desktop", sandbox.WindowsHelperCommand})
	if !ok {
		t.Fatal("helper subcommand not routed; sandboxed commands would boot a second GUI instance")
	}
	if code == 0 {
		t.Fatalf("helper with no payload should fail with a usage error, got exit code %d", code)
	}
}

func TestWindowsSandboxHelperRouteIgnoresNormalLaunch(t *testing.T) {
	for _, argv := range [][]string{
		{"reasonix-desktop"},
		{"reasonix-desktop", "--some-flag"},
		{"reasonix-desktop", "not-the-helper", sandbox.WindowsHelperCommand},
	} {
		if _, ok := runWindowsSandboxHelperIfRequested(argv); ok {
			t.Fatalf("argv %v should not be treated as a helper launch", argv)
		}
	}
}
