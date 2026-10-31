package isolation

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/platform/worktree"
)

const candidateRecordKind = "candidate-run-v1"

type candidatePhase string

const (
	candidateCreating candidatePhase = "creating"
	candidatePrepared candidatePhase = "prepared"
	candidateRunning  candidatePhase = "running"
	candidateUnknown  candidatePhase = "unknown"
	candidateReady    candidatePhase = "ready"
	candidateApplying candidatePhase = "applying"
	candidateCleanup  candidatePhase = "cleanup"
	candidateRemoved  candidatePhase = "removed"
)

// This journal lives outside the disposable worktree. Its presence is the
// already-supported legacy settlement fence, including after partial removal.
type candidateRecord struct {
	Kind        string            `json:"kind"`
	Phase       candidatePhase    `json:"phase"`
	Entry       durableEntry      `json:"entry"`
	Anchors     candidateAnchors  `json:"anchors"`
	Tree        string            `json:"tree,omitempty"`
	Changes     []worktree.Change `json:"changes,omitempty"`
	Disposition string            `json:"disposition,omitempty"`
	Updated     time.Time         `json:"updated"`
}

func candidateRecordName(id string) string {
	return strings.TrimSuffix(resultLockName(id), ".lock") + ".json"
}

func (s *Store) candidateRecords(create bool) (*os.Root, error) {
	if s == nil {
		return nil, errUnavailable
	}
	if create {
		if err := os.MkdirAll(s.managedRoot, 0o700); err != nil {
			return nil, err
		}
	}
	root, err := os.OpenRoot(s.managedRoot)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if create {
		if err := root.Mkdir(settlementFenceDirectory, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
	}
	if !recordDirectoryEntry(root, settlementFenceDirectory) {
		return nil, fmt.Errorf("candidate journal unavailable: %w", os.ErrNotExist)
	}
	return root.OpenRoot(settlementFenceDirectory)
}

func (s *Store) managedRecord(id string) (candidateRecord, bool, error) {
	var r candidateRecord
	root, err := s.candidateRecords(false)
	if errors.Is(err, os.ErrNotExist) {
		return r, false, nil
	}
	if err != nil {
		return r, true, err
	}
	defer root.Close()
	name := candidateRecordName(id)
	if _, err := root.Lstat(name); errors.Is(err, os.ErrNotExist) {
		return r, false, nil
	}
	if !recordRegularFile(root, name) {
		return r, true, managedRecordRefusal(id)
	}
	r, err = s.decodeManagedRecord(root, id)
	if err != nil {
		return r, true, err
	}
	switch r.Phase {
	case candidateCreating, candidatePrepared, candidateRunning, candidateUnknown, candidateReady, candidateApplying, candidateCleanup, candidateRemoved:
	default:
		return r, true, managedRecordRefusal(id)
	}
	if (r.Phase == candidateCreating || r.Phase == candidatePrepared || r.Phase == candidateRunning || r.Phase == candidateUnknown) && (r.Tree != "" || len(r.Changes) != 0 || r.Disposition != "") {
		return r, true, managedRecordRefusal(id)
	}
	if (r.Phase == candidateReady || r.Phase == candidateApplying) && r.Disposition != "" {
		return r, true, managedRecordRefusal(id)
	}
	if (r.Phase == candidateReady || r.Phase == candidateApplying) && r.Tree == "" {
		return r, true, managedRecordRefusal(id)
	}
	if (r.Phase == candidateCleanup || r.Phase == candidateRemoved) && r.Disposition != "applied" && r.Disposition != "discarded" {
		return r, true, managedRecordRefusal(id)
	}
	return r, true, nil
}

func managedRecordRefusal(id string) error {
	return fmt.Errorf("isolated result %s has an unsupported or invalid settlement record; compatible recovery required, files retained: %w", id, errUnavailable)
}

// The journal may survive missing worktree bytes. Bind its namespace and source
// here; operations which need a live candidate additionally use bindRecord.
func (s *Store) managedIdentity(d durableEntry) bool {
	if d.ID == "" || d.Snap.RepoRoot != s.repo.Top().WorkTree || d.Snap.Repo != s.repo.Top() {
		return false
	}
	if d.Snap.Prefix != "" && !filepath.IsLocal(filepath.FromSlash(d.Snap.Prefix)) {
		return false
	}
	source, err := openRecordDirectory(s.repo.Top().WorkTree)
	if err != nil {
		return false
	}
	defer source.root.Close()
	prefix := filepath.FromSlash(d.Snap.Prefix)
	if prefix == "" {
		prefix = "."
	}
	if !source.same(d.Workspace, prefix) || !source.same(s.workspace, prefix) {
		return false
	}
	managed, err := openRecordDirectory(s.managedRoot)
	if err != nil {
		return false
	}
	defer managed.root.Close()
	slot, err := filepath.Rel(managed.canonical, filepath.Dir(d.Cand.WorktreeRoot))
	if info, err := managed.root.Lstat(slot); err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
		return false
	}
	return err == nil && filepath.IsLocal(slot) && slot != "." && filepath.Dir(slot) == "." &&
		d.Cand.WorkspaceRoot == filepath.Join(d.Cand.WorktreeRoot, filepath.FromSlash(d.Snap.Prefix))
}

