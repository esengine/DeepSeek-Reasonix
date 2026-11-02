package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/repair"
)

func TestDoctorRepairSkipsConfigStartupMutation(t *testing.T) {
	if !isDoctorRepairCommand([]string{"doctor", "repair", "--json"}) {
		t.Fatal("doctor repair was not recognized")
	}
	for _, args := range [][]string{{"doctor"}, {"doctor", "capabilities"}, {"run"}} {
		if isDoctorRepairCommand(args) {
			t.Fatalf("unexpected repair match for %v", args)
		}
	}
}

func TestDoctorRepairDryRunDoesNotMigrateConfig(t *testing.T) {
	home := testenv.TempDir(t)
	root := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	path := filepath.Join(root, "reasonix.toml")
	body := []byte("[[plugins]]\nname = \"legacy\"\ncommand = \"legacy\"\ntier = \"lazy\"\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if code := Run([]string{"doctor", "repair", "--root", root, "--json"}, "test"); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("dry run changed config:\n%s", got)
	}
}

func runDoctorRepair(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	stdout, stderr = captureCLIOutput(t, func() {
		code = doctorCommand(append([]string{"repair"}, args...), "test-version")
	})
	return stdout, stderr, code
}

// An Lstat that fails for a reason other than absence leaves Exists=false. The
// report used to read that as "missing (defaults apply)" and exit 0, dropping
// the cause and telling the user the defaults apply to a file that is right
// there. The exact old line was "  global   missing (defaults apply)" with
// exit 0; the file and the access error never appeared.
func TestDoctorRepairUnreadableConfigIsNotMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a non-directory path component is reported as absence on Windows")
	}
	tests := []struct {
		name  string
		setup func(t *testing.T) (root, path, cause string)
	}{
		{
			name: "not a directory",
			setup: func(t *testing.T) (string, string, string) {
				home := filepath.Join(testenv.TempDir(t), "home-file")
				if err := os.WriteFile(home, []byte("not a directory\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("REASONIX_HOME", home)
				return testenv.TempDir(t), filepath.Join(home, "config.toml"), "not a directory"
			},
		},
		{
			name: "permission denied",
			setup: func(t *testing.T) (string, string, string) {
				if os.Geteuid() == 0 {
					t.Skip("root ignores directory permission bits")
				}
				home := testenv.TempDir(t)
				t.Setenv("REASONIX_HOME", home)
				if err := os.Chmod(home, 0); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(home, 0o700) })
				return testenv.TempDir(t), filepath.Join(home, "config.toml"), "permission denied"
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root, path, cause := tc.setup(t)
			stdout, stderr, code := runDoctorRepair(t, "--root", root)
			if code != 1 {
				t.Fatalf("exit = %d, want 1 (an unreadable config is not a clean pass)\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
			}
			if strings.Contains(stdout, "missing (defaults apply)\n             "+path) {
				t.Fatalf("unreadable config reported as missing:\n%s", stdout)
			}
			if !strings.Contains(stdout, "unreadable: cannot tell whether it is valid") {
				t.Fatalf("report does not say the config could not be read:\n%s", stdout)
			}
			if !strings.Contains(stdout, "             "+path+"\n") {
				t.Fatalf("report does not name the unreadable path %q:\n%s", path, stdout)
			}
			if !strings.Contains(stdout, "             cause: ") || !strings.Contains(stdout, cause) {
				t.Fatalf("report does not name the cause %q:\n%s", cause, stdout)
			}
		})
	}
}

