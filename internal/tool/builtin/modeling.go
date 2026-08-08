package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/blender"
	"reasonix/internal/modeling/meshparse"
	"reasonix/internal/tool"
	"time"
)

// ---------------------------------------------------------------------------
// Modelling tools (docs/MODELLING_OVERHAUL.md §6-C): give the agent precise,
// low-token control over polygonal meshes / voxels — pure Go, no Blender.
//   modeling_analyze  — compact geometric descriptor (~40 token)
//   modeling_optimize — deterministic ops (cleanup/triangulate/merge/decimate)
//   modeling_convert  — format conversion (obj/stl/ply/vox)
//   modeling_voxel    — voxelize a mesh to .vox + descriptor
// ---------------------------------------------------------------------------

func init() {
	tool.RegisterBuiltin(modelingAnalyze{})
	tool.RegisterBuiltin(modelingOptimize{})
	tool.RegisterBuiltin(modelingConvert{})
	tool.RegisterBuiltin(modelingVoxel{})
	tool.RegisterBuiltin(modelingAtomic{})
}

const (
	modelingMaxFileSize = 1 << 28 // 256MB cap
	modelingMaxFaces    = 5_000_000
)

// modelingResolvePath resolves a possibly-relative mesh path against the
// workspace dir and cleans it; empty workDir keeps the path as-is.
func modelingResolvePath(workDir, path string) string {
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) && workDir != "" {
		path = filepath.Join(workDir, path)
	}
	return filepath.Clean(path)
}

// modelingGuardRead confines mesh reads to the workspace: the resolved path
// must stay inside workDir (when bound) and outside the forbid-read roots
// (session temp etc.) — a mesh path cannot smuggle arbitrary files.
func modelingGuardRead(workDir string, forbidRoots []string, path string) error {
	if workDir != "" {
		// Compare against a symlink-free baseline: on macOS /var is a system
		// symlink to /private/var, so TempDir() under /var/folders resolves
		// elsewhere — comparing the resolved path against a raw workDir would
		// falsely reject every in-root read there. realPath (same helper the
		// write side uses) resolves the deepest existing ancestor + tail.
		realWork, err := realPath(workDir)
		if err != nil {
			realWork = workDir
		}
		rel, err := filepath.Rel(realWork, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
		}
		// Symlink escape: a link inside workDir may point outside it. Resolve
		// when the file exists (parse would read it) and require the resolved
		// path to stay inside the symlink-free baseline, mirroring confine's
		// realPath handling.
		if real, err := filepath.EvalSymlinks(path); err == nil {
			rrel, rerr := filepath.Rel(realWork, real)
			if rerr != nil || rrel == ".." || strings.HasPrefix(rrel, ".."+string(os.PathSeparator)) {
				return &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
			}
		}
	}
	if confineRead(forbidRoots, path) {
		return &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}
	return nil
}

// modelingGuardWrite rejects writes outside the workspace write roots and
// applies the session data guard (same as write_file): modeling tools must not
// overwrite session/agent-owned data even inside the write root.
func modelingGuardWrite(roots []string, guard SessionDataGuard, target string) error {
	if err := confine(roots, target); err != nil {
		return err
	}
	return guard.Check(target)
}

// ---------------------------------------------------------------------------
// modeling_analyze
// ---------------------------------------------------------------------------

type modelingAnalyze struct {
	workDir     string
	forbidRoots []string
}

func (modelingAnalyze) Name() string { return "modeling_analyze" }

func (modelingAnalyze) Description() string {
	return "Compute a compact geometric descriptor of a mesh file (obj/stl/ply/gltf) or voxel file (.vox) — ~40 token summary (verts/faces/tris/components/manifold/watertight/bounds/quality). Raw geometry is never returned; use this to perceive a model precisely with minimal tokens."
}

