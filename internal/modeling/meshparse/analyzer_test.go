package meshparse

import "testing"


// TestPartDescriptorsTwoComponents: two disconnected cubes → two parts with
// correct markers, counts, and bounds; agent can address "<part_0>"/"<part_1>".
func TestPartDescriptorsTwoComponents(t *testing.T) {
	// Cube A at origin, cube B shifted +10 on X (disconnected).
	m := &Mesh{
		Verts: []Vec3{
			{0, 0, 0}, {1, 0, 0}, {1, 1, 0}, {0, 1, 0}, {0, 0, 1}, {1, 0, 1}, {1, 1, 1}, {0, 1, 1},
			{10, 0, 0}, {11, 0, 0}, {11, 1, 0}, {10, 1, 0}, {10, 0, 1}, {11, 0, 1}, {11, 1, 1}, {10, 1, 1},
		},
		Faces: append(cubeFaces(0), cubeFaces(8)...),
	}
	parts := PartDescriptors(m)
	if len(parts) != 2 {
		t.Fatalf("want 2 parts, got %d", len(parts))
	}
	p0, p1 := parts[0], parts[1]
	if p0.Idx != 0 || p1.Idx != 1 {
		t.Errorf("markers: want <part_0>/<part_1>, got %d/%d", p0.Idx, p1.Idx)
	}
	if p0.Verts != 8 || p1.Verts != 8 {
		t.Errorf("verts per part: %d/%d", p0.Verts, p1.Verts)
	}
	if p0.Faces != 6 || p1.Faces != 6 {
		t.Errorf("faces per part: %d/%d", p0.Faces, p1.Faces)
	}
	if p0.Bounds.Min[0] != 0 || p1.Bounds.Min[0] != 10 {
		t.Errorf("part bounds: p0.min.x=%v p1.min.x=%v", p0.Bounds.Min[0], p1.Bounds.Min[0])
	}
	if p0.Frac < 0.49 || p0.Frac > 0.51 {
		t.Errorf("frac: %v", p0.Frac)
	}
}

// TestVoxelTokensCompact: tokens are "x,y,z;c" per voxel, index-0 base,
// self-terminating — the exact compact form the agent reads instead of raw
// grid coordinates.
func TestVoxelTokensCompact(t *testing.T) {
	vm := &VoxelModel{Size: [3]int{2, 2, 1}}
	vm.Voxels = []Voxel{
		{X: 0, Y: 0, Z: 0, Color: 1},
		{X: 1, Y: 1, Z: 0, Color: 2},
	}
	toks := VoxelTokens(vm)
	if len(toks) != 2 {
		t.Fatalf("want 2 tokens, got %d", len(toks))
	}
	if toks[0] != "0,0,0;1" || toks[1] != "1,1,0;2" {
		t.Errorf("tokens: %v", toks)
	}
}

// cubeFaces returns the 6 quad faces of a cube whose 8 verts start at base.
func cubeFaces(base int) []Face {
	return []Face{
		{Verts: []int{base + 0, base + 1, base + 2, base + 3}},
		{Verts: []int{base + 4, base + 7, base + 6, base + 5}},
		{Verts: []int{base + 0, base + 4, base + 5, base + 1}},
		{Verts: []int{base + 3, base + 2, base + 6, base + 7}},
		{Verts: []int{base + 0, base + 3, base + 7, base + 4}},
		{Verts: []int{base + 1, base + 5, base + 6, base + 2}},
	}
}
