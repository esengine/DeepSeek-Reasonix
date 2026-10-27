package pluginpkg

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func framesManifest(frames string, capabilities string) string {
	return `{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "board",
  "version": "1.0.0",
  "contributes": {"frames": ` + frames + `},
  "runtime": {"command": "./bin/board", "capabilities": ` + capabilities + `}
}`
}

func TestManifestDeclaresFramesTheRuntimePublishes(t *testing.T) {
	root := testenv.TempDir(t)
	writeTestFile(t, filepath.Join(root, "frames", "board.html"), "<!doctype html><p>board</p>")
	writeV2Plugin(t, root, framesManifest(`[{"id":"board","entry":"frames/board.html","title":"Task board"}]`, `["ui"]`))
	pkg, warnings, err := ParseDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	want := []FrameRef{{ID: "board", Entry: "frames/board.html", Title: "Task board"}}
	if len(pkg.Manifest.Frames) != 1 || pkg.Manifest.Frames[0] != want[0] {
		t.Fatalf("frames = %+v", pkg.Manifest.Frames)
	}
	if got := pkg.Inventory().Frames; len(got) != 1 || got[0].ID != "board" {
		t.Fatalf("inventory frames = %+v", got)
	}
}

func TestManifestRefusesFramesItCannotKeep(t *testing.T) {
	nine := make([]string, 9)
	for i := range nine {
		nine[i] = `{"id":"f` + string(rune('a'+i)) + `","entry":"frames/board.html","title":"t"}`
	}
	cases := map[string]string{
		"no ui runtime": framesManifest(`[{"id":"board","entry":"frames/board.html","title":"t"}]`, `[]`),
		"bad id":        framesManifest(`[{"id":"Board","entry":"frames/board.html","title":"t"}]`, `["ui"]`),
		"duplicate id":  framesManifest(`[{"id":"b","entry":"frames/board.html","title":"t"},{"id":"b","entry":"frames/board.html","title":"t"}]`, `["ui"]`),
		"too many":      framesManifest(`[`+strings.Join(nine, ",")+`]`, `["ui"]`),
		"no title":      framesManifest(`[{"id":"board","entry":"frames/board.html"}]`, `["ui"]`),
		"not html":      framesManifest(`[{"id":"board","entry":"frames/board.js","title":"t"}]`, `["ui"]`),
		"traversal":     framesManifest(`[{"id":"board","entry":"../board.html","title":"t"}]`, `["ui"]`),
		"absolute":      framesManifest(`[{"id":"board","entry":"/etc/board.html","title":"t"}]`, `["ui"]`),
		"unknown key":   framesManifest(`[{"id":"board","entry":"frames/board.html","title":"t","csp":"none"}]`, `["ui"]`),
		"missing file":  framesManifest(`[{"id":"board","entry":"frames/none.html","title":"t"}]`, `["ui"]`),
		"directory":     framesManifest(`[{"id":"board","entry":"frames","title":"t"}]`, `["ui"]`),
		"not a list":    framesManifest(`{"id":"board"}`, `["ui"]`),
	}
	for name, manifest := range cases {
		root := testenv.TempDir(t)
		writeTestFile(t, filepath.Join(root, "frames", "board.html"), "<p>x</p>")
		writeV2Plugin(t, root, manifest)
		if _, _, err := ParseDir(root); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestManifestRefusesAFrameThatLeavesThePackageThroughALink(t *testing.T) {
	root := testenv.TempDir(t)
	outside := filepath.Join(testenv.TempDir(t), "board.html")
	writeTestFile(t, outside, "<p>outside</p>")
	if err := os.MkdirAll(filepath.Join(root, "frames"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "frames", "board.html")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	writeV2Plugin(t, root, framesManifest(`[{"id":"board","entry":"frames/board.html","title":"t"}]`, `["ui"]`))
	if _, _, err := ParseDir(root); err == nil {
		t.Fatal("a frame outside the package was accepted")
	}
}

func TestFrameDocumentIsReadFromThePackageOnly(t *testing.T) {
	root := testenv.TempDir(t)
	writeTestFile(t, filepath.Join(root, "frames", "board.html"), "<p>board</p>")
	writeV2Plugin(t, root, framesManifest(`[{"id":"board","entry":"frames/board.html","title":"t"}]`, `["ui"]`))
	pkg, _, err := ParseDir(root)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := pkg.FrameDocument("board")
	if err != nil || string(doc) != "<p>board</p>" {
		t.Fatalf("doc = %q, %v", doc, err)
	}
	if _, err := pkg.FrameDocument("other"); !errors.Is(err, ErrFrameNotDeclared) {
		t.Fatalf("undeclared = %v", err)
	}

	writeTestFile(t, filepath.Join(root, "frames", "board.html"), strings.Repeat("x", MaxFrameDocumentBytes+1))
	if _, err := pkg.FrameDocument("board"); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("oversize = %v", err)
	}
	writeTestFile(t, filepath.Join(root, "frames", "board.html"), "\xff\xfe")
	if _, err := pkg.FrameDocument("board"); !errors.Is(err, ErrFrameNotUTF8) {
		t.Fatalf("binary = %v", err)
	}
	if err := os.Remove(filepath.Join(root, "frames", "board.html")); err != nil {
		t.Fatal(err)
	}
	if _, err := pkg.FrameDocument("board"); !errors.Is(err, ErrFrameMissing) {
		t.Fatalf("missing = %v", err)
	}
	outside := filepath.Join(testenv.TempDir(t), "board.html")
	writeTestFile(t, outside, "<p>outside</p>")
	if err := os.Symlink(outside, filepath.Join(root, "frames", "board.html")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if _, err := pkg.FrameDocument("board"); !errors.Is(err, ErrFrameOutsideRoot) {
		t.Fatalf("swapped for a link = %v", err)
	}
}