func (modelingAnalyze) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "path":{"type":"string","description":"Mesh (.obj/.stl/.ply) or voxel (.vox) file path."}
},
"required":["path"]
}`)
}

func (modelingAnalyze) ReadOnly() bool { return true }

func (r modelingAnalyze) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var a struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("modeling_analyze: %w", err)
	}
	if a.Path == "" {
		return "", fmt.Errorf("modeling_analyze: path required")
	}
	a.Path = modelingResolvePath(r.workDir, a.Path)
	if err := modelingGuardRead(r.workDir, r.forbidRoots, a.Path); err != nil {
		return "", err
	}
	ext := strings.ToLower(filepath.Ext(a.Path))
	if ext == ".vox" {
		vm, err := meshparse.ParseVoxPath(a.Path)
		if err != nil {
			return "", fmt.Errorf("modeling_analyze: %w", err)
		}
		d := meshparse.AnalyzeVox(vm)
		b, _ := json.Marshal(d)
		return string(b), nil
	}
	m, err := meshparse.Parse(a.Path)
	if err != nil {
		return "", fmt.Errorf("modeling_analyze: %w", err)
	}
	d := meshparse.Analyze(m)
	b, _ := json.Marshal(d)
	return string(b), nil
}

// ---------------------------------------------------------------------------
// modeling_optimize
// ---------------------------------------------------------------------------

type modelingOptimize struct {
	workDir     string
	forbidRoots []string
	roots       []string
	guard       SessionDataGuard
}

func (modelingOptimize) Name() string { return "modeling_optimize" }

func (modelingOptimize) Description() string {
	return "Apply a deterministic mesh operation (cleanup/triangulate/merge/decimate) to a mesh file (obj/stl/ply). The file is backed up to <path>.bak first; returns the before/after stat delta (token-minimal verification). decimate target is the desired face count."
}

func (modelingOptimize) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "path":{"type":"string","description":"Mesh file path (.obj/.stl/.ply)."},
  "op":{"type":"string","enum":["cleanup","triangulate","merge","decimate","retopo","unwrap"],"description":"Operation to apply (retopo/unwrap require Blender)."},
  "eps":{"type":"number","description":"Weld/merge epsilon (cleanup/merge); default 0 (no weld)."},
  "target_faces":{"type":"integer","description":"Target face count for decimate."}
},
"required":["path","op"]
}`)
}

func (modelingOptimize) ReadOnly() bool { return false }

func (r modelingOptimize) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var a struct {
		Path        string  `json:"path"`
		Op          string  `json:"op"`
		Eps         float64 `json:"eps"`
		TargetFaces int     `json:"target_faces"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("modeling_optimize: %w", err)
	}
	if a.Path == "" {
		return "", fmt.Errorf("modeling_optimize: path required")
	}
	a.Path = modelingResolvePath(r.workDir, a.Path)
	if err := modelingGuardRead(r.workDir, r.forbidRoots, a.Path); err != nil {
		return "", err
	}
	if err := modelingGuardWrite(r.roots, r.guard, a.Path); err != nil {
		return "", err
	}
	m, err := meshparse.Parse(a.Path)
	if err != nil {
		return "", fmt.Errorf("modeling_optimize: %w", err)
	}
	if len(m.Faces) > modelingMaxFaces {
		return "", fmt.Errorf("modeling_optimize: %d faces exceeds cap %d", len(m.Faces), modelingMaxFaces)
	}

	var out *meshparse.Mesh
	var res meshparse.OpResult
	switch a.Op {
	case "cleanup":
		out, res = meshparse.Cleanup(m, a.Eps)
	case "triangulate":
		out, res = meshparse.Triangulate(m)
	case "merge":
		if a.Eps <= 0 {
			return "", fmt.Errorf("modeling_optimize: merge requires eps>0")
		}
		out, res = meshparse.MergeVerts(m, a.Eps)
	case "decimate":
		if a.TargetFaces <= 0 {
			return "", fmt.Errorf("modeling_optimize: decimate requires target_faces>0")
		}
		out, res = meshparse.Decimate(m, a.TargetFaces)
	case "retopo":
		// Blender-heavy op: operates on the file directly (with .bak backup).
		if err := copyFile(a.Path, a.Path+".bak"); err != nil {
			return "", fmt.Errorf("modeling_optimize: backup: %w", err)
		}
		_, err := blender.Retopo(ctx, a.Path, a.TargetFaces, 0)
		if err != nil {
			return "", fmt.Errorf("modeling_optimize retopo: %w", err)
		}
		after, perr := meshparse.Parse(a.Path)
		if perr != nil {
			return "", fmt.Errorf("modeling_optimize: verify retopo: %w", perr)
		}
		res = meshparse.OpResult{Op: "retopo", VertsBefore: len(m.Verts), FacesBefore: len(m.Faces),
			VertsAfter: len(after.Verts), FacesAfter: len(after.Faces)}
		b, _ := json.Marshal(res)
		return string(b), nil
	case "unwrap":
		if err := copyFile(a.Path, a.Path+".bak"); err != nil {
			return "", fmt.Errorf("modeling_optimize: backup: %w", err)
		}
		if _, err := blender.Unwrap(ctx, a.Path, 0); err != nil {
			return "", fmt.Errorf("modeling_optimize unwrap: %w", err)
		}
		return `{"op":"unwrap"}`, nil
	default:
		return "", fmt.Errorf("modeling_optimize: unknown op %q", a.Op)
	}

	// Backup then write back.
	bak := a.Path + ".bak"
	if err := copyFile(a.Path, bak); err != nil {
		return "", fmt.Errorf("modeling_optimize: backup: %w", err)
	}
	if err := writeMesh(a.Path, out); err != nil {
		return "", fmt.Errorf("modeling_optimize: write: %w", err)
	}
	b, _ := json.Marshal(res)
	return string(b), nil
}

// ---------------------------------------------------------------------------
// modeling_convert
// ---------------------------------------------------------------------------

type modelingConvert struct {
	workDir     string
	forbidRoots []string
	roots       []string
	guard       SessionDataGuard
}

func (modelingConvert) Name() string { return "modeling_convert" }

func (modelingConvert) Description() string {
	return "Convert a mesh/voxel file to another format (pure Go). Supported: obj/stl/ply/vox. Output path default = input with new extension."
}

func (modelingConvert) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "path":{"type":"string","description":"Source file."},
  "format":{"type":"string","enum":["obj","stl","ply","vox","gltf","glb","fbx"],"description":"Target format (extension). gltf/glb/fbx require Blender."},
  "out":{"type":"string","description":"Optional output path (default: input with new extension)."}
},
"required":["path","format"]
}`)
}

