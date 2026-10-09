package pluginpkg

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestProfileSourcesShareResidentBoundedReader(t *testing.T) {
	root, outside := testenv.TempDir(t), testenv.TempDir(t)
	writeTestFile(t, filepath.Join(root, "agents", "ordinary.md"), "ORDINARY")
	writeTestFile(t, filepath.Join(root, "agents", "large.md"), strings.Repeat("x", maxAgentSourceBytes+1))
	writeTestFile(t, filepath.Join(root, "profiles", "resident.md"), "RESIDENT")
	writeTestFile(t, filepath.Join(root, "flat.md"), "FLAT")
	writeTestFile(t, filepath.Join(root, "bad name.md"), "INVALID STEM")
	writeTestFile(t, filepath.Join(outside, "profile.md"), "OUTSIDE")
	want := []string{"agents/ordinary.md=ORDINARY", "flat.md=FLAT"}
	if runtime.GOOS != "windows" {
		for _, link := range []struct{ name, target string }{
			{"resident.md", filepath.Join("..", "profiles", "resident.md")},
			{"outside.md", filepath.Join(outside, "profile.md")},
			{"absolute.md", filepath.Join(root, "profiles", "resident.md")},
			{"broken.md", "missing.md"},
			{"cycle", "."},
		} {
			if err := os.Symlink(link.target, filepath.Join(root, "agents", link.name)); err != nil {
				t.Fatal(err)
			}
		}
		want = append(want, "agents/resident.md=RESIDENT")
	}
	pkg := Package{Root: root, Manifest: Manifest{Skills: []string{"flat.md", "bad name.md", "missing.md", "../outside", "", outside}, Agents: []string{"agents", "flat.md"}}}
	var got []string
	pkg.WalkProfileSources(func(path string, body []byte) bool {
		got = append(got, filepath.ToSlash(path)+"="+string(body))
		return true
	})
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("sources=%v, want=%v", got, want)
	}
	Package{Root: filepath.Join(root, "missing")}.WalkProfileSources(func(string, []byte) bool { t.Fatal("missing root visited"); return true })
}

func TestProfileSourcesVisitorControlsDirectoryDescent(t *testing.T) {
	root := testenv.TempDir(t)
	for _, path := range []string{"agents/profile/SKILL.md", "agents/profile/child.md", "agents/group/deep/last/SKILL.md", "agents/group/deep/extra/too-deep/SKILL.md", "agents/scripts/ignored.md"} {
		writeTestFile(t, filepath.Join(root, filepath.FromSlash(path)), "BODY")
	}
	pkg := Package{Root: root, Manifest: Manifest{Agents: []string{"agents"}}}
	for _, accept := range []bool{false, true} {
		var paths []string
		pkg.WalkProfileSources(func(path string, _ []byte) bool { paths = append(paths, filepath.ToSlash(path)); return accept })
		want := []string{"agents/group/deep/last/SKILL.md", "agents/profile/SKILL.md"}
		if !accept {
			want = append(want, "agents/profile/SKILL.md", "agents/profile/child.md")
		}
		slices.Sort(paths)
		slices.Sort(want)
		if !slices.Equal(paths, want) {
			t.Errorf("accept=%v paths=%v, want=%v", accept, paths, want)
		}
	}
}

func TestProfileSourcesScanKeepsOpenedRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("moving an open root requires POSIX directory semantics")
	}
	parent := testenv.TempDir(t)
	rootPath := filepath.Join(parent, "package")
	writeTestFile(t, filepath.Join(rootPath, "agents", "original.md"), "ORIGINAL")
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.Rename(rootPath, filepath.Join(parent, "moved")); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(rootPath, "agents", "replacement.md"), "REPLACEMENT")
	var got []string
	var seen []os.FileInfo
	scanProfileSources(root, "agents", 1, &seen, func(path, _ string, body []byte, _ int) bool {
		got = append(got, filepath.ToSlash(path)+"="+string(body))
		return true
	})
	if !slices.Equal(got, []string{"agents/original.md=ORIGINAL"}) {
		t.Fatalf("opened-root sources=%v", got)
	}
}
