package config

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

// loadScoped writes the user and project files and loads the workspace.
func loadScoped(t *testing.T, user, project string) (*Config, string) {
	t.Helper()
	home := testenv.TempDir(t)
	root := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(user), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte(project), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, root
}

func ignoredKeys(cfg *Config) []string {
	var keys []string
	for _, ig := range cfg.IgnoredProjectSettings() {
		keys = append(keys, ig.Key)
	}
	return keys
}

func realDir(t *testing.T, dir string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

func TestProjectCannotTurnBashSandboxOff(t *testing.T) {
	for name, user := range map[string]string{
		"platform default": "",
		"explicit enforce": "[sandbox]\nbash = \"enforce\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			cfg, _ := loadScoped(t, user, "[sandbox]\nbash = \"off\"\n")
			if cfg.Sandbox.Bash == "off" || cfg.BashModeForGOOS("linux") != "enforce" {
				t.Fatalf("bash = %q, want the user's jailed mode kept", cfg.Sandbox.Bash)
			}
			if !slices.Contains(ignoredKeys(cfg), "sandbox.bash") {
				t.Fatalf("ignored = %v, want sandbox.bash reported", ignoredKeys(cfg))
			}
		})
	}
}

func TestProjectMayTurnBashSandboxOn(t *testing.T) {
	cfg, _ := loadScoped(t, "[sandbox]\nbash = \"off\"\n", "[sandbox]\nbash = \"enforce\"\n")
	if cfg.Sandbox.Bash != "enforce" {
		t.Fatalf("bash = %q, want the project's narrowing to enforce", cfg.Sandbox.Bash)
	}
	if len(cfg.IgnoredProjectSettings()) != 0 {
		t.Fatalf("ignored = %v, want nothing", ignoredKeys(cfg))
	}
}

func TestProjectCannotOpenSandboxNetwork(t *testing.T) {
	cfg, _ := loadScoped(t, "[sandbox]\nnetwork = false\n", "[sandbox]\nnetwork = true\n")
	if cfg.Sandbox.Network {
		t.Fatal("network = true, want the user's false kept")
	}
	if !slices.Contains(ignoredKeys(cfg), "sandbox.network") {
		t.Fatalf("ignored = %v, want sandbox.network reported", ignoredKeys(cfg))
	}
	closed, _ := loadScoped(t, "", "[sandbox]\nnetwork = false\n")
	if closed.Sandbox.Network {
		t.Fatal("network = true, want the project's narrowing to false")
	}
}

func TestProjectForbidReadOnlyAdds(t *testing.T) {
	cfg, _ := loadScoped(t, "[sandbox]\nforbid_read = [\"/secret/a\"]\n", "[sandbox]\nforbid_read = [\"/secret/b\"]\n")
	for _, want := range []string{"/secret/a", "/secret/b"} {
		if !slices.Contains(cfg.Sandbox.ForbidRead, want) {
			t.Fatalf("forbid_read = %v, want %s in the union", cfg.Sandbox.ForbidRead, want)
		}
	}
}

func TestProjectAllowWriteStaysInsideWorkspace(t *testing.T) {
	outside := testenv.TempDir(t)
	project := "[sandbox]\nallow_write = [\"build/out\", \"/\", \"../\", " + tomlQuote(outside) + "]\n"
	cfg, root := loadScoped(t, "[sandbox]\nallow_write = [\"/user/extra\"]\n", project)
	want := filepath.Join(realDir(t, root), "build", "out")
	if !slices.Contains(cfg.Sandbox.AllowWrite, "/user/extra") {
		t.Fatalf("allow_write = %v, want the user's entry kept", cfg.Sandbox.AllowWrite)
	}
	if !slices.Contains(cfg.Sandbox.AllowWrite, want) {
		t.Fatalf("allow_write = %v, want %s", cfg.Sandbox.AllowWrite, want)
	}
	if len(cfg.Sandbox.AllowWrite) != 2 {
		t.Fatalf("allow_write = %v, want only the user entry and the in-workspace one", cfg.Sandbox.AllowWrite)
	}
	if n := countKey(cfg, "sandbox.allow_write"); n != 3 {
		t.Fatalf("allow_write refusals = %d, want 3 (%v)", n, cfg.IgnoredProjectSettings())
	}
}

func TestProjectAllowWriteSymlinkOutOfWorkspaceIsRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	home := testenv.TempDir(t)
	root := testenv.TempDir(t)
	outside := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	project := "[sandbox]\nallow_write = [\"link/deeper\"]\nworkspace_root = \"link\"\n"
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte(project), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Sandbox.AllowWrite) != 0 || cfg.Sandbox.WorkspaceRoot != "" {
		t.Fatalf("sandbox = %+v, want the symlinked escape refused", cfg.Sandbox)
	}
}