func (modelingConvert) ReadOnly() bool { return false }

func (r modelingConvert) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var a struct {
		Path   string `json:"path"`
		Format string `json:"format"`
		Out    string `json:"out"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("modeling_convert: %w", err)
	}
	if a.Path == "" {
		return "", fmt.Errorf("modeling_convert: path required")
	}
	a.Path = modelingResolvePath(r.workDir, a.Path)
	if err := modelingGuardRead(r.workDir, r.forbidRoots, a.Path); err != nil {
		return "", err
	}
	ext := strings.ToLower(filepath.Ext(a.Path))
	if ext == ".vox" {
		vm, err := meshparse.ParseVoxPath(a.Path)
		if err != nil {
			return "", fmt.Errorf("modeling_convert: %w", err)
		}
		outPath := a.Out
		if outPath == "" {
			outPath = strings.TrimSuffix(a.Path, ext) + "." + a.Format
		}
		outPath = modelingResolvePath(r.workDir, outPath)
		if err := modelingGuardWrite(r.roots, r.guard, outPath); err != nil {
			return "", err
		}
		switch a.Format {
		case "vox":
			if err := vm.WriteVox(outPath); err != nil {
				return "", fmt.Errorf("modeling_convert: %w", err)
			}
		default:
			return "", fmt.Errorf("modeling_convert: vox→%s unsupported (use modeling_voxel for mesh→vox)", a.Format)
		}
		return fmt.Sprintf(`{"op":"convert","from":"vox","to":%q,"out":%q}`, a.Format, outPath), nil
	}
	m, err := meshparse.Parse(a.Path)
	if err != nil {
		return "", fmt.Errorf("modeling_convert: %w", err)
	}
	outPath := a.Out
	if outPath == "" {
		outPath = strings.TrimSuffix(a.Path, ext) + "." + a.Format
	}
	outPath = modelingResolvePath(r.workDir, outPath)
	if err := modelingGuardWrite(r.roots, r.guard, outPath); err != nil {
		return "", err
	}
	switch a.Format {
	case "obj", "stl", "ply":
		if err := writeMeshFormat(outPath, m, a.Format); err != nil {
			return "", fmt.Errorf("modeling_convert: %w", err)
		}
	case "gltf", "glb", "fbx":
		// Heavy formats need Blender as the import/export backend.
		res, err := blender.ConvertMesh(ctx, a.Path, outPath, a.Format, 0)
		if err != nil {
			return "", fmt.Errorf("modeling_convert (%s): %w", a.Format, err)
		}
		_ = res
	default:
		return "", fmt.Errorf("modeling_convert: unsupported target format %q", a.Format)
	}
	return fmt.Sprintf(`{"op":"convert","from":%q,"to":%q,"out":%q}`, ext[1:], a.Format, outPath), nil
}

// ---------------------------------------------------------------------------
// modeling_voxel
// ---------------------------------------------------------------------------

type modelingVoxel struct {
	workDir     string
	forbidRoots []string
	roots       []string
	guard       SessionDataGuard
}

func (modelingVoxel) Name() string { return "modeling_voxel" }

func (modelingVoxel) Description() string {
	return "Voxelize a closed mesh into a .vox model at the given resolution (longest axis, 4..512). Writes <path>.vox and returns the voxel descriptor (size/filled/colors/components/solidity)."
}

func (modelingVoxel) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "path":{"type":"string","description":"Mesh file path (.obj/.stl/.ply)."},
  "resolution":{"type":"integer","description":"Voxel grid resolution along the longest axis (default 64, range 4..512)."}
},
"required":["path"]
}`)
}

