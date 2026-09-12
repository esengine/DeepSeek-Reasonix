package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	toolpkg "reasonix/internal/tool"
)

func TestPresentValidatesAllFilesBeforePublishing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "one.md"), []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	presenter := present{workDir: dir}
	ctx, collected := toolpkg.WithPresentedFilesCollector(context.Background())
	_, err := presenter.Execute(ctx, json.RawMessage(`{"files":[{"path":"one.md"},{"path":"missing.md"}]}`))
	if err == nil || !strings.Contains(err.Error(), "missing.md") {
		t.Fatalf("expected missing file error, got %v", err)
	}
	if got := collected(); len(got) != 0 {
		t.Fatalf("partial presentation published: %#v", got)
	}
}

func TestPresentRecordsRelativePathsAndDescriptions(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "game.html"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	presenter := present{workDir: dir}
	ctx, collected := toolpkg.WithPresentedFilesCollector(context.Background())
	out, err := presenter.Execute(ctx, json.RawMessage(`{"files":[{"path":"./game.html","description":"Game"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if out != "Presented game.html" {
		t.Fatalf("output = %q", out)
	}
	got := collected()
	if len(got) != 1 || got[0].Path != "game.html" || got[0].Description != "Game" {
		t.Fatalf("metadata = %#v", got)
	}
}

func TestPresentRejectsFinalSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "link.txt")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	_, err := (present{workDir: dir}).Execute(context.Background(), json.RawMessage(`{"files":[{"path":"link.txt"}]}`))
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("expected symlink error, got %v", err)
	}
}

func TestPresentRejectsInvalidCardinalityAndDirectories(t *testing.T) {
	dir := t.TempDir()
	presenter := present{workDir: dir}
	for _, args := range []string{
		`{"files":[]}`,
		`{"files":[{"path":"a"},{"path":"b"},{"path":"c"},{"path":"d"},{"path":"e"},{"path":"f"},{"path":"g"},{"path":"h"},{"path":"i"}]}`,
	} {
		if _, err := presenter.Execute(context.Background(), json.RawMessage(args)); err == nil {
			t.Fatalf("invalid cardinality accepted: %s", args)
		}
	}
	if _, err := presenter.Execute(context.Background(), json.RawMessage(`{"files":[{"path":"."}]}`)); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("directory error = %v", err)
	}
}

func TestPresentAppliesResolvedForbidReadPolicy(t *testing.T) {
	root := t.TempDir()
	secret := filepath.Join(root, "secret")
	if err := os.MkdirAll(secret, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secret, "token.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(secret, alias); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	presenter := present{workDir: root, forbidRoots: []string{secret}}
	ctx, collected := toolpkg.WithPresentedFilesCollector(context.Background())
	if _, err := presenter.Execute(ctx, json.RawMessage(`{"files":[{"path":"alias/token.txt"}]}`)); err == nil {
		t.Fatal("parent-directory symlink bypassed forbid_read")
	}
	if got := collected(); len(got) != 0 {
		t.Fatalf("forbidden file was published: %#v", got)
	}
}

func TestPresentRecordsAuthorizedAbsolutePath(t *testing.T) {
	file := filepath.Join(t.TempDir(), "report.pdf")
	if err := os.WriteFile(file, []byte("pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	presenter := present{workDir: t.TempDir()}
	ctx, collected := toolpkg.WithPresentedFilesCollector(context.Background())
	args, err := json.Marshal(map[string]any{"files": []map[string]string{{"path": file}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := presenter.Execute(ctx, args); err != nil {
		t.Fatal(err)
	}
	if got := collected(); len(got) != 1 || got[0].Path != filepath.Clean(file) {
		t.Fatalf("absolute metadata = %#v", got)
	}
}
