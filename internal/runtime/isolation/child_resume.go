package isolation

import (
	"context"
	"errors"

	"reasonix/internal/base/filelock"
	"reasonix/internal/contract/tool"
)

func (s *Store) validateChildTarget(parent *Entry, children *Store, owner string) error {
	if s == nil || parent == nil || children == nil || owner == "" ||
		!sameDir(children.workspace, parent.Cand.WorkspaceRoot) || children.repo != parent.Cand.Repo {
		return tool.Refusal{Code: CodeOwnerMismatch, Message: "child environment must target its running direct parent"}
	}
	return nil
}

// childAdmission reads only metadata while the caller holds the parent entry.
func (s *Store) childAdmission(parent *Entry) (durableEntry, []byte, error) {
	release, err := filelock.TryAcquire(s.runLock(parent.ID))
	if err == nil {
		release()
		return durableEntry{}, nil, tool.Refusal{Code: CodeStaleExecution, Message: "parent execution no longer owns its run"}
	}
	if !errors.Is(err, filelock.ErrHeld) {
		return durableEntry{}, nil, err
	}
	if err := s.checkRunGeneration(parent); err != nil {
		return durableEntry{}, nil, err
	}
	d, raw, err := readLineageRecord(entryRecordPath(parent))
	if err != nil {
		return d, raw, err
	}
	if d.State != Running || (d.Lineage != nil && d.Lineage.Admission != "open") {
		return d, raw, dependentResults("parent execution no longer admits child executions")
	}
	return d, raw, nil
}

func (s *Store) ResumeChild(ctx context.Context, parent *Entry, children *Store, id, owner string) (*Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.validateChildTarget(parent, children, owner); err != nil {
		return nil, err
	}
	if err := s.checkChildContinuation(ctx, parent, children, id); err != nil {
		return nil, err
	}
	return children.resume(ctx, id, owner, func(ctx context.Context) (func(), error) {
		return s.holdChildContinuation(ctx, parent, children, id)
	})
}

func (s *Store) checkChildContinuation(ctx context.Context, parent *Entry, children *Store, id string) error {
	release, err := s.holdChildContinuation(ctx, parent, children, id)
	if err == nil {
		release()
	}
	return err
}

// Final admission takes child resources and entry before this parent entry.
// The parent lock remains held through publication of the child's new run.
func (s *Store) holdChildContinuation(ctx context.Context, parent *Entry, children *Store, id string) (release func(), resultErr error) {
	unlock, err := s.lockEntry(ctx, parent.ID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if release == nil {
			unlock()
		}
	}()
	d, _, err := s.childAdmission(parent)
	if err != nil {
		return nil, err
	}
	if d.Lineage == nil {
		return nil, dependentResults("parent has no registered child environment")
	}
	registration, exists := d.Lineage.Children[id]
	if !exists || registration.ID != id || !sameDir(registration.Root, children.managedRoot) || !sameDir(registration.Workspace, children.workspace) {
		return nil, dependentResults("child environment is not registered to this direct parent")
	}
	child, err := children.managedResultMetadata(id)
	if err != nil {
		return nil, err
	}
	if child.Lineage == nil || !matchesParentResult(child.Lineage.Parent, parent, registration.Token) {
		return nil, dependentResults("child continuation backlink differs from its direct parent")
	}
	return unlock, nil
}