func (modelingVoxel) ReadOnly() bool { return false }

func (r modelingVoxel) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var a struct {
		Path       string `json:"path"`
		Resolution int    `json:"resolution"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("modeling_voxel: %w", err)
	}
	if a.Path == "" {
		return "", fmt.Errorf("modeling_voxel: path required")
	}
	if a.Resolution == 0 {
		a.Resolution = 64
	}
	a.Path = modelingResolvePath(r.workDir, a.Path)
	if err := modelingGuardRead(r.workDir, r.forbidRoots, a.Path); err != nil {
		return "", err
	}
	m, err := meshparse.Parse(a.Path)
	if err != nil {
		return "", fmt.Errorf("modeling_voxel: %w", err)
	}
	vm, err := meshparse.Voxelize(m, a.Resolution)
	if err != nil {
		return "", fmt.Errorf("modeling_voxel: %w", err)
	}
	outPath := modelingResolvePath(r.workDir, a.Path+".vox")
	if err := modelingGuardWrite(r.roots, r.guard, outPath); err != nil {
		return "", err
	}
	if err := vm.WriteVox(outPath); err != nil {
		return "", fmt.Errorf("modeling_voxel: %w", err)
	}
	d := meshparse.AnalyzeVox(vm)
	d.Format = "vox"
	b, _ := json.Marshal(d)
	return fmt.Sprintf("%s\nout=%s", string(b), outPath), nil
}

// ---------------------------------------------------------------------------
// mesh writers (obj/stl/ply)
// ---------------------------------------------------------------------------

func writeMesh(path string, m *meshparse.Mesh) error {
	ext := strings.ToLower(filepath.Ext(path))
	return writeMeshFormat(path, m, strings.TrimPrefix(ext, "."))
}

func writeMeshFormat(path string, m *meshparse.Mesh, format string) error {
	var b strings.Builder
	switch format {
	case "obj":
		for _, v := range m.Verts {
			fmt.Fprintf(&b, "v %.6g %.6g %.6g\n", v.X, v.Y, v.Z)
		}
		for _, f := range m.Faces {
			fmt.Fprintf(&b, "f")
			for _, vi := range f.Verts {
				fmt.Fprintf(&b, " %d", vi+1)
			}
			b.WriteString("\n")
		}
	case "stl":
		b.WriteString("solid converted\n")
		for _, f := range m.Faces {
			if len(f.Verts) < 3 {
				continue
			}
			p0, p1, p2 := m.Verts[f.Verts[0]], m.Verts[f.Verts[1]], m.Verts[f.Verts[2]]
			nx, ny, nz := faceNormal(p0, p1, p2)
			fmt.Fprintf(&b, "facet normal %.6g %.6g %.6g\nouter loop\n", nx, ny, nz)
			for i := 0; i < 3; i++ {
				p := m.Verts[f.Verts[i]]
				fmt.Fprintf(&b, "vertex %.6g %.6g %.6g\n", p.X, p.Y, p.Z)
			}
			b.WriteString("endloop\nendfacet\n")
		}
		b.WriteString("endsolid converted\n")
	case "ply":
		b.WriteString("ply\nformat ascii 1.0\n")
		fmt.Fprintf(&b, "element vertex %d\nproperty float x\nproperty float y\nproperty float z\n", len(m.Verts))
		fmt.Fprintf(&b, "element face %d\nproperty list uchar int vertex_indices\nend_header\n", len(m.Faces))
		for _, v := range m.Verts {
			fmt.Fprintf(&b, "%.6g %.6g %.6g\n", v.X, v.Y, v.Z)
		}
		for _, f := range m.Faces {
			fmt.Fprintf(&b, "%d", len(f.Verts))
			for _, vi := range f.Verts {
				fmt.Fprintf(&b, " %d", vi)
			}
			b.WriteString("\n")
		}
	default:
		return fmt.Errorf("unsupported write format %q", format)
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func faceNormal(a, b, c meshparse.Vec3) (float64, float64, float64) {
	ux, uy, uz := b.X-a.X, b.Y-a.Y, b.Z-a.Z
	vx, vy, vz := c.X-a.X, c.Y-a.Y, c.Z-a.Z
	nx, ny, nz := uy*vz-uz*vy, uz*vx-ux*vz, ux*vy-uy*vx
	l := math.Sqrt(nx*nx + ny*ny + nz*nz)
	if l == 0 {
		return 0, 0, 1
	}
	return nx / l, ny / l, nz / l
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// ---------------------------------------------------------------------------
// modeling_atomic — L3GO-style deterministic atomic ops (docs/MODELLING_AGENT_
// RESEARCH.md §③): the agent picks an op + args instead of writing raw bpy.
// ---------------------------------------------------------------------------

type modelingAtomic struct {
	workDir     string
	forbidRoots []string
	roots       []string
	guard       SessionDataGuard
}

func (r modelingAtomic) Name() string { return "modeling_atomic" }

func (r modelingAtomic) Description() string {
	return "Run a deterministic Blender atomic operation (add_cube/add_uv_sphere/add_cylinder/boolean/bevel/delete_object) on the default scene or a .blend file. Ops are parameterized bpy snippets — precise and low-token. Args: op (name), args (object of op parameters), path (optional .blend). NOT ReadOnly: mutates the Blender scene (or saves the .blend when a path is given)."
}

func (r modelingAtomic) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "op":{"type":"string","description":"Atomic op name: add_cube, add_uv_sphere, add_cylinder, boolean, bevel, delete_object"},
  "args":{"type":"object","description":"Op parameters (see modeling_atomic_ops for the full registry)"},
  "path":{"type":"string","description":"Optional .blend path (default: default scene)"}
},
"required":["op"]
}`)
}

