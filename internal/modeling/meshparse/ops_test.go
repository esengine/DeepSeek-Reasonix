package meshparse

import (
	"strings"
	"testing"
)

// sphere-ish grid: 10x10 quad grid → 200 tris, 121 verts.
const gridOBJ = `# 10x10 grid (quads)
v 0 0 0
v 1 0 0
v 2 0 0
v 3 0 0
v 4 0 0
v 5 0 0
v 6 0 0
v 7 0 0
v 8 0 0
v 9 0 0
v 10 0 0
`

func quadGridMesh() *Mesh {
	// Build a 10x10 grid programmatically: verts (11x11), quads.
	m := &Mesh{Format: "obj"}
	n := 11
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			m.Verts = append(m.Verts, Vec3{float64(x), float64(y), 0})
		}
	}
	for y := 0; y < n-1; y++ {
		for x := 0; x < n-1; x++ {
			a := y*n + x
			m.Faces = append(m.Faces, Face{Verts: []int{a, a + 1, a + n + 1, a + n}})
		}
	}
	return m
}

func TestTriangulateGrid(t *testing.T) {
	m := quadGridMesh()
	out, res := Triangulate(m)
	if res.VertsBefore != 121 || res.FacesBefore != 100 {
		t.Fatalf("grid before: %d verts %d faces", res.VertsBefore, res.FacesBefore)
	}
	if res.FacesAfter != 200 {
		t.Errorf("triangulate faces = %d, want 200 (100 quads → 200 tris)", res.FacesAfter)
	}
	if len(out.Faces) != 200 || len(out.Verts) != 121 {
		t.Errorf("out mesh %d verts %d faces", len(out.Verts), len(out.Faces))
	}
}

func TestCleanupDegenerateAndWeld(t *testing.T) {
	// Mesh with a duplicate vertex and a degenerate face.
	obj := `v 0 0 0
v 1 0 0
v 0 1 0
v 0 0 0
v 1 1 0
f 1 2 3
f 4 5 1
f 2 2 2
`
	m, err := ParseOBJ(strings.NewReader(obj))
	if err != nil {
		t.Fatal(err)
	}
	out, res := Cleanup(m, 1e-6)
	if res.RemovedDup != 1 {
		t.Errorf("removed dup verts = %d, want 1", res.RemovedDup)
	}
	if res.RemovedDeg < 1 {
		t.Errorf("removed degenerate faces = %d, want ≥1", res.RemovedDeg)
	}
	if len(out.Faces) != 1 {
		t.Errorf("faces after cleanup = %d, want 1 (f 4 5 1 collapses to dup verts, f 2 2 2 degenerate)", len(out.Faces))
	}
	if res.VertsAfter != 4 {
		t.Errorf("verts after weld = %d, want 4", res.VertsAfter)
	}
}

func TestMergeVertsClusters(t *testing.T) {
	// 6 verts in 3 clusters of 2 (distance 0.05 < eps 0.1).
	obj := `v 0 0 0
v 0.05 0 0
v 5 0 0
v 5.05 0 0
v 10 0 0
v 10.05 0 0
f 1 2 3
f 4 5 6
`
	m, err := ParseOBJ(strings.NewReader(obj))
	if err != nil {
		t.Fatal(err)
	}
	out, res := MergeVerts(m, 0.1)
	if res.VertsAfter != 3 {
		t.Errorf("merge verts = %d, want 3 clusters", res.VertsAfter)
	}
	if len(out.Verts) != 3 {
		t.Errorf("out verts = %d, want 3", len(out.Verts))
	}
}

func TestDecimateGrid(t *testing.T) {
	m := quadGridMesh()
	tri, _ := Triangulate(m)
	if len(tri.Faces) != 200 {
		t.Fatalf("triangulated faces = %d, want 200", len(tri.Faces))
	}
	out, res := Decimate(m, 50) // below quad-face count → triangulate 200 → reduce to ≤50
	if res.FacesAfter <= 0 || res.FacesAfter > 50 {
		t.Errorf("decimate faces = %d, want ≤50 (target 50)", res.FacesAfter)
	}
	if res.FacesAfter >= res.FacesBefore {
		t.Errorf("decimate should reduce faces: %d → %d", res.FacesBefore, res.FacesAfter)
	}
	_ = out
}

func TestDecimateNoopWhenTargetMet(t *testing.T) {
	m := quadGridMesh()
	out, res := Decimate(m, 500) // target above current 100 faces
	if res.FacesAfter != 100 || len(out.Faces) != 100 {
		t.Errorf("noop decimate changed faces: %d → %d", res.FacesBefore, res.FacesAfter)
	}
}
