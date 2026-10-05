// file_revert.go — putting one file back, without rewinding the turn.
package checkpoint

import (
	"errors"
	"fmt"
	"os"
	"time"

	fileenc "reasonix/internal/base/fileutil/encoding"
)

// PrepareFileRevert prepares a single-file restore to the earliest session preimage.
func (s *Store) PrepareFileRevert(path string, sessionRev int64) (RewindPlan, error) {
	if s == nil {
		return RewindPlan{}, fmt.Errorf("checkpoints unavailable")
	}
	plan := RewindPlan{
		PlanID:          newID("plan"),
		Scope:           RewindCode,
		Path:            path,
		SessionRevision: sessionRev,
		CreatedAt:       time.Now(),
		WorkspaceToken:  fmt.Sprintf("%d", s.barrier.Generation()),
		Files:           []string{path},
		FileCount:       1,
	}
	if _, ok := s.FileState(path); !ok {
		plan.CanFiles = false
		plan.DisabledReason = "file is not session-owned"
		return plan, nil
	}
	abs, err := safePath(s.root, path)
	if err != nil {
		plan.CanFiles = false
		plan.DisabledReason = "path unsafe"
		plan.Conflicts = []RewindConflict{{Path: path, Reason: ConflictPathUnsafe}}
		return plan, nil
	}
	rev, owned, has := s.earliestRevisionFor(path)
	if !has {
		plan.CanFiles = false
		plan.DisabledReason = "file is not session-owned"
		return plan, nil
	}
	plan.Path = owned
	if rev.AfterExisted == nil && rev.AfterSHA256 == "" {
		// A v1 or incomplete capture has a preimage but no evidence that the
		// current file is still the session's last write. Do not turn the
		// generic conflict-overwrite affordance into an unsafe legacy restore.
		plan.PlanID = ""
		plan.CanFiles = false
		plan.Legacy = true
		plan.Coverage = CoverageLegacy
		plan.DisabledReason = "legacy checkpoint cannot verify later manual edits"
		return plan, nil
	}
	fp, fperr := FingerprintPath(s.root, abs)
	if fperr == nil {
		reason := CompareIdentity(fp, rev.AfterSHA256, rev.AfterExisted, rev.AfterMode)
		if reason != "" {
			plan.Conflicts = []RewindConflict{{
				Path: path, Reason: reason,
				CheckpointSHA: rev.SHA256, LastOwnedSHA: rev.AfterSHA256, CurrentSHA: fp.SHA256,
				CurrentExisted: fp.Existed, CheckpointExist: rev.Existed,
			}}
			plan.CanFiles = true
			plan.DisabledReason = "conflict requires explicit resolution"
		} else {
			plan.CanFiles = true
		}
	} else {
		plan.CanFiles = false
		plan.DisabledReason = "current file identity unavailable"
		plan.Conflicts = []RewindConflict{{Path: path, Reason: ConflictExternalChange}}
	}
	if rev.BlobRef == "" && rev.Content == nil && rev.Existed {
		plan.CanFiles = false
		plan.DisabledReason = "missing file payload"
		plan.Conflicts = append(plan.Conflicts, RewindConflict{Path: path, Reason: ConflictMissingPayload})
	}
	s.mu.Lock()
	if s.plans == nil {
		s.plans = map[string]preparedPlan{}
	}
	s.plans[plan.PlanID] = preparedPlan{plan: plan, created: time.Now(), previewFingerprint: &fp}
	s.dropStalePlansLocked()
	s.mu.Unlock()
	return plan, nil
}

