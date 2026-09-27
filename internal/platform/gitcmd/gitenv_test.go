package gitcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStripExternalDiff(t *testing.T) {
	out := StripExternalDiff([]string{"PATH=/bin", "GIT_EXTERNAL_DIFF=difft", "HOME=/h"})
	if len(out) != 2 {
		t.Fatalf("StripExternalDiff = %v, want the two non-GIT_EXTERNAL_DIFF entries", out)
	}
	joined := strings.Join(out, "\n")
	if strings.Contains(joined, "GIT_EXTERNAL_DIFF") {
		t.Fatalf("GIT_EXTERNAL_DIFF survived: %v", out)
	}
	if !strings.Contains(joined, "PATH=/bin") || !strings.Contains(joined, "HOME=/h") {
		t.Fatalf("unrelated entries dropped: %v", out)
	}
}

func TestWithConfigEnvAddsConfig(t *testing.T) {
	env := WithConfigEnv([]string{"PATH=/bin"})
	joined := strings.Join(env, "\n")
	for _, want := range []string{
		"PATH=/bin",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=rebase.abbreviateCommands",
		"GIT_CONFIG_VALUE_0=false",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("WithConfigEnv missing %q:\n%s", want, joined)
		}
	}
}

func TestWithConfigEnvExtendsExistingCount(t *testing.T) {
	env := WithConfigEnv([]string{
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=core.pager",
		"GIT_CONFIG_VALUE_0=cat",
		"GIT_CONFIG_KEY_1=color.ui",
		"GIT_CONFIG_VALUE_1=false",
	})
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "GIT_CONFIG_COUNT=3") {
		t.Fatalf("existing count not extended:\n%s", joined)
	}
	if !strings.Contains(joined, "GIT_CONFIG_KEY_2=rebase.abbreviateCommands") ||
		!strings.Contains(joined, "GIT_CONFIG_VALUE_2=false") {
		t.Fatalf("new entry not appended at index 2:\n%s", joined)
	}
	if !strings.Contains(joined, "GIT_CONFIG_KEY_0=core.pager") ||
		!strings.Contains(joined, "GIT_CONFIG_VALUE_1=false") {
		t.Fatalf("existing entries not preserved:\n%s", joined)
	}
}

func TestGlobalConfigWithoutExternalDiffFilters(t *testing.T) {
	src := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(src, []byte("[user]\n\tname = A\n[diff]\n\texternal = difftool\n"), 0o600); err != nil {
		t.Fatalf("write source config: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", src)

	got := GlobalConfigWithoutExternalDiff(t.TempDir())
	if got == "" {
		t.Fatal("expected a filtered copy path")
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("read filtered copy: %v", err)
	}
	body := string(data)
	if strings.Contains(body, "difftool") || strings.Contains(body, "external") {
		t.Fatalf("filtered copy still carries diff.external:\n%s", body)
	}
	if !strings.Contains(body, "name = A") {
		t.Fatalf("filtered copy dropped unrelated config:\n%s", body)
	}
}

func TestGlobalConfigWithoutExternalDiffNone(t *testing.T) {
	src := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(src, []byte("[user]\n\tname = A\n"), 0o600); err != nil {
		t.Fatalf("write source config: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", src)

	if got := GlobalConfigWithoutExternalDiff(t.TempDir()); got != "" {
		t.Fatalf("no diff.external should yield no copy, got %q", got)
	}
}

func TestGlobalConfigWithoutExternalDiffFlattensIncludes(t *testing.T) {
	dir := t.TempDir()
	included := filepath.Join(dir, "extra.gitconfig")
	if err := os.WriteFile(included, []byte("[diff]\n\texternal = difftool\n"), 0o600); err != nil {
		t.Fatalf("write included config: %v", err)
	}
	root := filepath.Join(dir, "gitconfig")
	body := "[user]\n\tname = A\n[include]\n\tpath = " + included + "\n"
	if err := os.WriteFile(root, []byte(body), 0o600); err != nil {
		t.Fatalf("write root config: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", root)

	got := GlobalConfigWithoutExternalDiff(t.TempDir())
	if got == "" {
		t.Fatal("expected a filtered copy when an included file sets diff.external")
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("read filtered copy: %v", err)
	}
	if strings.Contains(string(data), "external") {
		t.Fatalf("flattened copy still carries the included diff.external:\n%s", data)
	}
	if !strings.Contains(string(data), "name = A") {
		t.Fatalf("flattened copy dropped the root's own config:\n%s", data)
	}
}

func TestGlobalConfigWithoutExternalDiffMemoSkipsFlatten(t *testing.T) {
	globalCfgMemo = nil
	root := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(root, []byte("[user]\n\tname = A\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", root)
	state := t.TempDir()

	if got := GlobalConfigWithoutExternalDiff(state); got != "" {
		t.Fatalf("first call = %q, want no filtered copy", got)
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	// Rewrite to add diff.external but restore the recorded mtime: a fresh memo
	// must answer from memory without re-flattening and seeing the new content.
	if err := os.WriteFile(root, []byte("[diff]\n\texternal = difftool\n"), 0o600); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}
	if err := os.Chtimes(root, info.ModTime(), info.ModTime()); err != nil {
		t.Fatalf("restore mtime: %v", err)
	}
	if got := GlobalConfigWithoutExternalDiff(state); got != "" {
		t.Fatalf("memo did not skip flattening: got %q, want no filtered copy", got)
	}
	// Bump the mtime so the memo is stale and the new content is flattened.
	if err := os.Chtimes(root, time.Now().Add(time.Second), time.Now().Add(time.Second)); err != nil {
		t.Fatalf("bump mtime: %v", err)
	}
	if got := GlobalConfigWithoutExternalDiff(state); got == "" {
		t.Fatal("stale memo was not re-flattened: got no filtered copy, want one")
	}
}
