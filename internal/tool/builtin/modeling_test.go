package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/blender"
	"reasonix/internal/modeling/meshparse"
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

func TestModelingOptimizeRetopoWithBlender(t *testing.T) {
	if blender.BlenderPath() == "" {
		t.Skip("Blender not installed")
	}
	p := writeCube(t)
	args, _ := json.Marshal(map[string]any{"path": p, "op": "retopo", "target_faces": 200})
	out, err := modelingOptimize{}.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("retopo: %v", err)
	}
	var res struct {
		Op          string `json:"op"`
		FacesBefore int    `json:"faces_before"`
		FacesAfter  int    `json:"faces_after"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("retopo output not JSON: %s", out)
	}
	if res.Op != "retopo" {
		t.Errorf("op = %q, want retopo", res.Op)
	}
	if _, err := os.Stat(p + ".bak"); err != nil {
		t.Errorf("backup missing: %v", err)
	}
	// Result must be parseable back.
	if _, err := meshparse.Parse(p); err != nil {
		t.Errorf("retopo result unparseable: %v", err)
	}
}

func TestModelingOptimizeUnwrapWithBlender(t *testing.T) {
	if blender.BlenderPath() == "" {
		t.Skip("Blender not installed")
	}
	p := writeCube(t)
	args, _ := json.Marshal(map[string]any{"path": p, "op": "unwrap"})
	out, err := modelingOptimize{}.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unwrap: %v", err)
	}
	if !strings.Contains(out, `"op":"unwrap"`) {
		t.Errorf("unwrap output = %s", out)
	}
	if _, err := os.Stat(p + ".bak"); err != nil {
		t.Errorf("backup missing: %v", err)
	}
}

// TestModelingToolsRespectWorkspaceConfinement verifies the security_review
// finding (HIGH): modeling tools must not read/write outside the workspace
// read/write roots once bound via Workspace.Tools().
func TestModelingToolsRespectWorkspaceConfinement(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "ws")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	// Secret outside the workspace root.
	secret := filepath.Join(dir, "secret.bin")
	if err := os.WriteFile(secret, []byte("s3cr3t"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Sensitive forbid-read root (like session temp).
	sensitive := filepath.Join(dir, "session-temp")
	if err := os.MkdirAll(sensitive, 0o755); err != nil {
		t.Fatal(err)
	}

	// Read confinement: analyze must not read outside the workspace (workDir)
	// nor from forbid roots.
	an := modelingAnalyze{workDir: root, forbidRoots: []string{sensitive}}
	if _, err := an.Execute(context.Background(), json.RawMessage(`{"path":"../secret.bin"}`)); err == nil {
		t.Fatal("analyze read outside workDir must fail")
	}
	anAbs := modelingAnalyze{workDir: root, forbidRoots: []string{sensitive}}
	if _, err := anAbs.Execute(context.Background(), json.RawMessage(`{"path":"`+secret+`"}`)); err == nil {
		t.Fatal("analyze absolute read outside workDir must fail")
	}
	// Forbid-root read is rejected even inside workDir.
	if err := os.WriteFile(filepath.Join(root, "sensitive.bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (modelingAnalyze{workDir: root, forbidRoots: []string{sensitive}}).Execute(context.Background(), json.RawMessage(`{"path":"`+sensitive+`"}`)); err == nil {
		t.Fatal("analyze read from forbid root must fail")
	}
	// In-root read is allowed (missing file → parse error, not confinement error).
	_, err := an.Execute(context.Background(), json.RawMessage(`{"path":"inroot.obj"}`))
	if err == nil {
		t.Fatal("in-root missing file should still error (parse), not be allowed silently")
	}

	// --- Write confinement: valid in-root input + escaping output target, so
	// the guard under test (modelingGuardWrite) is actually reached. ---
	// A tiny valid cube in .obj.
	cubeOBJ := `v 0 0 0
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
f 4 3 7 8
f 1 4 8 5
f 2 6 7 3
`
	if err := os.WriteFile(filepath.Join(root, "cube.obj"), []byte(cubeOBJ), 0o644); err != nil {
		t.Fatal(err)
	}
	// A valid minimal .vox (VOX + version + MAIN with XYZI child, one voxel).
	minVox := append([]byte("VOX "), 0x96, 0x00, 0x00, 0x00) // version 150
	hdr := func(id string, content, kids uint32) []byte {
		b := append([]byte(id), 0, 0, 0, 0, 0, 0, 0, 0)
		b[4] = byte(content); b[5] = byte(content >> 8); b[6] = byte(content >> 16); b[7] = byte(content >> 24)
		b[8] = byte(kids); b[9] = byte(kids >> 8); b[10] = byte(kids >> 16); b[11] = byte(kids >> 24)
		return b
	}
	xyz := []byte{1, 0, 0, 0, 0, 0, 0, 0} // XYZI: 1 voxel at (0,0,0), color 0
	xyzBody := append([]byte{1, 0, 0, 0}, xyz...)
	minVox = append(minVox, hdr("MAIN", 0, uint32(len(xyzBody)+12))...)
	minVox = append(minVox, hdr("XYZI", uint32(len(xyzBody)), 0)...)
	minVox = append(minVox, xyzBody...)
	if err := os.WriteFile(filepath.Join(root, "src.vox"), minVox, 0o644); err != nil {
		t.Fatal(err)
	}

	// convert (non-vox) must reject out escaping the write roots — parse succeeds first.
	cv := modelingConvert{workDir: root, forbidRoots: []string{sensitive}, roots: []string{root}}
	_, err = cv.Execute(context.Background(), json.RawMessage(`{"path":"cube.obj","format":"stl","out":"`+secret+`"}`))
	if err == nil {
		t.Fatal("convert out outside roots must fail")
	}
	// convert .vox early-return branch must reject out escaping (review blocking).
	_, err = cv.Execute(context.Background(), json.RawMessage(`{"path":"src.vox","format":"vox","out":"`+secret+`"}`))
	if err == nil {
		t.Fatal("convert vox branch out outside roots must fail")
	}
	// In-root output still works (sanity: the guard is not over-eager).
	_, err = cv.Execute(context.Background(), json.RawMessage(`{"path":"cube.obj","format":"stl","out":"cube.stl"}`))
	if err != nil {
		t.Fatalf("in-root convert should succeed: %v", err)
	}
	// voxel/optimize escape their own derivation (output = input+.vox, in-place):
	// their write guard is only reachable via modelingGuardWrite directly.
	op := modelingOptimize{workDir: root, forbidRoots: []string{sensitive}, roots: []string{root}}
	if err := modelingGuardWrite([]string{root}, op.guard, filepath.Join(dir, "escape.obj")); err == nil {
		t.Fatal("modelingGuardWrite must reject outside roots")
	}
	vx := modelingVoxel{workDir: root, forbidRoots: []string{sensitive}, roots: []string{root}}
	if err := modelingGuardWrite([]string{root}, vx.guard, filepath.Join(dir, "escape.vox")); err == nil {
		t.Fatal("modelingGuardWrite must reject outside roots")
	}
}
