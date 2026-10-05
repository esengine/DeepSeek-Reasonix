package update

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func sum(b string) string {
	s := sha256.Sum256([]byte(b))
	return hex.EncodeToString(s[:])
}

func put(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

func handoff(t *testing.T, files map[string]string) TreeHandoff {
	t.Helper()
	base := testenv.TempDir(t)
	h := TreeHandoff{Version: "v2", InstallDir: filepath.Join(base, "install"), StagingDir: filepath.Join(base, "staged"), BackupDir: filepath.Join(base, "backup")}
	for rel, body := range files {
		put(t, h.StagingDir, rel, body)
		h.Files = append(h.Files, StagedFile{Path: rel, SHA256: sum(body)})
	}
	put(t, h.InstallDir, "app.exe", "old app")
	put(t, h.InstallDir, "resources/bin/host.exe", "old host")
	put(t, h.InstallDir, "Uninstall Reasonix Studio.exe", "uninstaller")
	return h
}

func quickRetries(t *testing.T) {
	old := fsRetries
	fsRetries = 2
	t.Cleanup(func() { fsRetries = old })
}

// The swap replaces what the release lists, adds what is new, and leaves what
// it does not list — the installer's own uninstaller — where it was.
func TestApplyTreeInstallsTheStagedFilesAndKeepsTheRest(t *testing.T) {
	quickRetries(t)
	h := handoff(t, map[string]string{"app.exe": "new app", "resources/bin/host.exe": "new host", "resources/new.js": "added"})
	if err := ApplyTree(h); err != nil {
		t.Fatalf("ApplyTree: %v", err)
	}
	for rel, want := range map[string]string{"app.exe": "new app", "resources/bin/host.exe": "new host", "resources/new.js": "added", "Uninstall Reasonix Studio.exe": "uninstaller"} {
		if got := read(t, h.InstallDir, rel); got != want {
			t.Fatalf("%s = %q, want %q", rel, got, want)
		}
	}
	if got := read(t, h.BackupDir, "app.exe"); got != "old app" {
		t.Fatalf("backup of app.exe = %q", got)
	}
}

// A staged file changed after it was verified is caught before anything in
// the install is touched.
func TestApplyTreeRefusesAStagedFileThatChanged(t *testing.T) {
	quickRetries(t)
	h := handoff(t, map[string]string{"app.exe": "new app", "resources/bin/host.exe": "new host"})
	put(t, h.StagingDir, "resources/bin/host.exe", "tampered")
	if err := ApplyTree(h); !errors.Is(err, ErrStagedTreeChanged) {
		t.Fatalf("err = %v, want ErrStagedTreeChanged", err)
	}
	if read(t, h.InstallDir, "app.exe") != "old app" || read(t, h.InstallDir, "resources/bin/host.exe") != "old host" {
		t.Fatal("a refused swap changed the install")
	}
}

// A file that cannot be installed midway puts back every file already moved:
// the install is either the new release or the old one, never a mixture.
func TestApplyTreeRestoresEverythingWhenAFileCannotBeInstalled(t *testing.T) {
	quickRetries(t)
	h := handoff(t, map[string]string{"app.exe": "new app", "resources/bin/host.exe": "new host", "resources/blocked/x.js": "new"})
	// A file standing where the release needs a directory stops the swap there.
	put(t, h.InstallDir, "resources/blocked", "a file, not a directory")
	if err := ApplyTree(h); err == nil {
		t.Fatal("a swap that could not install a file reported success")
	}
	for rel, want := range map[string]string{"app.exe": "old app", "resources/bin/host.exe": "old host", "resources/blocked": "a file, not a directory"} {
		if got := read(t, h.InstallDir, rel); got != want {
			t.Fatalf("after rollback %s = %q, want %q", rel, got, want)
		}
	}
}

func TestTreeHandoffRoundTripsThroughItsFile(t *testing.T) {
	h := handoff(t, map[string]string{"app.exe": "new app"})
	h.WaitPIDs, h.Relaunch = []int{42}, `C:\Studio\Reasonix Studio.exe`
	path, err := WriteTreeHandoff(h)
	if err != nil {
		t.Fatal(err)
	}
	got, err := readTreeHandoff(path)
	if err != nil || got.Version != "v2" || got.Relaunch != h.Relaunch || len(got.Files) != 1 || got.WaitPIDs[0] != 42 {
		t.Fatalf("read back %+v, %v", got, err)
	}
	if handled, _ := MaybeRunTreeHandoff([]string{"--something-else"}); handled {
		t.Fatal("an ordinary launch was taken for a handoff")
	}
}

// The probe that clears an install for a swap leaves nothing behind in either
// directory, so a check that passes costs the install nothing.
func TestCheckTreeSwapLeavesNoTrace(t *testing.T) {
	install, backup := testenv.TempDir(t), filepath.Join(testenv.TempDir(t), "delta")
	if err := CheckTreeSwap(install, backup); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{install, backup} {
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Fatalf("%s kept %d entries", dir, len(entries))
		}
	}
}

func TestCheckTreeSwapRefusesAnInstallItCannotWrite(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("a directory mode does not deny writes here")
	}
	install := testenv.TempDir(t)
	if err := os.Chmod(install, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(install, 0o755) })
	if err := CheckTreeSwap(install, filepath.Join(testenv.TempDir(t), "delta")); !errors.Is(err, ErrTreeNotSwappable) {
		t.Fatalf("err = %v, want ErrTreeNotSwappable", err)
	}
}

// The helper is detached, so a swap that gives up leaves its reason where the
// restored build reads it on its next launch.
func TestAFailedSwapLeavesItsReasonForTheNextLaunch(t *testing.T) {
	quickRetries(t)
	h := handoff(t, map[string]string{"app.exe": "new app"})
	put(t, h.StagingDir, "app.exe", "tampered")
	h.Outcome = filepath.Join(testenv.TempDir(t), "swap-outcome.txt")
	plan, err := WriteTreeHandoff(h)
	if err != nil {
		t.Fatal(err)
	}
	if handled, code := MaybeRunTreeHandoff([]string{treeHandoffArg, plan}); !handled || code != 1 {
		t.Fatalf("handled=%v code=%d, want a handled failure", handled, code)
	}
	if got := read(t, filepath.Dir(h.Outcome), "swap-outcome.txt"); !strings.Contains(got, "app.exe") {
		t.Fatalf("outcome = %q, want the failing file named", got)
	}
}
