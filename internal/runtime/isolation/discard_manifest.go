package isolation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/tool"
)

func (s *Store) discardSnapshot(ctx context.Context, id string) (*Entry, error) {
	unlock, err := s.lockEntry(ctx, id)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := s.recoverChildReservationsLocked(id); err != nil {
		return nil, err
	}
	return s.discardManifest(id)
}

// A missing candidate has no tree to recover. Only explicit discard can settle
// its stopped, managed identity; apply and resume retain their ordinary checks.
func (s *Store) discardManifest(id string) (*Entry, error) {
	e, err := s.operationManifest(id)
	if err != nil {
		return nil, err
	}
	if !errors.Is(candidateAbsent(e), os.ErrNotExist) || e.State == Applying || e.State == Integrated {
		return s.lookup(id)
	}
	parent := filepath.Dir(e.Cand.WorktreeRoot)
	info, err := os.Lstat(parent)
	if err != nil {
		return nil, err
	}
	expected := filepath.Join(e.Cand.WorktreeRoot, filepath.FromSlash(e.Snap.Prefix))
	if !info.IsDir() || !sameDir(filepath.Dir(parent), s.managedRoot) || !sameDir(e.Snap.RepoRoot, s.repo.WorkTree) ||
		e.Snap.Repo != s.repo.Top() || e.Cand.WorkspaceRoot != expected || (e.Cand.Repo.Valid() &&
		(!sameMissingPath(e.Cand.Repo.WorkTree, e.Cand.WorktreeRoot) || !sameDir(e.Cand.Repo.CommonDir, s.repo.CommonDir))) {
		return nil, tool.Refusal{Code: CodeUnsupportedState, Message: "missing candidate identity is not managed by this workspace; record retained"}
	}
	if err := s.repo.Verify(); err != nil {
		return nil, err
	}
	if e.State == Preparing || (e.State == RecoveryRequired && e.Tree == "") {
		e.State = RecoveryRequired
	}
	if e.State == Running {
		e.State = Interrupted
	}
	return e, nil
}

func sameMissingPath(a, b string) bool {
	first, firstErr := fileutil.CanonicalWritePath(a)
	second, secondErr := fileutil.CanonicalWritePath(b)
	return firstErr == nil && secondErr == nil && (first == second || (runtime.GOOS == "windows" && strings.EqualFold(first, second)))
}
