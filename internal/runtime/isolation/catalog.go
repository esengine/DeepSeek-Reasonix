package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/base/filelock"
	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/worktree"
)

type ResultSummary struct {
	ID              string            `json:"id"`
	Kind            string            `json:"kind"`
	ResultID        string            `json:"resultId,omitempty"`
	Owner           string            `json:"owner,omitempty"`
	ParentID        string            `json:"parentId,omitempty"`
	TargetWorkspace string            `json:"targetWorkspace,omitempty"`
	Generation      string            `json:"generation,omitempty"`
	State           State             `json:"state"`
	Activity        string            `json:"activity"`
	CandidateRoot   string            `json:"candidateRoot,omitempty"`
	BaselineTree    string            `json:"baselineTree,omitempty"`
	Tree            string            `json:"tree,omitempty"`
	Changes         []worktree.Change `json:"changes"`
	Verification    string            `json:"verification"`
	CleanupComplete bool              `json:"cleanupComplete"`
	Reason          string            `json:"reason,omitempty"`
	Actions         []string          `json:"actions"`
	Recovery        *RecoveryTarget   `json:"recovery,omitempty"`
}

type CatalogDiagnostic struct {
	Record string `json:"record"`
	Reason string `json:"reason"`
}

type Catalog struct {
	Enabled          bool                `json:"enabled"`
	Results          []ResultSummary     `json:"results"`
	Diagnostics      []CatalogDiagnostic `json:"diagnostics"`
	RecoveryRequired bool                `json:"recoveryRequired"`
	RecoveryReason   string              `json:"recoveryReason,omitempty"`
	Recovery         *RecoveryTarget     `json:"recovery,omitempty"`
	RootScene        *RootSceneSummary   `json:"rootScene,omitempty"`
}

type RootSceneSummary struct {
	EnvironmentID   string          `json:"environmentId"`
	Generation      string          `json:"generation"`
	Runtime         string          `json:"runtime"`
	Owner           string          `json:"owner"`
	SourceWorkspace string          `json:"sourceWorkspace"`
	LiveWorkspace   string          `json:"liveWorkspace"`
	PublicationID   string          `json:"publicationId"`
	ResultID        string          `json:"resultId"`
	State           string          `json:"state"`
	Verification    string          `json:"verification"`
	Actions         []string        `json:"actions"`
	Recovery        *RecoveryTarget `json:"recovery,omitempty"`
}

type RootSceneRequest struct {
	EnvironmentID string `json:"environmentId"`
	Generation    string `json:"generation"`
	Runtime       string `json:"runtime"`
	Owner         string `json:"owner"`
	PublicationID string `json:"publicationId"`
	Action        string `json:"action"`
	Revision      string `json:"revision,omitempty"`
}

type RecoveryTarget struct {
	ResultID  string   `json:"resultId,omitempty"`
	Workspace string   `json:"workspace"`
	Runtime   string   `json:"runtime"`
	Revision  string   `json:"revision"`
	Paths     []string `json:"paths"`
}

type RecoveryRequest struct {
	ResultID  string `json:"resultId,omitempty"`
	Workspace string `json:"workspace"`
	Runtime   string `json:"runtime"`
	Revision  string `json:"revision"`
}

func (t RecoveryTarget) Request() RecoveryRequest {
	return RecoveryRequest{ResultID: t.ResultID, Workspace: t.Workspace, Runtime: t.Runtime, Revision: t.Revision}
}

