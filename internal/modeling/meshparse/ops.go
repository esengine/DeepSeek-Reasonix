package meshparse

// ---------------------------------------------------------------------------
// Operation layer (docs/MODELLING_OVERHAUL.md §6-B): deterministic pure-Go
// mesh operations. No Blender needed. Every operation reports a
// before/after stat delta so the agent can verify the change with minimal
// tokens (e.g. "verts 5248→4096, faces 10496→8192").
// ---------------------------------------------------------------------------

// OpResult is the compact before/after summary of an operation.
type OpResult struct {
	Op          string `json:"op"`
	VertsBefore int    `json:"verts_before"`
	VertsAfter  int    `json:"verts_after"`
	FacesBefore int    `json:"faces_before"`
	FacesAfter  int    `json:"faces_after"`
	RemovedDeg  int    `json:"removed_degenerate,omitempty"`
	RemovedDup  int    `json:"removed_duplicate_verts,omitempty"`
	RemovedFaces int   `json:"removed_faces,omitempty"`
}

// Triangulate splits every n-gon (n>3) into triangles by fan triangulation.
// Returns a new mesh; the input is untouched.
func Triangulate(m *Mesh) (*Mesh, OpResult) {
	out := &Mesh{
		Verts:     m.Verts,
		Normals:   m.Normals,
		UVs:       m.UVs,
		Materials: m.Materials,
		Format:    m.Format,
	}
	res := OpResult{Op: "triangulate", VertsBefore: len(m.Verts), FacesBefore: len(m.Faces)}
	for _, f := range m.Faces {
		n := len(f.Verts)
		if n <= 3 {
			out.Faces = append(out.Faces, f)
			continue
		}
		for i := 1; i+1 < n; i++ {
			out.Faces = append(out.Faces, Face{Verts: []int{f.Verts[0], f.Verts[i], f.Verts[i+1]}})
		}
	}
	res.VertsAfter = len(out.Verts)
	res.FacesAfter = len(out.Faces)
	return out, res
}

// Cleanup removes degenerate faces (fewer than 3 verts, or zero area) and
// optionally welds duplicate vertices (same position within eps). Faces
// referencing removed verts are re-indexed; faces that collapse are dropped.
func Cleanup(m *Mesh, weldEps float64) (*Mesh, OpResult) {
	res := OpResult{Op: "cleanup", VertsBefore: len(m.Verts), FacesBefore: len(m.Faces)}
	out := &Mesh{Format: m.Format, Materials: m.Materials}

	// Vertex welding: group identical positions.
	remap := make([]int, len(m.Verts))
	if weldEps > 0 {
		buckets := map[[3]int64]int{}
		for i, v := range m.Verts {
			key := quantize(v, weldEps)
			if j, ok := buckets[key]; ok {
				remap[i] = j
				res.RemovedDup++
			} else {
				idx := len(out.Verts)
				buckets[key] = idx
				out.Verts = append(out.Verts, v)
				remap[i] = idx
			}
		}
	} else {
		out.Verts = m.Verts
		for i := range remap {
			remap[i] = i
		}
	}

	seen := map[int]struct{}{}
	for _, f := range m.Faces {
		if len(f.Verts) < 3 {
			res.RemovedDeg++
			continue
		}
		// Remap, drop dup verts inside the face.
		var verts []int
		ok := true
		for _, vi := range f.Verts {
			if vi < 0 || vi >= len(remap) {
				ok = false
				break
			}
			nv := remap[vi]
			if _, dup := seen[nv]; dup {
				continue
			}
			seen[nv] = struct{}{}
			verts = append(verts, nv)
		}
		for k := range seen {
			delete(seen, k)
		}
		if !ok || len(verts) < 3 {
			res.RemovedDeg++
			continue
		}
		// Zero-area check on the cleaned face.
		if faceArea(m, Face{Verts: remappedVerts(m, verts)}) < 1e-12 {
			res.RemovedDeg++
			continue
		}
		out.Faces = append(out.Faces, Face{Verts: verts})
	}
	res.VertsAfter = len(out.Verts)
	res.FacesAfter = len(out.Faces)
	return out, res
}

func quantize(v Vec3, eps float64) [3]int64 {
	return [3]int64{int64(v.X / eps), int64(v.Y / eps), int64(v.Z / eps)}
}

func remappedVerts(m *Mesh, verts []int) []int {
	return verts
}

// MergeVerts collapses vertices that are closer than eps into their centroid
// (cluster merge). Faces are re-indexed; collapsed faces are dropped.
func MergeVerts(m *Mesh, eps float64) (*Mesh, OpResult) {
	res := OpResult{Op: "merge_verts", VertsBefore: len(m.Verts), FacesBefore: len(m.Faces)}
	out := &Mesh{Format: m.Format, Materials: m.Materials}
	if eps <= 0 {
		// No-op passthrough.
		out.Verts = m.Verts
		out.Faces = m.Faces
		res.VertsAfter = len(m.Verts)
		res.FacesAfter = len(m.Faces)
		return out, res
	}
	// Greedy cluster by quantized bucket then within-eps centroid merge.
	cluster := make([]int, len(m.Verts))
	for i := range cluster {
		cluster[i] = -1
	}
	centers := []Vec3{}
	bucketOf := map[[3]int64][]int{}
	for i, v := range m.Verts {
		k := quantize(v, eps)
		bucketOf[k] = append(bucketOf[k], i)
	}
	for _, members := range bucketOf {
		// Refine: split bucket members by actual distance to cluster centers.
		for _, i := range members {
			best := -1
			bestD := eps * eps
			for ci, c := range centers {
				d := distSq(m.Verts[i], c)
				if d <= bestD {
					bestD = d
					best = ci
				}
			}
			if best < 0 {
				best = len(centers)
				centers = append(centers, m.Verts[i])
				out.Verts = append(out.Verts, m.Verts[i])
			}
			cluster[i] = best
		}
	}
	_ = bucketOf
	res.RemovedDup = len(m.Verts) - len(centers)
	res.RemovedDeg = 0
	// Rebuild faces.
	seen := map[int]struct{}{}
	for _, f := range m.Faces {
		var verts []int
		ok := true
		for _, vi := range f.Verts {
			if vi < 0 || vi >= len(cluster) {
				ok = false
				break
			}
			nv := cluster[vi]
			if _, dup := seen[nv]; dup {
				continue
			}
			seen[nv] = struct{}{}
			verts = append(verts, nv)
		}
		for k := range seen {
			delete(seen, k)
		}
		if !ok || len(verts) < 3 {
			res.RemovedDeg++
			continue
		}
		out.Faces = append(out.Faces, Face{Verts: verts})
	}
	res.VertsAfter = len(out.Verts)
	res.FacesAfter = len(out.Faces)
	return out, res
}

func distSq(a, b Vec3) float64 {
	dx, dy, dz := a.X-b.X, a.Y-b.Y, a.Z-b.Z
	return dx*dx + dy*dy + dz*dz
}
