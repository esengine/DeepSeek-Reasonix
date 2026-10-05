package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func reasonsFor(cfg *Config, key string) []IgnoredProjectReason {
	var out []IgnoredProjectReason
	for _, ig := range cfg.IgnoredProjectSettings() {
		if ig.Key == key {
			out = append(out, ig.Reason)
		}
	}
	return out
}

// A shell or ripgrep elsewhere on the machine waits for approval; one inside the
// checkout is refused outright, since what runs there can rewrite it.
func TestProjectShellAndRipgrepWaitForApproval(t *testing.T) {
	home := testenv.TempDir(t)
	root := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	shell := installedPath("sh")
	project := "[tools.shell]\npath = " + tomlQuote(shell) + "\n[tools.search]\nrg_path = \"tools/rg\"\n"
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte(project), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tools.Shell.Path != "" || cfg.Tools.Search.RgPath != "" {
		t.Fatalf("tools = %+v, want the project's programs held back", cfg.Tools)
	}
	if !slices.Equal(reasonsFor(cfg, "tools.shell.path"), []IgnoredProjectReason{ProjectAwaitingApproval}) {
		t.Fatalf("shell reasons = %v", reasonsFor(cfg, "tools.shell.path"))
	}
	if !slices.Equal(reasonsFor(cfg, "tools.search.rg_path"), []IgnoredProjectReason{ProjectProgramWritable}) {
		t.Fatalf("rg reasons = %v", reasonsFor(cfg, "tools.search.rg_path"))
	}
	pending := cfg.PendingProjectPrograms()
	if len(pending) != 1 {
		t.Fatalf("pending = %+v, want only the shell", pending)
	}
	if err := NewProjectProgramStore(home).Approve(root, pending...); err != nil {
		t.Fatal(err)
	}
	if cfg, err = LoadForRootReadOnly(root); err != nil {
		t.Fatal(err)
	}
	if cfg.Tools.Shell.Path != shell || cfg.Tools.Search.RgPath != "" {
		t.Fatalf("tools = %+v, want the approved shell and still no workspace ripgrep", cfg.Tools)
	}
}

