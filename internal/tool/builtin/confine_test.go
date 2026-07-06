package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/sandbox"
)

func TestWithin(t *testing.T) {
	root := filepath.FromSlash("/work/proj")
	cases := []struct {
		path string
		want bool
	}{
		{filepath.FromSlash("/work/proj"), true},           // the root itself
		{filepath.FromSlash("/work/proj/a/b.go"), true},    // nested
		{filepath.FromSlash("/work/proj/../proj/x"), true}, // normalises back inside
		{filepath.FromSlash("/work/other"), false},         // sibling
		{filepath.FromSlash("/work/proj-2"), false},        // prefix collision, not within
		{filepath.FromSlash("/etc/passwd"), false},         // elsewhere
		{filepath.FromSlash("/work"), false},               // parent
	}
	for _, c := range cases {
		if got := within(root, filepath.Clean(c.path)); got != c.want {
			t.Errorf("within(%q, %q) = %v, want %v", root, c.path, got, c.want)
		}
	}
}

func TestConfineUnconfinedWhenNoRoots(t *testing.T) {
	if err := confine(nil, "/anywhere/at/all"); err != nil {
		t.Errorf("empty roots should be unconfined, got %v", err)
	}
}

func TestConfineInsideAndOutside(t *testing.T) {
	root := t.TempDir()
	roots := realRoots([]string{root})

	if err := confine(roots, filepath.Join(root, "src", "main.go")); err != nil {
		t.Errorf("path inside root rejected: %v", err)
	}
	// A sibling of the root and a parent escape must both be refused.
	if err := confine(roots, filepath.Join(root, "..", "escape.txt")); err == nil {
		t.Error("parent-escape path accepted, want error")
	}
	if err := confine(roots, filepath.Join(filepath.Dir(root), "neighbour", "x")); err == nil {
		t.Error("sibling path accepted, want error")
	}
}

func TestConfineRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	// A symlinked directory inside the root pointing outside must not become a
	// tunnel: a write "within" the link still resolves outside the root.
	link := filepath.Join(root, "out")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	roots := realRoots([]string{root})
	if err := confine(roots, filepath.Join(link, "evil.txt")); err == nil {
		t.Error("write through symlinked dir escaped the root, want error")
	}
	// A normal file under the real root still passes.
	if err := confine(roots, filepath.Join(root, "ok.txt")); err != nil {
		t.Errorf("legit path rejected: %v", err)
	}
}

func TestWriteFileConfinement(t *testing.T) {
	root := t.TempDir()
	w := writeFile{roots: realRoots([]string{root})}

	// Inside: written.
	in := filepath.Join(root, "a", "in.txt")
	args, _ := json.Marshal(map[string]string{"path": in, "content": "hi"})
	if _, err := w.Execute(context.Background(), args); err != nil {
		t.Fatalf("write inside root failed: %v", err)
	}
	if _, err := os.Stat(in); err != nil {
		t.Errorf("file not created inside root: %v", err)
	}

	// Outside: refused, and the file must not be created.
	out := filepath.Join(t.TempDir(), "out.txt")
	args, _ = json.Marshal(map[string]string{"path": out, "content": "nope"})
	if _, err := w.Execute(context.Background(), args); err == nil {
		t.Error("write outside root should error")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Error("file outside root must not be created")
	}
}

func TestWriteFileDefaultRootsDenyUserConfigUnlessAllowed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))

	project := filepath.Join(home, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	w := writeFile{roots: realRoots(cfg.WriteRootsForRoot(project))}

	userConfig := config.UserConfigPath()
	args, _ := json.Marshal(map[string]string{
		"path":    userConfig,
		"content": "default_model = \"deepseek\"\n",
	})
	if _, err := w.Execute(context.Background(), args); err == nil {
		t.Fatalf("write user config should be denied by default")
	}
	if _, err := os.Stat(userConfig); !os.IsNotExist(err) {
		t.Fatalf("user config must not be created by default, stat err=%v", err)
	}

	cfg.Sandbox.AllowWrite = []string{filepath.Dir(userConfig)}
	w = writeFile{roots: realRoots(cfg.WriteRootsForRoot(project))}
	if _, err := w.Execute(context.Background(), args); err != nil {
		t.Fatalf("write user config should be allowed with allow_write: %v", err)
	}
	if _, err := os.Stat(userConfig); err != nil {
		t.Fatalf("user config was not created with allow_write: %v", err)
	}
}

