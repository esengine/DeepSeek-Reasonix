package gitcmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestStripDiffExternal(t *testing.T) {
	cases := []struct {
		name        string
		in          string
		wantRemoved bool
		want        string
	}{
		{"plain", "[diff]\n\texternal = difft\n[user]\n\tname = x\n", true, "[diff]\n[user]\n\tname = x\n"},
		{"no-equals", "[diff]\n\texternal\n", true, "[diff]\n"},
		{"unrelated", "[user]\n\tname = x\n", false, "[user]\n\tname = x\n"},
		{"subsection kept", "[diff \"driver\"]\n\tcommand = foo\n", false, "[diff \"driver\"]\n\tcommand = foo\n"},
		{"comment kept", "[diff]\n\t# external = x\n", false, "[diff]\n\t# external = x\n"},
	}
	for _, c := range cases {
		got, removed := stripDiffExternal(c.in)
		if removed != c.wantRemoved || got != c.want {
			t.Errorf("%s: stripDiffExternal(%q) = (%q, %v), want (%q, %v)", c.name, c.in, got, removed, c.want, c.wantRemoved)
		}
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// isolatedHome points HOME/XDG_CONFIG_HOME at fresh dirs and clears
// GIT_CONFIG_GLOBAL so the global-config resolution is deterministic.
func isolatedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv("GIT_CONFIG_GLOBAL", "")
	return home
}

func TestGlobalConfigWithoutExternalDiffFilters(t *testing.T) {
	home := isolatedHome(t)
	writeFile(t, filepath.Join(home, ".gitconfig"), "[user]\n\tname = t\n[diff]\n\texternal = difft\n[alias]\n\tco = checkout\n")
	state := t.TempDir()
	path := GlobalConfigWithoutExternalDiff(state)
	if want := filepath.Join(state, "git", "gitconfig"); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	body := readFile(t, path)
	if strings.Contains(body, "external") {
		t.Fatalf("diff.external survived the filter:\n%s", body)
	}
	if !strings.Contains(body, "co = checkout") || !strings.Contains(body, "name = t") {
		t.Fatalf("filter dropped unrelated config:\n%s", body)
	}
}

func TestGlobalConfigWithoutExternalDiffNoOverride(t *testing.T) {
	home := isolatedHome(t)
	writeFile(t, filepath.Join(home, ".gitconfig"), "[user]\n\tname = t\n")
	if got := GlobalConfigWithoutExternalDiff(t.TempDir()); got != "" {
		t.Fatalf("expected no override without diff.external, got %q", got)
	}
}

// TestGlobalConfigWithoutExternalDiffMergesXDGAndHome proves both files git
// reads are folded into one filtered copy.
func TestGlobalConfigWithoutExternalDiffMergesXDGAndHome(t *testing.T) {
	home := isolatedHome(t)
	writeFile(t, filepath.Join(home, "xdg", "git", "config"), "[user]\n\tname = xdg\n[diff]\n\texternal = difft\n")
	writeFile(t, filepath.Join(home, ".gitconfig"), "[alias]\n\tco = checkout\n")
	path := GlobalConfigWithoutExternalDiff(t.TempDir())
	if path == "" {
		t.Fatal("expected a filtered config path")
	}
	body := readFile(t, path)
	if strings.Contains(body, "external") {
		t.Fatalf("diff.external survived the merge:\n%s", body)
	}
	if !strings.Contains(body, "name = xdg") || !strings.Contains(body, "co = checkout") {
		t.Fatalf("merge dropped a source file:\n%s", body)
	}
}

// TestGlobalConfigWithoutExternalDiffRefreshesOnChange proves a newer source
// invalidates the cached copy.
func TestGlobalConfigWithoutExternalDiffRefreshesOnChange(t *testing.T) {
	home := isolatedHome(t)
	cfg := filepath.Join(home, ".gitconfig")
	writeFile(t, cfg, "[diff]\n\texternal = a\n[user]\n\tname = first\n")
	state := t.TempDir()
	if first := readFile(t, GlobalConfigWithoutExternalDiff(state)); !strings.Contains(first, "name = first") {
		t.Fatalf("first copy = %q", first)
	}
	writeFile(t, cfg, "[diff]\n\texternal = a\n[user]\n\tname = second\n")
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(cfg, future, future); err != nil {
		t.Fatal(err)
	}
	if second := readFile(t, GlobalConfigWithoutExternalDiff(state)); !strings.Contains(second, "name = second") {
		t.Fatalf("stale copy reused:\n%s", second)
	}
}

// childEnv builds a git child environment with HOME (and the other config
// locations) forced to the test's values, dropping any ambient ones so the
// child reads exactly the test's global config.
func childEnv(home string, extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "HOME", "XDG_CONFIG_HOME", "GIT_CONFIG_GLOBAL":
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "HOME="+home)
	return append(env, extra...)
}

