//go:build windows

package config

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

const configTestFilename = "reasonix.toml"

func TestLoadAndSaveThroughDirectoryJunction(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	project := t.TempDir()
	configPath := filepath.Join(project, configTestFilename)
	if err := os.WriteFile(configPath, []byte("default_model = \"deepseek-pro\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	junction := createConfigTestJunction(t, project)
	loaded, err := LoadForRootReadOnly(junction)
	if err != nil {
		t.Fatalf("LoadForRootReadOnly through directory junction: %v", err)
	}
	if loaded.DefaultModel != "deepseek-pro" {
		t.Fatalf("default_model = %q, want deepseek-pro", loaded.DefaultModel)
	}

	resolved, err := resolveExistingConfigPath(filepath.Join(junction, configTestFilename))
	if err != nil {
		t.Fatalf("resolve config through directory junction: %v", err)
	}
	if !strings.HasPrefix(resolved, `\\?\`) {
		t.Fatalf("resolved config = %q, want extended Windows namespace path", resolved)
	}
	gotInfo, err := os.Stat(resolved)
	if err != nil {
		t.Fatal(err)
	}
	wantInfo, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(gotInfo, wantInfo) {
		t.Fatalf("resolved config = %q, want same file as %q", resolved, configPath)
	}

	loaded.Agent.Temperature = 0.42
	if err := loaded.SaveTo(filepath.Join(junction, configTestFilename)); err != nil {
		t.Fatalf("SaveTo through directory junction: %v", err)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "temperature = 0.42") {
		t.Fatalf("physical config was not updated:\n%s", raw)
	}
}

func TestLoadAllowsMissingConfigThroughDirectoryJunction(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	project := t.TempDir()
	junction := createConfigTestJunction(t, project)
	if _, err := LoadForRootReadOnly(junction); err != nil {
		t.Fatalf("LoadForRootReadOnly with missing config through directory junction: %v", err)
	}

	resolved, err := resolveConfigAccessPath(filepath.Join(junction, configTestFilename), false)
	if err != nil {
		t.Fatalf("resolve missing config through directory junction: %v", err)
	}
	if filepath.Base(resolved) != configTestFilename {
		t.Fatalf("resolved config base = %q, want %q", filepath.Base(resolved), configTestFilename)
	}
	gotRootInfo, err := os.Stat(filepath.Dir(resolved))
	if err != nil {
		t.Fatal(err)
	}
	wantRootInfo, err := os.Stat(project)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(gotRootInfo, wantRootInfo) {
		t.Fatalf("resolved config = %q, want parent same as %q", resolved, project)
	}
}

func TestResolveConfigAccessPathRejectsBrokenDirectoryJunction(t *testing.T) {
	project := t.TempDir()
	junction := createConfigTestJunction(t, project)
	if err := os.Remove(project); err != nil {
		t.Fatalf("remove junction target: %v", err)
	}

	if _, err := resolveConfigAccessPath(filepath.Join(junction, configTestFilename), false); err == nil {
		t.Fatal("resolveConfigAccessPath accepted a broken directory junction")
	}
	if _, err := os.Lstat(junction); err != nil {
		t.Fatalf("failed resolution removed broken directory junction: %v", err)
	}
}

func TestResolveFinalConfigWindowsPathPreservesExtendedNamespace(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "drive", path: `\\?\C:\Projects.\Reasonix`},
		{name: "UNC", path: `\\?\UNC\server\share\Reasonix`},
		{name: "volume GUID", path: `\\?\Volume{00000000-0000-0000-0000-000000000000}\Reasonix`},
		{name: "ordinary", path: `C:\Projects\Reasonix`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveFinalConfigWindowsPath(func(uint32) (string, error) {
				return test.path, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if got != test.path {
				t.Fatalf("resolveFinalConfigWindowsPath() = %q, want exact path %q", got, test.path)
			}
		})
	}
}

func TestResolveFinalConfigWindowsPathFallsBackToVolumeGUID(t *testing.T) {
	const guidPath = `\\?\Volume{00000000-0000-0000-0000-000000000000}\Reasonix`
	var calls []uint32
	got, err := resolveFinalConfigWindowsPath(func(flags uint32) (string, error) {
		calls = append(calls, flags)
		if flags == finalConfigVolumeNameDOS {
			return "", windows.ERROR_PATH_NOT_FOUND
		}
		return guidPath, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != guidPath {
		t.Fatalf("resolveFinalConfigWindowsPath() = %q, want %q", got, guidPath)
	}
	if len(calls) != 2 || calls[0] != finalConfigVolumeNameDOS || calls[1] != finalConfigVolumeNameGUID {
		t.Fatalf("query flags = %v, want [%d %d]", calls, finalConfigVolumeNameDOS, finalConfigVolumeNameGUID)
	}
}

func TestResolveFinalConfigWindowsPathDoesNotMaskOtherErrors(t *testing.T) {
	calls := 0
	_, err := resolveFinalConfigWindowsPath(func(uint32) (string, error) {
		calls++
		return "", windows.ERROR_ACCESS_DENIED
	})
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("resolveFinalConfigWindowsPath() error = %v, want access denied", err)
	}
	if calls != 1 {
		t.Fatalf("query calls = %d, want 1", calls)
	}
}

func TestResolveFinalConfigWindowsPathFailsClosedWhenGUIDFallbackFails(t *testing.T) {
	calls := 0
	_, err := resolveFinalConfigWindowsPath(func(flags uint32) (string, error) {
		calls++
		if flags == finalConfigVolumeNameDOS {
			return "", windows.ERROR_PATH_NOT_FOUND
		}
		return "", windows.ERROR_ACCESS_DENIED
	})
	if !errors.Is(err, windows.ERROR_PATH_NOT_FOUND) || !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("resolveFinalConfigWindowsPath() error = %v, want both query errors", err)
	}
	if calls != 2 {
		t.Fatalf("query calls = %d, want 2", calls)
	}
}

func TestConfigFileEditLockPathResolvedNormalizesExtendedNamespace(t *testing.T) {
	tests := []struct {
		name     string
		ordinary string
		extended string
	}{
		{name: "drive", ordinary: `C:\Projects.\Reasonix\reasonix.toml`, extended: `\\?\C:\Projects.\Reasonix\reasonix.toml`},
		{name: "UNC", ordinary: `\\server\share\Reasonix\reasonix.toml`, extended: `\\?\UNC\server\share\Reasonix\reasonix.toml`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ordinaryLock, err := configFileEditLockPathResolved(test.ordinary)
			if err != nil {
				t.Fatal(err)
			}
			extendedLock, err := configFileEditLockPathResolved(test.extended)
			if err != nil {
				t.Fatal(err)
			}
			if ordinaryLock != extendedLock {
				t.Fatalf("lock paths differ: ordinary %q, extended %q", ordinaryLock, extendedLock)
			}
			if test.name == "drive" {
				withoutTrailingDot, err := configFileEditLockPathResolved(`C:\Projects\Reasonix\reasonix.toml`)
				if err != nil {
					t.Fatal(err)
				}
				if ordinaryLock == withoutTrailingDot {
					t.Fatal("lock key discarded a trailing dot from a path component")
				}
			}
		})
	}
}

func createConfigTestJunction(t *testing.T, target string) string {
	t.Helper()
	junction := filepath.Join(t.TempDir(), "project")
	output, err := exec.Command("cmd", "/c", "mklink", "/J", junction, target).CombinedOutput()
	if err != nil {
		t.Fatalf("create directory junction: %v: %s", err, output)
	}
	return junction
}
