package mutation

// Opaque exclusion spans the trusted registry, including tree-external aliases.
// Its paths still bound nested admission and legacy workspace compatibility.
func recordsOverlap(a, b record) bool {
	if len(a.Paths) == 0 || len(b.Paths) == 0 {
		return false
	}
	return a.Effect == OpaqueWrite || b.Effect == OpaqueWrite || overlaps(a.Paths, b.Paths)
}

func coversEffect(held, requested Effect) bool {
	return requested != OpaqueWrite || held == OpaqueWrite
}
