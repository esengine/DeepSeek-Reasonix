package testenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsolateUserStateRedirectsAndRestoresCallerEnvironment(t *testing.T) {
	callerHome := t.TempDir()
	callerEnvironment := map[string]string{
		"HOME":                callerHome,
		"USERPROFILE":         callerHome,
		"XDG_CONFIG_HOME":     filepath.Join(callerHome, "caller-config"),
		"XDG_CACHE_HOME":      filepath.Join(callerHome, "caller-xdg-cache"),
		"XDG_STATE_HOME":      filepath.Join(callerHome, "caller-xdg-state"),
		"AppData":             filepath.Join(callerHome, "caller-appdata"),
		"LocalAppData":        filepath.Join(callerHome, "caller-local-appdata"),
		"REASONIX_HOME":       filepath.Join(callerHome, "explicit-reasonix-home"),
		"REASONIX_STATE_HOME": filepath.Join(callerHome, "caller-state"),
		"REASONIX_CACHE_HOME": filepath.Join(callerHome, "caller-cache"),
	}
	for key, value := range callerEnvironment {
		t.Setenv(key, value)
	}

	cleanup, err := IsolateUserState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	isolateHome := os.Getenv("HOME")
	if isolateHome == "" || isolateHome == callerHome {
		t.Fatalf("HOME = %q, want a disposable home distinct from caller %q", isolateHome, callerHome)
	}
	if rel, err := filepath.Rel(callerHome, isolateHome); err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		t.Fatalf("isolated home %q is not contained by caller home %q", isolateHome, callerHome)
	}
	wantIsolatedEnvironment := map[string]string{
		"HOME":            isolateHome,
		"USERPROFILE":     isolateHome,
		"XDG_CONFIG_HOME": filepath.Join(isolateHome, ".config"),
		"XDG_CACHE_HOME":  filepath.Join(isolateHome, ".cache"),
		"XDG_STATE_HOME":  filepath.Join(isolateHome, ".local", "state"),
		"AppData":         filepath.Join(isolateHome, "AppData", "Roaming"),
		"LocalAppData":    filepath.Join(isolateHome, "AppData", "Local"),
	}
	for key, want := range wantIsolatedEnvironment {
		if got := os.Getenv(key); got != want {
			t.Errorf("%s inside isolated test process = %q, want %q", key, got, want)
		}
	}
	for _, key := range []string{"REASONIX_HOME", "REASONIX_STATE_HOME", "REASONIX_CACHE_HOME"} {
		if _, ok := os.LookupEnv(key); ok {
			t.Fatalf("%s remained set inside isolated test process", key)
		}
	}

	cleanup()
	for key, want := range callerEnvironment {
		if got := os.Getenv(key); got != want {
			t.Errorf("%s after cleanup = %q, want %q", key, got, want)
		}
	}
	if _, err := os.Stat(isolateHome); !os.IsNotExist(err) {
		t.Fatalf("isolated home still exists after cleanup: %v", err)
	}
}
