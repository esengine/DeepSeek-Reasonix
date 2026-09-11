package main

import (
	"context"
	"fmt"
	goruntime "runtime"
	"slices"
	"sync"
	"testing"
)

// recordingNativeHost captures host calls in order; onCall observes App state
// at call time so ordering claims can be asserted.
type recordingNativeHost struct {
	mu         sync.Mutex
	calls      []string
	dialogPath string
	screens    []nativeScreen
	minimised  bool
	onCall     func(name string)
}

func (h *recordingNativeHost) record(name string) {
	h.mu.Lock()
	h.calls = append(h.calls, name)
	hook := h.onCall
	h.mu.Unlock()
	if hook != nil {
		hook(name)
	}
}

func (h *recordingNativeHost) callNames() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.calls)
}

func (h *recordingNativeHost) ShowWindow(context.Context)           { h.record("ShowWindow") }
func (h *recordingNativeHost) ShowApplication(context.Context)      { h.record("ShowApplication") }
func (h *recordingNativeHost) HideWindow(context.Context)           { h.record("HideWindow") }
func (h *recordingNativeHost) HideApplication(context.Context)      { h.record("HideApplication") }
func (h *recordingNativeHost) MaximiseWindow(context.Context)       { h.record("MaximiseWindow") }
func (h *recordingNativeHost) UnmaximiseWindow(context.Context)     { h.record("UnmaximiseWindow") }
func (h *recordingNativeHost) MinimiseWindow(context.Context)       { h.record("MinimiseWindow") }
func (h *recordingNativeHost) UnminimiseWindow(context.Context)     { h.record("UnminimiseWindow") }
func (h *recordingNativeHost) ToggleMaximiseWindow(context.Context) { h.record("ToggleMaximiseWindow") }
func (h *recordingNativeHost) CenterWindow(context.Context)         { h.record("CenterWindow") }
func (h *recordingNativeHost) WindowIsMaximised(context.Context) bool {
	h.record("WindowIsMaximised")
	return false
}
func (h *recordingNativeHost) Quit(context.Context)         { h.record("Quit") }
func (h *recordingNativeHost) OpenDevTools(context.Context) { h.record("OpenDevTools") }

func (h *recordingNativeHost) WindowIsMinimised(context.Context) bool {
	h.record("WindowIsMinimised")
	return h.minimised
}

func (h *recordingNativeHost) SetWindowPosition(_ context.Context, x, y int) {
	h.record(fmt.Sprintf("SetWindowPosition(%d,%d)", x, y))
}

func (h *recordingNativeHost) SetWindowTitle(_ context.Context, title string) {
	h.record("SetWindowTitle:" + title)
}

func (h *recordingNativeHost) Screens(context.Context) ([]nativeScreen, error) {
	h.record("Screens")
	return h.screens, nil
}

func (h *recordingNativeHost) OpenDirectoryDialog(_ context.Context, opts nativeDialogOptions) (string, error) {
	h.record("OpenDirectoryDialog:" + opts.Title)
	return h.dialogPath, nil
}

func (h *recordingNativeHost) OpenFileDialog(_ context.Context, opts nativeDialogOptions) (string, error) {
	h.record("OpenFileDialog:" + opts.Title)
	return h.dialogPath, nil
}

func (h *recordingNativeHost) SaveFileDialog(_ context.Context, opts nativeDialogOptions) (string, error) {
	h.record("SaveFileDialog:" + opts.Title)
	return h.dialogPath, nil
}

func (h *recordingNativeHost) MessageDialog(_ context.Context, opts nativeMessageOptions) (string, error) {
	h.record("MessageDialog:" + opts.Title)
	return opts.DefaultButton, nil
}

func (h *recordingNativeHost) OpenExternal(_ context.Context, url string) {
	h.record("OpenExternal:" + url)
}

