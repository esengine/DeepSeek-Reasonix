package meshparse

import (
	"fmt"
	"math"
	"sort"
)

// ---------------------------------------------------------------------------
// MeshAnalyzer: compact geometric descriptor (docs/MODELLING_OVERHAUL.md §3).
// The descriptor (~40 token) is the ONLY mesh representation fed to the LLM —
// raw geometry never enters the prompt. Analysis is read-only and side-effect
// free.
// ---------------------------------------------------------------------------

// Descriptor is the compact, JSON-serializable mesh summary.
type Descriptor struct {
	Format   string   `json:"format"`
	Verts    int      `json:"verts"`
	Faces    int      `json:"faces"`
	Tris     int      `json:"tris"` // after triangulation (faces with n>3 split)
	Edges    int      `json:"edges"` // unique undirected edges
	Comps    int      `json:"components"`
	Manifold bool     `json:"manifold"` // every edge borders ≤2 faces
	Degener  int      `json:"degenerate"` // faces with zero area / dup verts
	Holes    int      `json:"holes"`     // boundary loops (open edges)
	Bounds   Bounds   `json:"bounds"`
	UV       UVInfo   `json:"uv"`
	Mats     int      `json:"materials"`
	Quality  float64  `json:"quality"` // 0..1 heuristic (manifold+clean+watertight)
	Water    bool     `json:"watertight"` // closed: no boundary edges
}

// Bounds is the axis-aligned bounding box (compact form).
type Bounds struct {
	Min      [3]float64 `json:"min"`
	Max      [3]float64 `json:"max"`
	Size     [3]float64 `json:"size"`
	Diameter float64    `json:"diameter"`
}

// UVInfo describes UV presence/coverage.
type UVInfo struct {
	Present bool `json:"present"`
	Seams   int  `json:"seams,omitempty"`
}

// Analyze computes the compact descriptor for a mesh.
func Analyze(m *Mesh) Descriptor {
	d := Descriptor{
		Format:    m.Format,
		Verts:     len(m.Verts),
		Faces:     len(m.Faces),
		Mats:      len(m.Materials),
		Manifold:  true,
		Water:     true,
		UV:        UVInfo{Present: len(m.UVs) > 0},
	}
	if len(m.Verts) == 0 {
		return d
	}

	// Bounds.
	minV, maxV := m.Bounds()
	d.Bounds = Bounds{
		Min: [3]float64{minV.X, minV.Y, minV.Z},
		Max: [3]float64{maxV.X, maxV.Y, maxV.Z},
		Size: [3]float64{
			maxV.X - minV.X, maxV.Y - minV.Y, maxV.Z - minV.Z,
		},
	}
	dx := maxV.X - minV.X
	dy := maxV.Y - minV.Y
	dz := maxV.Z - minV.Z
	d.Bounds.Diameter = math.Sqrt(dx*dx + dy*dy + dz*dz)

	// Triangulation count + degenerate detection.
	tris := 0
	deg := 0
	for _, f := range m.Faces {
		n := len(f.Verts)
		if n < 3 {
			deg++
			continue
		}
		tris += n - 2
		if faceArea(m, f) < 1e-12 {
			deg++
		}
	}
	d.Tris = tris
	d.Degener = deg

	// Edge manifold / watertight / holes (via directed-edge boundary count).
	edgeCount := 0
	boundary := 0
	edges := map[[2]int]int{}
	for _, f := range m.Faces {
		n := len(f.Verts)
		for i := 0; i < n; i++ {
			a, b := f.Verts[i], f.Verts[(i+1)%n]
			if a < 0 || b < 0 || a >= len(m.Verts) || b >= len(m.Verts) {
				d.Manifold = false
				continue
			}
			if a == b {
				deg++
				continue
			}
			key := edgeKey(a, b)
			if edges[key] == 0 {
				edgeCount++
			}
			edges[key]++
		}
	}
	for _, cnt := range edges {
		if cnt != 2 {
			boundary += cnt
		}
		if cnt > 2 {
			d.Manifold = false
		}
	}
	d.Edges = edgeCount
	d.Holes = boundary / 2
	d.Water = boundary == 0 && d.Manifold

	// Connected components (union-find over vertices via faces).
	d.Comps = components(m)

	// Quality heuristic: manifold(0.4) + no degenerate(0.2) + watertight(0.2)
	// + reasonable aspect(0.2).
	q := 0.0
	if d.Manifold {
		q += 0.4
	}
	if d.Degener == 0 && d.Faces > 0 {
		q += 0.2
	}
	if d.Water {
		q += 0.2
	}
	if d.Bounds.Diameter > 1e-9 {
		aspect := d.Bounds.Size[0] + d.Bounds.Size[1] + d.Bounds.Size[2]
		if aspect > 0 {
			// penalize slivers: size components should all be non-negligible.
			minSide := math.Min(d.Bounds.Size[0], math.Min(d.Bounds.Size[1], d.Bounds.Size[2]))
			if minSide/d.Bounds.Diameter > 0.01 {
				q += 0.2
			}
		}
	}
	d.Quality = math.Round(q*100) / 100
	return d
}

