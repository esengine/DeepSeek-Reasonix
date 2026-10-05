package builtin

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ignoredTree is a repository whose only match lives where the ignore rules
// point away — which is where a build writes, and where a search that reports
// "no matches" is at its most misleading.
func ignoredTree(t *testing.T, needle string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitignore", "dist/\nnode_modules/\n")
	write("src/main.go", "package main\n")
	write("dist/bundle.js", "var x = \""+needle+"\";\n")
	write("node_modules/dep/index.js", "// "+needle+"\n")
	return root
}

// A result the ignore rules filtered out is not the same answer as one that is
// not there, and only the host can tell them apart. Reporting the first as the
// second is what sends a reader looking somewhere else entirely.
func TestASearchThatFindsNothingTrackedLooksWhereTheRulesPointAway(t *testing.T) {
	root := ignoredTree(t, "SENTINEL_TOKEN")

	out := runTool(t, grepTool{}, map[string]any{"pattern": "SENTINEL_TOKEN", "path": root})
	if !strings.Contains(out, "SENTINEL_TOKEN") {
		t.Fatalf("the match in an ignored path was never reached:\n%s", out)
	}
	if !strings.Contains(out, "no match in the tracked files") {
		t.Fatalf("matches from ignored paths were not said to be from there:\n%s", out)
	}
	if !strings.Contains(out, "dist") && !strings.Contains(out, "node_modules") {
		t.Fatalf("the answer names no ignored path it came from:\n%s", out)
	}
}

// The tracked answer stays the tracked answer. A match in the working tree must
// not drag the build output in behind it — the second pass exists for an empty
// result, not as a wider default.
func TestATrackedMatchIsAnsweredWithoutWideningTheSearch(t *testing.T) {
	root := ignoredTree(t, "SENTINEL_TOKEN")
	if err := os.WriteFile(filepath.Join(root, "src", "hit.go"), []byte("// SENTINEL_TOKEN\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out := runTool(t, grepTool{}, map[string]any{"pattern": "SENTINEL_TOKEN", "path": root})
	if strings.Contains(out, "no match in the tracked files") {
		t.Fatalf("a tracked match was reported as coming from an ignored path:\n%s", out)
	}
	if strings.Contains(out, "dist") || strings.Contains(out, "node_modules") {
		t.Fatalf("a tracked match dragged the ignored paths in with it:\n%s", out)
	}
}

// Absent and filtered read the same to a caller unless the host says which. A
// search that reached everywhere says so, so the caller can stop looking.
func TestNothingAnywhereSaysTheIgnoredPathsWereSearchedToo(t *testing.T) {
	root := ignoredTree(t, "SENTINEL_TOKEN")

	out := runTool(t, grepTool{}, map[string]any{"pattern": "NOTHING_HAS_THIS_STRING", "path": root})
	if !strings.Contains(out, "absent rather than filtered") {
		t.Fatalf("an empty answer did not say how far it looked:\n%s", out)
	}
}

// Widening the search widens the ignore rules and nothing else. The VCS store
// is history rather than a place a build writes, and reading it is not what a
// caller asked for.
func TestWideningDoesNotReachIntoTheVCSStore(t *testing.T) {
	root := ignoredTree(t, "SENTINEL_TOKEN")
	if err := os.WriteFile(filepath.Join(root, ".git", "COMMIT_EDITMSG"), []byte("VCS_ONLY_TOKEN\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out := runTool(t, grepTool{}, map[string]any{"pattern": "VCS_ONLY_TOKEN", "path": root})
	if strings.Contains(out, "VCS_ONLY_TOKEN") {
		t.Fatalf("the widened search read the VCS store:\n%s", out)
	}
}

// The same contract through the engine that actually runs it. ripgrep is the
// production path wherever it is installed, and its ignore handling is its own
// -- flags rather than the walk above -- so the promise has to be checked
// against it and not only against the native scanner.
func TestRipgrepAnswersTheSameWayAboutIgnoredPaths(t *testing.T) {
	rg, err := exec.LookPath("rg")
	if err != nil {
		t.Skip("no ripgrep on this host")
	}
	root := ignoredTree(t, "SENTINEL_TOKEN")
	if err := os.WriteFile(filepath.Join(root, ".git", "COMMIT_EDITMSG"), []byte("VCS_ONLY_TOKEN\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	g := grepTool{rg: rg}

	out := runTool(t, g, map[string]any{"pattern": "SENTINEL_TOKEN", "path": root})
	if !strings.Contains(out, "SENTINEL_TOKEN") || !strings.Contains(out, "no match in the tracked files") {
		t.Fatalf("ripgrep did not reach or did not label the ignored match:\n%s", out)
	}

	if out := runTool(t, g, map[string]any{"pattern": "VCS_ONLY_TOKEN", "path": root}); strings.Contains(out, "VCS_ONLY_TOKEN") {
		t.Fatalf("the widened ripgrep search read the VCS store:\n%s", out)
	}
	if out := runTool(t, g, map[string]any{"pattern": "NOTHING_HAS_THIS_STRING", "path": root}); !strings.Contains(out, "absent rather than filtered") {
		t.Fatalf("ripgrep's empty answer did not say how far it looked:\n%s", out)
	}
}