func TestProjectWorkspaceRootMustBeInsideWorkspace(t *testing.T) {
	for name, value := range map[string]string{
		"filesystem root": "/",
		"parent":          "..",
		"sibling prefix":  "../" + "x-sibling",
	} {
		t.Run(name, func(t *testing.T) {
			cfg, _ := loadScoped(t, "", "[sandbox]\nworkspace_root = "+tomlQuote(value)+"\n")
			if cfg.Sandbox.WorkspaceRoot != "" {
				t.Fatalf("workspace_root = %q, want the user's (unset) kept", cfg.Sandbox.WorkspaceRoot)
			}
			if !slices.Contains(ignoredKeys(cfg), "sandbox.workspace_root") {
				t.Fatalf("ignored = %v, want sandbox.workspace_root reported", ignoredKeys(cfg))
			}
		})
	}
	cfg, root := loadScoped(t, "", "[sandbox]\nworkspace_root = \"src\"\n")
	if want := filepath.Join(realDir(t, root), "src"); cfg.Sandbox.WorkspaceRoot != want {
		t.Fatalf("workspace_root = %q, want the subdirectory %q", cfg.Sandbox.WorkspaceRoot, want)
	}
}

// A root whose name extends the workspace's is not inside it.
func TestProjectWorkspaceRootSharingANamePrefixIsOutside(t *testing.T) {
	home := testenv.TempDir(t)
	base := testenv.TempDir(t)
	root := filepath.Join(base, "repo")
	evil := filepath.Join(base, "repo-evil")
	for _, dir := range []string{root, evil} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("REASONIX_HOME", home)
	project := "[sandbox]\nworkspace_root = " + tomlQuote(evil) + "\nallow_write = [" + tomlQuote(evil) + "]\n"
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte(project), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sandbox.WorkspaceRoot != "" || len(cfg.Sandbox.AllowWrite) != 0 {
		t.Fatalf("sandbox = %+v, want the sibling refused", cfg.Sandbox)
	}
}

func TestProjectPermissionsOnlyNarrow(t *testing.T) {
	user := "[permissions]\nmode = \"ask\"\ndeny = [\"Bash(rm -rf*)\"]\nask = [\"Edit(src/**)\"]\nallow = [\"Bash(go test:*)\"]\n"
	project := "[permissions]\nmode = \"allow\"\nallow_dynamic_bash = true\nallow = [\"Bash\"]\ndeny = [\"Bash(git push*)\"]\nask = [\"Write\"]\n"
	cfg, _ := loadScoped(t, user, project)
	p := cfg.Permissions
	if p.Mode != "ask" || p.AllowDynamicBash {
		t.Fatalf("mode = %q dynamic = %v, want the user's ask and false", p.Mode, p.AllowDynamicBash)
	}
	if !slices.Equal(p.Allow, []string{"Bash(go test:*)"}) {
		t.Fatalf("allow = %v, want only the user's rule", p.Allow)
	}
	if !slices.Equal(p.Deny, []string{"Bash(rm -rf*)", "Bash(git push*)"}) {
		t.Fatalf("deny = %v, want the union", p.Deny)
	}
	if !slices.Equal(p.Ask, []string{"Edit(src/**)", "Write"}) {
		t.Fatalf("ask = %v, want the union", p.Ask)
	}
	for _, key := range []string{"permissions.mode", "permissions.allow", "permissions.allow_dynamic_bash"} {
		if !slices.Contains(ignoredKeys(cfg), key) {
			t.Fatalf("ignored = %v, want %s reported", ignoredKeys(cfg), key)
		}
	}
}

func TestProjectCannotClearUserDeny(t *testing.T) {
	cfg, _ := loadScoped(t, "[permissions]\ndeny = [\"Bash(curl*)\"]\n", "[permissions]\ndeny = []\n")
	if !slices.Contains(cfg.Permissions.Deny, "Bash(curl*)") {
		t.Fatalf("deny = %v, want the user's rule kept", cfg.Permissions.Deny)
	}
}

// A project file may not skip the memory confirmation: that would widen what the
// agent persists in that clone without asking, which is the user's call.
func TestProjectCannotSkipTheRememberConfirmation(t *testing.T) {
	cfg, _ := loadScoped(t, "[memory]\nauto_confirm_project_remember = false\n", "[memory]\nauto_confirm_project_remember = true\n")
	if cfg.Memory.AutoConfirmProjectRemember {
		t.Fatal("a project file skipped the memory confirmation")
	}
	if !slices.Contains(ignoredKeys(cfg), "memory.auto_confirm_project_remember") {
		t.Fatalf("ignored = %v, want memory.auto_confirm_project_remember reported", ignoredKeys(cfg))
	}
}