// Catalog reads retained decisions without pruning, rewriting or materializing
// candidates. Activity is a momentary observation, never authority for an action.
func (s *Store) catalogLocal(ctx context.Context) (Catalog, error) {
	out := Catalog{Enabled: s != nil, Results: []ResultSummary{}, Diagnostics: []CatalogDiagnostic{}}
	if s == nil {
		return out, nil
	}
	if s.repo.Valid() {
		if err := worktree.CheckApplyRecovery(s.repo); err != nil {
			out.RecoveryRequired, out.RecoveryReason = true, err.Error()
		}
		if preview, err := s.PreviewRecovery(ctx); err != nil {
			out.RecoveryRequired, out.RecoveryReason = true, err.Error()
		} else if preview != nil {
			out.RecoveryRequired = true
			out.RecoveryReason = "interrupted publication records require recovery; candidate results are retained"
		}
	}
	markers, err := s.markerPaths()
	if err != nil {
		return out, err
	}
	seen := map[string]int{}
	counts := map[string]int{}
	for _, marker := range markers {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		raw, err := os.ReadFile(marker)
		var d durableEntry
		if err == nil {
			err = json.Unmarshal(raw, &d)
		}
		if err != nil || d.Workspace == "" || !validID(d.ID) {
			out.Diagnostics = append(out.Diagnostics, CatalogDiagnostic{Record: filepath.Join(filepath.Base(filepath.Dir(marker)), filepath.Base(marker)), Reason: "unreadable or unidentified isolation record retained"})
			continue
		}
		counts[d.ID]++
		if position, ok := seen[d.ID]; ok {
			out.Results[position].Reason = "duplicate isolation records retained; resolve the record identity before continuing"
			out.Results[position].Actions = []string{}
			continue
		}
		if !sameDir(d.Workspace, s.workspace) {
			continue
		}
		row := s.resultSummary(d)
		if counts[d.ID] > 1 {
			row.Reason = "duplicate isolation records retained; resolve the record identity before continuing"
			row.Actions = []string{}
		}
		seen[d.ID] = len(out.Results)
		out.Results = append(out.Results, row)
	}
	summaries, diagnostics := s.integrationSummaries()
	out.Results = append(out.Results, summaries...)
	out.Diagnostics = append(out.Diagnostics, diagnostics...)
	slices.SortFunc(out.Results, func(a, b ResultSummary) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

func (s *Store) resultSummary(d durableEntry) ResultSummary {
	row := ResultSummary{ID: d.ID, Kind: "result", Owner: d.ResumeOwner, Generation: d.RunGeneration, State: d.State,
		CandidateRoot: d.Cand.WorktreeRoot, BaselineTree: d.Snap.Tree, Tree: d.Tree, Changes: append([]worktree.Change{}, d.Changes...),
		Verification: "unknown", CleanupComplete: d.CleanupComplete, Activity: "unknown", Actions: []string{}}
	if !supportedResultVersion(d) || !supportedLifecycle(d) {
		row.Reason = "unsupported isolation state retained; a compatible reader is required"
	} else if d.Version != 0 {
		row.Activity, row.Reason = s.resultActivity(d.ID)
		if row.State == Running && row.Activity == "inactive" {
			row.State = Interrupted
		}
	}
	if supportedResultVersion(d) && supportedLifecycle(d) && row.Activity != "active" && row.State != Applying && row.State != Integrated && !row.CleanupComplete {
		if err := s.requireSettledChildren(d.entry()); err != nil && !s.dependenciesRecoverable(d.entry()) {
			row.Reason = err.Error()
		} else {
			row.Actions = []string{"discard_preview"}
		}
	}
	return row
}

func (s *Store) resultActivity(id string) (string, string) {
	release, err := filelock.TryAcquire(filepath.Join(s.managedRoot, "."+id+".lock"))
	if errors.Is(err, filelock.ErrHeld) {
		return "unknown", "lifecycle transition in progress"
	}
	if err != nil {
		return "unknown", err.Error()
	}
	defer release()
	releaseRun, err := filelock.TryAcquire(s.runLock(id))
	if errors.Is(err, filelock.ErrHeld) {
		return "active", ""
	}
	if err != nil {
		return "unknown", err.Error()
	}
	releaseRun()
	return "inactive", ""
}

func (s *Store) integrationSummaries() ([]ResultSummary, []CatalogDiagnostic) {
	rows := []ResultSummary{}
	paths, err := s.integrationRecordPaths()
	if err != nil {
		return rows, []CatalogDiagnostic{{Record: filepath.Base(s.integrationRoot()), Reason: err.Error()}}
	}
	for _, path := range paths {
		id := strings.TrimSuffix(filepath.Base(path), ".json")
		attempt, _, err := s.readIntegration(id)
		if attempt.Source.RepoRoot != "" && !sameDir(attempt.Source.RepoRoot, s.repo.WorkTree) {
			continue
		}
		if err == nil {
			_, scopeErr := s.managedResultMetadata(attempt.ResultID)
			var refusal tool.Refusal
			if errors.As(scopeErr, &refusal) && refusal.Code == CodeOwnerMismatch {
				continue
			}
			if scopeErr != nil {
				err = scopeErr
			}
		}
		row := ResultSummary{ID: id, Kind: "integration", Activity: "unknown", Verification: "unknown", Changes: []worktree.Change{}, Actions: []string{}}
		if err != nil {
			row.State, row.Reason = RecoveryRequired, err.Error()
		} else {
			row = s.integrationSummary(attempt)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (s *Store) integrationSummary(attempt IntegrationAttempt) ResultSummary {
	row := ResultSummary{ID: attempt.ID, Kind: "integration", ResultID: attempt.ResultID, Owner: attempt.Owner,
		State: attempt.State, Activity: "unknown", Verification: "unknown", CandidateRoot: attempt.Candidate.WorktreeRoot,
		BaselineTree: attempt.Source.Tree, Tree: attempt.Tree, Changes: append([]worktree.Change{}, attempt.Changes...),
		CleanupComplete: attempt.CleanupComplete, Reason: attempt.Reason, Actions: []string{}}
	if attempt.Verification == "invalid" {
		row.Verification = "invalid"
	}
	if s.mutations != nil && row.State != Applying && row.State != Integrated && !row.CleanupComplete {
		row.Actions = []string{"discard_preview"}
	}
	return row
}
