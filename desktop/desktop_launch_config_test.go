package main

import "testing"

func TestDesktopWindowFramelessOnlyOnWindows(t *testing.T) {
	cases := []struct {
		goos string
		want bool
	}{
		{goos: "windows", want: true},
		{goos: "darwin", want: false},
		{goos: "linux", want: false},
	}
	for _, tt := range cases {
		if got := desktopWindowFrameless(tt.goos); got != tt.want {
			t.Fatalf("desktopWindowFrameless(%q) = %v, want %v", tt.goos, got, tt.want)
		}
	}
}

func TestInitialDesktopWindowSizeRestoresSavedGeometry(t *testing.T) {
	isolateDesktopUserDirs(t)
	resetLastKnownWindowStateForTest()
	t.Cleanup(resetLastKnownWindowStateForTest)

	app := NewApp()
	saved := DesktopWindowState{Width: 1100, Height: 900, X: 40, Y: 60, Maximised: false}
	if err := app.SaveWindowState(saved); err != nil {
		t.Fatalf("SaveWindowState: %v", err)
	}

	geometry := initialDesktopWindowGeometry()
	w, h := geometry.Width, geometry.Height
	if geometry.Position == nil || geometry.Position.X != saved.X || geometry.Position.Y != saved.Y {
		t.Fatalf("saved position lost: %+v", geometry.Position)
	}
	if w != saved.Width || h != saved.Height {
		t.Fatalf("main window size = %dx%d, want %dx%d", w, h, saved.Width, saved.Height)
	}
}

func TestInitialDesktopWindowSizePreservesMaximisedGeometry(t *testing.T) {
	isolateDesktopUserDirs(t)
	resetLastKnownWindowStateForTest()
	t.Cleanup(resetLastKnownWindowStateForTest)

	app := NewApp()
	saved := DesktopWindowState{Width: 1100, Height: 700, X: 40, Y: 50, Maximised: true}
	if err := app.SaveWindowState(saved); err != nil {
		t.Fatalf("SaveWindowState: %v", err)
	}

	geometry := initialDesktopWindowGeometry()
	w, h := geometry.Width, geometry.Height
	if geometry.Position == nil || geometry.Position.X != saved.X || geometry.Position.Y != saved.Y {
		t.Fatalf("saved position lost: %+v", geometry.Position)
	}
	if w != saved.Width || h != saved.Height {
		t.Fatalf("maximised saved state = %dx%d, want %dx%d", w, h, saved.Width, saved.Height)
	}
}

func TestInitialDesktopWindowSizeFallsBackToDefaults(t *testing.T) {
	isolateDesktopUserDirs(t)
	resetLastKnownWindowStateForTest()
	t.Cleanup(resetLastKnownWindowStateForTest)

	geometry := initialDesktopWindowGeometry()
	w, h := geometry.Width, geometry.Height
	if geometry.Position != nil {
		t.Fatal("missing state must request centering")
	}
	if w != defaultDesktopWindowWidth || h != defaultDesktopWindowHeight {
		t.Fatalf("no saved state = %dx%d, want default %dx%d",
			w, h, defaultDesktopWindowWidth, defaultDesktopWindowHeight)
	}
}