func (s *Store) openManagedSlot(d durableEntry) (*os.Root, error) {
	managed, err := openRecordDirectory(s.managedRoot)
	if err != nil {
		return nil, err
	}
	defer managed.root.Close()
	slot, err := filepath.Rel(managed.canonical, filepath.Dir(d.Cand.WorktreeRoot))
	if err != nil || !filepath.IsLocal(slot) || filepath.Dir(slot) != "." || !recordDirectoryEntry(managed.root, slot) {
		return nil, errUnavailable
	}
	return managed.root.OpenRoot(slot)
}

func (s *Store) saveManagedRecord(r candidateRecord, create bool) error {
	root, err := s.candidateRecords(true)
	if err != nil {
		return err
	}
	defer root.Close()
	name := candidateRecordName(r.Entry.ID)
	if create {
		if _, err := root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
			return os.ErrExist
		}
	} else if !recordRegularFile(root, name) {
		return errUnavailable
	}
	identityRevision, err := writeCandidateImage(root, r.Entry.ID, candidateImage{Entry: r.Entry, Anchors: r.Anchors})
	if err != nil {
		return err
	}
	progress := candidateProgress{Kind: candidateRecordKind, ID: r.Entry.ID, Workspace: candidateWorkspace(r.Entry), SourceCommon: r.Entry.Snap.Repo.CommonDir, IdentityRevision: identityRevision, Phase: r.Phase, Disposition: r.Disposition, Updated: time.Now().UTC()}
	var previous candidateProgress
	if !create {
		previous, err = readCandidateProgress(root, r.Entry.ID)
		if err != nil {
			return managedRecordRefusal(r.Entry.ID)
		}
		if previous.IdentityRevision != identityRevision {
			initial, err := readCandidateImage(root, r.Entry.ID, previous.IdentityRevision)
			before, after := initial.Entry, r.Entry
			after.Cand.Repo = before.Cand.Repo
			if err != nil || previous.Phase != candidateCreating || r.Phase != candidatePrepared || before != after || before.Cand.Repo.Valid() || initial.Anchors.Source != r.Anchors.Source || initial.Anchors.Common != r.Anchors.Common || initial.Anchors.Workspace != r.Anchors.Workspace {
				return managedRecordRefusal(r.Entry.ID)
			}
		}
	}
	if r.Tree != "" {
		progress.ResultRevision, err = writeCandidateImage(root, r.Entry.ID, candidateImage{Entry: r.Entry, Anchors: r.Anchors, Tree: r.Tree, Changes: r.Changes})
		if err != nil {
			return err
		}
	}
	if previous.ResultRevision != "" && previous.ResultRevision != progress.ResultRevision {
		return managedRecordRefusal(r.Entry.ID)
	}
	raw, err := json.Marshal(progress)
	if err != nil {
		return err
	}
	tmp, err := writeCandidateTemp(root, name, raw)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(tmp) }()
	if err := root.Rename(tmp, name); err != nil {
		return err
	}
	// Some platforms do not support directory fsync; publication already happened.
	if dir, err := root.Open("."); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

func (s *Store) managedIDs() []string {
	workspace, err := fileutil.ResolveExistingPath(s.workspace)
	if err != nil {
		return nil
	}
	root, err := s.candidateRecords(false)
	if err != nil {
		return nil
	}
	defer root.Close()
	files, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil
	}
	var ids []string
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".json") || !recordRegularFile(root, file.Name()) {
			continue
		}
		raw, err := readCandidateRecord(root, file.Name())
		var p candidateProgress
		if err != nil || json.Unmarshal(raw, &p) != nil || p.ID == "" || file.Name() != candidateRecordName(p.ID) {
			continue
		}
		// The index keeps a diagnosable original ID even when its immutable
		// image is damaged. These owner hints never authorize settlement.
		if p.Workspace != workspace || p.SourceCommon != s.repo.Top().CommonDir {
			continue
		}
		if p.Kind != candidateRecordKind || p.Phase != candidateRemoved {
			ids = append(ids, p.ID)
		}
	}
	return ids
}

// Bound hostile or corrupt metadata before decoding it. The execution's
// complete change list must fit this limit before it can become Ready.
const maxCandidateRecordBytes = 8 << 20

func readCandidateRecord(root *os.Root, name string) ([]byte, error) {
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxCandidateRecordBytes+1))
	if len(raw) > maxCandidateRecordBytes {
		return nil, errUnavailable
	}
	return raw, err
}
