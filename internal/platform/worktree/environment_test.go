package worktree

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEnvironmentCopiesDeclaredDependenciesWithoutSharingWrites(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	writeRepoFile(t, source, "deps/package/code", "dependency\n")
	writeRepoFile(t, source, "undeclared", "not included\n")
	if err := os.MkdirAll(filepath.Join(source, "deps", "empty"), 0755); err != nil {
		t.Fatal(err)
	}
	snap, candidate := Snapshot{RepoRoot: source}, Candidate{WorktreeRoot: destination}
	receipt, err := PrepareEnvironment(t.Context(), snap, candidate, []string{"deps", "deps/package/code"})
	if err != nil {
		t.Fatal(err)
	}
	if receipt == nil || receipt.Version != 1 || receipt.Entries != 4 || receipt.Bytes != int64(len("dependency\n")) || receipt.ContentDigest == "" {
		t.Fatalf("prepared inputs not witnessed: %+v", receipt)
	}
	input, err := os.Stat(filepath.Join(source, "deps/package/code"))
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.Stat(filepath.Join(destination, "deps/package/code"))
	if err != nil || os.SameFile(input, output) {
		t.Fatalf("dependency aliases its source: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(destination, "undeclared")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("undeclared input copied: %v", err)
	}
	writeRepoFile(t, destination, "deps/package/code", "candidate wrote\n")
	if got := readRepoFile(t, source, "deps/package/code"); got != "dependency\n" {
		t.Fatalf("candidate dependency write reached source: %q", got)
	}
}

func TestEnvironmentPreparationRejectsUnsafeInputsBeforeCopying(t *testing.T) {
	for _, path := range []string{".", "../outside", "/absolute", "deps/.env", "deps/.git/config", "deps/private.key", "deps/.ssh/id", "deps/secret-link", "alias/file", "missing"} {
		t.Run(path, func(t *testing.T) {
			source, target := t.TempDir(), t.TempDir()
			writeRepoFile(t, source, "deps/safe", "public")
			writeRepoFile(t, source, "deps/.env", "credential")
			if path == "deps/secret-link" || path == "alias/file" {
				link, destination := filepath.Join(source, "deps/secret-link"), filepath.Join(source, "deps/.env")
				if path == "alias/file" {
					link, destination = filepath.Join(source, "alias"), t.TempDir()
					writeRepoFile(t, destination, "file", "external")
				}
				if err := os.Symlink(destination, link); err != nil {
					t.Skipf("host cannot create fixture symlink: %v", err)
				}
			}
			_, err := PrepareEnvironment(t.Context(), Snapshot{RepoRoot: source}, Candidate{WorktreeRoot: target}, []string{path})
			if err == nil {
				t.Fatal("unsafe environment input accepted")
			}
			entries, err := os.ReadDir(target)
			if err != nil || len(entries) != 0 {
				t.Fatalf("preflight refusal partially copied inputs: %v %v", entries, err)
			}
		})
	}
}

func TestEnvironmentPreparationHonorsReadDenials(t *testing.T) {
	for _, scope := range []string{"file", "subdirectory", "ancestor"} {
		t.Run(scope, func(t *testing.T) {
			source, target := t.TempDir(), t.TempDir()
			writeRepoFile(t, source, "deps/package/code", "private")
			deny := filepath.Join(source, "deps/package/code")
			switch scope {
			case "subdirectory":
				deny = filepath.Join(source, "deps/package")
			case "ancestor":
				deny = source
			}
			_, err := PrepareEnvironment(t.Context(), Snapshot{RepoRoot: source}, Candidate{WorktreeRoot: target}, []string{"deps"}, deny)
			if !errors.Is(err, ErrEnvironment) {
				t.Fatalf("read denial not enforced: %v", err)
			}
			entries, err := os.ReadDir(target)
			if err != nil || len(entries) != 0 {
				t.Fatalf("denied input reached candidate: %v %v", entries, err)
			}
		})
	}
}

func TestEnvironmentRefusesDestinationAliasAndCancellation(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	writeRepoFile(t, source, "deps/file", "input")
	if err := os.Symlink(filepath.Join(source, "deps"), filepath.Join(target, "deps")); err != nil {
		t.Skipf("host cannot create fixture symlink: %v", err)
	}
	if _, err := PrepareEnvironment(t.Context(), Snapshot{RepoRoot: source}, Candidate{WorktreeRoot: target}, []string{"deps"}); err == nil {
		t.Fatal("candidate alias toward source accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := PrepareEnvironment(ctx, Snapshot{RepoRoot: source}, Candidate{WorktreeRoot: t.TempDir()}, []string{"deps"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled preparation: %v", err)
	}
}
