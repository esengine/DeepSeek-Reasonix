package checkpoint

import "slices"

// List returns every checkpoint's metadata, oldest turn first.
func (s *Store) List() []Meta {
	s.mu.Lock()
	defer s.mu.Unlock()
	cps := s.all()
	out := make([]Meta, len(cps))
	seen := make(map[string]bool)
	for i, c := range slices.Backward(cps) {
		paths := make([]string, len(c.Files))
		for j, f := range c.Files {
			paths[j] = f.Path
			seen[NormalizeRelPath(s.root, f.Path)] = true
		}
		meta := Meta{
			Turn:               c.Turn,
			MsgIndex:           c.MsgIndex,
			Time:               c.Time,
			Prompt:             c.Prompt,
			Paths:              paths,
			RewindFiles:        len(seen),
			Coverage:           c.Coverage,
			CoverageGaps:       append([]CoverageGap(nil), c.CoverageGaps...),
			ExpiredFilePayload: c.ExpiredFilePayload,
			ActiveWriters:      append([]ActiveWriter(nil), c.ActiveWriters...),
			Legacy:             c.Legacy || c.Coverage == CoverageLegacy,
		}
		switch {
		case meta.Legacy:
			meta.DisabledReason = "legacy checkpoint cannot verify later manual edits"
		case meta.ExpiredFilePayload:
			meta.DisabledReason = "file recovery payload expired"
		}
		out[i] = meta
	}
	return out
}
