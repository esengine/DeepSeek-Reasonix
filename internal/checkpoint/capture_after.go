package checkpoint

import "slices"

// CaptureAfter records the after fingerprint and reports a preimage change.
func (s *Store) CaptureAfter(path string, opts CaptureAfterOpts) bool {
	if path == "" {
		return false
	}
	pathKey := NormalizeRelPath(s.root, path)
	fp, gap, err := CapturePath(path, CaptureOptions{WorkspaceRoot: s.root, ReadContent: true})
	if gap != nil {
		s.RecordGap(*gap)
	}
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mutationSeq = opts.Seq
	if s.cur != nil {
		s.cur.LastMutationSeq = opts.Seq
		for i := range s.cur.Files {
			if NormalizeRelPath(s.root, s.cur.Files[i].Path) != pathKey {
				continue
			}
			changed := updateAfterFingerprint(&s.cur.Files[i], fp)
			s.recomputeCoverageLocked(s.cur)
			s.persistBestEffort(s.cur)
			s.lastUndo = nil
			return changed
		}
	}
	for _, c := range slices.Backward(s.done) {
		for j := range c.Files {
			if NormalizeRelPath(s.root, c.Files[j].Path) != pathKey {
				continue
			}
			changed := updateAfterFingerprint(&c.Files[j], fp)
			s.persistBestEffort(c)
			s.lastUndo = nil
			return changed
		}
	}
	return false
}

func updateAfterFingerprint(before *FileSnap, after Fingerprint) bool {
	existed := before.Content != nil || before.BlobRef != "" || before.SHA256 != ""
	changed := existed != after.Existed || before.SHA256 != after.SHA256 || before.Mode != after.Mode
	before.AfterExisted, before.AfterSHA256, before.AfterMode = &after.Existed, after.SHA256, after.Mode
	return changed
}
