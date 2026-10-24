package worktree

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"reasonix/internal/base/filelock"
	"reasonix/internal/platform/gitcmd"
)

type recoveryAuthorizationKey struct{}
type recoveryRevisionKey struct{}

var ErrRecoveryChanged = errors.New("integration recovery target changed")

type RecoveryPreview struct {
	Revision string   `json:"revision"`
	Paths    []string `json:"paths"`
}

func PreviewRecovery(repo gitcmd.Repo) (*RecoveryPreview, error) {
	if !repo.Valid() {
		return nil, nil
	}
	j, err := readApplyJournal(repo)
	if err != nil || j == nil {
		return nil, err
	}
	changes := make([]Change, 0, len(j.Files))
	for _, file := range j.Files {
		changes = append(changes, Change{Path: file.Path})
	}
	paths, err := ChangePaths(Snapshot{RepoRoot: repo.WorkTree}, changes)
	if err != nil {
		return nil, err
	}
	return &RecoveryPreview{Revision: recoveryRevision(j), Paths: paths}, nil
}

func recoveryRevision(j *applyJournal) string {
	raw, _ := json.Marshal(j)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// RecoverIntegrationExpected rechecks the previewed journal under its OS lock.
func RecoverIntegrationExpected(ctx context.Context, repo gitcmd.Repo, revision string) error {
	if len(revision) != sha256.Size*2 {
		return ErrRecoveryChanged
	}
	return RecoverIntegration(context.WithValue(ctx, recoveryRevisionKey{}, revision), repo)
}

// RecoverIntegration settles an interrupted journal without applying another candidate.
func RecoverIntegration(ctx context.Context, repo gitcmd.Repo) error {
	if err := repo.Verify(); err != nil {
		return err
	}
	release, err := lockApply(ctx, repo)
	if err != nil {
		return err
	}
	release()
	return nil
}

// WithRecoveryAuthorization rechecks the live mutation scope under the journal lock.
func WithRecoveryAuthorization(ctx context.Context, check func([]string) error) context.Context {
	return context.WithValue(ctx, recoveryAuthorizationKey{}, check)
}

func AuthorizeRecoveryPaths(ctx context.Context, paths []string) error {
	check, _ := ctx.Value(recoveryAuthorizationKey{}).(func([]string) error)
	if check == nil {
		return fmt.Errorf("%w: no authorization for journal recovery", ErrApplyRecovery)
	}
	return check(paths)
}

func authorizeRecovery(ctx context.Context, j *applyJournal) error {
	changes := make([]Change, 0, len(j.Files))
	for _, file := range j.Files {
		changes = append(changes, Change{Path: file.Path})
	}
	paths, err := ChangePaths(Snapshot{RepoRoot: j.Root}, changes)
	if err != nil {
		return err
	}
	if err := AuthorizeRecoveryPaths(ctx, paths); err != nil {
		return fmt.Errorf("%w: recovery resources changed: %w", ErrApplyRecovery, err)
	}
	return nil
}

// ApplyRecoveryPaths names every resource an interrupted integration may restore.
// Unknown journal versions refuse recovery without rewriting their contents.
func ApplyRecoveryPaths(snap Snapshot) ([]string, error) {
	repo := snap.Repo
	if !repo.Valid() {
		return nil, nil
	}
	j, err := readApplyJournal(repo)
	if err != nil || j == nil {
		return nil, err
	}
	changes := make([]Change, 0, len(j.Files))
	for _, file := range j.Files {
		changes = append(changes, Change{Path: file.Path})
	}
	return ChangePaths(Snapshot{RepoRoot: repo.WorkTree}, changes)
}

// CheckApplyRecovery blocks new writes after a dead integration owner left a journal.
func CheckApplyRecovery(repo gitcmd.Repo) error {
	if !repo.Valid() {
		return nil
	}
	j, err := readApplyJournal(repo)
	if err != nil || j == nil {
		return err
	}
	release, err := filelock.TryAcquire(journalPath(repo) + ".lock")
	if errors.Is(err, filelock.ErrHeld) {
		return nil
	}
	if err != nil {
		return err
	}
	defer release()
	j, err = readApplyJournal(repo)
	if err != nil || j == nil || j.Committed {
		return err
	}
	return ErrApplyRecovery
}

// CheckApplyWriteBarrier also refuses active publications. Legacy optimistic
// callers may waive their claim and cannot rely on the coordinator's queue.
func CheckApplyWriteBarrier(repo gitcmd.Repo) error {
	if !repo.Valid() {
		return nil
	}
	j, err := readApplyJournal(repo)
	if err != nil {
		return err
	}
	if j != nil && !j.Committed {
		return ErrApplyRecovery
	}
	return nil
}

func readApplyJournal(repo gitcmd.Repo) (*applyJournal, error) {
	raw, err := os.ReadFile(journalPath(repo))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var j applyJournal
	if err := json.Unmarshal(raw, &j); err != nil {
		return nil, fmt.Errorf("%w: invalid journal: %w", ErrApplyRecovery, err)
	}
	if j.Version != 1 || j.Root != repo.WorkTree || j.Started < 0 || j.Started > len(j.Files) {
		return nil, fmt.Errorf("%w: unsupported journal identity", ErrApplyRecovery)
	}
	return &j, nil
}
