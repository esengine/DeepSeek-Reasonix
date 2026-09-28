package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/installlayout"
	"reasonix/internal/repair"
)

const (
	legacyChildModeEnv = "REASONIX_LEGACY_MIGRATOR_TEST_CHILD"
	legacyChildRootEnv = "REASONIX_LEGACY_MIGRATOR_TEST_ROOT"
	legacyFromVersion  = "v1.19.2"
	legacyToVersion    = "v1.39.4"
	childShellMissing  = 3
)

func writeShellTree(t *testing.T, root, label string) {
	t.Helper()
	for _, name := range installlayout.ShellRequiredNames(runtime.GOOS) {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(label+"-"+name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func snapshotFlat(t *testing.T, root string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			out[entry.Name()] = "<dir>"
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[entry.Name()] = string(data)
	}
	return out
}

func assertNothingCommitted(t *testing.T, root string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(root, installlayout.CurrentFileName)); !os.IsNotExist(err) {
		t.Fatalf("current.json written for a release unit without a shell: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "versions")); !os.IsNotExist(err) {
		t.Fatalf("versions/ created for a release unit without a shell: %v", err)
	}
}

func assertRefusalLogged(t *testing.T, want ...string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(config.ReasonixHomeDir(), "desktop-shell", "logs", migrateLogName))
	if err != nil {
		t.Fatalf("refusal was not logged: %v", err)
	}
	for _, w := range append([]string{"code=shell_missing"}, want...) {
		if !strings.Contains(string(data), w) {
			t.Fatalf("refusal log %q lacks %q", data, w)
		}
	}
}

func TestMigrateRefusesFlatUnitWithoutShell(t *testing.T) {
	cases := map[string]func(t *testing.T, root string){
		"absent": func(*testing.T, string) {},
		"incomplete": func(t *testing.T, root string) {
			p := filepath.Join(root, installlayout.AppShellDirName, "resources", "app.asar")
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("asar"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, shell := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("REASONIX_HOME", t.TempDir())
			root := t.TempDir()
			writeFlatUnit(t, root, "new")
			if err := os.WriteFile(filepath.Join(root, "reasonix-guard"), []byte("migrator"), 0o755); err != nil {
				t.Fatal(err)
			}
			shell(t, root)
			before := snapshotFlat(t, root)

			err := migrateWithRelaunch(root, legacyToVersion, false)
			if !errors.Is(err, errShellMissing) {
				t.Fatalf("migrate = %v, want errShellMissing", err)
			}
			assertNothingCommitted(t, root)
			if after := snapshotFlat(t, root); fmt.Sprint(after) != fmt.Sprint(before) {
				t.Fatalf("install root changed on refusal:\nbefore %v\nafter  %v", before, after)
			}
			assertRefusalLogged(t, "rolled_back=false")
		})
	}
}

// legacyUpdatedInstall reproduces what a 1.18–1.19.2 updater leaves behind: it
// runs the updater's own transaction steps from inside the install, publishing
// only desktop, CLI and the migrator under the Guard name.
func legacyUpdatedInstall(t *testing.T, withShell bool) (root string, oldDesktop []byte) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the file-by-file legacy updater is the Unix portable path")
	}
	t.Setenv("REASONIX_HOME", t.TempDir())
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	oldDesktop, err = os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	root = t.TempDir()
	for name, body := range map[string][]byte{
		"reasonix-desktop": oldDesktop,
		"reasonix-guard":   []byte("old-guard"),
		"reasonix":         []byte("old-cli"),
	} {
		if err := os.WriteFile(filepath.Join(root, name), body, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if withShell {
		writeShellTree(t, root, "new")
	}
	if code, out := runLegacyChildProcess(t, filepath.Join(root, "reasonix-desktop"), "legacy-update", root); code != 0 {
		t.Fatalf("legacy updater child exited %d: %s", code, out)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "reasonix-desktop")); string(got) != "new-desktop" {
		t.Fatalf("legacy updater did not publish the new desktop: %q", got)
	}
	return root, oldDesktop
}

func runLegacyChildProcess(t *testing.T, exe, mode, root string) (int, string) {
	t.Helper()
	cmd := exec.Command(exe, "-test.run=^$")
	cmd.Env = append(os.Environ(), legacyChildModeEnv+"="+mode, legacyChildRootEnv+"="+root)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), out.String()
	}
	if err != nil {
		t.Fatalf("run %s child: %v", mode, err)
	}
	return 0, out.String()
}

