package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/repair"
)

func TestRunLaunchAppliesAndForwardsHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dir := t.TempDir()
	profile := filepath.Join(dir, "profile with spaces")
	output := filepath.Join(dir, "args")
	app := filepath.Join(dir, "desktop")
	script := "#!/bin/sh\nprintf '%s\\n' \"$REASONIX_HOME\" \"$REASONIX_STATE_HOME\" \"$REASONIX_CACHE_HOME\" \"$@\" > \"$REASONIX_GUARD_TEST_OUTPUT\"\n"
	if err := os.WriteFile(app, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REASONIX_GUARD_TEST_OUTPUT", output)
	t.Setenv("REASONIX_HOME", filepath.Join(dir, "old-home"))
	t.Setenv("REASONIX_STATE_HOME", filepath.Join(dir, "old-state"))
	t.Setenv("REASONIX_CACHE_HOME", filepath.Join(dir, "old-cache"))

	if code := runLaunch([]string{"--app", app, "--detach=false", "--home", profile, "--safe-mode", "extra"}); code != 0 {
		t.Fatalf("runLaunch exit code = %d", code)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(data)), "\n")
	want := []string{profile, profile, filepath.Join(profile, "cache"), "--safe-mode", "--home", profile, "extra"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("child environment and args = %#v, want %#v", got, want)
	}
}

func TestRunLaunchRejectsInvalidHomeBeforeStartingChild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile-file")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if code := runLaunch([]string{"--home", path, "--app", filepath.Join(t.TempDir(), "missing")}); code != 2 {
		t.Fatalf("runLaunch exit code = %d, want 2", code)
	}
}

func TestCheckReportsInvalidProjectConfig(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("[broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"check", "--root", root, "--json"}); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestFailedInstallBlocksLaunch(t *testing.T) {
	cases := []struct {
		name   string
		result repair.UpdateRollbackResult
		err    error
		want   bool
	}{
		{name: "no error never blocks", result: repair.UpdateRollbackResult{}, err: nil, want: false},
		{name: "incomplete rollback fails closed", result: repair.UpdateRollbackResult{}, err: errors.New("stage failed"), want: true},
		{name: "uncompensated rollback fails closed", result: repair.UpdateRollbackResult{MixedInstall: true}, err: errors.New("restore failed"), want: true},
		{name: "completed restore with marker cleanup error launches", result: repair.UpdateRollbackResult{RolledBack: true}, err: errors.New("remove marker: permission denied"), want: false},
	}
	for _, tc := range cases {
		if got := failedInstallBlocksLaunch(tc.result, tc.err); got != tc.want {
			t.Errorf("%s: failedInstallBlocksLaunch = %v, want %v", tc.name, got, tc.want)
		}
	}
}
