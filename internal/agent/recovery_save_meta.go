package agent

import "strings"

func (s *Session) prepareRecoveryBranchMetaLocked(path string, opts RecoveryBranchOptions, preview string, turns int, digest string, depth int, contentUnchanged bool) (BranchMeta, error) {
	existing, ok, err := LoadBranchMeta(path)
	if err != nil {
		return BranchMeta{}, err
	}
	meta := opts.BranchMeta
	meta.ID = BranchID(path)
	if strings.TrimSpace(meta.Name) == "" {
		meta.Name = firstNonEmpty(strings.TrimSpace(opts.Name), RecoveryBranchDefaultName)
	}
	if strings.TrimSpace(meta.ParentID) == "" {
		meta.ParentID = recoveryRootID(opts.OriginalPath)
	}
	meta.ForkTurn = -1
	meta.ForkMessageIndex = len(s.Snapshot())
	meta.Preview = preview
	meta.Turns = turns
	meta.SchemaVersion = BranchMetaCountsVersion
	meta.Recovered = true
	meta.RecoveryReason = firstNonEmpty(strings.TrimSpace(opts.Reason), "session snapshot conflict")
	meta.RecoveryDigest = digest
	meta.RecoveryDepth = depth
	meta.Revision = 1
	ledgerCurrent := false
	if ok {
		ledgerCurrent = contentUnchanged && existing.Revision > 0 &&
			strings.TrimSpace(existing.ContentDigest) == digest &&
			strings.TrimSpace(existing.RecoveryDigest) == digest
		if ledgerCurrent {
			meta.Revision = existing.Revision
		} else {
			meta.Revision = max(int64(1), existing.Revision+1)
		}
		meta.InFlightTurn = existing.InFlightTurn
		meta.DismissedTodoBatches = MergeDismissedTodoBatches(existing.DismissedTodoBatches, meta.DismissedTodoBatches)
		if ledgerCurrent && strings.TrimSpace(existing.WriterID) != "" {
			meta.WriterID = existing.WriterID
		}
	}
	meta.ContentDigest = digest
	stampSessionListingProjection(&meta)
	if !ledgerCurrent || strings.TrimSpace(meta.WriterID) == "" {
		meta.WriterID = SessionWriterID()
	}
	return meta, nil
}

func (s *Session) saveRecoveryBranchMetaLocked(path string, opts RecoveryBranchOptions, preview string, turns int, digest string, depth int, contentUnchanged bool) (BranchMeta, error) {
	meta, err := s.prepareRecoveryBranchMetaLocked(path, opts, preview, turns, digest, depth, contentUnchanged)
	if err != nil {
		return BranchMeta{}, err
	}
	if err := saveBranchMeta(path, meta, true); err != nil {
		return BranchMeta{}, err
	}
	return meta, nil
}