func newRecordingHostApp(t *testing.T) (*App, *recordingNativeHost) {
	t.Helper()
	app := NewApp()
	app.ctx = context.Background()
	host := &recordingNativeHost{}
	app.setNativeHost(host)
	return app, host
}

func assertHostCalls(t *testing.T, host *recordingNativeHost, want ...string) {
	t.Helper()
	if got := host.callNames(); !slices.Equal(got, want) {
		t.Fatalf("host calls = %q, want %q", got, want)
	}
}

func TestBareAppUsesNoopHost(t *testing.T) {
	if _, ok := NewApp().nativeHost().(noopNativeHost); !ok {
		t.Fatal("a fresh App must fall back to the no-op host until the shell attaches")
	}
	if _, ok := (&App{}).nativeHost().(noopNativeHost); !ok {
		t.Fatal("an App without a host must fall back to the no-op host")
	}
	var nilApp *App
	if _, ok := nilApp.nativeHost().(noopNativeHost); !ok {
		t.Fatal("a nil App must fall back to the no-op host")
	}
}

func TestQuitAppSetsForceQuitBeforeHostQuit(t *testing.T) {
	app, host := newRecordingHostApp(t)
	forceQuitAtQuit := false
	host.onCall = func(name string) {
		if name == "Quit" {
			forceQuitAtQuit = app.forceQuit.Load()
		}
	}
	app.quitApp()
	assertHostCalls(t, host, "Quit")
	if !forceQuitAtQuit {
		t.Fatal("forceQuit must be set before the host quits")
	}
}

func TestQuitAppWithoutContextNeverReachesHost(t *testing.T) {
	app := NewApp()
	host := &recordingNativeHost{}
	app.setNativeHost(host)
	app.quitApp()
	assertHostCalls(t, host)
	if app.forceQuit.Load() {
		t.Fatal("quitApp without a context must not arm forceQuit")
	}
}

func TestHideForBackgroundChoosesPlatformHide(t *testing.T) {
	host := &recordingNativeHost{}
	hideForBackground(context.Background(), host)
	want := "HideWindow"
	if backgroundCloseUsesApplicationHide(goruntime.GOOS) {
		want = "HideApplication"
	}
	assertHostCalls(t, host, want)
}

func TestHideToBackgroundRoutesThroughCoordinatorHost(t *testing.T) {
	app, host := newRecordingHostApp(t)
	if !app.desktopShell.coordinator.hideToBackground(app.ctx, func() bool { return true }) {
		t.Fatal("hideToBackground should succeed when the tray check passes")
	}
	want := "HideWindow"
	if backgroundCloseUsesApplicationHide(goruntime.GOOS) {
		want = "HideApplication"
	}
	assertHostCalls(t, host, want)
}

func seedWindowStateFile(t *testing.T, state DesktopWindowState) {
	t.Helper()
	isolateDesktopUserDirs(t)
	resetLastKnownWindowStateForTest()
	t.Cleanup(resetLastKnownWindowStateForTest)
	if state != (DesktopWindowState{}) {
		if err := writeWindowState(state); err != nil {
			t.Fatalf("writeWindowState: %v", err)
		}
	}
}

func TestRestoreWindowGeometryPreservesShellPosition(t *testing.T) {
	seedWindowStateFile(t, DesktopWindowState{Width: 1280, Height: 800, X: 40, Y: 50})
	app, host := newRecordingHostApp(t)
	host.screens = []nativeScreen{{Width: 1920, Height: 1080, Primary: true, Scale: 1}}
	app.restoreWindowGeometry()
	assertHostCalls(t, host)
	if app.backgroundMaximised.Load() {
		t.Fatal("a non-maximised state must not arm the maximise-before-show plan")
	}
}