func TestLegacyUpdaterWithoutShellRestoresPreviousRelease(t *testing.T) {
	root, oldDesktop := legacyUpdatedInstall(t, false)
	code, out := runLegacyChildProcess(t, filepath.Join(root, "reasonix-guard"), "migrate", root)
	if code != childShellMissing {
		t.Fatalf("migrator child exited %d, want errShellMissing: %s", code, out)
	}
	assertNothingCommitted(t, root)
	for name, want := range map[string][]byte{
		"reasonix-desktop": oldDesktop,
		"reasonix-guard":   []byte("old-guard"),
		"reasonix":         []byte("old-cli"),
	} {
		if got, err := os.ReadFile(filepath.Join(root, name)); err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s was not restored to the previous release (err %v)", name, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(root, installlayout.LauncherBinaryName())); !os.IsNotExist(err) {
		t.Fatalf("thin launcher installed beside a restored legacy release: %v", err)
	}
	if repair.PendingUpdateExists() {
		t.Fatal("legacy transaction still pending after rollback")
	}
	assertRefusalLogged(t, "rolled_back=true")
}

func TestLegacyUpdaterWithShellStillCommitsTransaction(t *testing.T) {
	root, _ := legacyUpdatedInstall(t, true)
	if code, out := runLegacyChildProcess(t, filepath.Join(root, "reasonix-guard"), "migrate", root); code != 0 {
		t.Fatalf("migrator child exited %d: %s", code, out)
	}
	ptr, err := installlayout.ReadCurrent(root)
	if err != nil || ptr.ActiveVersion != legacyToVersion {
		t.Fatalf("pointer=%+v err=%v", ptr, err)
	}
	desktop, err := installlayout.ActiveDesktopPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(desktop); string(got) != "new-desktop" {
		t.Fatalf("active desktop = %q", got)
	}
	if !installlayout.HasActiveShell(root) {
		t.Fatal("active version lost its shell")
	}
	if repair.PendingUpdateExists() {
		t.Fatal("legacy transaction was not committed")
	}
}

func runLegacyChild(mode string) int {
	root := os.Getenv(legacyChildRootEnv)
	var err error
	switch mode {
	case "legacy-update":
		err = applyLegacyUpdate(root)
	case "migrate":
		err = migrateWithRelaunch(root, legacyToVersion, false)
		if errors.Is(err, errShellMissing) {
			fmt.Fprintln(os.Stderr, err)
			return childShellMissing
		}
	default:
		err = fmt.Errorf("unknown child mode %q", mode)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// applyLegacyUpdate follows applyLinux from v1.19.2 desktop/updater.go step
// for step, with the running desktop as the updater.
func applyLegacyUpdate(root string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	migrator, err := os.ReadFile(exe)
	if err != nil {
		return err
	}
	desktop := filepath.Join(root, "reasonix-desktop")
	guard := filepath.Join(root, "reasonix-guard")
	cli := filepath.Join(root, "reasonix")
	tx, err := repair.PrepareFileUpdate(legacyFromVersion, legacyToVersion, desktop, guard, cli)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	claimed, release, err := repair.ClaimPendingFileUpdateExact(tx.ToVersion, tx.CreatedAt, repair.UpdateTransactionID(tx), desktop, []string{desktop, guard, cli}, 2*time.Minute)
	if err != nil {
		return fmt.Errorf("claim: %w", err)
	}
	defer release()
	if err := repair.MarkUpdateApplyFailedExact(claimed, "Linux update publish did not complete"); err != nil {
		return err
	}
	var receipts []repair.FileUpdateInstallReceipt
	for _, member := range []struct {
		path string
		body []byte
	}{{cli, []byte("new-cli")}, {guard, migrator}, {desktop, []byte("new-desktop")}} {
		receipt, err := repair.PublishClaimedFileUpdateMemberExact(claimed, member.path, member.body, 0o700)
		if err != nil {
			return fmt.Errorf("publish %s: %w", filepath.Base(member.path), err)
		}
		receipts = append(receipts, receipt)
	}
	if _, err := repair.RecordClaimedFileUpdateInstalled(claimed, receipts...); err != nil {
		return fmt.Errorf("record installed: %w", err)
	}
	return repair.ClearUpdateApplyFailureExact(claimed)
}
