package meshparse

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const cubeOBJ = `# unit cube
v 0 0 0
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

func TestParseOBJQuadCube(t *testing.T) {
	m, err := ParseOBJ(strings.NewReader(cubeOBJ))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Verts) != 8 || len(m.Faces) != 6 {
		t.Fatalf("expected 8 verts 6 faces, got %d/%d", len(m.Verts), len(m.Faces))
	}
	d := Analyze(m)
	if d.Verts != 8 || d.Faces != 6 || d.Tris != 12 {
		t.Errorf("descriptor verts/faces/tris = %d/%d/%d, want 8/6/12", d.Verts, d.Faces, d.Tris)
	}
	if !d.Manifold || !d.Water || d.Comps != 1 {
		t.Errorf("cube should be manifold+watertight+1 comp, got %v/%v/%d", d.Manifold, d.Water, d.Comps)
	}
	if d.Degener != 0 {
		t.Errorf("cube has no degenerate faces, got %d", d.Degener)
	}
	if d.Quality < 0.99 {
		t.Errorf("cube quality should be ~1.0, got %v", d.Quality)
	}
}

func TestParseSTLAsciiTriangle(t *testing.T) {
	stl := `solid tri
facet normal 0 0 1
 outer loop
  vertex 0 0 0
  vertex 1 0 0
  vertex 0 1 0
 endloop
endfacet
endsolid tri
`
	m, err := ParseSTL(strings.NewReader(stl))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Verts) != 3 || len(m.Faces) != 1 {
		t.Fatalf("expected 3 verts 1 face, got %d/%d", len(m.Verts), len(m.Faces))
	}
	d := Analyze(m)
	if d.Tris != 1 || d.Edges != 3 {
		t.Errorf("descriptor tris/edges = %d/%d, want 1/3", d.Tris, d.Edges)
	}
}

func TestParsePLYAscii(t *testing.T) {
	ply := `ply
format ascii 1.0
element vertex 3
property float x
property float y
property float z
element face 1
property list uchar int vertex_indices
end_header
0 0 0
1 0 0
0 1 0
3 0 1 2
`
	m, err := ParsePLY(strings.NewReader(ply))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Verts) != 3 || len(m.Faces) != 1 {
		t.Fatalf("ply: %d verts %d faces", len(m.Verts), len(m.Faces))
	}
}

func TestParseByExtensionAndVoxelize(t *testing.T) {
	dir := t.TempDir()
	obj := filepath.Join(dir, "cube.obj")
	if err := os.WriteFile(obj, []byte(cubeOBJ), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Parse(obj)
	if err != nil {
		t.Fatal(err)
	}
	vm, err := Voxelize(m, 8)
	if err != nil {
		t.Fatal(err)
	}
	if vm.Size[0] != 8 || vm.Size[1] != 8 || vm.Size[2] != 8 {
		t.Errorf("voxel size = %v, want [8 8 8]", vm.Size)
	}
	// A solid unit cube at res 8 → interior cells > surface cells.
	if len(vm.Voxels) < 100 {
		t.Errorf("expected >100 filled voxels for solid cube, got %d", len(vm.Voxels))
	}
	d := AnalyzeVox(vm)
	if d.Comps != 1 || d.Colors != 1 {
		t.Errorf("vox descriptor comps/colors = %d/%d, want 1/1", d.Comps, d.Colors)
	}
	// Round-trip: write .vox and re-parse.
	vox := filepath.Join(dir, "cube.vox")
	if err := vm.WriteVox(vox); err != nil {
		t.Fatal(err)
	}
	vm2, err := ParseVoxPath(vox)
	if err != nil {
		t.Fatal(err)
	}
	if vm2.Size != vm.Size || len(vm2.Voxels) != len(vm.Voxels) {
		t.Errorf("vox round-trip mismatch: size %v/%v voxels %d/%d",
			vm2.Size, vm.Size, len(vm2.Voxels), len(vm.Voxels))
	}
}

func TestDescriptorBoundsAndComponents(t *testing.T) {
	// Two separated triangles (2 components).
	two := `v 0 0 0
v 1 0 0
v 0 1 0
v 10 10 10
v 11 10 10
v 10 11 10
f 1 2 3
f 4 5 6
`
	m, err := ParseOBJ(strings.NewReader(two))
	if err != nil {
		t.Fatal(err)
	}
	d := Analyze(m)
	if d.Comps != 2 {
		t.Errorf("components = %d, want 2", d.Comps)
	}
	if d.Water {
		t.Error("open triangles must not be watertight")
	}
	if d.Holes < 2 {
		t.Errorf("two open tris → ≥2 holes (boundary loops), got %d", d.Holes)
	}
	if d.Bounds.Size[0] < 9.9 || d.Bounds.Diameter < 15 {
		t.Errorf("bounds size/diameter = %v/%v, want spanning components",
			d.Bounds.Size, d.Bounds.Diameter)
	}
}