// faceArea computes the (unsigned) area of a planar face by fan triangulation.
func faceArea(m *Mesh, f Face) float64 {
	if len(f.Verts) < 3 {
		return 0
	}
	p0 := m.Verts[f.Verts[0]]
	total := 0.0
	for i := 1; i+1 < len(f.Verts); i++ {
		p1 := m.Verts[f.Verts[i]]
		p2 := m.Verts[f.Verts[i+1]]
		cx := (p1.Y-p0.Y)*(p2.Z-p0.Z) - (p1.Z-p0.Z)*(p2.Y-p0.Y)
		cy := (p1.Z-p0.Z)*(p2.X-p0.X) - (p1.X-p0.X)*(p2.Z-p0.Z)
		cz := (p1.X-p0.X)*(p2.Y-p0.Y) - (p1.Y-p0.Y)*(p2.X-p0.X)
		total += math.Sqrt(cx*cx + cy*cy + cz*cz) / 2
	}
	return total
}

// edgeKey canonicalizes an undirected edge (a<b).
func edgeKey(a, b int) [2]int {
	if a < b {
		return [2]int{a, b}
	}
	return [2]int{b, a}
}

// components counts connected components via union-find on vertices.
func components(m *Mesh) int {
	n := len(m.Verts)
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[ra] = rb
		}
	}
	for _, f := range m.Faces {
		for i := 0; i < len(f.Verts); i++ {
			union(f.Verts[i], f.Verts[(i+1)%len(f.Verts)])
		}
	}
	roots := map[int]struct{}{}
	for i := 0; i < n; i++ {
		roots[find(i)] = struct{}{}
	}
	return len(roots)
}

// SortVertsForStableDescriptor is a helper for tests: returns indices sorted by
// (x,y,z) for reproducible comparisons.
func SortVertsForStableDescriptor(m *Mesh) []int {
	idx := make([]int, len(m.Verts))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		va, vb := m.Verts[idx[a]], m.Verts[idx[b]]
		if va.X != vb.X {
			return va.X < vb.X
		}
		if va.Y != vb.Y {
			return va.Y < vb.Y
		}
		return va.Z < vb.Z
	})
	return idx
}

// ---------------------------------------------------------------------------
// Part-level descriptors (3D-PLOT-LLM pattern): instead of addressing "verts
// 100-200", the agent addresses "<part_2>" — each connected component gets a
// marker + cheap stats. Pure Go, read-only, adds a handful of tokens.
// ---------------------------------------------------------------------------

// PartDescriptor is one connected component's compact summary.
type PartDescriptor struct {
	Idx    int     `json:"idx"`    // marker index (<part_k>)
	Verts  int     `json:"verts"`  // vertices in this part
	Faces  int     `json:"faces"`  // faces in this part
	Bounds Bounds  `json:"bounds"` // part-local AABB
	Frac   float64 `json:"frac"`   // verts share of the whole mesh (0..1)
}

// PartDescriptors groups the mesh into connected components and returns a
// per-part descriptor (marker + stats) so the agent can address a part by its
// marker instead of a vertex range. Returns a single part when the mesh is one
// component or has no faces.
func PartDescriptors(m *Mesh) []PartDescriptor {
	n := len(m.Verts)
	if n == 0 {
		return nil
	}
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[ra] = rb
		}
	}
	for _, f := range m.Faces {
		for i := 0; i < len(f.Verts); i++ {
			union(f.Verts[i], f.Verts[(i+1)%len(f.Verts)])
		}
	}
	// Group vertex indices by root (preserve first-seen order for stable idx).
	order := []int{}
	groups := map[int][]int{}
	for i := 0; i < n; i++ {
		r := find(i)
		if _, ok := groups[r]; !ok {
			order = append(order, r)
		}
		groups[r] = append(groups[r], i)
	}
	vertsInPart := map[int]bool{}
	parts := make([]PartDescriptor, 0, len(order))
	for k, root := range order {
		verts := groups[root]
		p := PartDescriptor{Idx: k, Verts: len(verts)}
		// Face membership: a face belongs to the part of its first vertex.
		for _, f := range m.Faces {
			if len(f.Verts) == 0 {
				continue
			}
			if find(f.Verts[0]) == root {
				p.Faces++
			}
		}
		// Part-local bounds.
		var minV, maxV Vec3
		for i, vi := range verts {
			v := m.Verts[vi]
			if i == 0 {
				minV, maxV = v, v
				continue
			}
			minV = Vec3{minF(minV.X, v.X), minF(minV.Y, v.Y), minF(minV.Z, v.Z)}
			maxV = Vec3{maxF(minV.X, v.X), maxF(minV.Y, v.Y), maxF(minV.Z, v.Z)}
		}
		_ = vertsInPart
		p.Bounds = Bounds{
			Min: [3]float64{minV.X, minV.Y, minV.Z},
			Max: [3]float64{maxV.X, maxV.Y, maxV.Z},
			Size: [3]float64{
				maxV.X - minV.X, maxV.Y - minV.Y, maxV.Z - minV.Z,
			},
		}
		p.Bounds.Diameter = math.Sqrt(p.Bounds.Size[0]*p.Bounds.Size[0] + p.Bounds.Size[1]*p.Bounds.Size[1] + p.Bounds.Size[2]*p.Bounds.Size[2])
		p.Frac = float64(len(verts)) / float64(n)
		parts = append(parts, p)
	}
	return parts
}

// VoxelTokens renders the voxel model as a compact token sequence
// ("x,y,z;c" per voxel, index-0 base) — SuperVoxelGPT-style: the agent reads
// tokens, not raw grid coordinates. The 0-colour terminator makes the stream
// self-terminating for generation back-ends.
func VoxelTokens(vm *VoxelModel) []string {
	toks := make([]string, 0, len(vm.Voxels))
	for _, v := range vm.Voxels {
		toks = append(toks, fmt.Sprintf("%d,%d,%d;%d", v.X, v.Y, v.Z, v.Color))
	}
	return toks
}
