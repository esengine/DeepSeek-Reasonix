// Package blender gives the agent deterministic, low-token control over
// Blender scenes (docs/BLENDER_MODELING.md). It drives the local Blender
// headless via bpy scripts: scene summaries (structured JSON — the agent
// never reads a binary .blend), precise operation primitives (merge/decimate/
// cleanup/rename/…), and format conversion (.gltf/.fbx/.obj/…).
//
// Correctness/safety: operations back up the source first; every run has a
// timeout and an output cap; the package only touches explicit paths.
package blender

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// BlenderPath returns the detected blender executable ("" if not found).
func BlenderPath() string {
	for _, p := range []string{
		`C:\Program Files\Blender Foundation\Blender 4.2\blender.exe`,
		`C:\Program Files\Blender Foundation\Blender 4.1\blender.exe`,
		`C:\Program Files\Blender Foundation\Blender 4.0\blender.exe`,
		`/usr/bin/blender`,
		`/Applications/Blender.app/Contents/MacOS/Blender`,
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// Summary is the structured scene digest (docs/BLENDER_MODELING.md §2.1).
type SceneSummary struct {
	Objects     int      `json:"objects"`
	Meshes      int      `json:"meshes"`
	Materials   int      `json:"materials"`
	Vertices    int      `json:"vertices"`
	Tris        int      `json:"tris"`
	ObjectNames []string `json:"object_names,omitempty"`
	Format      string   `json:"format,omitempty"`
}

// SummaryScript prints the scene digest as one JSON line.
const SummaryScript = `
import bpy, json
objs = bpy.data.objects
meshes = bpy.data.meshes
mats = bpy.data.materials
verts = sum(len(m.vertices) for m in meshes)
tris = sum(len(m.loop_triangles) for m in meshes)
print("BLENDER_SUMMARY " + json.dumps({
  "objects": len(objs), "meshes": len(meshes), "materials": len(mats),
  "vertices": verts, "tris": tris,
  "object_names": [o.name for o in objs][:32],
}))
`

// Summary returns the scene digest for blendPath ("" = default scene).
func Summary(ctx context.Context, blendPath string, timeout time.Duration) (*SceneSummary, error) {
	out, err := runBlender(ctx, blendPath, SummaryScript, timeout)
	if err != nil {
		return nil, err
	}
	line, ok := findMarker(out, "BLENDER_SUMMARY ")
	if !ok {
		return nil, fmt.Errorf("blender: summary marker missing (output %d bytes)", len(out))
	}
	var s SceneSummary
	if err := json.Unmarshal([]byte(line), &s); err != nil {
		return nil, fmt.Errorf("blender: bad summary json: %v", err)
	}
	s.Format = filepath.Ext(blendPath)
	return &s, nil
}

// Result is one operation run outcome.
type Result struct {
	Script string `json:"script,omitempty"`
	Output string `json:"output"` // trimmed, ≤2KB
	OK     bool   `json:"ok"`
}

// RunScript executes a bpy script against blendPath ("" = default scene) and
// saves the result back to blendPath when save is true. Always backs up first
// when save is true.
func RunScript(ctx context.Context, blendPath, script string, save bool, timeout time.Duration) (*Result, error) {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	if save && blendPath != "" {
		if err := backup(blendPath); err != nil {
			return nil, err
		}
	}
	script = strings.TrimSpace(script) + "\n"
	if save && blendPath != "" {
		script += fmt.Sprintf("\nbpy.ops.wm.save_as_mainfile(filepath=%q)\n", blendPath)
	}
	out, err := runBlender(ctx, blendPath, script, timeout)
	if err != nil {
		return nil, err
	}
	return &Result{Script: summarizeScript(script), Output: capOut(out), OK: true}, nil
}

// ConvertMesh imports a mesh file (obj/stl/ply) into Blender and exports it to
// outPath in the given format (gltf/glb/fbx/obj/stl). Uses the local Blender;
// returns an error when Blender is unavailable.
func ConvertMesh(ctx context.Context, srcPath, outPath, format string, timeout time.Duration) (*Result, error) {
	if BlenderPath() == "" {
		return nil, fmt.Errorf("blender: not found (required for %s conversion)", format)
	}
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	ext := strings.ToLower(filepath.Ext(srcPath))
	importOp := map[string]string{
		".obj": "bpy.ops.wm.obj_import(filepath=%q)",
		".stl": "bpy.ops.wm.stl_import(filepath=%q)",
		".ply": "bpy.ops.wm.ply_import(filepath=%q)",
	}[ext]
	if importOp == "" {
		return nil, fmt.Errorf("blender: unsupported source format %q", ext)
	}
	export := map[string]string{
		"gltf": `bpy.ops.export_scene.gltf(filepath=%q, export_format="GLTF_SEPARATE")`,
		"glb":  `bpy.ops.export_scene.gltf(filepath=%q, export_format="GLB")`,
		"fbx":  `bpy.ops.export_scene.fbx(filepath=%q)`,
		"obj":  `bpy.ops.wm.obj_export(filepath=%q)`,
		"stl":  `bpy.ops.wm.stl_export(filepath=%q)`,
	}[format]
	if export == "" {
		return nil, fmt.Errorf("blender: unsupported target format %q", format)
	}
	script := fmt.Sprintf(`
import bpy
`+importOp+`
bpy.ops.object.select_all(action="DESELECT")
for o in bpy.data.objects:
    if o.type == "MESH":
        o.select_set(True)
out = %q
`+export+`
print("BLENDER_CONVERT_OK " + out)
`, srcPath, outPath, outPath)
	out, err := runBlender(ctx, "", script, timeout)
	if err != nil {
		return nil, err
	}
	if _, ok := findMarker(out, "BLENDER_CONVERT_OK"); !ok {
		return nil, fmt.Errorf("blender: convert failed (output %d bytes)", len(out))
	}
	return &Result{Script: "convert " + format, Output: capOut(out), OK: true}, nil
}

// Convert exports blendPath to outPath in the given format (gltf/glb/fbx/obj/stl).
func Convert(ctx context.Context, blendPath, outPath, format string, timeout time.Duration) (*Result, error) {
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	script := fmt.Sprintf(`
import bpy
bpy.ops.wm.open_mainfile(filepath=%q)
out = %q
fmt = %q
if fmt in ("gltf","glb"):
    bpy.ops.export_scene.gltf(filepath=out, export_format="GLTF_SEPARATE" if fmt=="gltf" else "GLB")
elif fmt == "fbx":
    bpy.ops.export_scene.fbx(filepath=out)
elif fmt == "obj":
    bpy.ops.wm.obj_export(filepath=out)
elif fmt == "stl":
    bpy.ops.wm.stl_export(filepath=out)
else:
    raise SystemExit("unsupported format " + fmt)
print("BLENDER_CONVERT_OK " + out)
`, blendPath, outPath, format)
	out, err := runBlender(ctx, "", script, timeout)
	if err != nil {
		return nil, err
	}
	if _, ok := findMarker(out, "BLENDER_CONVERT_OK"); !ok {
		return nil, fmt.Errorf("blender: convert failed (output %d bytes)", len(out))
	}
	return &Result{Script: "convert " + format, Output: capOut(out), OK: true}, nil
}

func runBlender(ctx context.Context, blendPath, script string, timeout time.Duration) (string, error) {
	exe := BlenderPath()
	if exe == "" {
		return "", fmt.Errorf("blender: executable not found")
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	args := []string{"-b"}
	if blendPath != "" {
		args = append(args, blendPath)
	}
	// Blender's -P requires a real file path (stdin "-" is treated as a path).
	tmp, err := os.CreateTemp("", "blender_script_*.py")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.WriteString(script); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	args = append(args, "-P", tmpName)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, exe, args...)
	out, err := cmd.CombinedOutput()
	if runCtx.Err() != nil {
		return "", fmt.Errorf("blender: timeout after %v", timeout)
	}
	if err != nil {
		return "", fmt.Errorf("blender: %v (output %d bytes)", err, len(out))
	}
	return string(out), nil
}

func backup(path string) error {
	bak := path + ".bak"
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(bak, data, 0o644)
}

func findMarker(out, marker string) (string, bool) {
	for _, line := range strings.Split(out, "\n") {
		if i := strings.Index(line, marker); i >= 0 {
			return strings.TrimSpace(line[i+len(marker):]), true
		}
	}
	return "", false
}

func capOut(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 2048 {
		return s[:2048]
	}
	return s
}

func summarizeScript(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 160 {
		return s[:160] + "…"
	}
	return s
}

// ---------------------------------------------------------------------------
// Heavy modelling primitives (Blender backend): retopology and UV unwrap.
// Both import a mesh, apply the operation, and save back to the same path
// (backed up as <path>.bak by the caller). Pure-Go ops live in
// internal/modeling/meshparse; these need Blender's geometry kernels.
// ---------------------------------------------------------------------------

// Retopo retopologizes a mesh at an approximate target face count using
// Blender's voxel remesh. The file at srcPath is rewritten in place.
// targetFaces is a hint: voxel remesh derives size from the mesh's bounding
// box, so the result is approximate (documented in the returned summary).
func Retopo(ctx context.Context, srcPath string, targetFaces int, timeout time.Duration) (*Result, error) {
	if BlenderPath() == "" {
		return nil, fmt.Errorf("blender: not found (required for retopo)")
	}
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	script := fmt.Sprintf(`
import bpy, math
bpy.ops.wm.obj_import(filepath=%q)
bpy.ops.object.select_all(action="DESELECT")
sel = [o for o in bpy.data.objects if o.type == "MESH"]
for o in sel:
    o.select_set(True)
if not sel:
    raise SystemExit("no mesh to retopo")
bpy.context.view_layer.objects.active = sel[0]
# Voxel size from bounding box: max_dim / cbrt(2 * target)
dims = []
for o in sel:
    dims.append(max(o.dimensions))
max_dim = max(dims) if dims else 1.0
target = max(100, %d)
vsize = max_dim / math.pow(2.0 * target, 1.0/3.0)
bpy.ops.object.modifier_add(type="REMESH")
mod = bpy.context.object.modifiers[-1]
mod.mode = "VOXEL"
mod.voxel_size = vsize
mod.adaptivity = 0.0
bpy.ops.object.modifier_apply(modifier=mod.name)
bpy.ops.wm.obj_export(filepath=%q)
print(f"BLENDER_RETOPO_OK target_approx={target}")
`, srcPath, targetFaces, srcPath)
	out, err := runBlender(ctx, "", script, timeout)
	if err != nil {
		return nil, err
	}
	if _, ok := findMarker(out, "BLENDER_RETOPO_OK"); !ok {
		return nil, fmt.Errorf("blender: retopo failed (output %d bytes)", len(out))
	}
	return &Result{Script: fmt.Sprintf("retopo target~%d", targetFaces), Output: capOut(out), OK: true}, nil
}

// Unwrap unwraps UVs of all mesh objects in the file (smart UV project).
func Unwrap(ctx context.Context, srcPath string, timeout time.Duration) (*Result, error) {
	if BlenderPath() == "" {
		return nil, fmt.Errorf("blender: not found (required for unwrap)")
	}
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	script := fmt.Sprintf(`
import bpy
bpy.ops.wm.obj_import(filepath=%q)
bpy.ops.object.select_all(action="DESELECT")
for o in bpy.data.objects:
    if o.type == "MESH":
        o.select_set(True)
if bpy.context.selected_objects:
    bpy.context.view_layer.objects.active = bpy.context.selected_objects[0]
    bpy.ops.object.mode_set(mode="EDIT")
    bpy.ops.mesh.select_all(action="SELECT")
    bpy.ops.uv.smart_project(angle_limit=66, island_margin=0.02)
    bpy.ops.object.mode_set(mode="OBJECT")
bpy.ops.wm.obj_export(filepath=%q)
print("BLENDER_UNWRAP_OK")
`, srcPath, srcPath)
	out, err := runBlender(ctx, "", script, timeout)
	if err != nil {
		return nil, err
	}
	if _, ok := findMarker(out, "BLENDER_UNWRAP_OK"); !ok {
		return nil, fmt.Errorf("blender: unwrap failed (output %d bytes)", len(out))
	}
	return &Result{Script: "unwrap", Output: capOut(out), OK: true}, nil
}