func TestRestoreWindowGeometryPreservesShellOffscreenCorrection(t *testing.T) {
	seedWindowStateFile(t, DesktopWindowState{Width: 1280, Height: 800, X: 50_000, Y: 50})
	app, host := newRecordingHostApp(t)
	host.screens = []nativeScreen{{Width: 1920, Height: 1080, Primary: true, Scale: 1}}
	app.restoreWindowGeometry()
	assertHostCalls(t, host)
}

func TestRestoreWindowGeometryPreservesShellDefaultPosition(t *testing.T) {
	seedWindowStateFile(t, DesktopWindowState{})
	app, host := newRecordingHostApp(t)
	app.restoreWindowGeometry()
	assertHostCalls(t, host)
}

func TestRestoreWindowGeometryMaximisedFollowsPlatformOrdering(t *testing.T) {
	seedWindowStateFile(t, DesktopWindowState{Width: 1280, Height: 800, X: 40, Y: 50, Maximised: true})
	app, host := newRecordingHostApp(t)
	host.screens = []nativeScreen{{Width: 1920, Height: 1080}}
	app.restoreWindowGeometry()
	// The shell owns the restore rectangle; Go only applies presentation state.
	if goruntime.GOOS == "windows" {
		assertHostCalls(t, host)
		if !app.backgroundMaximised.Load() {
			t.Fatal("Windows must defer maximise to the presentation plan")
		}
		return
	}
	assertHostCalls(t, host, "MaximiseWindow")
}

func TestDOMReadyPreservesShellGeometryWhenPresenting(t *testing.T) {
	seedWindowStateFile(t, DesktopWindowState{Width: 1280, Height: 800, X: 40, Y: 50})
	app, host := newRecordingHostApp(t)
	host.screens = []nativeScreen{{Width: 1920, Height: 1080}}
	app.domReady(app.ctx)
	calls := host.callNames()
	position := slices.Index(calls, "SetWindowPosition(40,50)")
	show := slices.Index(calls, "ShowWindow")
	if position >= 0 || slices.Contains(calls, "CenterWindow") || show < 0 {
		t.Fatalf("domReady must preserve shell geometry when presenting, got %q", calls)
	}
}

func TestPickBlankProjectParentReturnsHostChoice(t *testing.T) {
	dir := t.TempDir()
	app, host := newRecordingHostApp(t)
	host.dialogPath = dir
	got, err := app.PickBlankProjectParent()
	if err != nil {
		t.Fatalf("PickBlankProjectParent: %v", err)
	}
	if got != dir {
		t.Fatalf("PickBlankProjectParent = %q, want %q", got, dir)
	}
	assertHostCalls(t, host, "OpenDirectoryDialog:Choose where to create the project")
}

func TestPickSkillFolderCancelledReturnsEmpty(t *testing.T) {
	app, host := newRecordingHostApp(t)
	got, err := app.PickSkillFolder()
	if err != nil || got != "" {
		t.Fatalf("cancelled picker = (%q, %v), want empty", got, err)
	}
	assertHostCalls(t, host, "OpenDirectoryDialog:Choose skills folder")
}

func TestConfirmActionMapsButtonsThroughHost(t *testing.T) {
	app, host := newRecordingHostApp(t)
	ok, err := app.ConfirmAction(NativeConfirmRequest{Title: "Delete?", Message: "Gone forever", Destructive: true})
	if err != nil {
		t.Fatalf("ConfirmAction: %v", err)
	}
	if ok {
		t.Fatal("a destructive confirm must default to cancel")
	}
	assertHostCalls(t, host, "MessageDialog:Delete?")
}

func TestWindowControlsWithoutHostAreNoops(t *testing.T) {
	app := &App{ctx: context.Background()}
	app.MinimiseMainWindow()
	app.ToggleMaximiseMainWindow()
	if app.IsMainWindowMaximised() {
		t.Fatal("the no-op host must report an unmaximised window")
	}
	if got, err := app.PickSkillFolder(); err != nil || got != "" {
		t.Fatalf("no-op host picker = (%q, %v), want empty", got, err)
	}
}