func TestBashSandboxConfinement(t *testing.T) {
	if !sandbox.Available() {
		t.Skip("OS sandbox not available")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	work, err := os.MkdirTemp(home, ".reasonix-bashsb-*")
	if err != nil {
		t.Skipf("cannot create work dir under home: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(work) })
	t.Chdir(work)
	var timeout []time.Duration
	if runtime.GOOS == "windows" {
		wait := 20 * time.Second
		t.Setenv("WINDOWS_SANDBOX_WAIT_MS", fmt.Sprint(wait.Milliseconds()))
		timeout = []time.Duration{wait}
	}
	spec := sandbox.Spec{Mode: "enforce", WriteRoots: []string{work}, Network: true}
	if runtime.GOOS == "windows" {
		spec.Shell = sandbox.ResolveShell("powershell", "", nil)
	}
	b := ConfineBash(spec, timeout...)

	// Writing inside the root works; writing to a sibling under $HOME is denied
	// by the sandbox the bash tool wrapped the command in.
	inCommand := "echo hi > " + filepath.Join(work, "in.txt")
	if runtime.GOOS == "windows" {
		inCommand = "Set-Content -LiteralPath " + psQuoteForBuiltinTest(filepath.Join(work, "in.txt")) + " -Value hi"
	}
	inArgs, _ := json.Marshal(map[string]string{"command": inCommand})
	if _, err := b.Execute(context.Background(), inArgs); err != nil {
		t.Fatalf("bash write inside root failed: %v", err)
	}
	outPath := filepath.Join(home, ".reasonix-bashsb-escape.txt")
	t.Cleanup(func() { os.Remove(outPath) })
	outCommand := "echo nope > " + outPath
	if runtime.GOOS == "windows" {
		outCommand = "Set-Content -LiteralPath " + psQuoteForBuiltinTest(outPath) + " -Value nope"
	}
	outArgs, _ := json.Marshal(map[string]string{"command": outCommand})
	if _, err := b.Execute(context.Background(), outArgs); err == nil {
		t.Error("bash write outside the workspace should be denied by the sandbox")
	}
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Error("escaping write must not create the file")
	}
}

func psQuoteForBuiltinTest(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func TestBashEnforceRejectsWhenSandboxUnavailable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native Windows sandbox availability is helper-backed and independent of PATH")
	}
	t.Setenv("PATH", t.TempDir())

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b := bash{
		sb: sandbox.Spec{
			Mode:       "enforce",
			WriteRoots: []string{t.TempDir()},
		},
		shell: sandbox.Shell{Kind: sandbox.ShellBash, Path: exe},
	}

	args, _ := json.Marshal(map[string]string{"command": "ignored"})
	out, err := b.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("bash should reject enforce mode when the OS sandbox is unavailable")
	}
	if !strings.Contains(err.Error(), "bash sandbox requested but unavailable") {
		t.Fatalf("error = %q, want sandbox unavailable", err)
	}
	if out != "" {
		t.Fatalf("output = %q, want no command execution", out)
	}
}

