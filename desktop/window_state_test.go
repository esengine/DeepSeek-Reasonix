package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestValidateWindowStateAcceptsWindowsBorderOrigin(t *testing.T) {
	state := DesktopWindowState{Width: 1240, Height: 720, X: -8, Y: -8, Maximised: false}
	if err := validateWindowState(state); err != nil {
		t.Fatalf("x=-8,y=-8 must be accepted: %v", err)
	}
}

func TestValidateWindowStateRejectsIllegalSizes(t *testing.T) {
	cases := []DesktopWindowState{
		{Width: 0, Height: 720, X: 10, Y: 10},
		{Width: 100, Height: 720, X: 10, Y: 10},
		{Width: 1240, Height: 100, X: 10, Y: 10},
		{Width: maxWindowDimension + 1, Height: 720, X: 10, Y: 10},
		{Width: 1240, Height: 720, X: minWindowOrigin - 1, Y: 10},
		{Width: 1240, Height: 720, X: 10, Y: maxWindowOriginAbs + 1},
	}
	for _, c := range cases {
		if err := validateWindowState(c); err == nil {
			t.Fatalf("expected rejection for %+v", c)
		}
	}
}

func TestParseWindowStateJSONCorruptPayload(t *testing.T) {
	if _, err := parseWindowStateJSON([]byte(`{not-json`)); err == nil {
		t.Fatal("corrupt JSON must fail")
	}
	if _, err := parseWindowStateJSON([]byte(`{"width":50,"height":50,"x":0,"y":0}`)); err == nil {
		t.Fatal("undersized geometry must fail")
	}
}

func TestWindowPositionRestorableAllowsBorderAndRejectsOffscreen(t *testing.T) {
	border := DesktopWindowState{Width: 1240, Height: 720, X: -8, Y: -8}
	if !windowPositionRestorable(border, 1920, 1080) {
		t.Fatal("border origin should restore")
	}
	offscreen := DesktopWindowState{Width: 1240, Height: 720, X: 10_000, Y: 40}
	if windowPositionRestorable(offscreen, 1920, 1080) {
		t.Fatal("far off-screen origin should not restore")
	}
	tooNegative := DesktopWindowState{Width: 1240, Height: 720, X: -200, Y: 40}
	if windowPositionRestorable(tooNegative, 1920, 1080) {
		t.Fatal("deeply negative origin should not restore")
	}
}

func TestSaveWindowStatePersistsAndSeedsLastKnown(t *testing.T) {
	isolateDesktopUserDirs(t)
	resetLastKnownWindowStateForTest()
	t.Cleanup(resetLastKnownWindowStateForTest)

	app := NewApp()
	want := DesktopWindowState{Width: 1100, Height: 700, X: -8, Y: -8, Maximised: true}
	if err := app.SaveWindowState(want); err != nil {
		t.Fatalf("SaveWindowState: %v", err)
	}

	got, ok := loadWindowState()
	if !ok {
		t.Fatal("loadWindowState failed after save")
	}
	if got != want {
		t.Fatalf("loaded state = %+v, want %+v", got, want)
	}
	last, ok := lastKnownWindowState()
	if !ok || last != want {
		t.Fatalf("last known = %+v ok=%v, want %+v", last, ok, want)
	}
}

func TestSaveWindowStateRejectsInvalidWithoutMutatingLastKnown(t *testing.T) {
	isolateDesktopUserDirs(t)
	resetLastKnownWindowStateForTest()
	t.Cleanup(resetLastKnownWindowStateForTest)

	app := NewApp()
	good := DesktopWindowState{Width: 1000, Height: 600, X: 12, Y: 24, Maximised: false}
	if err := app.SaveWindowState(good); err != nil {
		t.Fatal(err)
	}
	if err := app.SaveWindowState(DesktopWindowState{Width: 10, Height: 10, X: 0, Y: 0}); err == nil {
		t.Fatal("expected invalid size rejection")
	}
	last, ok := lastKnownWindowState()
	if !ok || last != good {
		t.Fatalf("last known mutated to %+v ok=%v", last, ok)
	}
	// Disk must still hold the last valid report.
	got, ok := loadWindowState()
	if !ok || got != good {
		t.Fatalf("disk state = %+v ok=%v, want %+v", got, ok, good)
	}
}

