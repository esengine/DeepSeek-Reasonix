package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/blender"
)

const modelingCubeOBJ = `v 0 0 0
v 1 0 0
v 1 1 0
v 0 1 0
v 0 0 1
v 1 0 1
v 1 1 1
v 0 1 1
f 1 2 3 4
f 5 8 7 6
f 1 5 6 2
f 2 6 7 3
f 3 7 8 4
f 4 8 5 1
`

func writeCube(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "cube.obj")
	if err := os.WriteFile(p, []byte(modelingCubeOBJ), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestModelingAnalyzeTool(t *testing.T) {
	p := writeCube(t)
	args, _ := json.Marshal(map[string]any{"path": p})
	out, err := modelingAnalyze{}.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	var d struct {
		Format string  `json:"format"`
		Verts  int     `json:"verts"`
		Faces  int     `json:"faces"`
		Tris   int     `json:"tris"`
		Water  bool    `json:"watertight"`
		Quality float64 `json:"quality"`
	}
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("analyze output not JSON: %q", out)
	}
	if d.Verts != 8 || d.Faces != 6 || d.Tris != 12 || !d.Water {
		t.Errorf("analyze descriptor = %+v, want 8/6/12 watertight", d)
	}
	if d.Quality < 0.99 {
		t.Errorf("quality = %v, want ~1.0", d.Quality)
	}
}

func TestModelingOptimizeDecimateTool(t *testing.T) {
	p := writeCube(t)
	args, _ := json.Marshal(map[string]any{"path": p, "op": "triangulate"})
	out, err := modelingOptimize{}.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("optimize: %v", err)
	}
	var res struct {
		Op          string `json:"op"`
		FacesBefore int    `json:"faces_before"`
		FacesAfter  int    `json:"faces_after"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("optimize output: %q", out)
	}
	if res.Op != "triangulate" || res.FacesBefore != 6 || res.FacesAfter != 12 {
		t.Errorf("triangulate result = %+v, want op=triangulate 6→12", res)
	}
	// Backup file must exist.
	if _, err := os.Stat(p + ".bak"); err != nil {
		t.Errorf("backup missing: %v", err)
	}
	// File was rewritten as triangles.
	re, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(re), "\nf ") != 12 {
		t.Errorf("rewritten file should have 12 faces, got %d", strings.Count(string(re), "\nf "))
	}
}

func TestModelingConvertToSTL(t *testing.T) {
	p := writeCube(t)
	args, _ := json.Marshal(map[string]any{"path": p, "format": "stl"})
	out, err := modelingConvert{}.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	stl := strings.TrimSuffix(p, ".obj") + ".stl"
	var r struct {
		Out string `json:"out"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("convert output not JSON: %s", out)
	}
	if r.Out != stl {
		t.Errorf("convert out = %q, want %q", r.Out, stl)
	}
	data, err := os.ReadFile(stl)
	if err != nil {
		t.Fatalf("stl not written: %v", err)
	}
	if !strings.HasPrefix(string(data), "solid ") || !strings.Contains(string(data), "facet normal") {
		t.Errorf("stl content malformed")
	}
}

func TestModelingVoxelTool(t *testing.T) {
	p := writeCube(t)
	args, _ := json.Marshal(map[string]any{"path": p, "resolution": 16})
	out, err := modelingVoxel{}.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("voxel: %v", err)
	}
	voxPath := p + ".vox"
	if _, err := os.Stat(voxPath); err != nil {
		t.Fatalf("vox file missing: %v", err)
	}
	if !strings.Contains(out, `"filled"`) {
		t.Errorf("voxel output should contain descriptor: %s", out)
	}
	// Analyze the .vox back.
	vargs, _ := json.Marshal(map[string]any{"path": voxPath})
	ao, err := modelingAnalyze{}.Execute(context.Background(), vargs)
	if err != nil {
		t.Fatalf("analyze vox: %v", err)
	}
	if !strings.Contains(ao, `"format":"vox"`) {
		t.Errorf("analyze vox output: %s", ao)
	}
}

func TestModelingConvertToGLBWithBlender(t *testing.T) {
	if blender.BlenderPath() == "" {
		t.Skip("Blender not installed")
	}
	p := writeCube(t)
	args, _ := json.Marshal(map[string]any{"path": p, "format": "glb"})
	out, err := modelingConvert{}.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("convert glb: %v", err)
	}
	glb := strings.TrimSuffix(p, ".obj") + ".glb"
	var r struct {
		Out string `json:"out"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("convert output not JSON: %s", out)
	}
	if r.Out != glb {
		t.Errorf("glb out = %q, want %q", r.Out, glb)
	}
	info, err := os.Stat(glb)
	if err != nil {
		t.Fatalf("glb missing: %v", err)
	}
	if info.Size() < 100 {
		t.Errorf("glb suspiciously small: %d bytes", info.Size())
	}
}
