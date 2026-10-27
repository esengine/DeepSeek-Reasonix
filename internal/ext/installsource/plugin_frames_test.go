package installsource

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestPluginPlanListsEveryFrameWithItsSizeAndDigest(t *testing.T) {
	src := testenv.TempDir(t)
	doc := "<!doctype html><p>board\x1b[31m</p>"
	writeFile(t, filepath.Join(src, "reasonix-plugin.json"), `{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "board",
  "version": "1.0.0",
  "contributes": {"frames": [{"id": "board", "entry": "frames/board.html", "title": "Task board\u001b[2J"}]},
  "runtime": {"command": "${REASONIX_PLUGIN_ROOT}/bin/board", "capabilities": ["ui"]}
}`)
	writeFile(t, filepath.Join(src, "frames", "board.html"), doc)
	writeFile(t, filepath.Join(src, "bin", "board"), "#!/bin/sh\n")

	tl := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: testenv.TempDir(t)})
	resp := execInstall(t, tl, map[string]any{"source": src, "kind": "plugin", "apply": false})
	if !resp.OK || len(resp.Actions) != 1 {
		t.Fatalf("response = %+v", resp)
	}
	frames := resp.Actions[0].Frames
	if len(frames) != 1 {
		t.Fatalf("frames = %+v", frames)
	}
	sum := sha256.Sum256([]byte(doc))
	f := frames[0]
	if f.ID != "board" || f.Entry != "frames/board.html" || f.Bytes != int64(len(doc)) || f.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("frame = %+v", f)
	}
	if f.Title == "Task board\x1b[2J" {
		t.Fatalf("title kept a hidden escape: %q", f.Title)
	}
}

func TestAThemePackageThatShipsAFrameIsNotThemesOnly(t *testing.T) {
	if themesOnly([]action{{Kind: "plugin", Name: "a", ThemeCount: 1, Frames: []FramePlanInfo{{ID: "board"}}}}) {
		t.Fatal("a package with a frame passed as themes only")
	}
}