func TestSaveWindowStateSyncUsesLastKnownWithoutNativeQuery(t *testing.T) {
	// Regression: shutdown/beforeClose must not call WindowGetSize/Position/
	// IsMaximised. saveWindowStateSync only re-writes the last frontend report.
	isolateDesktopUserDirs(t)
	resetLastKnownWindowStateForTest()
	t.Cleanup(resetLastKnownWindowStateForTest)

	app := NewApp()
	// Simulate a frontend report that was accepted in-memory but not yet on disk
	// (e.g. crash between remember and write, or deleted file).
	want := DesktopWindowState{Width: 1280, Height: 800, X: 40, Y: 50, Maximised: false}
	rememberWindowState(want)
	_ = os.Remove(windowStatePath())

	app.saveWindowStateSync()

	raw, err := os.ReadFile(windowStatePath())
	if err != nil {
		t.Fatalf("expected re-persist: %v", err)
	}
	var got DesktopWindowState
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("re-persisted = %+v, want %+v", got, want)
	}
}

func TestSaveWindowStateSyncNoopWithoutLastKnown(t *testing.T) {
	isolateDesktopUserDirs(t)
	resetLastKnownWindowStateForTest()
	t.Cleanup(resetLastKnownWindowStateForTest)

	app := NewApp()
	app.saveWindowStateSync() // must not panic or create a file
	if _, err := os.Stat(windowStatePath()); !os.IsNotExist(err) {
		t.Fatalf("expected no file, err=%v", err)
	}
}

func TestLoadWindowStateCorruptJSON(t *testing.T) {
	isolateDesktopUserDirs(t)
	resetLastKnownWindowStateForTest()
	t.Cleanup(resetLastKnownWindowStateForTest)

	path := windowStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadWindowState(); ok {
		t.Fatal("corrupt JSON must not restore")
	}
}

func TestBackgroundHideUsesLastKnownMaximised(t *testing.T) {
	isolateDesktopUserDirs(t)
	resetLastKnownWindowStateForTest()
	t.Cleanup(resetLastKnownWindowStateForTest)

	app := NewApp()
	if app.lastKnownMaximised() {
		t.Fatal("default maximised should be false")
	}
	if err := app.SaveWindowState(DesktopWindowState{Width: 900, Height: 600, X: 0, Y: 0, Maximised: true}); err != nil {
		t.Fatal(err)
	}
	if !app.lastKnownMaximised() {
		t.Fatal("expected maximised from last report")
	}
}

func TestSaveWindowStateConcurrentReports(t *testing.T) {
	// Shutdown race: concurrent frontend polls + saveWindowStateSync must not
	// panic or leave invalid JSON.
	isolateDesktopUserDirs(t)
	resetLastKnownWindowStateForTest()
	t.Cleanup(resetLastKnownWindowStateForTest)

	app := NewApp()
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			state := DesktopWindowState{
				Width:  800 + i,
				Height: 600 + i,
				X:      -8 + i%3,
				Y:      -8 + i%5,
			}
			_ = app.SaveWindowState(state)
			app.saveWindowStateSync()
		}(i)
	}
	wg.Wait()

	got, ok := loadWindowState()
	if !ok {
		t.Fatal("final load failed after concurrent writes")
	}
	if err := validateWindowState(got); err != nil {
		t.Fatalf("final state invalid: %v (%+v)", err, got)
	}
}

func TestSaveWindowStateDPIZeroShutdownPathNeverQueriesNative(t *testing.T) {
	// Contract test: saveWindowStateSync must succeed with ctx nil (no Wails
	// runtime) and never require native DPI/window APIs. This is the regression
	// guard for ScaleToDefaultDPI panics during Windows shutdown.
	isolateDesktopUserDirs(t)
	resetLastKnownWindowStateForTest()
	t.Cleanup(resetLastKnownWindowStateForTest)

	app := NewApp()
	app.ctx = nil
	if err := app.SaveWindowState(DesktopWindowState{Width: 1024, Height: 768, X: -8, Y: -8}); err != nil {
		t.Fatal(err)
	}
	// Wipe disk and re-persist from memory only.
	_ = os.Remove(windowStatePath())
	app.saveWindowStateSync()
	got, ok := loadWindowState()
	if !ok {
		t.Fatal("expected re-persisted state without native queries")
	}
	if got.X != -8 || got.Y != -8 {
		t.Fatalf("border origin lost: %+v", got)
	}
}