func TestUnconfinedWriterWritesAnywhere(t *testing.T) {
	// A zero-value writer (roots nil, as registered at init) is unconfined.
	out := filepath.Join(t.TempDir(), "free.txt")
	args, _ := json.Marshal(map[string]string{"path": out, "content": "ok"})
	if _, err := (writeFile{}).Execute(context.Background(), args); err != nil {
		t.Fatalf("unconfined write failed: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("unconfined writer did not write: %v", err)
	}
}

// --- confineRead & ConfineReaders ---

func TestConfineReadEmpty(t *testing.T) {
	if confineRead(nil, "/anywhere") {
		t.Error("empty forbidRoots should be unconfined")
	}
}

func TestConfineReadInsideAndOutside(t *testing.T) {
	root := t.TempDir()
	forbidRoots := realRoots([]string{root})

	if !confineRead(forbidRoots, filepath.Join(root, "secret", "key.pem")) {
		t.Error("path inside forbid root should be forbidden")
	}
	// A path outside must pass.
	if confineRead(forbidRoots, filepath.Join(t.TempDir(), "ok.txt")) {
		t.Error("path outside forbid root should not be forbidden")
	}
}

func TestConfineReadBlocksReadFile(t *testing.T) {
	forbidDir := t.TempDir()
	secretPath := filepath.Join(forbidDir, "secret.txt")
	if err := os.WriteFile(secretPath, []byte("classified"), 0o644); err != nil {
		t.Fatal(err)
	}
	forbidRoots := realRoots([]string{forbidDir})
	rf := readFile{forbidRoots: forbidRoots}
	args, _ := json.Marshal(map[string]string{"path": secretPath})
	_, err := rf.Execute(context.Background(), args)
	if err == nil {
		t.Error("read_file should refuse a forbid-read path")
	}
	if _, ok := err.(*os.PathError); !ok {
		t.Errorf("read_file forbid-read error should be *os.PathError, got %T: %v", err, err)
	}
	// Unconfined (nil forbidRoots) should work.
	rfUnconfined := readFile{}
	if _, err := rfUnconfined.Execute(context.Background(), args); err != nil {
		t.Errorf("unconfined read_file should work: %v", err)
	}
}

// --- grep forbid-read ---

func TestConfineReadBlocksGrepFile(t *testing.T) {
	forbidDir := t.TempDir()
	secretPath := filepath.Join(forbidDir, "secret.txt")
	if err := os.WriteFile(secretPath, []byte("needle in a haystack"), 0o644); err != nil {
		t.Fatal(err)
	}
	forbidRoots := realRoots([]string{forbidDir})
	g := grepTool{forbidRoots: forbidRoots}
	args, _ := json.Marshal(map[string]string{"pattern": "needle", "path": secretPath})
	_, err := g.Execute(context.Background(), args)
	if err == nil {
		t.Error("grep on a forbid-read file should error, not return (no matches)")
	}
	if _, ok := err.(*os.PathError); !ok {
		t.Errorf("grep forbid-read error should be *os.PathError, got %T: %v", err, err)
	}
	// Unconfined (nil forbidRoots) should work.
	gUnconfined := grepTool{}
	if out, err := gUnconfined.Execute(context.Background(), args); err != nil {
		t.Errorf("unconfined grep should work: %v", err)
	} else if out == "(no matches)" {
		t.Error("unconfined grep should find the needle")
	}
}

func TestConfineReadBlocksNativeGrepDirectoryRoot(t *testing.T) {
	root := t.TempDir()
	forbidDir := filepath.Join(root, "secret")
	secretPath := filepath.Join(forbidDir, "secret.txt")
	if err := os.MkdirAll(forbidDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretPath, []byte("needle in a haystack"), 0o644); err != nil {
		t.Fatal(err)
	}

	g := grepTool{workDir: root, forbidRoots: realRoots([]string{forbidDir})}
	out, err := g.Execute(context.Background(), argsJSON(t, map[string]any{"pattern": "needle", "path": "secret"}))
	if err != nil {
		t.Fatalf("grep forbidden directory should look empty, got error: %v", err)
	}
	if out != "(no matches)" {
		t.Fatalf("grep forbidden directory = %q, want (no matches)", out)
	}
}

func TestConfineReadFiltersPlainGlobMatches(t *testing.T) {
	root := t.TempDir()
	forbidDir := filepath.Join(root, "secret")
	if err := os.MkdirAll(forbidDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(forbidDir, "secret.go"), []byte("package secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	g := globTool{workDir: root, forbidRoots: realRoots([]string{forbidDir})}
	out, err := g.Execute(context.Background(), argsJSON(t, map[string]any{"pattern": "secret/*.go"}))
	if err != nil {
		t.Fatalf("glob forbidden directory: %v", err)
	}
	if out != "(no matches)" {
		t.Fatalf("glob leaked forbidden paths:\n%s", out)
	}
}
