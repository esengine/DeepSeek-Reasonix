package isolation

import (
	"context"
	"errors"
	"reflect"

	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/worktree"
)

// SnapshotOrigin keeps the frozen root scene separate from the original
// source comparison baseline. Version 3 results require an aware reader.
type SnapshotOrigin struct {
	Version               int                         `json:"version"`
	Kind                  string                      `json:"kind,omitempty"`
	PublicationID         string                      `json:"publication_id"`
	EnvironmentID         string                      `json:"environment_id"`
	EnvironmentGeneration string                      `json:"environment_generation"`
	Materialization       worktree.Snapshot           `json:"materialization"`
	Environment           *worktree.FrozenEnvironment `json:"environment,omitempty"`
}

type SnapshotImport struct {
	ID       string
	Owner    string
	Baseline worktree.Snapshot
	Origin   SnapshotOrigin
}

func resultVersion(e *Entry) int {
	if e.Origin != nil && e.Origin.Kind == "ancestor_copy" {
		return 6
	}
	if e.Lineage != nil {
		return 4
	}
	if e.Origin != nil {
		return 3
	}
	return 2
}

func supportedResultVersion(d durableEntry) bool {
	if d.Version == 6 {
		return supportedLineage(d.Lineage) && d.Origin != nil && d.Origin.Version == 2 && d.Origin.Kind == "ancestor_copy" &&
			validSnapshotOrigin(*d.Origin) && d.Origin.Materialization.Repo == d.Snap.Repo && d.Origin.Materialization.Prefix == d.Snap.Prefix
	}
	if !supportedLineage(d.Lineage) || (d.Lineage != nil && d.Version != 4 && d.Version != 5) ||
		((d.Version == 4 || d.Version == 5) && d.Lineage == nil) ||
		(d.Version == 5 && d.Lineage.Parent == nil) {
		return false
	}
	if d.Origin == nil {
		return d.Version == 0 || d.Version == 2 || d.Version == 4 || d.Version == 5
	}
	return (d.Version == 3 || d.Version == 4 || d.Version == 5) && d.Origin.Version == 1 && validSnapshotOrigin(*d.Origin) &&
		d.Origin.Materialization.Repo.CommonDir == d.Snap.Repo.CommonDir && d.Origin.Materialization.Prefix == d.Snap.Prefix
}

func validSnapshotOrigin(origin SnapshotOrigin) bool {
	return (origin.Environment == nil || (origin.Environment.Version == 1 && origin.Environment.Receipt.Version == 1)) &&
		((origin.Version == 1 && origin.Kind == "" && origin.PublicationID != "") || (origin.Version == 2 && origin.Kind == "ancestor_copy" && origin.PublicationID == "")) && origin.EnvironmentID != "" && origin.EnvironmentGeneration != "" &&
		origin.Materialization.Commit != "" && origin.Materialization.Tree != "" && origin.Materialization.Repo.Valid()
}

// ImportSnapshotResult creates a disposable copy of a frozen scene, never of
// a running worktree. Retrying an import preserves its result and generation.
func (s *Store) ImportSnapshotResult(ctx context.Context, request SnapshotImport) (*Entry, error) {
	if s == nil || s.mutations == nil {
		return nil, errUnavailable
	}
	if !validID(request.ID) || request.Owner == "" || request.Origin.Version != 1 || !validSnapshotOrigin(request.Origin) ||
		request.Baseline.Repo != s.repo.Top() || !sameDir(request.Baseline.RepoRoot, s.repo.WorkTree) ||
		request.Baseline.Tree == "" || request.Baseline.Commit == "" ||
		request.Origin.Materialization.Repo.CommonDir != s.repo.CommonDir || request.Origin.Materialization.Prefix != request.Baseline.Prefix {
		return nil, tool.Refusal{Code: CodeOwnerMismatch, Message: "snapshot import does not belong to this source"}
	}
	existing, err := s.operationSnapshot(ctx, request.ID)
	if err == nil {
		if !matchesSnapshotImport(existing, request) {
			return nil, tool.Refusal{Code: CodeOwnerMismatch, Message: "publication result identity differs; original retained"}
		}
		if !settleable(existing.State) && !terminalState(existing.State) && !((existing.State == Running || existing.State == Interrupted) && existing.Tree != "") {
			return nil, tool.Refusal{Code: CodeRecoveryRequired, Message: "snapshot import was interrupted; original candidate retained"}
		}
		return existing, nil
	}
	var refusal tool.Refusal
	if !errors.As(err, &refusal) || refusal.Code != CodeUnknownID {
		return nil, err
	}
	if err := worktree.ValidatePublicationModes(ctx, request.Baseline, request.Origin.Materialization); err != nil {
		return nil, err
	}
	candidate, err := worktree.PlanCandidate(request.Origin.Materialization, s.managedRoot)
	if err != nil {
		return nil, err
	}
	origin := request.Origin
	origin.Environment = worktree.CloneFrozenEnvironment(origin.Environment)
	e := &Entry{ID: request.ID, Workspace: s.workspace, Snap: request.Baseline, Cand: candidate,
		State: Preparing, ResumeOwner: request.Owner, Origin: &origin, DiscardPolicy: hostDiscardPolicy}
	if _, err := s.prepareEntry(ctx, e); err != nil {
		return nil, err
	}
	if _, err := s.Finish(ctx, e); err != nil {
		return nil, err
	}
	return e, nil
}

func matchesSnapshotImport(e *Entry, request SnapshotImport) bool {
	return e.ID == request.ID && e.ResumeOwner == request.Owner && e.Snap == request.Baseline && reflect.DeepEqual(e.Origin, &request.Origin)
}

// ImportedResultIntegrated reads the immutable terminal fact independently of
// cleanup success. It makes no claim about current workspace verification.
func (s *Store) ImportedResultIntegrated(ctx context.Context, request SnapshotImport) (bool, error) {
	e, err := s.operationSnapshot(ctx, request.ID)
	if err != nil {
		return false, err
	}
	if !matchesSnapshotImport(e, request) || e.Tree != request.Origin.Materialization.Tree {
		return false, tool.Refusal{Code: CodeOwnerMismatch, Message: "publication receipt does not match the exported root snapshot"}
	}
	if err := worktree.ValidatePublicationModes(ctx, request.Baseline, request.Origin.Materialization); err != nil {
		return false, err
	}
	return e.State == Integrated, nil
}

func (s *Store) ImportedResultState(ctx context.Context, request SnapshotImport) (State, error) {
	e, err := s.operationSnapshot(ctx, request.ID)
	if err != nil {
		return "", err
	}
	if !matchesSnapshotImport(e, request) {
		return "", tool.Refusal{Code: CodeOwnerMismatch, Message: "publication result identity differs; original retained"}
	}
	return e.State, nil
}