func TestProjectCannotChooseToolApprovalPosture(t *testing.T) {
	cfg, _ := loadScoped(t, "", "[desktop]\ndefault_tool_approval_mode = \"yolo\"\n")
	if got := cfg.DesktopDefaultToolApprovalMode(); got == "yolo" {
		t.Fatalf("approval mode = %q, want the user's default", got)
	}
	if !slices.Contains(ignoredKeys(cfg), "desktop.default_tool_approval_mode") {
		t.Fatalf("ignored = %v, want the posture reported", ignoredKeys(cfg))
	}
}

// Auto-submit commits a user's ask answers without a confirmation, so it is the
// user's preference: a project file must not turn it on.
func TestProjectCannotTurnOnAutoSubmit(t *testing.T) {
	cfg, _ := loadScoped(t, "", "auto_submit = true\n")
	if cfg.AutoSubmit {
		t.Fatal("auto_submit = true, want the user's false kept")
	}
	if !slices.Contains(ignoredKeys(cfg), "auto_submit") {
		t.Fatalf("ignored = %v, want auto_submit reported", ignoredKeys(cfg))
	}
}

func TestUserAutoSubmitSurvivesProjectValue(t *testing.T) {
	cfg, _ := loadScoped(t, "auto_submit = true\n", "auto_submit = false\n")
	if !cfg.AutoSubmit {
		t.Fatal("auto_submit = false, want the user's true kept")
	}
}

// Equal values are not a widening and deserve no notice.
func TestProjectRepeatingUserValuesIsQuiet(t *testing.T) {
	same := "[sandbox]\nbash = \"enforce\"\nnetwork = true\n[permissions]\nmode = \"ask\"\n"
	cfg, _ := loadScoped(t, same, same)
	if len(cfg.IgnoredProjectSettings()) != 0 {
		t.Fatalf("ignored = %v, want nothing", cfg.IgnoredProjectSettings())
	}
}

func countKey(cfg *Config, key string) int {
	n := 0
	for _, ig := range cfg.IgnoredProjectSettings() {
		if ig.Key == key {
			n++
		}
	}
	return n
}

func tomlQuote(s string) string {
	return "'" + s + "'"
}

// A project value that expands to a literal "${B}" must not be expanded again
// later from the project's own .env.
func TestProjectPathsAreNotExpandedTwice(t *testing.T) {
	home := testenv.TempDir(t)
	root := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	up := strings.Repeat("../", 12)
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("B="+up+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	project := "[sandbox]\nallow_write = [\"${REASONIX_TEST_UNSET_A:-$}{B}\"]\nworkspace_root = \"${REASONIX_TEST_UNSET_A:-$}{B}\"\n"
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte(project), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	ws := realDir(t, root)
	for _, dir := range cfg.WriteRootsForRoot(root) {
		abs, err := evalSymlinksAllowMissing(dir)
		if err != nil {
			t.Fatal(err)
		}
		if rel, err := filepath.Rel(ws, abs); err != nil || strings.HasPrefix(rel, "..") {
			t.Fatalf("write roots = %v, want every root inside %s", cfg.WriteRootsForRoot(root), ws)
		}
	}
}

// A user's own ${VAR} never resolves from a workspace .env.
func TestUserSandboxPathsIgnoreTheWorkspaceEnv(t *testing.T) {
	home := testenv.TempDir(t)
	root := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[sandbox]\nforbid_read = [\"${RX_TEST_UNSET_SECRETS_DIR:-/secrets}\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("RX_TEST_UNSET_SECRETS_DIR=/nothing-here\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	roots := cfg.ForbidReadRootsForRoot(root)
	if len(roots) != 1 || filepath.Base(roots[0]) != "secrets" {
		t.Fatalf("forbid_read = %v, want the user's default /secrets", roots)
	}
}

func TestProjectCannotChooseTheNetworkProxy(t *testing.T) {
	cfg, _ := loadScoped(t, "", "[network]\nproxy_mode = \"custom\"\nproxy_url = \"http://proxy.invalid:8080\"\n")
	if cfg.Network.ProxyURL != "" || !slices.Contains(ignoredKeys(cfg), "network") {
		t.Fatalf("network = %+v ignored = %v, want the user's proxy settings", cfg.Network, ignoredKeys(cfg))
	}
}
