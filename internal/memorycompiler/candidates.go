package memorycompiler

// StableNoisePattern is a failure pattern Memory v5 observed across at least
// minCount separate turns, stable enough to surface as a durable memory
// candidate for explicit user confirmation.
type StableNoisePattern struct {
	Pattern string
	Count   int
}

// StableNoisePatterns returns repeated failure patterns ordered by how often
// they recurred. Read-only: it never mutates compiler state. minCount < 1
// defaults to 1; maxItems <= 0 uses a default.
func (r *Runtime) StableNoisePatterns(minCount, maxItems int) []StableNoisePattern {
	if r == nil {
		return nil
	}
	if minCount < 1 {
		minCount = 1
	}
	if maxItems <= 0 {
		maxItems = 8
	}
	st := r.loadState()
	var out []StableNoisePattern
	for _, ref := range sortedNoisyRefs(st.NoisyRefs) {
		if ref.count < minCount {
			continue
		}
		out = append(out, StableNoisePattern{Pattern: ref.ref, Count: ref.count})
		if len(out) >= maxItems {
			break
		}
	}
	return out
}