// Changing a server's declaration, or a workspace file it names, needs a new
// approval; a changed file after loading is caught before the server starts.
func TestProjectLanguageServerFilesAreCheckedAgainBeforeStart(t *testing.T) {
	home := testenv.TempDir(t)
	root := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	server := filepath.Join(root, "bin", "pyls")
	if err := os.MkdirAll(filepath.Dir(server), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(server, []byte("one"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("[lsp.servers.python]\ncommand = \"./bin/pyls\"\nenv = { PYTHONPATH = \"x\" }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	held, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if p := held.PendingProjectPrograms(); len(p) != 1 || !strings.Contains(p[0].Detail, "PYTHONPATH=x") || len(p[0].Files) != 1 {
		t.Fatalf("pending = %+v, want the env shown and the named file covered", p)
	}
	cfg := approveWorkspacePrograms(t, root)
	verify := cfg.ProjectProgramVerifier(ProjectProgramLSP, "python")
	if verify == nil || verify() != nil {
		t.Fatal("an approved, unchanged server does not verify")
	}
	if err := os.WriteFile(server, []byte("two"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := verify(); !errors.Is(err, ErrProjectProgramChanged) {
		t.Fatalf("verify after the change = %v, want ErrProjectProgramChanged", err)
	}
}

func TestProjectLanguageServerWaitsForApproval(t *testing.T) {
	home := testenv.TempDir(t)
	root := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	user := "[lsp.servers.go]\ncommand = \"gopls\"\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(user), 0o600); err != nil {
		t.Fatal(err)
	}
	write := func(args string) {
		project := "[lsp.servers.python]\ncommand = \"./bin/pyls\"\nargs = [" + args + "]\n"
		if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte(project), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(`"--stdio"`)
	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.LSP.Servers["python"]; ok {
		t.Fatalf("servers = %+v, want the project's server held back", cfg.LSP.Servers)
	}
	if cfg.LSP.Servers["go"].Command != "gopls" {
		t.Fatalf("servers = %+v, want the user's server kept", cfg.LSP.Servers)
	}
	if err := NewProjectProgramStore(home).Approve(root, cfg.PendingProjectPrograms()...); err != nil {
		t.Fatal(err)
	}
	if cfg, _ = LoadForRootReadOnly(root); cfg.LSP.Servers["python"].Command != "./bin/pyls" {
		t.Fatalf("servers = %+v, want the approved server", cfg.LSP.Servers)
	}
	write(`"--stdio", "--log=/tmp/x"`)
	if cfg, _ = LoadForRootReadOnly(root); cfg.LSP.Servers["python"].Command != "" {
		t.Fatalf("servers = %+v, want a changed declaration held back again", cfg.LSP.Servers)
	}
}

func TestProjectProgramsFailClosedOnAnUnreadableRecord(t *testing.T) {
	home := testenv.TempDir(t)
	root := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, projectProgramsFilename), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("[tools.shell]\npath = "+tomlQuote(installedPath("evil"))+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tools.Shell.Path != "" {
		t.Fatalf("shell = %q, want it held back", cfg.Tools.Shell.Path)
	}
	if !slices.Equal(reasonsFor(cfg, "tools.shell.path"), []IgnoredProjectReason{ProjectApprovalUnavailable}) {
		t.Fatalf("reasons = %v, want the unreadable record named", reasonsFor(cfg, "tools.shell.path"))
	}
	if _, err := NewProjectProgramStore(home).Approved(root, ProjectProgram{}); !errors.Is(err, ErrProjectProgramsUnavailable) {
		t.Fatalf("err = %v, want ErrProjectProgramsUnavailable", err)
	}
}

func TestWorkspaceGrantsApplyToTheirWorkspaceOnly(t *testing.T) {
	home := testenv.TempDir(t)
	root := testenv.TempDir(t)
	other := testenv.TempDir(t)
	extra := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	store := NewProjectGrantStore(home)
	if err := store.Update(root, func(g ProjectGrant) (ProjectGrant, error) {
		g.Allow = append(g.Allow, "Bash(go test:*)")
		g.AllowWrite = append(g.AllowWrite, extra)
		return g, nil
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(cfg.Permissions.Allow, "Bash(go test:*)") || !slices.Contains(cfg.Sandbox.AllowWrite, extra) {
		t.Fatalf("permissions = %+v sandbox = %+v, want the workspace's grants", cfg.Permissions, cfg.Sandbox)
	}
	elsewhere, err := LoadForRootReadOnly(other)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(elsewhere.Permissions.Allow, "Bash(go test:*)") || slices.Contains(elsewhere.Sandbox.AllowWrite, extra) {
		t.Fatalf("grants leaked to another workspace: %+v %+v", elsewhere.Permissions, elsewhere.Sandbox)
	}
	if err := store.Update(root, func(g ProjectGrant) (ProjectGrant, error) {
		g.Allow = append(g.Allow, "(x)")
		return g, nil
	}); err == nil {
		t.Fatal("an unparseable rule was stored")
	}
}

func TestProjectBrowserAndLayaWaitForApproval(t *testing.T) {
	home := testenv.TempDir(t)
	root := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	browser, python := installedPath("evil-browser"), installedPath("evil-python")
	project := "[browser]\nexecutable = " + tomlQuote(browser) + "\n[tools.system_one.laya]\npython = " + tomlQuote(python) + "\n"
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte(project), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Browser.Executable != "" || cfg.Tools.SystemOne.Laya.Python != "" || len(cfg.PendingProjectPrograms()) != 2 {
		t.Fatalf("browser = %+v laya = %q pending = %+v, want both held back", cfg.Browser, cfg.Tools.SystemOne.Laya.Python, cfg.PendingProjectPrograms())
	}
	if err := NewProjectProgramStore(home).Approve(root, cfg.PendingProjectPrograms()...); err != nil {
		t.Fatal(err)
	}
	if cfg, _ = LoadForRootReadOnly(root); cfg.Browser.Executable != browser || cfg.Tools.SystemOne.Laya.Python != python {
		t.Fatalf("approved programs not applied: %+v", cfg.Browser)
	}
}

// approveWorkspacePrograms approves what root's files name, as a person running
// `reasonix trust` there would, and returns the config loaded afterwards.
func approveWorkspacePrograms(t *testing.T, root string) *Config {
	t.Helper()
	cfg, err := LoadForRoot(root)
	if err != nil {
		t.Fatalf("LoadForRoot: %v", err)
	}
	if pending := cfg.PendingProjectPrograms(); len(pending) > 0 {
		if err := NewProjectProgramStore(cfg.Roots().Home()).Approve(root, pending...); err != nil {
			t.Fatal(err)
		}
		if cfg, err = LoadForRoot(root); err != nil {
			t.Fatalf("LoadForRoot: %v", err)
		}
	}
	return cfg
}

// Only the user turns the local Laya runtime on: it starts Python on the host.
func TestProjectCannotTurnOnTheLocalLayaRuntime(t *testing.T) {
	cfg, _ := loadScoped(t, "", "[tools.system_one.laya]\nlocal = true\n")
	if cfg.Tools.SystemOne.Laya.Local || !slices.Contains(ignoredKeys(cfg), "tools.system_one.laya.local") {
		t.Fatalf("laya = %+v ignored = %v, want local left off and reported", cfg.Tools.SystemOne.Laya, ignoredKeys(cfg))
	}
}

// installedPath is where an installed program could live: outside the
// workspace and outside anywhere the bash jail lets commands write.
func installedPath(name string) string {
	return filepath.Join(filepath.VolumeName(os.TempDir())+string(filepath.Separator), "opt", "reasonix-test", name)
}

func TestProjectProgramInATemporaryDirectoryIsRefused(t *testing.T) {
	shell := filepath.Join(os.TempDir(), "evil-sh")
	cfg, _ := loadScoped(t, "", "[tools.shell]\npath = "+tomlQuote(shell)+"\n")
	if cfg.Tools.Shell.Path != "" || !slices.Equal(reasonsFor(cfg, "tools.shell.path"), []IgnoredProjectReason{ProjectProgramWritable}) {
		t.Fatalf("shell = %q reasons = %v, want it refused", cfg.Tools.Shell.Path, reasonsFor(cfg, "tools.shell.path"))
	}
}

// A server's bare file argument is covered, resolved where the server runs.
func TestProjectLanguageServerBareFileArgumentIsCovered(t *testing.T) {
	home := testenv.TempDir(t)
	root := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	if err := os.WriteFile(filepath.Join(root, "server.js"), []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("[lsp.servers.js]\ncommand = \"node\"\nargs = [\"server.js\", \"--require=./boot.js\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := approveWorkspacePrograms(t, root)
	verify := cfg.ProjectProgramVerifier(ProjectProgramLSP, "js")
	if verify == nil || verify() != nil {
		t.Fatal("an approved, unchanged server does not verify")
	}
	if err := os.WriteFile(filepath.Join(root, "boot.js"), []byte("planted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verify(); !errors.Is(err, ErrProjectProgramChanged) {
		t.Fatalf("verify after a flag's file appeared = %v", err)
	}
}

func TestProjectProgramInAToolchainCacheIsRefused(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || runtime.GOOS == "windows" {
		t.Skip("no jailed toolchain caches here")
	}
	rg := filepath.Join(home, ".cargo", "bin", "rg")
	cfg, _ := loadScoped(t, "", "[tools.search]\nrg_path = "+tomlQuote(rg)+"\n")
	if cfg.Tools.Search.RgPath != "" || !slices.Equal(reasonsFor(cfg, "tools.search.rg_path"), []IgnoredProjectReason{ProjectProgramWritable}) {
		t.Fatalf("rg = %q reasons = %v, want the cache path refused", cfg.Tools.Search.RgPath, reasonsFor(cfg, "tools.search.rg_path"))
	}
}
