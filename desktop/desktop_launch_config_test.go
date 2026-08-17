package main

import "testing"

// The remote web child window has no Wails bindings and navigates to the host's
// Serve page, which carries no drag regions or window controls. Frameless there
// leaves a window that cannot be moved, minimised, or restored.
func TestDesktopWindowFramelessExcludesRemoteWindow(t *testing.T) {
	cases := []struct {
		goos         string
		remoteWindow bool
		want         bool
	}{
		{goos: "windows", remoteWindow: false, want: true},
		{goos: "windows", remoteWindow: true, want: false},
		{goos: "darwin", remoteWindow: false, want: false},
		{goos: "darwin", remoteWindow: true, want: false},
		{goos: "linux", remoteWindow: false, want: false},
		{goos: "linux", remoteWindow: true, want: false},
	}
	for _, tt := range cases {
		if got := desktopWindowFrameless(tt.goos, tt.remoteWindow); got != tt.want {
			t.Fatalf("desktopWindowFrameless(%q, %v) = %v, want %v", tt.goos, tt.remoteWindow, got, tt.want)
		}
	}
}

func TestInitialDesktopWindowSizeRestoresSavedGeometryForMainWindow(t *testing.T) {
	isolateDesktopUserDirs(t)
	resetLastKnownWindowStateForTest()
	t.Cleanup(resetLastKnownWindowStateForTest)

	app := NewApp()
	saved := DesktopWindowState{Width: 1100, Height: 900, X: 40, Y: 60, Maximised: false}
	if err := app.SaveWindowState(saved); err != nil {
		t.Fatalf("SaveWindowState: %v", err)
	}

	w, h := initialDesktopWindowSize(false)
	if w != saved.Width || h != saved.Height {
		t.Fatalf("main window size = %dx%d, want %dx%d", w, h, saved.Width, saved.Height)
	}
}

// desktop-window.json holds the main window's outer size, which for a maximised
// window exceeds the display. domReadyRemoteWindow centres the remote window, so
// inheriting that size would push its titlebar off-screen.
func TestInitialDesktopWindowSizeIgnoresSavedGeometryForRemoteWindow(t *testing.T) {
	isolateDesktopUserDirs(t)
	resetLastKnownWindowStateForTest()
	t.Cleanup(resetLastKnownWindowStateForTest)

	app := NewApp()
	maximised := DesktopWindowState{Width: 2574, Height: 1454, X: -11, Y: -11, Maximised: true}
	if err := app.SaveWindowState(maximised); err != nil {
		t.Fatalf("SaveWindowState: %v", err)
	}

	w, h := initialDesktopWindowSize(true)
	if w != defaultDesktopWindowWidth || h != defaultDesktopWindowHeight {
		t.Fatalf("remote window size = %dx%d, want default %dx%d",
			w, h, defaultDesktopWindowWidth, defaultDesktopWindowHeight)
	}
}

func TestInitialDesktopWindowSizeFallsBackToDefaults(t *testing.T) {
	isolateDesktopUserDirs(t)
	resetLastKnownWindowStateForTest()
	t.Cleanup(resetLastKnownWindowStateForTest)

	w, h := initialDesktopWindowSize(false)
	if w != defaultDesktopWindowWidth || h != defaultDesktopWindowHeight {
		t.Fatalf("no saved state = %dx%d, want default %dx%d",
			w, h, defaultDesktopWindowWidth, defaultDesktopWindowHeight)
	}
}
