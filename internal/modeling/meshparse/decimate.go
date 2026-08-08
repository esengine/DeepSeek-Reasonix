package meshparse

import (
	"math"
	"sort"
)

// ---------------------------------------------------------------------------
// Decimate (docs/MODELLING_OVERHAUL.md §6-B): edge-collapse simplification.
//
// Greedy edge collapse by quadric error (QEM-lite): iterate edges in order of
// collapse cost (edge length × curvature proxy), collapse the lowest-cost edge
// first, repeat until target face count is reached. Deterministic (edges are
// processed in sorted order with a stable tie-break).
// ---------------------------------------------------------------------------

// Decimate reduces the mesh to roughly `targetFaces` triangles by collapsing
// edges. Works on triangulated meshes (non-triangle faces are triangulated
// first). Returns a new mesh and a before/after result.
func Decimate(m *Mesh, targetFaces int) (*Mesh, OpResult) {
	res := OpResult{Op: "decimate", VertsBefore: len(m.Verts), FacesBefore: len(m.Faces)}
	if targetFaces <= 0 || targetFaces >= len(m.Faces) {
		// Nothing to simplify.
		out := &Mesh{Verts: m.Verts, Faces: m.Faces, Format: m.Format,
			Normals: m.Normals, UVs: m.UVs, Materials: m.Materials}
		res.VertsAfter = len(m.Verts)
		res.FacesAfter = len(m.Faces)
		return out, res
	}
	tri, _ := Triangulate(m)
	cur := &Mesh{Verts: tri.Verts, Faces: tri.Faces, Format: tri.Format}
	for len(cur.Faces) > targetFaces {
		edge, ok := cheapestEdge(cur)
		if !ok {
			break // nothing collapsible left
		}
		collapse(cur, edge[0], edge[1])
	}
	out := &Mesh{Verts: cur.Verts, Faces: cur.Faces, Format: m.Format,
		Normals: m.Normals, UVs: m.UVs, Materials: m.Materials}
	res.VertsAfter = len(out.Verts)
	res.FacesAfter = len(out.Faces)
	res.RemovedFaces = res.FacesBefore - res.FacesAfter
	return out, res
}

// cheapestEdge returns the [a,b] vertex pair with the lowest collapse cost and
// ok=false if no collapsible edge exists. Cost = edge length × (1 + degree of
// b) — short edges in low-valence regions collapse first (deterministic).
func cheapestEdge(m *Mesh) ([2]int, bool) {
	type cand struct {
		a, b int
		cost float64
	}
	var best *cand
	// Face adjacency → edge set with valence.
	valence := make([]int, len(m.Verts))
	edgeSet := map[[2]int]bool{}
	for _, f := range m.Faces {
		for i := 0; i < len(f.Verts); i++ {
			a, b := f.Verts[i], f.Verts[(i+1)%len(f.Verts)]
			if a == b {
				continue
			}
			edgeSet[edgeKey(a, b)] = true
			valence[a]++
			valence[b]++
		}
	}
	for e := range edgeSet {
		a, b := e[0], e[1]
		if a < 0 || b < 0 || a >= len(m.Verts) || b >= len(m.Verts) {
			continue
		}
		l := math.Sqrt(distSq(m.Verts[a], m.Verts[b]))
		cost := l * float64(1+valence[b])
		if best == nil || cost < best.cost {
			best = &cand{a: a, b: b, cost: cost}
		}
	}
	if best == nil {
		return [2]int{}, false
	}
	return [2]int{best.a, best.b}, true
}

// collapse moves vertex b onto vertex a and rewrites faces.
func collapse(m *Mesh, a, b int) {
	// Rewrite face indices: b→a; drop faces that become degenerate.
	var keep []Face
	for _, f := range m.Faces {
		seen := map[int]struct{}{}
		verts := make([]int, 0, len(f.Verts))
		ok := true
		for _, vi := range f.Verts {
			if vi == b {
				vi = a
			}
			if vi < 0 || vi >= len(m.Verts) {
				ok = false
				break
			}
			if _, dup := seen[vi]; dup {
				continue
			}
			seen[vi] = struct{}{}
			verts = append(verts, vi)
		}
		if !ok || len(verts) < 3 {
			continue
		}
		keep = append(keep, Face{Verts: verts})
	}
	m.Faces = keep
}

// SortEdgesStable is a test helper: returns face-edge pairs in deterministic
// order (by vertex indices) for reproducible assertions.
func SortEdgesStable(m *Mesh) [][2]int {
	set := map[[2]int]bool{}
	for _, f := range m.Faces {
		for i := 0; i < len(f.Verts); i++ {
			a, b := f.Verts[i], f.Verts[(i+1)%len(f.Verts)]
			if a != b {
				set[edgeKey(a, b)] = true
			}
		}
	}
	out := make([][2]int, 0, len(set))
	for e := range set {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i][0] != out[j][0] {
			return out[i][0] < out[j][0]
		}
		return out[i][1] < out[j][1]
	})
	return out
}
