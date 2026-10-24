package worktree

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"reasonix/internal/base/filelock"
	"reasonix/internal/base/fileutil"
	"reasonix/internal/platform/gitcmd"
)

var ErrApplyRecovery = errors.New("workspace integration requires recovery")

type applyRecord struct {
	Path   string    `json:"path"`
	Before fileImage `json:"before"`
	After  fileImage `json:"after"`
}

type applyJournal struct {
	Version   int           `json:"version"`
	Root      string        `json:"root"`
	Tree      string        `json:"tree"`
	Started   int           `json:"started"`
	Committed bool          `json:"committed"`
	Files     []applyRecord `json:"files"`
	ResultID  string        `json:"result_id,omitempty"`
	AttemptID string        `json:"attempt_id,omitempty"`
}

type applyIdentityKey struct{}
type applyIdentity struct{ resultID, attemptID string }

// WithApplyIdentity associates an integration's durable result and attempt with
// its file journal. It does not authorize writes or change recovery semantics.
func WithApplyIdentity(ctx context.Context, resultID, attemptID string) context.Context {
	return context.WithValue(ctx, applyIdentityKey{}, applyIdentity{resultID, attemptID})
}

func journalPath(repo gitcmd.Repo) string {
	return filepath.Join(repo.GitDir, "reasonix-apply-v1", "active.json")
}

func lockApply(ctx context.Context, repo gitcmd.Repo) (func(), error) {
	path := journalPath(repo)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	release, err := filelock.Acquire(ctx, path+".lock")
	if err != nil {
		return nil, err
	}
	if err := recoverApply(ctx, repo, path); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

func (j *applyJournal) save(path string) error {
	raw, err := json.Marshal(j)
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFileStrict(path, raw, 0600)
}

func recoverApply(ctx context.Context, repo gitcmd.Repo, path string) error {
	j, err := readApplyJournal(repo)
	if expected, ok := ctx.Value(recoveryRevisionKey{}).(string); err == nil && ok && (j == nil || recoveryRevision(j) != expected) {
		return ErrRecoveryChanged
	}
	if err != nil || j == nil {
		return err
	}
	_, previewed := ctx.Value(recoveryRevisionKey{}).(string)
	if !j.Committed || previewed {
		if err := authorizeRecovery(ctx, j); err != nil {
			return err
		}
	}
	if !j.Committed {
		if err := j.rollback(); err != nil {
			return err
		}
	}
	return os.Remove(path)
}

func (j *applyJournal) rollback() error {
	var failures []error
	for i := j.Started - 1; i >= 0; i-- {
		r := j.Files[i]
		live, err := readImage(j.Root, r.Path)
		if err == nil && !live.equal(r.Before) {
			if !live.equal(r.After) {
				err = &ConflictError{Paths: []string{r.Path}}
			} else {
				err = publishImage(j.Root, r.Path, r.After, r.Before)
			}
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", r.Path, err))
		}
	}
	if len(failures) != 0 {
		return fmt.Errorf("%w: %w", ErrApplyRecovery, errors.Join(failures...))
	}
	return nil
}

func writeChanges(ctx context.Context, repo gitcmd.Repo, tree string, changes []Change, expected map[string]gitFileVersion) error {
	if len(changes) == 0 {
		return nil
	}
	j, err := prepareApply(ctx, repo, tree, changes, expected)
	if err != nil {
		return err
	}
	if err := validateApplyPostimage(ctx, j); err != nil {
		return err
	}
	if identity, ok := ctx.Value(applyIdentityKey{}).(applyIdentity); ok {
		j.ResultID, j.AttemptID = identity.resultID, identity.attemptID
	}
	path := journalPath(repo)
	if err := j.save(path); err != nil {
		return err
	}
	if err := j.publish(ctx, path); err != nil {
		if rollbackErr := j.rollback(); rollbackErr != nil {
			return errors.Join(err, rollbackErr)
		}
		return errors.Join(err, os.Remove(path))
	}
	j.Committed = true
	if err := j.save(path); err != nil {
		return fmt.Errorf("%w: commit record: %w", ErrApplyRecovery, err)
	}
	return os.Remove(path)
}

func (j *applyJournal) publish(ctx context.Context, path string) error {
	for i, r := range j.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		j.Started = i + 1
		if err := j.save(path); err != nil {
			return err
		}
		if err := publishImage(j.Root, r.Path, r.Before, r.After); err != nil {
			return err
		}
	}
	for _, r := range j.Files {
		if err := checkImage(j.Root, r.Path, r.After); err != nil {
			return err
		}
	}
	return nil
}
