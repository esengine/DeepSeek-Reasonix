package blender

import (
	"strings"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func requireBlender(t *testing.T) {
	t.Helper()
	if BlenderPath() == "" {
		t.Skip("blender not installed; skipping live test")
	}
}

func TestSummaryLive(t *testing.T) {
	requireBlender(t)
	// Default scene: 3 objects (Cube/Light/Camera), 1 mesh.
	s, err := Summary(context.Background(), "", 60*time.Second)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if s.Objects < 2 || s.Meshes < 1 {
		t.Errorf("default scene summary unexpected: %+v", s)
	}
	if len(s.ObjectNames) == 0 {
		t.Error("object_names should be populated")
	}
}

func TestRunScriptDecimateLive(t *testing.T) {
	requireBlender(t)
	// Create a 128x128 grid (16384 verts), decimate to 25%, print before/after.
	script := `
import bpy
bpy.ops.mesh.primitive_grid_add(size=2, x_subdivisions=128, y_subdivisions=128)
b = sum(len(m.vertices) for m in bpy.data.meshes)
for o in bpy.data.objects:
    if o.type == "MESH":
        m = o.modifiers.new("dec", "DECIMATE")
        m.ratio = 0.25
        bpy.ops.object.modifier_apply(modifier=m.name)
a = sum(len(m.vertices) for m in bpy.data.meshes)
print("DEC_BEFORE", b)
print("DEC_AFTER", a)
assert a < b, f"decimate should reduce: {a} >= {b}"
`
	res, err := RunScript(context.Background(), "", script, false, 120*time.Second)
	if err != nil {
		t.Fatalf("decimate run: %v", err)
	}
	if !strings.Contains(res.Output, "DEC_AFTER") {
		t.Errorf("expected DEC_AFTER marker in output: %q", res.Output)
	}
}

func TestSummaryScriptMarkerParsing(t *testing.T) {
	line, ok := findMarker("noise\nBLENDER_SUMMARY {\"objects\": 3}\ntail", "BLENDER_SUMMARY ")
	if !ok || line != `{"objects": 3}` {
		t.Errorf("marker parse: ok=%v line=%q", ok, line)
	}
	if _, ok := findMarker("nothing here", "BLENDER_SUMMARY "); ok {
		t.Error("missing marker should not parse")
	}
}

func TestBackup(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.blend")
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := backup(src); err != nil {
		t.Fatalf("backup: %v", err)
	}
	bak := src + ".bak"
	b, err := os.ReadFile(bak)
	if err != nil || string(b) != "data" {
		t.Errorf("backup content: %q err=%v", b, err)
	}
}
