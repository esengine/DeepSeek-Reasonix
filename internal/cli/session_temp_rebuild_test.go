package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/boot"
	"reasonix/internal/control"
	"reasonix/internal/provider"
)

// TestCLIHotRebuildPathsKeepSessionTemp covers the CLI commands that replace a
// Controller without changing the logical session. Every path must keep the
// Manager identity, current generation, and files even after the outgoing
// Controller releases its owner reference.
func TestCLIHotRebuildPathsKeepSessionTemp(t *testing.T) {
	for _, command := range []string{"model", "effort", "reload"} {
		t.Run(command, func(t *testing.T) {
			isolateUserConfig(t)

			oldCtrl := control.New(control.Options{Label: "deepseek-flash"})
			t.Cleanup(oldCtrl.Close)
			oldManager := oldCtrl.SessionTemp()
			lease, err := oldManager.Acquire()
			if err != nil {
				t.Fatalf("acquire old session temp: %v", err)
			}
			oldDir := lease.Dir()
			marker := filepath.Join(oldDir, "cli-hot-rebuild.txt")
			if err := os.WriteFile(marker, []byte(command), 0o600); err != nil {
				lease.Release()
				t.Fatal(err)
			}
			lease.Release()

			m := newTestChatTUI()
			m.ctrl = oldCtrl
			m.modelRef = "deepseek-flash/deepseek-v4-flash"
			m.buildController = func(_ controllerBuildSpec, _ []provider.Message, _ string, outgoing control.SessionAPI) (*control.Controller, error) {
				return control.New(control.Options{
					Label:       "deepseek-flash",
					SessionTemp: sessionTempFromCLIController(outgoing),
				}), nil
			}
			m.rebuildRuntime = func(_ context.Context, _ controllerBuildSpec, outgoing *control.Controller) (*boot.BuildResult, error) {
				// Production delegates this path to boot.Rebuild, whose owning test
				// pins the same SessionTemp transfer. This seam exercises the CLI
				// command/swap lifecycle without booting providers or plugins.
				return &boot.BuildResult{Controller: control.New(control.Options{
					Label:       "deepseek-flash",
					SessionTemp: outgoing.SessionTemp(),
				})}, nil
			}

			switch command {
			case "model":
				m.runModelSubcommand("/model deepseek-flash/another-model")
				if m.pendingModelSwitch == nil {
					t.Fatal("/model did not schedule a replacement")
				}
				msg := m.pendingModelSwitch()
				next, _ := m.Update(msg)
				m = next.(chatTUI)
			case "effort":
				effortCmd := m.runEffortCommand("/effort max")
				if effortCmd == nil {
					t.Fatal("/effort did not schedule a replacement")
				}
				next, _ := m.Update(effortCmd())
				m = next.(chatTUI)
			case "reload":
				reloadCmd := m.runReloadCommand()
				if reloadCmd == nil {
					t.Fatal("/reload did not schedule a replacement")
				}
				next, _ := m.Update(reloadCmd())
				m = next.(chatTUI)
			}
			newCtrl, ok := m.ctrl.(*control.Controller)
			if !ok || newCtrl == nil || newCtrl == oldCtrl {
				t.Fatal("CLI rebuild did not install a replacement Controller")
			}
			t.Cleanup(newCtrl.Close)
			if newCtrl.SessionTemp() != oldManager {
				t.Fatalf("%s rebuild changed SessionTemp Manager: got %p want %p", command, newCtrl.SessionTemp(), oldManager)
			}

			// Match the TUI's deferred retirement: the replacement must already
			// own the shared Manager before the outgoing Controller releases it.
			oldCtrl.ReleaseResources()
			if oldManager.Sealed() {
				t.Fatalf("%s rebuild sealed the shared Manager after old release", command)
			}
			again, err := newCtrl.SessionTemp().Acquire()
			if err != nil {
				t.Fatalf("acquire after %s rebuild: %v", command, err)
			}
			if again.Dir() != oldDir {
				again.Release()
				t.Fatalf("%s rebuild rotated temp generation: got %q want %q", command, again.Dir(), oldDir)
			}
			body, err := os.ReadFile(marker)
			again.Release()
			if err != nil || string(body) != command {
				t.Fatalf("temp file lost across %s rebuild: %q %v", command, body, err)
			}
		})
	}
}

func TestSessionTempFromCLIControllerHelper(t *testing.T) {
	if sessionTempFromCLIController(nil) != nil {
		t.Fatal("nil controller should yield nil SessionTemp")
	}
	ctrl := control.New(control.Options{})
	defer ctrl.Close()
	if got := sessionTempFromCLIController(ctrl); got == nil || got != ctrl.SessionTemp() {
		t.Fatal("helper did not return the live Controller's SessionTemp")
	}
}