// CommitFileRevert commits a single-file restore.
func (s *Store) CommitFileRevert(planID string, resolution ConflictResolution) (RewindResult, error) {
	if s == nil {
		return RewindResult{}, fmt.Errorf("checkpoints unavailable")
	}
	pp, ok := s.takeFilePlan(planID)
	if !ok {
		err := errors.New("unknown or expired plan")
		return RewindResult{OK: false, Error: err.Error()}, err
	}
	if res, err := filePlanVerdict(pp.plan, resolution); res != nil {
		return *res, err
	}
	if !s.barrier.TryEnterExclusive() {
		err := errors.New("workspace mutation in progress")
		return RewindResult{OK: false, Error: err.Error(),
			Conflicts: []RewindConflict{{Path: pp.plan.Path, Reason: ConflictBusyWriter}}}, err
	}
	defer s.barrier.ExitExclusive()
	if res, err := s.verifyWorkspaceUnchanged(pp); res != nil {
		return *res, err
	}
	rev, _, has := s.earliestRevisionFor(pp.plan.Path)
	if !has {
		err := errors.New("file is not session-owned")
		return RewindResult{OK: false, Error: err.Error()}, err
	}
	abs, err := safePath(s.root, rev.Path)
	if err != nil {
		return RewindResult{OK: false, Error: err.Error()}, err
	}
	tx := &TransactionManifest{
		SchemaVersion: SchemaV2,
		ID:            newID("tx"),
		WorkspaceRoot: s.root,
		State:         TxPrepared,
		Kind:          "file_revert",
		Scope:         RewindCode,
		Path:          rev.Path,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	target, err := s.fileRevertTarget(rev, abs, tx.ID)
	if err != nil {
		return RewindResult{OK: false, Error: err.Error()}, err
	}
	tx.Targets = []TransactionTarget{target}
	if err := s.persistTransaction(tx); err != nil {
		return RewindResult{OK: false, Error: err.Error()}, err
	}
	return s.commitTransaction(tx, nil, nil)
}

// takeFilePlan removes a prepared plan from the store: one plan is good for one
// commit, whether or not that commit goes through.
func (s *Store) takeFilePlan(planID string) (preparedPlan, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pp, ok := s.plans[planID]
	if ok {
		delete(s.plans, planID)
	}
	return pp, ok
}

// filePlanVerdict answers whether the plan alone settles the commit. A non-nil
// result is the caller's answer, reached without touching the workspace.
func filePlanVerdict(plan RewindPlan, resolution ConflictResolution) (*RewindResult, error) {
	if plan.Path == "" {
		err := errors.New("not a file plan")
		return &RewindResult{OK: false, Error: err.Error()}, err
	}
	if !plan.CanFiles {
		return &RewindResult{OK: false, Error: plan.DisabledReason, Conflicts: plan.Conflicts},
			errors.New(plan.DisabledReason)
	}
	if len(plan.Conflicts) == 0 || resolution == ResolveOverwriteCheckpoint {
		return nil, nil
	}
	if resolution == ResolveKeepCurrent {
		return &RewindResult{OK: true, UndoAvailable: false}, nil
	}
	err := errors.New("conflict requires explicit resolution")
	return &RewindResult{OK: false, Error: err.Error(), Conflicts: plan.Conflicts}, err
}

// verifyWorkspaceUnchanged re-checks, under the barrier, what the preview
// measured: no background writer, the same workspace generation, the same file.
func (s *Store) verifyWorkspaceUnchanged(pp preparedPlan) (*RewindResult, error) {
	plan := pp.plan
	if conflicts := s.activeWriterConflicts(); len(conflicts) > 0 {
		err := errors.New("active background writer")
		return &RewindResult{OK: false, Error: err.Error(), Conflicts: conflicts}, err
	}
	if plan.WorkspaceToken != fmt.Sprintf("%d", s.barrier.Generation()) {
		err := errors.New("workspace changed since preview")
		return &RewindResult{OK: false, Error: err.Error(),
			Conflicts: []RewindConflict{{Path: plan.Path, Reason: ConflictStalePlan}}}, err
	}
	abs, err := safePath(s.root, plan.Path)
	if err != nil {
		return &RewindResult{OK: false, Error: err.Error()}, err
	}
	current, fperr := FingerprintPath(s.root, abs)
	if fperr != nil || pp.previewFingerprint == nil || !sameFingerprint(current, *pp.previewFingerprint) {
		stale := errors.New("file changed since preview; preview again")
		return &RewindResult{OK: false, Error: stale.Error(), Conflicts: []RewindConflict{{
			Path: plan.Path, Reason: ConflictStalePlan,
			CurrentSHA: current.SHA256, CurrentExisted: current.Existed,
		}}}, stale
	}
	return nil, nil
}

// earliestRevisionFor finds the session's earliest preimage for a path —
// reverting one file writes that preimage, not every file of the turn that
// first touched it. The recorded spelling can differ from the caller's, so a
// miss falls back to matching on the absolute path.
func (s *Store) earliestRevisionFor(path string) (FileRevision, string, bool) {
	revs := s.earliestRevisions(0)
	if rev, ok := revs[path]; ok {
		return rev, path, true
	}
	abs, err := safePath(s.root, path)
	if err != nil {
		return FileRevision{}, "", false
	}
	for p, r := range revs {
		if ap, e := safePath(s.root, p); e == nil && ap == abs {
			return r, p, true
		}
	}
	return FileRevision{}, "", false
}

// fileRevertTarget builds the single target this transaction publishes: the
// preimage to restore, and the bytes it replaces so undo has them.
func (s *Store) fileRevertTarget(rev FileRevision, abs, txID string) (TransactionTarget, error) {
	fwd, _, _ := CapturePath(abs, CaptureOptions{WorkspaceRoot: s.root, ReadContent: true})
	t := TransactionTarget{
		Path: rev.Path, AbsPath: abs,
		RestoreExisted: rev.Existed, RestoreMode: rev.Mode, RestoreSHA: rev.SHA256, RestoreBlob: rev.BlobRef, RestoreEncoding: rev.Encoding,
		ForwardExisted: fwd.Existed, ForwardMode: fwd.Mode, ForwardSHA: fwd.SHA256,
	}
	t.PublishTmp, t.BackupPath = transactionSiblingPaths(abs, txID, 0)
	if rev.Existed {
		if err := s.stageRestore(&t, rev, abs); err != nil {
			return t, err
		}
	} else {
		t.Action = "delete"
		t.PublishTmp = ""
	}
	if err := s.stageForward(&t, fwd); err != nil {
		return t, err
	}
	return t, nil
}

// stageRestore puts the preimage where the commit can publish it, re-encoding a
// v1 text capture when the blob behind it is gone.
func (s *Store) stageRestore(t *TransactionTarget, rev FileRevision, abs string) error {
	t.Action = "write"
	data, err := s.loadRevisionBytes(rev)
	if err != nil {
		if rev.Content == nil {
			return err
		}
		if data, err = fileenc.Encode(*rev.Content, s.revisionEncoding(rev, abs)); err != nil {
			return err
		}
	}
	mode := os.FileMode(0o644)
	if rev.Mode != 0 {
		mode = os.FileMode(rev.Mode)
	}
	if t.RestoreBlob == "" {
		if s.blobs == nil {
			t.RestoreInline = clonePayload(data)
		} else {
			ref, perr := s.blobs.Put(data)
			if perr != nil {
				return perr
			}
			t.RestoreBlob = ref
		}
	}
	return s.writePublishTemp(t.PublishTmp, data, mode)
}

// revisionEncoding is what a v1 text preimage is written back in: the encoding
// the capture recorded, else the one the file on disk uses today.
func (s *Store) revisionEncoding(rev FileRevision, abs string) fileenc.Kind {
	if rev.Encoding != nil {
		return *rev.Encoding
	}
	if current := s.detectCurrentEncoding(abs); current != nil {
		return *current
	}
	return fileenc.UTF8
}

// stageForward keeps the bytes the revert is about to replace.
func (s *Store) stageForward(t *TransactionTarget, fwd Fingerprint) error {
	if !fwd.Existed {
		return nil
	}
	if s.blobs == nil {
		t.ForwardInline = clonePayload(fwd.Content)
		return nil
	}
	ref, err := s.blobs.Put(fwd.Content)
	if err != nil {
		return err
	}
	t.ForwardBlob = ref
	return nil
}
