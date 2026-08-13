package agent

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"reasonix/internal/provider"
)

// writeRecoveryBranchAtPath persists one bounded recovery lane. collision is
// true only when path already contains content this Session did not persist;
// callers rotate lanes instead of overwriting that independent transcript.
func (s *Session) writeRecoveryBranchAtPath(
	path string,
	opts RecoveryBranchOptions,
	msgs []provider.Message,
	digest [sha256.Size]byte,
	version uint64,
	rewriteVersion int,
	preview string,
	turns int,
	digestText string,
	recoveryDepth int,
	shutdown bool,
) (RecoveryBranchInfo, bool, error) {
	unlockPath := lockSessionSavePath(path)
	defer unlockPath()
	unlockFile, err := lockSessionFile(path)
	if err != nil {
		return RecoveryBranchInfo{}, false, fmt.Errorf("lock recovery session file: %w", err)
	}
	defer unlockFile()
	if loaded, loadErr := loadSessionUnlocked(path); loadErr == nil && loaded != nil {
		existingDigest, digestErr := digestSessionMessages(loaded.Snapshot())
		if digestErr != nil {
			return RecoveryBranchInfo{}, false, digestErr
		}
		if bytes.Equal(existingDigest[:], digest[:]) {
			s.inheritParentProjection(opts.OriginalPath, path, msgs, version)
			meta, err := s.saveRecoveryBranchMeta(path, opts, preview, turns, digestText, recoveryDepth)
			if err != nil {
				return RecoveryBranchInfo{}, false, err
			}
			s.markPersisted(path, digest, version, meta.Revision, rewriteVersion)
			return RecoveryBranchInfo{Path: path, Digest: digestText, Existing: true, Meta: meta, Preview: preview, Turns: turns}, false, nil
		}
		state := s.persistState(path)
		if !state.ok || !bytes.Equal(state.digest[:], existingDigest[:]) {
			return RecoveryBranchInfo{}, true, nil
		}
	} else if loadErr != nil && !os.IsNotExist(loadErr) {
		return RecoveryBranchInfo{}, false, loadErr
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return RecoveryBranchInfo{}, false, fmt.Errorf("create recovery session dir: %w", err)
	}
	probe, err := probeSessionEventLog(path)
	if err != nil {
		return RecoveryBranchInfo{}, false, err
	}
	if probe.native {
		if err := writeRecoveryEventLog(path, msgs, digest, shutdown); err != nil {
			return RecoveryBranchInfo{}, false, err
		}
	}
	if err := writeSessionMessages(path, msgs); err != nil {
		return RecoveryBranchInfo{}, false, err
	}
	s.inheritParentProjection(opts.OriginalPath, path, msgs, version)
	meta, err := s.saveRecoveryBranchMeta(path, opts, preview, turns, digestText, recoveryDepth)
	if err != nil {
		return RecoveryBranchInfo{}, false, err
	}
	if err := writeSessionEventIndex(path, msgs, digest, meta.Revision); err != nil {
		slog.Warn("session: keeping recovery branch after event index write failure", "path", path, "err", err)
	}
	s.markPersisted(path, digest, version, meta.Revision, rewriteVersion)
	return RecoveryBranchInfo{Path: path, Digest: digestText, Meta: meta, Preview: preview, Turns: turns}, false, nil
}

// inheritParentProjection copies a valid context projection from the parent
// session onto the recovery fork and fails closed on content mismatch.
func (s *Session) inheritParentProjection(originalPath, recoveryPath string, msgs []provider.Message, version uint64) {
	if _, ok, err := LoadCompactionState(recoveryPath); err == nil && ok {
		return
	}
	st, ok, err := LoadCompactionState(originalPath)
	if err != nil || !ok {
		return
	}
	n := st.Projection.CoveredCount
	if len(st.Projection.Messages) == 0 || n <= 0 || n > len(msgs) ||
		st.Projection.CoveredPrefixHash == "" ||
		coveredPrefixHash(msgs, n) != st.Projection.CoveredPrefixHash {
		return
	}
	if err := SaveCompactionState(recoveryPath, st); err != nil {
		slog.Warn("session: recovery branch did not inherit context projection",
			"path", recoveryPath, "err", err)
	}
}

// healEmptyCheckpointFromWAL rebuilds a missing or 0-byte checkpoint from its
// own valid event log. Healthy checkpoints return before probing the WAL.
func healEmptyCheckpointFromWAL(path string) error {
	info, statErr := os.Stat(path)
	needHeal := statErr != nil && os.IsNotExist(statErr)
	if statErr == nil {
		needHeal = info.Size() == 0
	}
	if !needHeal {
		return nil
	}
	probe, err := probeSessionEventLog(path)
	if err != nil {
		return err
	}
	if !probe.native || probe.size <= 0 || probe.futureSchema {
		return nil
	}
	msgs, fromEvents, damaged, err := loadSessionMessages(path)
	if err != nil || !fromEvents || damaged || len(msgs) == 0 {
		return nil
	}
	if err := writeSessionMessages(path, msgs); err != nil {
		return fmt.Errorf("rebuild checkpoint from WAL: %w", err)
	}
	return nil
}