func TestDoctorRepairJSONCarriesUnreadableState(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a non-directory path component is reported as absence on Windows")
	}
	home := filepath.Join(testenv.TempDir(t), "home-file")
	if err := os.WriteFile(home, []byte("not a directory\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REASONIX_HOME", home)
	root := testenv.TempDir(t)
	stdout, stderr, code := runDoctorRepair(t, "--root", root, "--json")
	if code != 1 {
		t.Fatalf("exit = %d, want 1\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	var report repair.ConfigReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("doctor repair --json output is not JSON: %v\n%s", err, stdout)
	}
	if len(report.Checks) != 2 {
		t.Fatalf("checks = %+v, want global and project", report.Checks)
	}
	global := report.Checks[0]
	if global.Scope != "global" || global.Path != filepath.Join(home, "config.toml") {
		t.Fatalf("global check = %+v", global)
	}
	if global.State != repair.ConfigStateUnreadable || global.Exists || global.Valid {
		t.Fatalf("global check cannot be trusted to describe the file: %+v", global)
	}
	if !strings.Contains(global.Error, "not a directory") {
		t.Fatalf("global check dropped the cause: %+v", global)
	}
	if project := report.Checks[1]; project.State != repair.ConfigStateMissing {
		t.Fatalf("project check = %+v, want missing", project)
	}
	if len(report.Applied) != 0 {
		t.Fatalf("applied = %v, want none", report.Applied)
	}
}

// Missing, invalid, and valid configs must read exactly as they did before the
// unreadable state was introduced.
func TestDoctorRepairKeepsMissingInvalidAndValidUnchanged(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	root := testenv.TempDir(t)
	path := filepath.Join(root, "reasonix.toml")

	stdout, stderr, code := runDoctorRepair(t, "--root", root)
	if code != 0 {
		t.Fatalf("missing config exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "  project  missing (defaults apply)\n             "+path+"\n") {
		t.Fatalf("missing project config rendering changed:\n%s", stdout)
	}
	if !strings.Contains(stdout, "  global   missing (defaults apply)\n             "+filepath.Join(home, "config.toml")+"\n") {
		t.Fatalf("missing global config rendering changed:\n%s", stdout)
	}

	if err := os.WriteFile(path, []byte("[broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := repair.InspectAndRepairConfig(repair.ConfigOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if report.Checks[1].State != repair.ConfigStateInvalid {
		t.Fatalf("invalid check = %+v", report.Checks[1])
	}
	wantInvalid := "  project  invalid: " + report.Checks[1].Error + "\n             " + path + "\n"
	stdout, stderr, code = runDoctorRepair(t, "--root", root)
	if code != 1 {
		t.Fatalf("invalid config exit = %d, want 1\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, wantInvalid) {
		t.Fatalf("invalid config rendering changed, want %q:\n%s", wantInvalid, stdout)
	}

	if err := os.WriteFile(path, []byte("model = \"x\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = runDoctorRepair(t, "--root", root)
	if code != 0 {
		t.Fatalf("valid config exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "  project  ok\n             "+path+"\n") {
		t.Fatalf("valid config rendering changed:\n%s", stdout)
	}
}

// --apply must not quarantine a config it could not read: the rename would
// destroy the node on the strength of a check that never ran.
func TestDoctorRepairApplyRefusesUnreadableConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a non-directory path component is reported as absence on Windows")
	}
	// Repair locks converge on the OS user cache, not REASONIX_HOME; keep the
	// test out of the real one.
	t.Setenv("HOME", testenv.TempDir(t))
	t.Setenv("XDG_CACHE_HOME", testenv.TempDir(t))
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))

	root := testenv.TempDir(t)
	node := filepath.Join(root, ".reasonix")
	body := []byte("not a directory\n")
	if err := os.WriteFile(node, body, 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runDoctorRepair(t, "--root", root, "--apply", "--project")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (repair could not run)\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "--apply refused: project config is unreadable") {
		t.Fatalf("--apply did not explain its refusal:\n%s", stdout)
	}
	if !strings.Contains(stdout, "not a directory") {
		t.Fatalf("refusal does not name the cause:\n%s", stdout)
	}
	if strings.Contains(stdout, "applied:") {
		t.Fatalf("--apply claimed a repair it could not perform:\n%s", stdout)
	}
	got, err := os.ReadFile(node)
	if err != nil {
		t.Fatalf("unreadable node changed: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("unreadable node changed:\n%s", got)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != ".reasonix" {
		t.Fatalf("repair wrote beside the unreadable target: %v", entries)
	}
}

func TestDoctorRepairApplyLeavesUnreadableFileInPlace(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission bits are not enforced here")
	}
	t.Setenv("HOME", testenv.TempDir(t))
	t.Setenv("XDG_CACHE_HOME", testenv.TempDir(t))
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	path := filepath.Join(home, "config.toml")
	if err := os.WriteFile(path, []byte("model = \"x\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	stdout, stderr, code := runDoctorRepair(t, "--root", testenv.TempDir(t), "--apply")
	if code != 1 {
		t.Fatalf("exit = %d, want 1\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "--apply refused: global config is unreadable") {
		t.Fatalf("--apply did not explain its refusal:\n%s", stdout)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("unreadable config was moved: %v", err)
	}
	if info.Mode().Perm() != 0 || info.Size() == 0 {
		t.Fatalf("unreadable config changed: %v", info.Mode())
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.toml" {
		t.Fatalf("apply wrote beside the unreadable config: %v", entries)
	}
}
