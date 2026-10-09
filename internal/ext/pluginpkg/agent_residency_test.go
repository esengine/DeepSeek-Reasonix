package pluginpkg

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestAgentInventoryReadsOnlyResidentRegularSources(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires host privileges")
	}
	for _, tc := range []struct{ label, manifest, body string }{
		{"native", NativeManifest, `{"apiVersion":"reasonix.io/plugin/v2","name":"resident-agents","contributes":{"agents":["agents"]}}`},
		{"Claude convention", ClaudeManifest, `{"name":"resident-agents"}`},
	} {
		t.Run(tc.label, func(t *testing.T) {
			root, outside := testenv.TempDir(t), testenv.TempDir(t)
			writeTestFile(t, filepath.Join(root, tc.manifest), tc.body)
			writeTestFile(t, filepath.Join(root, "agents", "ordinary.md"), "---\ndescription: Ordinary profile\n---\nORDINARY")
			writeTestFile(t, filepath.Join(root, "profiles", "resident.md"), "---\ndescription: Linked resident profile\n---\nRESIDENT")
			writeTestFile(t, filepath.Join(outside, "profile.md"), "---\ndescription: Nonresident profile\n---\nNONRESIDENT")
			if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
				t.Fatal(err)
			}
			for _, link := range []struct{ name, target string }{
				{"resident.md", filepath.Join("..", "profiles", "resident.md")},
				{"absolute.md", filepath.Join(root, "profiles", "resident.md")},
				{"nonresident.md", filepath.Join(outside, "profile.md")},
				{"nonregular.md", filepath.Join(root, "empty")},
				{"broken.md", filepath.Join(root, "missing.md")},
			} {
				if err := os.Symlink(link.target, filepath.Join(root, "agents", link.name)); err != nil {
					t.Fatal(err)
				}
			}
			t.Chdir(root)
			for _, inputRoot := range []string{root, "."} {
				pkg, _, err := ParseDir(inputRoot)
				if err != nil {
					t.Fatal(err)
				}
				agents := pkg.Inventory().Agents
				if len(agents) != 2 || pkg.AgentCount() != 2 || agents[0].Name != "ordinary" || agents[1].Name != "resident" {
					t.Fatalf("resident-only inventory=%+v, count=%d", agents, pkg.AgentCount())
				}
				if agents[1].Path != filepath.Join(inputRoot, "agents", "resident.md") || agents[1].Description != "Linked resident profile" {
					t.Fatalf("resident link projection=%+v", agents[1])
				}
			}
		})
	}
}

func TestAgentInventorySkipsOversizedSources(t *testing.T) {
	root := testenv.TempDir(t)
	writeTestFile(t, filepath.Join(root, NativeManifest), `{"apiVersion":"reasonix.io/plugin/v2","name":"bounded-agents","contributes":{"agents":["agents"]}}`)
	writeTestFile(t, filepath.Join(root, "agents", "ordinary.md"), "---\ndescription: Ordinary profile\n---\nBODY")
	writeTestFile(t, filepath.Join(root, "agents", "large.md"), "---\ndescription: Large profile\n---\n"+strings.Repeat("x", 1<<20))
	pkg, _, err := ParseDir(root)
	if err != nil {
		t.Fatal(err)
	}
	agents := pkg.Inventory().Agents
	if len(agents) != 1 || pkg.AgentCount() != 1 || agents[0].Name != "ordinary" {
		t.Fatalf("bounded inventory=%+v count=%d", agents, pkg.AgentCount())
	}
}

func TestAgentSourceBodyUsesRootHandle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("moving an open root requires POSIX directory semantics")
	}
	parent := testenv.TempDir(t)
	rootPath := filepath.Join(parent, "package")
	writeTestFile(t, filepath.Join(rootPath, "profile.md"), "checked bytes")
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.Rename(rootPath, filepath.Join(parent, "moved")); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(rootPath, "profile.md"), "replacement bytes")
	t.Chdir(rootPath)
	if body := agentSourceBody(root, "profile.md"); string(body) != "checked bytes" {
		t.Fatalf("production source read=%q", body)
	}
}

func TestAgentSourceBodyReadLimit(t *testing.T) {
	rootPath := testenv.TempDir(t)
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, size := range []int{maxAgentSourceBytes, maxAgentSourceBytes + 1} {
		writeTestFile(t, filepath.Join(rootPath, "profile.md"), strings.Repeat("x", size))
		body := agentSourceBody(root, "profile.md")
		if size == maxAgentSourceBytes && len(body) != size {
			t.Fatalf("exact-limit source size=%d", len(body))
		}
		if size > maxAgentSourceBytes && body != nil {
			t.Fatalf("oversized source size=%d", len(body))
		}
	}
}

func TestAgentRefsUseBorrowedRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("moving an open root requires POSIX directory semantics")
	}
	parent := testenv.TempDir(t)
	rootPath := filepath.Join(parent, "package")
	writeTestFile(t, filepath.Join(rootPath, "agents", "original.md"), "---\ndescription: Original directory\n---\nBODY")
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.Rename(rootPath, filepath.Join(parent, "moved")); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(rootPath, "agents", "replacement.md"), "---\ndescription: Replacement directory\n---\nBODY")
	for i := range 2 {
		agents := loadAgentRefs(root, "agents")
		if len(agents) != 1 || agents[0].Name != "original" || agents[0].Description != "Original directory" || agents[0].Path != filepath.Join(rootPath, "agents", "original.md") {
			t.Fatalf("borrowed-root scan %d = %+v", i, agents)
		}
	}
}

func TestAgentRefsRejectNonlocalDirectory(t *testing.T) {
	parent := testenv.TempDir(t)
	rootPath := filepath.Join(parent, "package")
	writeTestFile(t, filepath.Join(rootPath, "agents", "local.md"), "LOCAL")
	outside := filepath.Join(parent, "outside")
	writeTestFile(t, filepath.Join(outside, "outside.md"), "OUTSIDE")
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, dir := range []string{outside, filepath.Join("..", "outside")} {
		if agents := loadAgentRefs(root, dir); len(agents) != 0 {
			t.Fatalf("nonlocal directory %q projected %+v", dir, agents)
		}
	}
	if agents := loadAgentRefs(root, "agents"); len(agents) != 1 || agents[0].Name != "local" {
		t.Fatalf("local directory projected %+v", agents)
	}
}
