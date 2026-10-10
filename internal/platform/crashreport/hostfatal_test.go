package crashreport

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	crashChildDir     = "REASONIX_TEST_CRASH_DIR"
	crashChildRelease = "REASONIX_TEST_CRASH_RELEASE"
)

func TestMain(m *testing.M) {
	if dir := os.Getenv(crashChildDir); dir != "" {
		release := InstallFatalLog(dir, "2.31.0/rc 1")
		if os.Getenv(crashChildRelease) != "" {
			release()
			os.Exit(0)
		}
		panic("host went down")
	}
	os.Exit(m.Run())
}

func runCrashChild(t *testing.T, dir string, release bool) {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), crashChildDir+"="+dir)
	if release {
		cmd.Env = append(cmd.Env, crashChildRelease+"=1")
	}
	_ = cmd.Run()
}

func fatalLogs(t *testing.T, dir string) []string {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(dir, "host-*"+fatalSuffix))
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func TestFatalLogKeepsWhatTheRuntimePrintsUnderAStableName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "crash")
	runCrashChild(t, dir, false)

	logs := fatalLogs(t, dir)
	if len(logs) != 1 {
		t.Fatalf("crash files = %v, want one", logs)
	}
	name := filepath.Base(logs[0])
	if !strings.Contains(name, "2.31.0-rc-1") || strings.ContainsAny(name, " /\\") {
		t.Fatalf("file name %q must carry the version and stay path-safe", name)
	}
	raw, err := os.ReadFile(logs[0])
	if err != nil || !strings.Contains(string(raw), "panic: host went down") || !strings.Contains(string(raw), "goroutine ") {
		t.Fatalf("crash file = %q, %v", raw, err)
	}
}

func TestFatalLogLeavesNothingAfterACleanExit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "crash")
	runCrashChild(t, dir, true)
	if logs := fatalLogs(t, dir); len(logs) != 0 {
		t.Fatalf("a clean exit left %v", logs)
	}
}

func TestFatalLogBoundsWhatEarlierCrashesLeft(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().Add(-time.Hour)
	for i := range keepFatalLogs + 3 {
		path := filepath.Join(dir, "host-old-"+string(rune('a'+i))+fatalSuffix)
		if err := os.WriteFile(path, []byte(strings.Repeat("x", maxFatalLogBytes*2)), 0o600); err != nil {
			t.Fatal(err)
		}
		at := base.Add(time.Duration(i) * time.Minute)
		_ = os.Chtimes(path, at, at)
	}
	empty := filepath.Join(dir, "host-empty"+fatalSuffix)
	_ = os.WriteFile(empty, nil, 0o600)
	unrelated := filepath.Join(dir, "notes.txt")
	_ = os.WriteFile(unrelated, []byte("keep"), 0o600)

	release := InstallFatalLog(dir, "dev")
	defer release()

	logs := fatalLogs(t, dir)
	// keepFatalLogs counts the file this launch just opened.
	if len(logs) != keepFatalLogs {
		t.Fatalf("kept %d crash files, want %d: %v", len(logs), keepFatalLogs, logs)
	}
	for _, path := range logs {
		if info, _ := os.Stat(path); info.Size() > maxFatalLogBytes {
			t.Fatalf("%s is %d bytes, over the %d cap", path, info.Size(), maxFatalLogBytes)
		}
	}
	if _, err := os.Stat(empty); err == nil {
		t.Fatal("an empty crash file is a launch that ended cleanly and must be dropped")
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatal("files that are not crash files must be left alone")
	}
}
