package appidentity

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A single literal, so an adjacent string cannot be mistaken for the ID.
var jsAppUserModelIDRe = regexp.MustCompile(`APP_USER_MODEL_ID\s*=\s*"([^"]*)"`)

// The Electron shell is the process that owns the taskbar window, so the
// launcher's explicit ID reaches Windows only through the JS port below.
func TestElectronShellAdoptsAppUserModelID(t *testing.T) {
	identity, main := electronShellIdentitySources(t)

	declaration, err := os.ReadFile(identity)
	if err != nil {
		t.Fatal(err)
	}
	match := jsAppUserModelIDRe.FindSubmatch(declaration)
	if match == nil {
		t.Fatalf("no APP_USER_MODEL_ID literal in %s", identity)
	}
	if got := string(match[1]); got != AppUserModelID {
		t.Errorf("Electron APP_USER_MODEL_ID = %q, want %q (internal/appidentity/identity.go)", got, AppUserModelID)
	}

	entry, err := os.ReadFile(main)
	if err != nil {
		t.Fatal(err)
	}
	// The call has to run at module load: a window created earlier captures the
	// implicit executable-path identity and the pinned icon splits off again.
	call := strings.Index(string(entry), "applyAppUserModelId(app, process.platform)")
	if call < 0 {
		t.Fatalf("%s does not apply the Windows app identity", main)
	}
	for _, later := range []string{"app.whenReady()", "bootstrap(home)"} {
		if at := strings.Index(string(entry), later); at >= 0 && call > at {
			t.Errorf("%s applies the app identity after %q, too late for the window's taskbar group", main, later)
		}
	}
}

func electronShellIdentitySources(t *testing.T) (identity, main string) {
	t.Helper()
	packageDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// internal/appidentity -> repo root
	dir := filepath.Join(packageDir, "..", "..", "desktop", "electron", "src", "main")
	return filepath.Join(dir, "appIdentity.ts"), filepath.Join(dir, "index.ts")
}
