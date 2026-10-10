package serve

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/platform/gitstatus"
	"reasonix/internal/session/control"
)

type workspaceBranchesView struct {
	Repo     bool               `json:"repo"`
	Branches []gitstatus.Branch `json:"branches"`
}

// workspaceBranches lists the repository's local branches, the current one
// marked. Repo is false for a workspace that is not version-controlled, as for
// /changes and /workspace/git.
func (s *Server) workspaceBranches(w http.ResponseWriter, r *http.Request) {
	list, ok, err := gitstatus.Branches(r.Context(), workspaceRepo(s.ctl()))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if list == nil {
		list = []gitstatus.Branch{}
	}
	writeJSON(w, workspaceBranchesView{Repo: ok, Branches: list})
}

// workspaceSwitchBranch checks out the named local branch through the
// controller — the write belongs next to CommitStaged, where the turn gate can
// refuse it typed — and answers with the work tree's identity as it now
// stands, so the caller that asked updates its branch reading from the same
// round trip.
func (s *Server) workspaceSwitchBranch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&req); err != nil {
		refuse(w, http.StatusBadRequest, "branch.bad_request", "the request body is not a JSON branch switch", nil)
		return
	}
	info, ok, err := s.ctl().SwitchWorkspaceBranch(r.Context(), req.Name)
	if err != nil {
		refuseBranch(w, r, err)
		return
	}
	writeJSON(w, workspaceGitView{
		Repo: ok, Name: info.Name, Branch: info.Branch, Detached: info.Detached,
		Added: info.Added, Removed: info.Removed, Untracked: info.Untracked,
	})
}

// refuseBranch separates the switch refusals a frontend phrases for a reader
// from the ones it prints: a branch moving under a live turn is "not while
// this is running", a name no branch answers to is a bad request, and the
// rest are conflicts the tree itself stated.
func refuseBranch(w http.ResponseWriter, r *http.Request, err error) {
	var invalid *gitstatus.InvalidNameError
	switch {
	case r.Context().Err() != nil:
		// The person cancelled; nobody is left to read an answer.
	case errors.Is(err, control.ErrWorkspaceBusy):
		busy(w, "branch.workspace_busy", "another operation is using the workspace; try again shortly", nil)
	case errors.Is(err, control.ErrJobsRunning):
		busy(w, "branch.jobs_running", "background jobs are running; stop them before switching branches", nil)
	case errors.Is(err, control.ErrTurnRunning):
		busy(w, "branch.turn_running", "a turn is running; the branch cannot move under it", nil)
	case errors.Is(err, gitcmd.ErrNotRepository):
		refuse(w, http.StatusConflict, "branch.no_repository", err.Error(), nil)
	case errors.As(err, &invalid):
		refuse(w, http.StatusBadRequest, "branch.bad_name", err.Error(), map[string]any{"name": invalid.Name})
	case errors.Is(err, gitstatus.ErrBranchUnknown):
		refuse(w, http.StatusConflict, "branch.unknown", err.Error(), nil)
	case errors.Is(err, gitstatus.ErrLocalChanges):
		refuse(w, http.StatusConflict, "branch.local_changes", err.Error(), nil)
	case errors.Is(err, gitstatus.ErrBranchBusy):
		refuse(w, http.StatusConflict, "branch.in_use", err.Error(), nil)
	default:
		refuse(w, http.StatusInternalServerError, "branch.switch_failed", err.Error(), nil)
	}
}