func (r modelingAtomic) ReadOnly() bool { return false }

func (r modelingAtomic) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var a struct {
		Op   string         `json:"op"`
		Args map[string]any `json:"args"`
		Path string         `json:"path"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("modeling_atomic: %w", err)
	}
	if a.Op == "" {
		return "", fmt.Errorf("modeling_atomic: op required")
	}
	if _, ok := blender.FindAtomicOp(a.Op); !ok {
		return "", fmt.Errorf("modeling_atomic: unknown op %q (registry: %s)", a.Op, opNames())
	}
	if a.Path != "" {
		a.Path = modelingResolvePath(r.workDir, a.Path)
		if err := modelingGuardRead(r.workDir, r.forbidRoots, a.Path); err != nil {
			return "", err
		}
		// save=true writes the .blend back — guard the write the same way the
		// other modeling tools do (roots + session data).
		if err := modelingGuardWrite(r.roots, r.guard, a.Path); err != nil {
			return "", err
		}
	}
	res, err := blender.RunAtomic(ctx, a.Path, a.Op, a.Args, a.Path != "", 120*time.Second)
	if err != nil {
		return "", fmt.Errorf("modeling_atomic %s: %w", a.Op, err)
	}
	b, _ := json.Marshal(struct {
		Op  string `json:"op"`
		OK  bool   `json:"ok"`
		Out string `json:"output,omitempty"`
	}{Op: a.Op, OK: true, Out: trimOutput(res.Output)})
	return string(b), nil
}

func opNames() string {
	names := make([]string, 0, len(blender.AtomicOps))
	for _, o := range blender.AtomicOps {
		names = append(names, o.Name)
	}
	return strings.Join(names, ", ")
}

func trimOutput(out string) string {
	out = strings.TrimSpace(out)
	if len(out) > 300 {
		out = out[:300] + "..."
	}
	return out
}