// TestFilteredGlobalConfigRestoresInternalDiff proves the filtered copy, used
// as GIT_CONFIG_GLOBAL, neutralizes a global external diff end to end.
func TestFilteredGlobalConfigRestoresInternalDiff(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	home := isolatedHome(t)
	ext := filepath.Join(home, "ext.sh")
	writeFile(t, ext, "#!/bin/sh\necho EXTERNAL-DIFF-SENTINEL\n")
	if err := os.Chmod(ext, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, ".gitconfig"), "[diff]\n\texternal = "+ext+"\n")
	repo := t.TempDir()
	git := func(env []string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	base := childEnv(home)
	git(base, "init", "-q")
	git(base, "config", "user.email", "t@t")
	git(base, "config", "user.name", "t")
	writeFile(t, filepath.Join(repo, "f.txt"), "a\n")
	git(base, "add", "f.txt")
	git(base, "commit", "-q", "-m", "x")
	writeFile(t, filepath.Join(repo, "f.txt"), "b\n")

	if out := git(base, "diff"); !strings.Contains(out, "EXTERNAL-DIFF-SENTINEL") {
		t.Fatalf("baseline did not use the external diff:\n%s", out)
	}
	path := GlobalConfigWithoutExternalDiff(t.TempDir())
	if path == "" {
		t.Fatal("expected a filtered config path")
	}
	out := git(childEnv(home, "GIT_CONFIG_GLOBAL="+path), "diff")
	if strings.Contains(out, "EXTERNAL-DIFF-SENTINEL") {
		t.Fatalf("filtered config let the external diff run:\n%s", out)
	}
	if !strings.Contains(out, "@@") {
		t.Fatalf("filtered config did not yield a unified diff:\n%s", out)
	}
}

// TestGlobalConfigWithoutExternalDiffPrefersGitConfigGlobal proves a preexisting
// GIT_CONFIG_GLOBAL is the sole source rather than the default locations.
func TestGlobalConfigWithoutExternalDiffPrefersGitConfigGlobal(t *testing.T) {
	home := isolatedHome(t)
	writeFile(t, filepath.Join(home, ".gitconfig"), "[diff]\n\texternal = a\n")
	alt := filepath.Join(home, "alt.gitconfig")
	writeFile(t, alt, "[diff]\n\texternal = b\n[user]\n\tname = t\n")
	t.Setenv("GIT_CONFIG_GLOBAL", alt)

	path := GlobalConfigWithoutExternalDiff(t.TempDir())
	if path == "" {
		t.Fatal("expected a filtered config path")
	}
	body := readFile(t, path)
	if strings.Contains(body, "external") {
		t.Fatalf("diff.external survived:\n%s", body)
	}
	if !strings.Contains(body, "name = t") {
		t.Fatalf("GIT_CONFIG_GLOBAL was not used as the input:\n%s", body)
	}
}

// TestGlobalConfigWithoutExternalDiffInlinesIncludes proves a relative include
// is resolved against its own file and flattened into the copy.
func TestGlobalConfigWithoutExternalDiffInlinesIncludes(t *testing.T) {
	home := isolatedHome(t)
	writeFile(t, filepath.Join(home, ".gitconfig"), "[user]\n\tname = t\n[include]\n\tpath = extra.config\n")
	writeFile(t, filepath.Join(home, "extra.config"), "[diff]\n\texternal = difft\n[alias]\n\tco = checkout\n")

	path := GlobalConfigWithoutExternalDiff(t.TempDir())
	if path == "" {
		t.Fatal("expected a filtered config path")
	}
	body := readFile(t, path)
	if strings.Contains(body, "external") {
		t.Fatalf("diff.external from the included file survived:\n%s", body)
	}
	if !strings.Contains(body, "co = checkout") {
		t.Fatalf("included file was not inlined:\n%s", body)
	}
	if strings.Contains(body, "path = extra.config") {
		t.Fatalf("include directive was not flattened:\n%s", body)
	}
}

// TestGlobalConfigWithoutExternalDiffTracksIncludedMtime proves a change to an
// included file invalidates the cached copy.
func TestGlobalConfigWithoutExternalDiffTracksIncludedMtime(t *testing.T) {
	home := isolatedHome(t)
	writeFile(t, filepath.Join(home, ".gitconfig"), "[include]\n\tpath = extra.config\n")
	extra := filepath.Join(home, "extra.config")
	writeFile(t, extra, "[diff]\n\texternal = a\n[alias]\n\tco = first\n")
	state := t.TempDir()
	if first := readFile(t, GlobalConfigWithoutExternalDiff(state)); !strings.Contains(first, "co = first") {
		t.Fatalf("first copy = %q", first)
	}
	writeFile(t, extra, "[diff]\n\texternal = a\n[alias]\n\tco = second\n")
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(extra, future, future); err != nil {
		t.Fatal(err)
	}
	if second := readFile(t, GlobalConfigWithoutExternalDiff(state)); !strings.Contains(second, "co = second") {
		t.Fatalf("stale copy reused after an included file changed:\n%s", second)
	}
}

// TestGlobalConfigWithoutExternalDiffDetectsNewRoot proves a root that appears
// after the copy was cached invalidates it.
func TestGlobalConfigWithoutExternalDiffDetectsNewRoot(t *testing.T) {
	home := isolatedHome(t)
	writeFile(t, filepath.Join(home, ".gitconfig"), "[diff]\n\texternal = a\n")
	state := t.TempDir()
	GlobalConfigWithoutExternalDiff(state) // cache a copy with only ~/.gitconfig
	writeFile(t, filepath.Join(home, "xdg", "git", "config"), "[user]\n\tname = xdg\n")
	if !strings.Contains(readFile(t, GlobalConfigWithoutExternalDiff(state)), "name = xdg") {
		t.Fatal("new root not picked up")
	}
}
