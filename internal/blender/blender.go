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
