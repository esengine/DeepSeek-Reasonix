package main

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/control"
)

// TestDesktopHotRebuildPathsKeepSessionTemp covers the three same-session
// rebuilds that use boot.Build directly (not boot.Rebuild): model, effort,
// and token-mode switches. Each must pass the old Controller's SessionTemp so
// temporary files survive (Issue #7575).
func TestDesktopHotRebuildPathsKeepSessionTemp(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(*App, *WorkspaceTab)
		rebuild func(*App, *WorkspaceTab) error
	}{
		{
			name:    "model",
			prepare: func(*App, *WorkspaceTab) {},
			rebuild: func(app *App, tab *WorkspaceTab) error {
				return app.SetModelForTab(tab.ID, "alt/alt-model")
			},
		},
		{
			name:    "effort",
			prepare: func(*App, *WorkspaceTab) {},
			rebuild: func(app *App, tab *WorkspaceTab) error {
				return app.SetEffortForTab(tab.ID, "high")
			},
		},
		{
			name:    "token mode",
			prepare: func(*App, *WorkspaceTab) {},
			rebuild: func(app *App, tab *WorkspaceTab) error {
				return app.SetTokenModeForTab(tab.ID, "delivery")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, tab, oldAPI, _ := newGoalDeliveryYoloTestApp(t, control.GoalStatusRunning)
			oldCtrl, ok := oldAPI.(*control.Controller)
			if !ok || oldCtrl == nil {
				t.Fatal("old controller is not *control.Controller")
			}
			tc.prepare(app, tab)

			// Plant a file in the session-private temporary directory.
			lease, err := oldCtrl.SessionTemp().Acquire()
			if err != nil {
				t.Fatalf("acquire old session temp: %v", err)
			}
			oldMgr := oldCtrl.SessionTemp()
			oldDir := lease.Dir()
			marker := filepath.Join(oldDir, "desktop-hot-rebuild.txt")
			if err := os.WriteFile(marker, []byte(tc.name), 0o600); err != nil {
				t.Fatal(err)
			}
			lease.Release()

			if err := tc.rebuild(app, tab); err != nil {
				t.Fatalf("rebuild: %v", err)
			}

			newAPI := app.controllerForTab(tab)
			newCtrl, ok := newAPI.(*control.Controller)
			if !ok || newCtrl == nil || newCtrl == oldCtrl {
				t.Fatal("rebuild did not install a replacement *control.Controller")
			}
			if newCtrl.SessionTemp() != oldMgr {
				t.Fatalf("%s rebuild did not reuse SessionTemp Manager (got %p want %p)",
					tc.name, newCtrl.SessionTemp(), oldMgr)
			}
			if oldMgr.Sealed() {
				t.Fatal("shared manager sealed after hot rebuild (replacement must retain first)")
			}

			again, err := newCtrl.SessionTemp().Acquire()
			if err != nil {
				t.Fatalf("acquire after rebuild: %v", err)
			}
			defer again.Release()
			if again.Dir() != oldDir {
				t.Fatalf("%s rebuild rotated temporary generation: got %q want %q",
					tc.name, again.Dir(), oldDir)
			}
			body, err := os.ReadFile(marker)
			if err != nil || string(body) != tc.name {
				t.Fatalf("temp file lost across %s rebuild: %q %v", tc.name, body, err)
			}
		})
	}
}

func TestSessionTempFromControllerHelper(t *testing.T) {
	if sessionTempFromController(nil) != nil {
		t.Fatal("nil controller should yield nil SessionTemp")
	}
	ctrl := control.New(control.Options{})
	defer ctrl.Close()
	if sessionTempFromController(ctrl) == nil {
		t.Fatal("want non-nil SessionTemp from live controller")
	}
	if sessionTempFromController(ctrl) != ctrl.SessionTemp() {
		t.Fatal("helper must return the controller's Manager")
	}
}
