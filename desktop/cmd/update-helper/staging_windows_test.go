//go:build windows

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/filelock"
)

func isolatedTemp(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("TMP", root)
	t.Setenv("TEMP", root)
	return root
}

func stageTestInstaller(t *testing.T) (string, func() error) {
	t.Helper()
	source := filepath.Join(t.TempDir(), "installer.exe")
	content := []byte("verified-installer")
	if err := os.WriteFile(source, content, 0o700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	staged, cleanup, err := stageVerifiedInstaller(source, hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	return staged, cleanup
}

func backdate(t *testing.T, path string) {
	t.Helper()
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}

func junction(t *testing.T, link, target string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J: %v: %s", err, out)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestInstallerCleanupRetriesWhileAFileInsideIsOpen(t *testing.T) {
	isolatedTemp(t)
	staged, cleanup := stageTestInstaller(t)
	dir := filepath.Dir(staged)
	held, err := os.Open(staged)
	if err != nil {
		t.Fatal(err)
	}
	time.AfterFunc(400*time.Millisecond, func() { _ = held.Close() })
	if err := cleanup(); err != nil {
		t.Fatalf("cleanup gave up on a handle that was released shortly after: %v", err)
	}
	if exists(dir) {
		t.Fatal("installer staging survived a successful cleanup")
	}
}

func TestInstallerCleanupThatFailsLeavesADirectoryTheNextStagingSweeps(t *testing.T) {
	isolatedTemp(t)
	staged, cleanup := stageTestInstaller(t)
	dir := filepath.Dir(staged)
	held, err := os.Open(staged)
	if err != nil {
		t.Fatal(err)
	}
	cleanupBudget = 300 * time.Millisecond
	t.Cleanup(func() { cleanupBudget = 5 * time.Second })
	if err := cleanup(); !errors.Is(err, ErrStagingPreserved) {
		t.Fatalf("cleanup with a file still open = %v, want ErrStagingPreserved", err)
	}
	_ = held.Close()
	if !exists(dir) {
		t.Fatal("a failed cleanup must leave the directory in place for the sweep")
	}
	backdate(t, dir)
	next := runStagingChild(t)
	if exists(dir) {
		t.Fatal("the next helper process did not sweep the directory the failed cleanup left")
	}
	if !exists(next) {
		t.Fatal("the sweep removed the new staging")
	}
}

const stagingChildEnv = "REASONIX_TEST_STAGING_CHILD"

func TestStagingChildProcess(t *testing.T) {
	if os.Getenv(stagingChildEnv) == "" {
		t.Skip("runs only as the child of the sweep test")
	}
	staged, _ := stageTestInstaller(t)
	if err := os.WriteFile(os.Getenv(stagingChildEnv), []byte(staged), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runStagingChild(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "staged-path")
	cmd := exec.Command(os.Args[0], "-test.run=^TestStagingChildProcess$")
	cmd.Env = append(os.Environ(), stagingChildEnv+"="+out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child: %v: %s", err, b)
	}
	path, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(string(path))
}

func TestStagingSweepTouchesOnlyAbandonedOwnDirectories(t *testing.T) {
	temp := isolatedTemp(t)
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.MkdirAll(victim, 0o700); err != nil {
		t.Fatal(err)
	}
	victimFile := filepath.Join(victim, "keep.txt")
	if err := os.WriteFile(victimFile, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	live := filepath.Join(temp, installerPrefix+"live")
	if err := os.Mkdir(live, 0o700); err != nil {
		t.Fatal(err)
	}
	release, err := filelock.TryAcquire(filepath.Join(live, ".owner.lock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	backdate(t, live)

	link := filepath.Join(temp, installerPrefix+"link")
	junction(t, link, victim)

	bystander := filepath.Join(temp, "other-tool-1234")
	if err := os.Mkdir(bystander, 0o700); err != nil {
		t.Fatal(err)
	}
	backdate(t, bystander)

	prefixedFile := filepath.Join(temp, installerPrefix+"file")
	if err := os.WriteFile(prefixedFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	abandoned := filepath.Join(temp, installerPrefix+"abandoned")
	if err := os.Mkdir(abandoned, 0o700); err != nil {
		t.Fatal(err)
	}
	junction(t, filepath.Join(abandoned, "inner"), victim)
	backdate(t, abandoned)

	_, cleanup := stageTestInstaller(t)
	t.Cleanup(func() { _ = cleanup() })

	if exists(abandoned) {
		t.Fatal("abandoned directory was not swept")
	}
	if !exists(victimFile) {
		t.Fatal("sweep followed a junction and deleted its target")
	}
	for name, path := range map[string]string{"live owner": live, "top-level junction": link, "unrelated directory": bystander, "prefixed file": prefixedFile} {
		if !exists(path) {
			t.Fatalf("sweep removed %s", name)
		}
	}
}

func TestUpdateStagingPayloadStaysEmptyForTheInstaller(t *testing.T) {
	isolatedTemp(t)
	staging, err := createUpdateStaging(stagePrefix)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := staging.payloadDir()
	if err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(payload); err != nil || len(entries) != 0 {
		t.Fatalf("payload = %v, %v; the installer refuses a non-empty target", entries, err)
	}
	if !exists(filepath.Join(staging.root(), ".owner.lock")) {
		t.Fatal("owner lock missing from the staging root")
	}
	if err := staging.cleanup(); err != nil {
		t.Fatal(err)
	}
	if exists(staging.root()) {
		t.Fatal("staging survived cleanup")
	}
}
